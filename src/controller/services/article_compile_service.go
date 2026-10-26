package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	stderrors "errors"
	"fmt"
	"maps"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/controller/services/internal/worker"
	"knowledge_ingestion/src/domain"
	"knowledge_ingestion/src/infrastructure/caches"

	"github.com/redis/go-redis/v9"
)

// IArticleCompileService worker phase 2 compile (P2.4 phase 1b): đọc PageIndex
// của bài đã index từ Qdrant, gom text từng mục, gọi LLM sinh summary rồi ghi
// bảng article_sections — chỉ worker gọi, trả (int, error) thuần như 2 phase
// trước. Bật mặc định: bài index done tự vào hàng đợi compile.
//
// Vì sao chạy SAU index chứ không nhân tiện trong index:
//   - Đổi config chunker/prompt không tốn lại OCR lẫn embed — index giữ
//     nguyên checkpoint, compile chỉ việc quét lại mục.
//   - LLM summary chậm và hay retry (provider free dính 429); nhét vào loop
//     embed là kéo dài lease của phase index oan.
//
// Cache Redis theo hash (model|tên mục|nội dung): compile lại bài không đổi
// nội dung thì không tốn lượt LLM nào — cùng ý tưởng embed cache ngày cũ
// nhưng đặt ở tầng có chi phí thật (gọi LLM), không phải gọi embed local.
type IArticleCompileService interface {
	CompilePending(ctx context.Context) (int, error)
}

type articleCompileService struct {
	articleRepo domain.ArticleCompileQueue
	sectionRepo domain.IArticleSectionRepository
	vectors     domain.IVectorStore
	summarizer  domain.ISummarizer
	cache       *redis.Client
	cacheTTL    time.Duration
	// maxChunksPerSection trần chunk nạp cho 1 mục — loader/test truyền tay
	// để test ép được 1 mà không đổi hằng số thật.
	maxChunksPerSection int
}

func NewArticleCompileService(
	articleRepo domain.ArticleCompileQueue,
	sectionRepo domain.IArticleSectionRepository,
	vectors domain.IVectorStore,
	summarizer domain.ISummarizer,
	cache caches.ICache,
) IArticleCompileService {
	maxChunks := constants.COMPILE_MAX_CHUNKS_PER_SECTION
	if maxChunks <= 0 {
		maxChunks = 6
	}
	var client *redis.Client
	if cache != nil {
		client = cache.GetClient()
	}
	return &articleCompileService{
		articleRepo:         articleRepo,
		sectionRepo:         sectionRepo,
		vectors:             vectors,
		summarizer:          summarizer,
		cache:               client,
		cacheTTL:            constants.COMPILE_CACHE_TTL,
		maxChunksPerSection: maxChunks,
	}
}

// CompilePending claim 1 đợt bài rồi compile song song trong pool giới hạn,
// trả số bài xong — cùng khung IndexPending: lỗi 1 bài chỉ log + backoff,
// không chặn bài khác; job bị hủy (shutdown) thì trả phần chưa vào pool về
// pending, không đốt attempts.
func (s *articleCompileService) CompilePending(ctx context.Context) (int, error) {
	traceID, err := worker.EnsureTraceID(ctx)
	if err != nil {
		return 0, err
	}
	logs.Infow("compile: bắt đầu kỳ quét", "trace_id", traceID)
	articles, err := s.articleRepo.ClaimCompilePending(ctx, constants.COMPILE_CLAIM_LIMIT, constants.COMPILE_LEASE)
	if err != nil {
		return 0, err
	}
	if len(articles) > 0 {
		logs.Infow("compile: nhận lô bài", "trace_id", traceID,
			"count", len(articles), "articles", articleTags(articles))
	}
	var compiled atomic.Int64
	var wg sync.WaitGroup
	sem := make(chan struct{}, constants.COMPILE_POOL_SIZE)
	pending := make(map[int64]struct{}, len(articles))
	for _, article := range articles {
		pending[article.ID] = struct{}{}
	}
loop:
	for _, article := range articles {
		if ctx.Err() != nil {
			break
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break loop
		}
		delete(pending, article.ID)
		wg.Add(1)
		go func(a *domain.Article) {
			defer wg.Done()
			defer func() { <-sem }()
			s.compileClaimed(ctx, traceID, a, &compiled)
		}(article)
	}
	wg.Wait()
	if ctx.Err() != nil && len(pending) > 0 {
		s.releaseClaims(ctx, traceID, slices.Collect(maps.Keys(pending)))
	}
	n := int(compiled.Load())
	logs.Infow("compile: xong 1 kỳ quét", "trace_id", traceID, "found", len(articles), "compiled", n)
	return n, nil
}

