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
	"knowledge_ingestion/src/common/llmusage"
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
// Ba quyết định giữ chi phí LLM trong tầm kiểm soát (đợt 29):
//  1. GỘP LÔ: 1 lượt gọi tóm tắt COMPILE_SECTION_BATCH mục thay vì 1 mục —
//     bài 2000 mục giảm từ 2000 request còn ~250.
//  2. CHECKPOINT: sau mỗi lô đã tóm tắt thì ghi ngay vào article_sections và
//     đếm compile_done_sections. Job chết/hết giờ thì kỳ sau làm nốt phần còn
//     lại thay vì tóm tắt lại từ đầu.
//  3. TIMEOUT THEO LƯỢT: mỗi lượt gọi LLM có deadline riêng (COMPILE_CALL_TIMEOUT)
//     nên 1 lượt kẹt không giết cả bài.
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
	// usageRepo ghi số liệu tiêu thụ LLM theo tài khoản — bảng audit
	// llm_usage_daily. nil = tắt audit (test/fake), không chặn compile.
	usageRepo domain.ILLMUsageRepository
	cache     *redis.Client
	cacheTTL  time.Duration
	// maxChunksPerSection trần chunk nạp cho 1 mục — loader/test truyền tay
	// để test ép được 1 mà không đổi hằng số thật.
	maxChunksPerSection int
	// batchSize số mục gộp trong 1 lượt gọi LLM.
	batchSize int
	// callTimeout deadline riêng cho mỗi lượt gọi LLM.
	callTimeout time.Duration
	// cycleLLMCalls/cycleCacheHits đếm trong 1 kỳ quét để dòng "xong 1 kỳ
	// quét" trả lời được câu "kỳ này worker có gọi LLM không": llm_calls=0
	// khi UI đã báo xong là bằng chứng worker im lặng, dashboard provider
	// nhảy thì phải truy site khác (chat/classify).
	cycleLLMCalls  atomic.Int64
	cycleCacheHits atomic.Int64
}

