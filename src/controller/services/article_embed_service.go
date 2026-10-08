package services

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/domain"
)

const (
	// EmbedInterval là nhịp worker quét bài chưa embed — export để worker lấy
	// làm chu kỳ ticker: 1 nguồn sự thật duy nhất như PurgeGracePeriod.
	EmbedInterval = time.Minute
	// embedClaimLimit số bài hốt mỗi kỳ — ollama remote chậm, bài 500 trang
	// (~1500 chunk ≈ 47 lần gọi) nuốt vài phút nên giữ ít.
	embedClaimLimit = 3
	// embedPoolSize số bài embed song song — ollama remote là bottleneck nên
	// chỉ 2, tránh dội request làm nó nghẽn rồi timeout hàng loạt.
	embedPoolSize = 2
	// embedLease thời gian giữ claim — đủ cho bài to nhất (500 trang) embed +
	// upsert xong trong pool; crash thì quá lease tự reclaim.
	embedLease = 30 * time.Minute
	// embedBatchSize số text mỗi lần gọi ollama — khớp giới hạn input 1 lần
	// gọi của /api/embed, nhiều chunk thì chia nhiều đợt.
	embedBatchSize = 32
	// embedCallAttempts số lần thử lại 1 lần gọi ollama/qdrant chập chờn, nghỉ
	// backoff nhân đôi từ 2s.
	embedCallAttempts  = 3
	embedCallBaseDelay = 2 * time.Second
)

// withRetry chạy fn tối đa attempts lần, nghỉ backoff nhân đôi giữa các lần,
// tôn trọng ctx hủy — dùng cho ollama/qdrant chập chờn mạng. Lỗi cuối cùng
// trả về để service đánh MarkEmbedError theo backoff queue.
func withRetry(ctx context.Context, attempts int, base time.Duration, fn func(ctx context.Context) error) error {
	var err error
	delay := base
	for i := 0; i < attempts; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err = fn(ctx); err == nil {
			return nil
		}
		if i == attempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
			delay *= 2
		}
	}
	return err
}

// IArticleEmbedService worker phase 2: đọc chunk phase 1 đã cắt, embed bằng
// ollama rồi upsert Qdrant — chỉ worker gọi, không qua HTTP nên trả (int,
// error) thuần thay vì dtos.Result. Trả số chunk đã lên Qdrant trong kỳ.
type IArticleEmbedService interface {
	EmbedPending(ctx context.Context) (int, error)
}

type articleEmbedService struct {
	articleRepo domain.IArticleRepository
	chunkRepo   domain.IArticleChunkRepository
	embedder    domain.Embedder
	vectors     domain.IVectorStore
}

func NewArticleEmbedService(articleRepo domain.IArticleRepository, chunkRepo domain.IArticleChunkRepository, embedder domain.Embedder, vectors domain.IVectorStore) IArticleEmbedService {
	return &articleEmbedService{articleRepo: articleRepo, chunkRepo: chunkRepo, embedder: embedder, vectors: vectors}
}

// EmbedPending claim 1 đợt bài (đã chunk done) rồi embed song song trong pool
// giới hạn. Mỗi bài: embed theo đợt 32 → upsert từng đợt → mark từng đợt, nên
// crash giữa bài 500 trang thì kỳ sau chỉ làm nốt phần còn lại. Thứ tự
// Qdrant-trước-Mark-sau: point mồ côi mang ID cũ, upsert lại ghi đè — hội tụ
// như purge. Cùng quy ước trace_id với ChunkPending/PurgeDeleted.
func (s *articleEmbedService) EmbedPending(ctx context.Context) (int, error) {
	traceID := utils.TraceIDFromCtx(ctx)
	if traceID == "" {
		var err error
		if traceID, err = utils.NewUUIDv7(); err != nil {
			return 0, err
		}
	}
	logs.Infow("embed: bắt đầu kỳ quét", "trace_id", traceID)
	articles, err := s.articleRepo.ClaimEmbedPending(ctx, embedClaimLimit, embedLease)
	if err != nil {
		return 0, err
	}
	var embedded atomic.Int64
	var wg sync.WaitGroup
	sem := make(chan struct{}, embedPoolSize)
	for _, article := range articles {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(a *domain.Article) {
			defer wg.Done()
			defer func() { <-sem }()
			s.embedClaimed(ctx, traceID, a, &embedded)
		}(article)
	}
	wg.Wait()
	n := int(embedded.Load())
	logs.Infow("embed: xong 1 kỳ quét", "trace_id", traceID, "found", len(articles), "embedded", n)
	return n, nil
}

