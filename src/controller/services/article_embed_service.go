package services

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/domain"
)

// errEmbedContract báo embedder trả thiếu vector (sai contract do service tự
// phát hiện) — retry vô ích nên failed luôn, cùng nhóm với lỗi vĩnh viễn của
// embedder/vector store (hỏi qua domain interface, không import infra).
var errEmbedContract = errors.New("embed trả thiếu vector")

const (
	// EmbedInterval là nhịp worker quét bài chưa embed — export để worker lấy
	// làm chu kỳ ticker. 10s để dev thấy kết quả nhanh; production tải cao
	// thì nâng lên 1 phút để đỡ quét DB trống.
	EmbedInterval = 10 * time.Second
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

// IArticleEmbedService worker phase 2: đọc chunk phase 1 đã cắt, embed bằng
// ollama rồi upsert Qdrant — chỉ worker gọi, không qua HTTP nên trả (int,
// error) thuần thay vì dtos.Result. Trả số chunk đã lên Qdrant trong kỳ.
type IArticleEmbedService interface {
	EmbedPending(ctx context.Context) (int, error)
}

type articleEmbedService struct {
	articleRepo domain.ArticleEmbedQueue
	chunkRepo   domain.IArticleChunkRepository
	embedder    domain.Embedder
	vectors     domain.IVectorStore
	uow         domain.IUnitOfWork
}

func NewArticleEmbedService(articleRepo domain.ArticleEmbedQueue, chunkRepo domain.IArticleChunkRepository, embedder domain.Embedder, vectors domain.IVectorStore, uow domain.IUnitOfWork) IArticleEmbedService {
	return &articleEmbedService{articleRepo: articleRepo, chunkRepo: chunkRepo, embedder: embedder, vectors: vectors, uow: uow}
}

// EmbedPending claim 1 đợt bài (đã chunk done) rồi embed song song trong pool
// giới hạn. Mỗi bài: embed theo đợt 32 → upsert từng đợt → mark từng đợt, nên
// crash giữa bài 500 trang thì kỳ sau chỉ làm nốt phần còn lại. Thứ tự
// Qdrant-trước-Mark-sau: point mồ côi mang ID cũ, upsert lại ghi đè — hội tụ
// như purge. Cùng quy ước trace_id với ChunkPending/PurgeDeleted.
func (s *articleEmbedService) EmbedPending(ctx context.Context) (int, error) {
	traceID, err := ensureTraceID(ctx)
	if err != nil {
		return 0, err
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
		// Nhánh rỗng: chunk đã hết (mark đủ rồi crash trước done, hoặc trim
		// xong crash trước done) thì vector phải > 0 mới được done — count 0
		// nghĩa là chưa có gì lên Qdrant (bài restore/chunk mất), retry thay
		// vì done âm thầm với search trống.
		n, err := s.vectors.CountByArticle(ctx, article.ID)
		if err != nil {
			s.markEmbedError(ctx, dbCtx, traceID, article, fmt.Errorf("đếm vector: %w", err))
			return
		}
		if n == 0 {
			s.markEmbedError(ctx, dbCtx, traceID, article, fmt.Errorf("không còn chunk dở mà Qdrant cũng 0 point"))
			return
		}
		if err := s.finishEmbed(dbCtx, article); err != nil {
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
	if err := s.finishEmbed(dbCtx, article); err != nil {
		logs.Error(err, "embed: không đánh dấu done được", "trace_id", traceID, "article_id", article.ID)
		return
	}
	logs.Infow("embed: xong 1 bài", "trace_id", traceID, "article_id", article.ID, "chunks", len(items))
}

// finishEmbed chốt 1 bài đã embed xong: dọn chunk + đánh done trong ĐÚNG 1
// transaction qua IUnitOfWork (dbConn của 2 repo cùng nhặt tx từ ctx) — crash
// giữa chừng rollback sạch, kỳ sau làm lại từ đầu mà không nửa vời. Vector +
// text đã nằm Qdrant payload nên DB không cần giữ chunk nữa.
func (s *articleEmbedService) finishEmbed(dbCtx context.Context, article *domain.Article) error {
	return s.uow.InTx(dbCtx, func(txCtx context.Context) error {
		if err := s.chunkRepo.DeleteByArticleID(txCtx, article.ID); err != nil {
			return fmt.Errorf("dọn chunk: %w", err)
		}
		if err := s.articleRepo.MarkEmbedDone(txCtx, article.ID); err != nil {
			return fmt.Errorf("đánh dấu done: %w", err)
		}
		return nil
	})
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
	attempts := article.EmbedAttempts + 1
	// Lỗi vĩnh viễn (sai dim/model, request sai 4xx, sai contract nội bộ)
	// retry cũng vậy — failed luôn như chunk làm với extractor.IsPermanent,
	// khỏi backoff 10 lần. Hỏi tính vĩnh viễn qua domain interface để service
	// không import infra (DIP).
	if errors.Is(err, errEmbedContract) || s.embedder.IsPermanentError(err) || s.vectors.IsPermanentError(err) {
		attempts = domain.MaxQueueAttempts
		logs.Warnw("embed: lỗi vĩnh viễn, failed luôn",
			"trace_id", traceID, "article_id", article.ID, "error", err)
	} else {
		logs.Warnw("embed: bỏ qua bài, thử lại theo backoff",
			"trace_id", traceID, "article_id", article.ID, "error", err)
	}
	if merr := s.articleRepo.MarkEmbedError(dbCtx, article.ID, attempts); merr != nil {
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
	// Log trước/sau mỗi lần gọi để nhìn thấy nhịp batch Ollama trong log
	// worker (1 dòng/lần gọi, không log từng chunk để khỏi spam).
	logs.Infow("ollama: gọi embed", "article_id", batch[0].Chunk.ArticleID,
		"texts", len(texts), "model", s.embedder.ModelName())
	embedStart := time.Now()
	if err := withRetry(ctx, embedCallAttempts, embedCallBaseDelay, func(ctx context.Context) error {
		var err error
		vectors, err = s.embedder.EmbedBatch(ctx, texts)
		return err
	}); err != nil {
		return 0, fmt.Errorf("gọi embed: %w", err)
	}
	logs.Infow("ollama: embed xong", "article_id", batch[0].Chunk.ArticleID,
		"texts", len(texts), "elapsed_ms", time.Since(embedStart).Milliseconds())
	// Contract EmbedBatch không đảm bảo số vector trả về — thiếu là lỗi
	// embedder, không được index mù gây panic cả job.
	if len(vectors) != len(batch) {
		return 0, fmt.Errorf("%w: embed trả %d vector cho %d text", errEmbedContract, len(vectors), len(batch))
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