func NewArticleCompileService(
	articleRepo domain.ArticleCompileQueue,
	sectionRepo domain.IArticleSectionRepository,
	vectors domain.IVectorStore,
	summarizer domain.ISummarizer,
	cache caches.ICache,
	usageRepo domain.ILLMUsageRepository,
) IArticleCompileService {
	maxChunks := constants.COMPILE_MAX_CHUNKS_PER_SECTION
	if maxChunks <= 0 {
		maxChunks = 6
	}
	var client *redis.Client
	if cache != nil {
		client = cache.GetClient()
	}
	if client == nil {
		// Không có cache thì mọi lần compile lại (reset queue, lease hết hạn)
		// đều là lời gọi LLM thật — phải nói ra, đừng để "sao provider nhiều
		// request thế" thành câu hỏi không có đáp án.
		logs.Warnw("compile: không có Redis — summary không được cache, " +
			"mọi lần compile lại đều gọi LLM thật")
	}
	return &articleCompileService{
		articleRepo:         articleRepo,
		sectionRepo:         sectionRepo,
		vectors:             vectors,
		summarizer:          summarizer,
		usageRepo:           usageRepo,
		cache:               client,
		cacheTTL:            constants.COMPILE_CACHE_TTL,
		maxChunksPerSection: maxChunks,
		batchSize:           constants.COMPILE_SECTION_BATCH,
		callTimeout:         constants.COMPILE_CALL_TIMEOUT,
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
	logs.Debugw("compile: bắt đầu kỳ quét", "trace_id", traceID)
	s.cycleLLMCalls.Store(0)
	s.cycleCacheHits.Store(0)
	articles, err := s.articleRepo.ClaimCompilePending(ctx, constants.COMPILE_CLAIM_LIMIT, constants.COMPILE_LEASE)
	if err != nil {
		return 0, err
	}
	var compiled atomic.Int64
	var wg sync.WaitGroup
	sem := make(chan struct{}, constants.COMPILE_POOL_SIZE)
	pending := make(map[int64]struct{}, len(articles))
	reclaimed := 0
	for _, article := range articles {
		pending[article.ID] = struct{}{}
		// Row trả về là trạng thái TRƯỚC khi claim: processing nghĩa là nhận
		// lại bài của kỳ trước (worker chết / job hết giờ) — đây là 1 nguồn
		// "compile lại" tốn LLM.
		if article.CompileStatus == domain.QueueProcessing {
			reclaimed++
		}
	}
	if len(articles) > 0 {
		logs.Infow("compile: nhận lô bài", "trace_id", traceID,
			"count", len(articles), "reclaimed", reclaimed, "articles", articleTags(articles))
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
	logs.Infow("compile: xong 1 kỳ quét", "trace_id", traceID, "found", len(articles),
		"compiled", n, "llm_calls", s.cycleLLMCalls.Load(),
		"cache_hits", s.cycleCacheHits.Load())
	return n, nil
}

// compileClaimed xử lý 1 bài đã claim — cùng luật ghi trạng thái bằng detached
// ctx như indexClaimed: canceled thì trả về pending, hết giờ (deadline) thì
// để lease reclaim, lỗi thật mới đếm attempts.
func (s *articleCompileService) compileClaimed(ctx context.Context, traceID string, article *domain.Article, done *atomic.Int64) {
	logs.Infow("compile: bắt đầu bài", "trace_id", traceID,
		"article_id", article.ID, "name", article.Name, "file", fileTag(article),
		"doc_type", article.DocType, "content_type", article.ContentType)
	if err := s.compileOneSafe(ctx, traceID, article); err != nil {
		if ctx.Err() != nil {
			if stderrors.Is(ctx.Err(), context.Canceled) {
				s.releaseClaims(ctx, traceID, []int64{article.ID})
			} else {
				logs.Infow("compile: job hết giờ, để lease trả bài",
					"trace_id", traceID, "article_id", article.ID, "name", article.Name,
					"file", fileTag(article))
			}
			return
		}
		attempts := article.CompileAttempts + 1
		// Lỗi vĩnh viễn từ vector store (filter sai, 4xx) retry cũng vậy —
		// failed luôn thay vì backoff 10 lần.
		if s.vectors.IsPermanentError(err) {
			attempts = utils.MaxQueueAttempts
			logs.Warnw("compile: lỗi vĩnh viễn, failed luôn",
				"trace_id", traceID, "article_id", article.ID, "name", article.Name,
				"file", fileTag(article), "error", err)
		} else {
			logs.Warnw("compile: bỏ qua bài, thử lại theo backoff",
				"trace_id", traceID, "article_id", article.ID, "name", article.Name,
				"file", fileTag(article), "error", err)
		}
		det := worker.Detach(ctx)
		defer det.Close()
		if merr := s.articleRepo.MarkCompileError(det.Ctx, article.ID, attempts); merr != nil {
			logs.Error(merr, "compile: không đánh dấu lỗi được", "trace_id", traceID,
				"article_id", article.ID, "name", article.Name, "file", fileTag(article))
		}
		return
	}
	det := worker.Detach(ctx)
	defer det.Close()
	if err := s.articleRepo.MarkCompileDone(det.Ctx, article.ID); err != nil {
		logs.Error(err, "compile: không đánh dấu done được", "trace_id", traceID,
			"article_id", article.ID, "name", article.Name, "file", fileTag(article))
		return
	}
	done.Add(1)
	// Mốc UI chuyển "xong": compile là phase cuối (extract → index → compile)
	// nên sau dòng này KHÔNG được còn request LLM nào cho bài này. Dashboard
	// provider vẫn nhảy thì đối chiếu dòng "llm: request" để biết site thật.
	logs.Infow("pipeline: bài xong cả 3 phase (UI báo done)", "trace_id", traceID,
		"article_id", article.ID, "name", article.Name, "file", fileTag(article),
		"compiled_at", time.Now().Format(time.RFC3339))
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
				"file", fileTag(article), "stack", string(debug.Stack()))
			err = fmt.Errorf("panic khi compile bài %d: %v", article.ID, rec)
		}
	}()
	return s.compileOne(ctx, traceID, article)
}