// compileClaimed xử lý 1 bài đã claim — cùng luật ghi trạng thái bằng detached
// ctx như indexClaimed: canceled thì trả về pending, hết giờ (deadline) thì
// để lease reclaim, lỗi thật mới đếm attempts.
func (s *articleCompileService) compileClaimed(ctx context.Context, traceID string, article *domain.Article, done *atomic.Int64) {
	logs.Infow("compile: bắt đầu bài", "trace_id", traceID,
		"article_id", article.ID, "name", article.Name)
	if err := s.compileOneSafe(ctx, traceID, article); err != nil {
		if ctx.Err() != nil {
			if stderrors.Is(ctx.Err(), context.Canceled) {
				s.releaseClaims(ctx, traceID, []int64{article.ID})
			} else {
				logs.Infow("compile: job hết giờ, để lease trả bài",
					"trace_id", traceID, "article_id", article.ID, "name", article.Name)
			}
			return
		}
		attempts := article.CompileAttempts + 1
		// Lỗi vĩnh viễn từ vector store (filter sai, 4xx) retry cũng vậy —
		// failed luôn thay vì backoff 10 lần.
		if s.vectors.IsPermanentError(err) {
			attempts = utils.MaxQueueAttempts
			logs.Warnw("compile: lỗi vĩnh viễn, failed luôn",
				"trace_id", traceID, "article_id", article.ID, "name", article.Name, "error", err)
		} else {
			logs.Warnw("compile: bỏ qua bài, thử lại theo backoff",
				"trace_id", traceID, "article_id", article.ID, "name", article.Name, "error", err)
		}
		det := worker.Detach(ctx)
		defer det.Close()
		if merr := s.articleRepo.MarkCompileError(det.Ctx, article.ID, attempts); merr != nil {
			logs.Error(merr, "compile: không đánh dấu lỗi được", "trace_id", traceID, "article_id", article.ID, "name", article.Name)
		}
		return
	}
	det := worker.Detach(ctx)
	defer det.Close()
	if err := s.articleRepo.MarkCompileDone(det.Ctx, article.ID); err != nil {
		logs.Error(err, "compile: không đánh dấu done được", "trace_id", traceID, "article_id", article.ID, "name", article.Name)
		return
	}
	done.Add(1)
}

// releaseClaims trả 1 lô bài về pending khi job bị hủy do shutdown — 1 câu
// UPDATE, không N+1. Detached ctx vì ctx gốc đã cancel; WHERE ... processing
// trong repo bảo vệ bài đã kịp done.
func (s *articleCompileService) releaseClaims(ctx context.Context, traceID string, ids []int64) {
	if len(ids) == 0 {
		return
	}
	det := worker.Detach(ctx)
	defer det.Close()
	if err := s.articleRepo.ReleaseCompileClaims(det.Ctx, ids); err != nil {
		logs.Error(err, "compile: không trả claim được, bài chờ hết lease",
			"trace_id", traceID, "articles", len(ids))
		return
	}
	logs.Infow("compile: job bị hủy (shutdown), trả bài về pending",
		"trace_id", traceID, "articles", len(ids), "article_ids", ids)
}

// compileOneSafe bọc compileOne để panic trong goroutine con (không có
// gin.Recovery) không giết process: biến panic thành error để đường xử lý lỗi
// hiện có đánh dấu bài + backoff, thay vì kẹt processing đến hết lease.
func (s *articleCompileService) compileOneSafe(ctx context.Context, traceID string, article *domain.Article) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			logs.Error(fmt.Errorf("panic: %v", rec), "compile: panic khi xử lý bài",
				"trace_id", traceID, "article_id", article.ID, "name", article.Name,
				"stack", string(debug.Stack()))
			err = fmt.Errorf("panic khi compile bài %d: %v", article.ID, rec)
		}
	}()
	return s.compileOne(ctx, traceID, article)
}