// embedClaimed embed hết chunk chưa xong của 1 bài đã claim. Bài không còn
// chunk dở (mark rồi nhưng crash trước khi done) thì đánh done luôn.
func (s *articleEmbedService) embedClaimed(ctx context.Context, traceID string, article *domain.Article, embedded *atomic.Int64) {
	dbCtx, cancel := detachCtx(ctx)
	defer cancel()
	items, err := s.chunkRepo.ListUnembeddedByArticle(ctx, article.ID)
	if err != nil {
		s.markEmbedError(ctx, dbCtx, traceID, article, fmt.Errorf("quét chunk dở: %w", err))
		return
	}
	// Check sống TRƯỚC nhánh rỗng: bài chết mà chunk đã sạch (purge dọn hoặc
	// chưa từng có) thì phải cleanup + bỏ qua, không đánh done bừa.
	alive, err := s.articleRepo.IsAlive(ctx, article.ID)
	if err != nil {
		s.markEmbedError(ctx, dbCtx, traceID, article, fmt.Errorf("kiểm tra bài còn sống: %w", err))
		return
	}
	if !alive {
		s.cleanupDeleted(dbCtx, traceID, article)
		return
	}
	if len(items) == 0 {
		if err := s.articleRepo.MarkEmbedDone(dbCtx, article.ID); err != nil {
			logs.Error(err, "embed: không đánh dấu done được", "trace_id", traceID, "article_id", article.ID)
		}
		return
	}
	for i := 0; i < len(items); i += embedBatchSize {
		end := i + embedBatchSize
		if end > len(items) {
			end = len(items)
		}
		// Bài bị xóa giữa chừng thì dừng ngay (đỡ tốn tiền ollama), dọn sạch
		// vector + chunk đã làm để không còn orphan mà purge không thấy.
		// Check mỗi batch vì 1 bài 500 trang chạy hàng phút — check 1 SELECT
		// rẻ hơn nhiều so với gọi ollama thừa.
		alive, err := s.articleRepo.IsAlive(ctx, article.ID)
		if err != nil {
			s.markEmbedError(ctx, dbCtx, traceID, article, fmt.Errorf("kiểm tra bài còn sống: %w", err))
			return
		}
		if !alive {
			s.cleanupDeleted(dbCtx, traceID, article)
			return
		}
		n, err := s.embedBatch(ctx, dbCtx, items[i:end])
		embedded.Add(int64(n))
		if err != nil {
			s.markEmbedError(ctx, dbCtx, traceID, article, err)
			return
		}
	}
	if err := s.articleRepo.MarkEmbedDone(dbCtx, article.ID); err != nil {
		logs.Error(err, "embed: không đánh dấu done được", "trace_id", traceID, "article_id", article.ID)
		return
	}
	logs.Infow("embed: xong 1 bài", "trace_id", traceID, "article_id", article.ID, "chunks", len(items))
}

// cleanupDeleted dọn vector + chunk của bài đã chết — cả 2 đều idempotent nên
// chạy đè với purge cũng an toàn. Dùng detached ctx để dọn được dù job hết timeout.
func (s *articleEmbedService) cleanupDeleted(dbCtx context.Context, traceID string, article *domain.Article) {
	logs.Infow("embed: bài bị xóa giữa chừng, đã dọn vector + chunk", "trace_id", traceID, "article_id", article.ID)
	if err := s.vectors.DeleteByArticle(dbCtx, article.ID); err != nil {
		logs.Error(err, "embed: không dọn vector bài đã xóa được", "trace_id", traceID, "article_id", article.ID)
	}
	if err := s.chunkRepo.DeleteByArticleID(dbCtx, article.ID); err != nil {
		logs.Error(err, "embed: không dọn chunk bài đã xóa được", "trace_id", traceID, "article_id", article.ID)
	}
}

func (s *articleEmbedService) markEmbedError(ctx, dbCtx context.Context, traceID string, article *domain.Article, err error) {
	// Job hết timeout (ctx hủy) không phải lỗi của bài — bỏ qua không đánh
	// lỗi, lease hết hạn bài tự được hốt lại. Đánh lỗi bừa sẽ đốt attempts oan.
	if ctx.Err() != nil {
		logs.Infow("embed: job hết timeout, bỏ qua không đánh lỗi",
			"trace_id", traceID, "article_id", article.ID)
		return
	}
	logs.Warnw("embed: bỏ qua bài, thử lại theo backoff",
		"trace_id", traceID, "article_id", article.ID, "error", err)
	if merr := s.articleRepo.MarkEmbedError(dbCtx, article.ID, article.EmbedAttempts+1); merr != nil {
		logs.Error(merr, "embed: không đánh dấu lỗi được", "trace_id", traceID, "article_id", article.ID)
	}
}

// embedBatch embed + upsert + mark 1 đợt chunk — point ID lấy từ DB (không
// sinh mới) nên đợt chạy lại ghi đè đúng point cũ. Mark bằng detached ctx để
// giữ tiến độ dù job hết timeout.
func (s *articleEmbedService) embedBatch(ctx, dbCtx context.Context, batch []*domain.ChunkWithOwner) (int, error) {
	texts := make([]string, 0, len(batch))
	for _, it := range batch {
		texts = append(texts, it.Chunk.Content)
	}
	var vectors [][]float32
	if err := withRetry(ctx, embedCallAttempts, embedCallBaseDelay, func(ctx context.Context) error {
		var err error
		vectors, err = s.embedder.EmbedBatch(ctx, texts)
		return err
	}); err != nil {
		return 0, fmt.Errorf("gọi embed: %w", err)
	}
	// Contract EmbedBatch không đảm bảo số vector trả về — thiếu là lỗi
	// embedder, không được index mù gây panic cả job.
	if len(vectors) != len(batch) {
		return 0, fmt.Errorf("embed trả %d vector cho %d text", len(vectors), len(batch))
	}
	points := make([]domain.VectorPoint, 0, len(batch))
	ids := make([]int64, 0, len(batch))
	for i, it := range batch {
		points = append(points, domain.VectorPoint{
			ID:         it.Chunk.PointID,
			Vector:     vectors[i],
			ArticleID:  it.Chunk.ArticleID,
			UserID:     it.UserID,
			CategoryID: it.CategoryID,
			ChunkIndex: it.Chunk.ChunkIndex,
			PageNum:    it.Chunk.PageNum,
			Text:       it.Chunk.Content,
		})
		ids = append(ids, it.Chunk.ID)
	}
	if err := withRetry(ctx, embedCallAttempts, embedCallBaseDelay, func(ctx context.Context) error {
		return s.vectors.UpsertPoints(ctx, points)
	}); err != nil {
		return 0, fmt.Errorf("upsert qdrant: %w", err)
	}
	if err := s.chunkRepo.MarkEmbedded(dbCtx, ids); err != nil {
		return 0, fmt.Errorf("đánh dấu embedded: %w", err)
	}
	return len(batch), nil
}