// compileOne chỉ điều phối: đọc danh sách mục rồi chạy vòng lô (compileRun) —
// chi tiết ghi checkpoint nằm ở compileRun bên dưới.
func (s *articleCompileService) compileOne(ctx context.Context, traceID string, article *domain.Article) error {
	start := time.Now()
	filter := domain.SearchFilter{UserID: article.UserID, ArticleID: article.ID}
	sections, err := s.vectors.ListSections(ctx, filter)
	if err != nil {
		return fmt.Errorf("đọc page index: %w", err)
	}
	if len(sections) == 0 {
		// Bài index done mà không có mục nào (tài liệu không tiêu đề) — vẫn
		// dọn dòng cũ, coi như compile xong.
		det := worker.Detach(ctx)
		defer det.Close()
		if err := s.sectionRepo.Replace(det.Ctx, article.ID, nil); err != nil {
			return fmt.Errorf("dọn section cũ: %w", err)
		}
		logs.Infow("compile: bài không có mục nào", "trace_id", traceID,
			"article_id", article.ID, "name", article.Name, "file", fileTag(article))
		return nil
	}
	r := newCompileRun(s, ctx, traceID, article, filter, sections)
	if err := r.run(); err != nil {
		return err
	}
	logs.Infow("compile: xong 1 bài", "trace_id", traceID, "article_id", article.ID,
		"name", article.Name, "file", fileTag(article), "sections", r.done,
		"llm_calls", r.stats.llmCalls, "cache_hits", r.stats.cacheHits,
		"elapsed_ms", time.Since(start).Milliseconds())
	return nil
}

// compileStats đếm lượt gọi LLM THẬT và lượt ăn cache trong 1 lần compile bài.
// llm_calls là số lượt gọi MODEL (mỗi lượt sinh đúng 1 dòng "llm: request"
// site=compile); cache_hits là số mục không phải gọi LLM vì đã có trong cache.
type compileStats struct {
	llmCalls  int
	cacheHits int
}

// compileRun là trạng thái của MỘT bài đang compile: danh sách mục, checkpoint
// đã xong, bộ đếm lượt gọi LLM và collector audit. Tách thành struct để
// compileOne chỉ còn điều phối — mỗi hàm dưới đây nằm gọn trong ngân sách
// hàm của repo (src/quality/function_budget_test.go).
type compileRun struct {
	svc      *articleCompileService
	ctx      context.Context
	traceID  string
	article  *domain.Article
	filter   domain.SearchFilter
	sections []domain.SectionSpan
	// collector gom số liệu tiêu thụ LLM của bài này (mọi lượt model trong
	// lô) — đổ vào bảng audit theo tài khoản sau mỗi lô.
	collector *llmusage.Collector
	stats     compileStats
	// done là số mục ĐẦU TIÊN đã tóm tắt xong — checkpoint compile_done_sections.
	done int
}

func newCompileRun(svc *articleCompileService, ctx context.Context, traceID string, article *domain.Article,
	filter domain.SearchFilter, sections []domain.SectionSpan) *compileRun {
	collector := llmusage.NewCollector()
	// Checkpoint: số mục đầu đã tóm tắt xong. Chặn trên độ dài danh sách để
	// checkpoint cũ (bài bị index lại, mục ít hơn) không chỉ tới ngoài.
	done := article.CompileDoneSections
	if done < 0 {
		done = 0
	}
	if done > len(sections) {
		done = len(sections)
	}
	return &compileRun{
		svc:       svc,
		ctx:       llmusage.WithCollector(ctx, collector),
		traceID:   traceID,
		article:   article,
		filter:    filter,
		sections:  sections,
		collector: collector,
		done:      done,
	}
}