// compileOne đọc PageIndex của bài từ Qdrant, gom text từng mục, gọi LLM tóm
// tắt (có cache), rồi ghi cả loạt vào article_sections (Replace — dọn dòng cũ
// trước). Mục lấy theo thứ tự chunk; nội dung 1 mục là cửa sổ chunk từ
// sec.From tới trước mục kế tiếp, cắt thêm ở trần maxChunksPerSection để
// prompt không phình quá cửa sổ context của model free.
func (s *articleCompileService) compileOne(ctx context.Context, traceID string, article *domain.Article) error {
	start := time.Now()
	filter := domain.SearchFilter{UserID: article.UserID, ArticleID: article.ID}
	sections, err := s.vectors.ListSections(ctx, filter)
	if err != nil {
		return fmt.Errorf("đọc page index: %w", err)
	}
	if len(sections) == 0 {
		// Bài index done mà không có mục nào (tài liệu không tiêu đề) — vẫn
		// Replace rỗng để dọn dòng cũ, coi như compile xong.
		det := worker.Detach(ctx)
		defer det.Close()
		if err := s.sectionRepo.Replace(det.Ctx, article.ID, nil); err != nil {
			return fmt.Errorf("dọn section cũ: %w", err)
		}
		logs.Infow("compile: bài không có mục nào", "trace_id", traceID,
			"article_id", article.ID, "name", article.Name)
		return nil
	}
	rows := make([]*domain.ArticleSection, 0, len(sections))
	for i, sec := range sections {
		span := s.maxChunksPerSection
		if i+1 < len(sections) {
			if w := sections[i+1].From - sec.From; w < span {
				span = w
			}
		}
		if span <= 0 {
			span = 1
		}
		text, err := s.sectionText(ctx, filter, sec.From, span)
		if err != nil {
			return fmt.Errorf("đọc nội dung mục %q: %w", sec.Name, err)
		}
		summary, err := s.summarize(ctx, traceID, article, sec.Name, text)
		if err != nil {
			return fmt.Errorf("tóm tắt mục %q: %w", sec.Name, err)
		}
		rows = append(rows, &domain.ArticleSection{
			ArticleID:  article.ID,
			Name:       sec.Name,
			StartChunk: sec.From,
			Summary:    summary,
		})
		// Bài bị xoá / job bị hủy giữa chừng thì dừng, đừng tốn LLM cho các
		// mục còn lại (lỗi compile là transient — trả về để backoff).
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	det := worker.Detach(ctx)
	defer det.Close()
	if err := s.sectionRepo.Replace(det.Ctx, article.ID, rows); err != nil {
		return fmt.Errorf("ghi article_sections: %w", err)
	}
	logs.Infow("compile: xong 1 bài", "trace_id", traceID, "article_id", article.ID,
		"name", article.Name, "sections", len(rows), "elapsed_ms", time.Since(start).Milliseconds())
	return nil
}

// sectionText gom text các chunk của 1 mục từ Qdrant (ScrollRange cửa sổ
// [from, from+span)) — nguồn nội dung cho LLM tóm tắt. Chunk đã mang sẵn
// tiêu đề mục prepend lúc index nên càng thêm ngữ cảnh, không sao.
func (s *articleCompileService) sectionText(ctx context.Context, filter domain.SearchFilter, from, span int) (string, error) {
	chunks, err := s.vectors.ScrollRange(ctx, filter, from, span)
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(chunks))
	for _, c := range chunks {
		if strings.TrimSpace(c.Text) != "" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

// summarize đọc cache Redis theo hash (model|tên mục|nội dung), miss mới gọi
// LLM rồi ghi cache — cùng pattern enhancer.Keywords nhưng chi phí thật ở đây
// là lời gọi LLM. Cache không quyết định tính đúng: đọc/ghi lỗi thì log + gọi
// thẳng LLM, kết quả vẫn ghi DB.
func (s *articleCompileService) summarize(ctx context.Context, traceID string, article *domain.Article, sectionName, text string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", nil
	}
	key := s.cacheKey(sectionName, text)
	if s.cache != nil {
		v, err := s.cache.Get(ctx, key).Result()
		if err == nil && v != "" {
			return v, nil
		}
		if err != nil && !stderrors.Is(err, redis.Nil) {
			logs.Error(err, "compile: đọc cache lỗi, gọi LLM thẳng",
				"trace_id", traceID, "article_id", article.ID, "name", article.Name)
		}
	}
	summary, err := s.summarizer.Summarize(ctx, sectionName, text)
	if err != nil {
		return "", err
	}
	if s.cache != nil && summary != "" {
		if err := s.cache.Set(ctx, key, summary, s.cacheTTL).Err(); err != nil {
			logs.Error(err, "compile: ghi cache lỗi (không chặn kết quả)",
				"trace_id", traceID, "article_id", article.ID, "name", article.Name)
		}
	}
	return summary, nil
}

// cacheKey băm (model|tên mục|nội dung) — đổi model hoặc sửa nội dung mục thì
// key khác, tự động gọi LLM lại thay vì ăn summary cũ.
func (s *articleCompileService) cacheKey(name, text string) string {
	sum := sha256.Sum256([]byte(s.summarizer.ModelID() + "|" + name + "|" + text))
	return "compile:sum:" + hex.EncodeToString(sum[:])
}