// run là vòng lô của compile: mỗi lô gom text → tra cache → gọi LLM cho phần
// miss → ghi ngay article_sections (checkpoint) → đếm tiến trình.
//
// Ghi thứ tự quan trọng: upsert TRƯỚC, đếm checkpoint SAU. Nếu chết giữa
// chừng thì checkpoint chỉ LAG chứ không LEAD — kỳ sau làm lại đúng 1 lô đó
// (upsert idempotent theo khoá article_id+name) chứ không bỏ sót mục nào.
func (r *compileRun) run() error {
	if err := r.prepare(); err != nil {
		return err
	}
	for r.done < len(r.sections) {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		if err := r.step(); err != nil {
			return err
		}
	}
	return nil
}

// prepare dọn dòng cũ khi compile CHẠY TỪ ĐẦU — đổi chunker làm danh sách mục
// đổi theo, mục cũ phải mất đi chứ không nằm lại. Chạy lại có checkpoint thì
// KHÔNG dọn (đó là lý do tồn tại): dọn lúc đó là xoá luôn phần đã làm xong.
func (r *compileRun) prepare() error {
	if r.done != 0 {
		return nil
	}
	det := worker.Detach(r.ctx)
	defer det.Close()
	if err := r.svc.sectionRepo.Replace(det.Ctx, r.article.ID, nil); err != nil {
		return fmt.Errorf("dọn section cũ: %w", err)
	}
	return nil
}

// step xử lý 1 lô mục: gom text, gọi LLM cho phần chưa có cache, ghi
// article_sections rồi cập nhật checkpoint + audit.
func (r *compileRun) step() error {
	batch := r.batch()
	rows, err := r.summarizeBatch(batch)
	if err != nil {
		// Lượt gọi hỏng vẫn đã tốn token đầu vào và vẫn hiện trên dashboard
		// provider — ghi audit luôn, không thì báo cáo lệch đúng lúc cần
		// đối chiếu nhất.
		_ = r.flushUsage()
		return err
	}
	newDone := r.done + len(batch)
	if err := r.persist(rows, newDone); err != nil {
		return err
	}
	r.done = newDone
	return r.flushUsage()
}

// batch cắt lô mục kế tiếp — cuối lô có thể ngắn hơn COMPILE_SECTION_BATCH.
func (r *compileRun) batch() []domain.SectionSpan {
	end := min(r.done+r.svc.batchSize, len(r.sections))
	return r.sections[r.done:end]
}

// persist ghi lô mục vừa tóm tắt vào article_sections rồi đếm checkpoint.
// Upsert theo khoá (article_id, name) nên chạy lại lô cũ không nhân bản.
func (r *compileRun) persist(rows []*domain.ArticleSection, newDone int) error {
	det := worker.Detach(r.ctx)
	defer det.Close()
	if len(rows) > 0 {
		if err := r.svc.sectionRepo.Upsert(det.Ctx, rows); err != nil {
			return fmt.Errorf("ghi article_sections: %w", err)
		}
	}
	if err := r.svc.articleRepo.MarkCompileProgress(det.Ctx, r.article.ID, newDone); err != nil {
		return fmt.Errorf("ghi checkpoint compile: %w", err)
	}
	return nil
}

// flushUsage đẩy số liệu LLM của lô vừa rồi vào bảng audit theo tài khoản.
// Lỗi ghi log ra chứ KHÔNG làm hỏng compile — số liệu audit thiếu 1 lô vẫn
// hơn là bài không compile xong.
func (r *compileRun) flushUsage() error {
	bySite := r.collector.Drain()
	if len(bySite) == 0 {
		return nil
	}
	det := worker.Detach(r.ctx)
	defer det.Close()
	err := llmusage.Record(det.Ctx, r.svc.usageRepo, r.article.UserID,
		r.svc.summarizer.ProviderID(), r.svc.summarizer.ModelID(), bySite)
	if err != nil {
		logs.Error(err, "compile: ghi audit usage lỗi", "trace_id", r.traceID,
			"article_id", r.article.ID, "name", r.article.Name, "file", fileTag(r.article))
	}
	return nil
}

// summarizeBatch tóm tắt 1 lô mục: mục nào đã có trong cache thì lấy luôn
// (không tốn lượt LLM), phần còn lại gộp trong 1 lượt gọi LLM.
//
// Cache theo từng mục (hash model|tên|nội dung) nên lô có thể đẹp lẻ: 3 mục
// hit + 5 mục miss thì chỉ gọi LLM cho 5 mục đó.
func (r *compileRun) summarizeBatch(batch []domain.SectionSpan) ([]*domain.ArticleSection, error) {
	rows, pending, err := r.gather(batch)
	if err != nil {
		return nil, err
	}
	if len(pending) > 0 {
		sums, err := r.callLLM(pending)
		if err != nil {
			return nil, err
		}
		for i, p := range pending {
			rows[p.row].summary = sums[i]
			r.svc.saveSummary(r.ctx, r.article, p.name, p.text, sums[i])
		}
	}
	out := make([]*domain.ArticleSection, 0, len(rows))
	for _, row := range rows {
		out = append(out, &domain.ArticleSection{
			ArticleID:  r.article.ID,
			Name:       row.name,
			StartChunk: row.start,
			Summary:    row.summary,
		})
	}
	return out, nil
}

// compileRow là 1 mục của lô — text đã gom, summary đã có (từ cache) hoặc
// chưa (cần gọi LLM).
type compileRow struct {
	name    string
	start   int
	text    string
	summary string
	// row là vị trí trong slice rows của lô — để gán kết quả LLM về đúng mục.
	row int
}

// gather gom text từng mục trong lô và tra cache — trả về mọi mục của lô và
// danh sách mục còn THIẾU summary (cần gọi LLM).
func (r *compileRun) gather(batch []domain.SectionSpan) ([]*compileRow, []*compileRow, error) {
	rows := make([]*compileRow, 0, len(batch))
	pending := make([]*compileRow, 0, len(batch))
	for i, sec := range batch {
		row := &compileRow{name: sec.Name, start: sec.From, row: i}
		text, err := r.sectionText(sec.From, r.spanOf(r.done+i))
		if err != nil {
			return nil, nil, err
		}
		row.text = text
		rows = append(rows, row)
		if strings.TrimSpace(text) == "" {
			// Mục không có chunk nào → không có gì để tóm tắt, không tốn lượt
			// LLM nào (và cũng không ăn cache nào).
			continue
		}
		if sum, ok := r.svc.cachedSummary(r.ctx, r.traceID, r.article, sec.Name, text); ok {
			row.summary = sum
			r.stats.cacheHits++
			r.svc.cycleCacheHits.Add(1)
			continue
		}
		pending = append(pending, row)
	}
	return rows, pending, nil
}

// spanOf tính số chunk nạp cho mục ở vị trí idx — cửa sổ tới trước mục kế
// tiếp, cắt thêm ở trần maxChunksPerSection để prompt không phình quá cửa
// sổ context của model free.
func (r *compileRun) spanOf(idx int) int {
	span := r.svc.maxChunksPerSection
	if idx+1 < len(r.sections) {
		if w := r.sections[idx+1].From - r.sections[idx].From; w < span {
			span = w
		}
	}
	if span <= 0 {
		span = 1
	}
	return span
}

// sectionText gom text các chunk của 1 mục từ Qdrant (ScrollRange cửa sổ
// [from, from+span)) — nguồn nội dung cho LLM tóm tắt. Chunk đã mang sẵn
// tiêu đề mục prepend lúc index nên càng thêm ngữ cảnh, không sao.
func (r *compileRun) sectionText(from, span int) (string, error) {
	chunks, err := r.svc.vectors.ScrollRange(r.ctx, r.filter, from, span)
	if err != nil {
		return "", fmt.Errorf("đọc nội dung mục tại chunk %d: %w", from, err)
	}
	parts := make([]string, 0, len(chunks))
	for _, c := range chunks {
		if strings.TrimSpace(c.Text) != "" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

// callLLM gọi LLM 1 lượt cho cả lô mục còn thiếu — timeout riêng cho lượt này
// (COMPILE_CALL_TIMEOUT) để 1 lượt kẹt không giết cả bài. Đếm vào bộ đếm của
// bài lẫn của kỳ quét.
func (r *compileRun) callLLM(pending []*compileRow) ([]string, error) {
	items := make([]domain.SectionContent, 0, len(pending))
	for _, p := range pending {
		items = append(items, domain.SectionContent{Name: p.name, Text: p.text})
	}
	// 1 dòng = 1 call LLM thật: đây là chỗ DUY NHẤT trong pipeline sinh lời
	// gọi LLM (index chỉ gọi bge-m3), nên đếm dòng này là biết chi phí của bài.
	// site=compile lan xuống transport để dashboard provider khớp được.
	logs.Infow("compile: gọi LLM tóm tắt lô mục", "trace_id", r.traceID,
		"article_id", r.article.ID, "name", r.article.Name, "file", fileTag(r.article),
		"sections", len(items), "model", r.svc.summarizer.ModelID())
	ctx, cancel := context.WithTimeout(r.ctx, r.svc.callTimeout)
	defer cancel()
	ctx = utils.WithLLMSite(ctx, constants.LLMSiteCompile)
	sums, err := r.svc.summarizer.SummarizeBatch(ctx, items)
	if err != nil {
		return nil, fmt.Errorf("tóm tắt lô mục: %w", err)
	}
	r.stats.llmCalls++
	r.svc.cycleLLMCalls.Add(1)
	return sums, nil
}

// cachedSummary tra cache Redis theo hash (model|tên mục|nội dung) — cờ
// thứ hai báo có dùng cache hay không để tầng trên đếm được chi phí thật.
// Cache không quyết định tính đúng: đọc lỗi thì coi như miss, gọi LLM thẳng.
func (s *articleCompileService) cachedSummary(ctx context.Context, traceID string, article *domain.Article,
	sectionName, text string) (string, bool) {
	if s.cache == nil {
		return "", false
	}
	v, err := s.cache.Get(ctx, s.cacheKey(sectionName, text)).Result()
	if err == nil && v != "" {
		logs.Debugw("compile: cache hit, không gọi LLM", "trace_id", traceID,
			"article_id", article.ID, "file", fileTag(article), "section", sectionName)
		return v, true
	}
	if err != nil && !stderrors.Is(err, redis.Nil) {
		logs.Error(err, "compile: đọc cache lỗi, gọi LLM thẳng",
			"trace_id", traceID, "article_id", article.ID, "name", article.Name,
			"file", fileTag(article))
	}
	return "", false
}

// saveSummary ghi summary vừa sinh vào cache — lần compile sau (bài bị reset,
// lease hết hạn) không tốn lượt LLM lại nếu nội dung không đổi.
func (s *articleCompileService) saveSummary(ctx context.Context, article *domain.Article,
	sectionName, text, summary string) {
	if s.cache == nil || summary == "" {
		return
	}
	if err := s.cache.Set(ctx, s.cacheKey(sectionName, text), summary, s.cacheTTL).Err(); err != nil {
		logs.Error(err, "compile: ghi cache lỗi (không chặn kết quả)",
			"article_id", article.ID, "name", article.Name, "file", fileTag(article),
			"section", sectionName)
	}
}

// cacheKey băm (model|tên mục|nội dung) — đổi model hoặc sửa nội dung mục thì
// key khác, tự động gọi LLM lại thay vì ăn summary cũ.
func (s *articleCompileService) cacheKey(name, text string) string {
	sum := sha256.Sum256([]byte(s.summarizer.ModelID() + "|" + name + "|" + text))
	return "compile:sum:" + hex.EncodeToString(sum[:])
}
