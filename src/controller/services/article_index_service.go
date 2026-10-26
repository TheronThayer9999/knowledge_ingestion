package services

import (
	"context"
	stderrors "errors"
	"fmt"
	"maps"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"knowledge_ingestion/src/common/chunker"
	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/enhancement"
	"knowledge_ingestion/src/common/extractor"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/controller/services/internal/worker"
	"knowledge_ingestion/src/domain"
)

// IArticleIndexService worker phase 1: đọc toàn văn bản đã trích ở phase 0,
// cắt chunk, embed rồi upsert Qdrant — chỉ worker gọi, không qua HTTP nên trả
// (int, error) thuần thay vì dtos.Result. Trả số bài đã index xong.
//
// VÌ SAO CHUNK VÀ EMBED GỘP LÀI 1 PHASE:
//  1. Không cần bảng trung gian nữa. Chunk sau khi tách phase extract chỉ còn
//     là CPU + INSERT — viết 1500 row rồi đọc lại chỉ để xoá là vô nghĩa, và
//     Qdrant đã giữ text + vector trong payload làm nguồn sự thật.
//  2. Không còn trạng thái trung gian "chunk xong nhưng chưa embed" — một bài
//     hoặc đã có đủ vector trên Qdrant, hoặc chưa, không có trạng thái nửa vời.
//  3. Checkpoint chỉ cần MỘT SỐ NGUYÊN (articles.index_done_chunks) thay cho
//     cả bảng.
//
// ĐIỀU KIỆN ĐỂ GỘP ĐƯỢC: PointID phải tất định (utils.NewPointID) để chạy
// lại ghi đè đúng point cũ chứ không nhân bản — xem hàm đó.
type IArticleIndexService interface {
	IndexPending(ctx context.Context) (int, error)
}

type articleIndexService struct {
	articleRepo domain.ArticleIndexQueue
	pageRepo    domain.IArticlePageRepository
	embedder    domain.Embedder
	vectors     domain.IVectorStore
	// baseSplitter chiến lược cắt nền (Eino recursive/semantic/manual) —
	// loader chọn theo config worker.chunker, dựng 1 lần rồi tái dùng.
	// Template (laws/table/...) bọc base này để áp chiến lược theo loại tài liệu.
	baseSplitter chunker.Chunker
	// enhancer sinh metadata (keywords) cho chunk — best-effort, nil = tắt.
	enhancer enhancement.Enhancer
	// NOTE: embed_cache đã bị loại bỏ (hit rate 99.99% count=1 → vô dụng).
	// Vector lưu trong Qdrant; rebuild dùng checkpoint index_done_chunks.
}

func NewArticleIndexService(
	articleRepo domain.ArticleIndexQueue,
	pageRepo domain.IArticlePageRepository,
	embedder domain.Embedder,
	vectors domain.IVectorStore,
	baseSplitter chunker.Chunker,
	enhancer enhancement.Enhancer) IArticleIndexService {
	if enhancer == nil {
		enhancer = enhancement.Noop{}
	}
	return &articleIndexService{
		articleRepo:  articleRepo,
		pageRepo:     pageRepo,
		embedder:     embedder,
		vectors:      vectors,
		baseSplitter: baseSplitter,
		enhancer:     enhancer,
	}
}

// IndexPending claim 1 đợt bài rồi index song song trong pool giới hạn, trả
// số bài xong. Bài nào lỗi cũng chỉ log + MarkIndexError (backoff, quá số lần
// thì failed), không chặn bài khác. Claim bằng SKIP LOCKED nên N worker cùng
// chạy không giẫm nhau. Cùng quy ước trace_id với ExtractPending/PurgeDeleted.
func (s *articleIndexService) IndexPending(ctx context.Context) (int, error) {
	traceID, err := worker.EnsureTraceID(ctx)
	if err != nil {
		return 0, err
	}
	logs.Infow("index: bắt đầu kỳ quét", "trace_id", traceID)
	articles, err := s.articleRepo.ClaimIndexPending(ctx, constants.INDEX_CLAIM_LIMIT, constants.INDEX_LEASE)
	if err != nil {
		return 0, err
	}
	if len(articles) > 0 {
		logs.Infow("index: nhận lô bài", "trace_id", traceID,
			"count", len(articles), "articles", articleTags(articles))
	}
	var indexed atomic.Int64
	var wg sync.WaitGroup
	sem := make(chan struct{}, constants.INDEX_POOL_SIZE)
	// pending giữ bài đã claim nhưng CHƯA vào goroutine; xóa khi đã start.
	// Dùng map thay vì quét lại slice: vừa tự dedupe id, vừa lấy phần còn
	// lại bằng range keys — Go không có LINQ nên map là cách gọn nhất.
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
			s.indexClaimed(ctx, traceID, a, &indexed)
		}(article)
	}
	wg.Wait()
	if ctx.Err() != nil && len(pending) > 0 {
		// maps.Keys + slices.Collect thay cho vòng for thủ công; DB chỉ nhận
		// ĐÚNG 1 UPDATE ... WHERE id IN (...) — không N+1.
		ids := slices.Collect(maps.Keys(pending))
		s.releaseClaims(ctx, traceID, ids)
	}
	n := int(indexed.Load())
	logs.Infow("index: xong 1 kỳ quét", "trace_id", traceID, "found", len(articles), "indexed", n)
	return n, nil
}

// indexClaimed xử lý 1 bài đã claim: lỗi thì MarkIndexError, xong thì
// MarkIndexDone. Đánh dấu bằng detached ctx để ghi được dù job hết timeout;
// job hết timeout (ctx hủy) thì bỏ qua không đánh lỗi oan.
func (s *articleIndexService) indexClaimed(ctx context.Context, traceID string, article *domain.Article, done *atomic.Int64) {
	logs.Infow("index: bắt đầu bài", "trace_id", traceID,
		"article_id", article.ID, "name", article.Name)
	// KHÔNG tạo dbCtx dùng chung ở đây: 1 bài 500 trang (~1500 chunk) gọi
	// Ollama hàng chục phút, ctx 30s tạo sớm chết trước khi tới lượt ghi.
	if err := s.indexOneSafe(ctx, traceID, article); err != nil {
		if ctx.Err() != nil {
			// Canceled = runner dừng (shutdown) → trả bài về pending ngay để
			// restart làm tiếp, không đốt attempts. DeadlineExceeded = job vượt
			// trần 45 phút → để lease reclaim, tránh retry nóng vô hạn.
			if stderrors.Is(ctx.Err(), context.Canceled) {
				s.releaseClaims(ctx, traceID, []int64{article.ID})
			} else {
				logs.Infow("index: job hết giờ, để lease trả bài",
					"trace_id", traceID, "article_id", article.ID, "name", article.Name)
			}
			return
		}
		attempts := article.IndexAttempts + 1
		// Lỗi vĩnh viễn (sai dim/model, request sai 4xx, sai contract nội
		// bộ) retry cũng vậy — failed luôn thay vì backoff 10 lần. Hỏi tính
		// vĩnh viễn qua domain interface để service không import infra (DIP).
		if stderrors.Is(err, errIndexContract) || s.embedder.IsPermanentError(err) || s.vectors.IsPermanentError(err) {
			attempts = utils.MaxQueueAttempts
			logs.Warnw("index: lỗi vĩnh viễn, failed luôn",
				"trace_id", traceID, "article_id", article.ID, "name", article.Name, "error", err)
		} else {
			logs.Warnw("index: bỏ qua bài, thử lại theo backoff",
				"trace_id", traceID, "article_id", article.ID, "name", article.Name, "error", err)
		}
		det := worker.Detach(ctx)
		defer det.Close()
		if merr := s.articleRepo.MarkIndexError(det.Ctx, article.ID, attempts); merr != nil {
			logs.Error(merr, "index: không đánh dấu lỗi được", "trace_id", traceID, "article_id", article.ID, "name", article.Name)
		}
		return
	}
	det := worker.Detach(ctx)
	defer det.Close()
	if err := s.articleRepo.MarkIndexDone(det.Ctx, article.ID); err != nil {
		logs.Error(err, "index: không đánh dấu done được", "trace_id", traceID, "article_id", article.ID, "name", article.Name)
		return
	}
	done.Add(1)
}

// releaseClaims trả 1 lô bài về pending khi job bị hủy do shutdown — 1 câu
// UPDATE, không N+1. Detached ctx vì ctx gốc đã cancel; WHERE ... processing
// trong repo bảo vệ bài đã kịp done.
func (s *articleIndexService) releaseClaims(ctx context.Context, traceID string, ids []int64) {
	if len(ids) == 0 {
		return
	}
	det := worker.Detach(ctx)
	defer det.Close()
	if err := s.articleRepo.ReleaseIndexClaims(det.Ctx, ids); err != nil {
		logs.Error(err, "index: không trả claim được, bài chờ hết lease",
			"trace_id", traceID, "articles", len(ids))
		return
	}
	logs.Infow("index: job bị hủy (shutdown), trả bài về pending",
		"trace_id", traceID, "articles", len(ids), "article_ids", ids)
}

// indexOneSafe bọc indexOne để panic trong goroutine con (không có
// gin.Recovery) không giết process: biến panic thành error để đường xử lý lỗi
// hiện có đánh dấu bài + backoff, thay vì kẹt processing đến hết lease.
func (s *articleIndexService) indexOneSafe(ctx context.Context, traceID string, article *domain.Article) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			logs.Error(fmt.Errorf("panic: %v", rec), "index: panic khi xử lý bài",
				"trace_id", traceID, "article_id", article.ID, "name", article.Name,
				"stack", string(debug.Stack()))
			err = fmt.Errorf("panic khi index bài %d: %v", article.ID, rec)
		}
	}()
	return s.indexOne(ctx, traceID, article)
}

// indexOne cắt + embed + upsert 1 bài, đọc nội dung theo CỬA SỔ TRƯỢT — chỉ
// điều phối stage, chi tiết nằm ở indexRun bên dưới.
//
// BA BƯỚC, khác hẳn mô hình cũ (cắt từng trang rời rồi ghi bảng trung gian):
//  1. Quét 1 lượt để chốt quyết định toàn file: bỏ dòng lặp header/footer,
//     khử chữ rải hay không (xem extractor.Cleaner). Lượt này CHỈ CỘNG SỐ,
//     không giữ text — nên file 5000 trang vẫn chỉ tốn vài chục KB.
//  2. Quét lượt 2 theo cửa sổ INDEX_PAGE_WINDOW trang: ghép thành văn bản
//     liền mạch rồi SplitBlocks TRONG CỬA SỔ. Nhờ vậy nhìn Điều/mục được
//     ở tầm vài trang thay vì bị cắt vụn ở từng trang như cách cũ.
//  3. Mỗi block cắt tại RANH GIỚI TRANG rồi mới split theo size — chunk
//     không bao giờ tràn 2 trang nên PageNum chính xác cho trích dẫn.
//
// Chunk đủ INDEX_BATCH_SIZE thì embed + upsert + ghi checkpoint rồi đi tiếp,
// nên bộ nhớ chỉ ôm 1 cửa sổ + 1 đợt, KHÔNG phụ thuộc kích thước file.
func (s *articleIndexService) indexOne(ctx context.Context, traceID string, article *domain.Article) error {
	r := newIndexRun(s, ctx, traceID, article)
	if err := r.prepare(); err != nil {
		return err
	}
	if err := eachPageWindow(ctx, s.pageRepo, article.ID, r.indexWindow); err != nil {
		return err
	}
	if r.stopped {
		// Bài bị xoá giữa chừng: dừng im lặng, không mark done.
		return nil
	}
	if err := r.flush(); err != nil {
		return err
	}
	return r.finish()
}

// eachPageWindow duyệt article_pages theo cửa sổ INDEX_PAGE_WINDOW trang cho tới
// khi HẾT TRANG — điều kiện dừng DUY NHẤT nằm đúng một chỗ ở đây.
//
// TUYỆT ĐỐI không suy ra "hết file" từ một lượt đọc ngắn hơn window: cửa sổ bị
// cắt ngắn vì chạm INDEX_WINDOW_MAX_CHARS vẫn trả về đủ trang, coi lượt ngắn là
// hết file là âm thầm bỏ sót nửa tài liệu (TestIndexOneReadsInWindows khoá đúng
// cái này). fn trả seq của trang kế tiếp CẦN đọc — trang chưa dùng hết trong
// lượt này phải được đọc lại ở lượt sau, không được nhảy qua.
func eachPageWindow(ctx context.Context, repo domain.IArticlePageRepository, articleID int64,
	fn func(pages []*domain.ArticlePage) (next int, err error)) error {
	afterSeq := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		pages, err := repo.ListPagesRange(ctx, articleID, afterSeq, constants.INDEX_PAGE_WINDOW)
		if err != nil {
			return fmt.Errorf("đọc trang: %w", err)
		}
		if len(pages) == 0 {
			return nil
		}
		next, err := fn(pages)
		if err != nil {
			return err
		}
		afterSeq = next
	}
}

// indexRun là trạng thái của MỘT bài đang index: cleaner + template đã chốt,
// checkpoint, đợt chunk đang gom và carry tiêu đề mục. Tách thành struct để
// indexOne chỉ còn điều phối stage — mỗi hàm dưới đây nằm gọn trong ngân sách
// hàm của repo (src/quality/function_budget_test.go).
type indexRun struct {
	svc     *articleIndexService
	ctx     context.Context
	traceID string
	article *domain.Article
	start   time.Time

	cleaner    *extractor.Cleaner
	tmpl       chunker.Chunker
	resumeFrom int
	total      int
	carry      string
	piecesSeen int
	stopped    bool
	batch      []*domain.VectorPoint
}

func newIndexRun(svc *articleIndexService, ctx context.Context, traceID string, article *domain.Article) *indexRun {
	return &indexRun{
		svc:        svc,
		ctx:        ctx,
		traceID:    traceID,
		article:    article,
		start:      time.Now(),
		resumeFrom: article.IndexDoneChunks,
		// total khởi từ checkpoint: chunk đầu tiên của lần này mang đúng số
		// thứ tự tiếp theo, không đánh lại từ 0.
		total: article.IndexDoneChunks,
		batch: make([]*domain.VectorPoint, 0, constants.INDEX_BATCH_SIZE),
	}
}

// prepare chốt các quyết định toàn file MỘT lần thay vì mỗi lát cắt: pass 1 dò
// header/footer + chữ rải (buildCleaner) và chọn template theo loại tài liệu.
// Template vốn stateless nên dựng lại từng piece chỉ tốn cấp phát vô ích.
func (r *indexRun) prepare() error {
	cleaner, err := r.svc.buildCleaner(r.ctx, r.article)
	if err != nil {
		return err
	}
	r.cleaner = cleaner
	r.tmpl = r.svc.resolveTemplate(r.article)
	return r.dropStaleVectors()
}

// dropStaleVectors dọn point cũ khi chạy lại từ đầu: đổi cấu hình chunker sinh
// NÊN chunk khác, không dọn thì Qdrant còn đuôi thừa. Delete theo filter là
// idempotent — bài chưa có point nào thì khớp 0 vẫn success.
func (r *indexRun) dropStaleVectors() error {
	if r.resumeFrom != 0 {
		return nil
	}
	// Đừng bắt đầu xóa toàn bộ vector của bài nếu job đã bị hủy — thao tác này
	// dùng detached ctx nên sẽ chạy tiếp bất chấp shutdown.
	if err := r.ctx.Err(); err != nil {
		return err
	}
	det := worker.Detach(r.ctx)
	defer det.Close()
	if err := r.svc.vectors.DeleteByArticle(det.Ctx, r.article.ID); err != nil {
		return fmt.Errorf("dọn vector cũ: %w", err)
	}
	return nil
}

// indexWindow nhận 1 lượt đọc trang, ghép thành cửa sổ rồi cắt chunk. Trả seq
// của trang kế tiếp cần đọc (xem eachPageWindow).
func (r *indexRun) indexWindow(pages []*domain.ArticlePage) (int, error) {
	win := newPageWindow()
	used := 0
	for _, p := range pages {
		win.add(p.PageNum, r.cleaner.Apply(p.Text))
		r.piecesSeen++
		used++
		// Cửa sổ vừa đủ rồi thì dừng nhét thêm: bảo đảm trần ký tự kể cả với
		// loại file 1-trang chứa cả file. Trang chưa nhét vào cửa sổ này vẫn
		// nằm trong lượt đọc đã lấy về nên next phải là trang CHƯA dùng.
		if win.chars() >= constants.INDEX_WINDOW_MAX_CHARS {
			break
		}
	}
	stop, err := r.consumeWindow(win)
	r.stopped = stop
	return pages[used-1].Seq + 1, err
}

// consumeWindow cắt cửa sổ thành block → lát theo trang → chunk. Trả stop =
// true khi bài đã bị xoá giữa chừng (dừng im lặng, không mark done).
//
// SplitBlocks chạy TRONG cửa sổ nên vẫn thấy tiêu đề Điều ở tầm vài trang;
// mỗi block cắt tại ranh giới trang trước khi split để chunk không tràn 2 trang.
func (r *indexRun) consumeWindow(win *pageWindow) (bool, error) {
	for _, b := range chunker.SplitBlocks(win.text()) {
		for _, pc := range win.cuts(b.Start, b.End) {
			if strings.TrimSpace(pc.text) == "" {
				continue
			}
			stop, err := r.appendPiece(pc, b)
			if err != nil || stop {
				return stop, err
			}
		}
	}
	return false, nil
}

// appendPiece split 1 lát rồi gán mục cho từng chunk. Lỗi splitter (tầng
// semantic gọi Ollama) là transient — trả về để caller backoff, không failed
// oan như lỗi dữ liệu hỏng.
func (r *indexRun) appendPiece(pc piece, b chunker.Block) (bool, error) {
	parts, err := r.tmpl.Split(r.ctx, pc.text)
	if err != nil {
		return false, fmt.Errorf("cắt chunk: %w", err)
	}
	// Gán mục cho từng chunk: block có tiêu đề thì lấy đó làm gốc, không thì
	// thừa hưởng carry.
	base := r.carry
	if b.Heading != "" {
		base = b.Heading
	}
	annotated, carryNext := chunker.Annotate(parts, base)
	r.carry = carryNext
	return r.addChunks(annotated, pc.page)
}

// addChunks đẩy chunk vào đợt; đủ INDEX_BATCH_SIZE thì flush rồi hỏi bài còn
// sống không. Check mỗi đợt vì 1 bài 500 trang chạy hàng chục phút — 1 SELECT
// rẻ hơn nhiều so với gọi Ollama thừa cho bài đã bị xoá.
func (r *indexRun) addChunks(annotated []chunker.SectionedChunk, page int) (bool, error) {
	for _, p := range annotated {
		// Chunk đã upsert ở lần chạy trước: bỏ qua, chỉ đếm để index nối tiếp.
		if r.total < r.resumeFrom {
			r.total++
			continue
		}
		// Prepend tiêu đề mục (chunker.Annotate đã gán lúc cắt) để vector +
		// Qdrant payload luôn mang ngữ cảnh Điều — agent attribute đúng mục,
		// full-text "ĐIỀU 5" cũng trúng chunk giữa Điều. Bản lưu trong payload
		// là bản dùng thật.
		r.batch = append(r.batch, &domain.VectorPoint{
			ID:         utils.NewPointID(r.article.ID, r.total),
			ArticleID:  r.article.ID,
			UserID:     r.article.UserID,
			CategoryID: r.article.CategoryID,
			ChunkIndex: r.total,
			PageNum:    page,
			Section:    p.Section,
			Text:       chunker.HeaderFor(p.Section) + p.Text,
		})
		r.total++
		if len(r.batch) < constants.INDEX_BATCH_SIZE {
			continue
		}
		if err := r.flush(); err != nil {
			return false, err
		}
		alive, err := r.svc.ensureAlive(r.ctx, r.traceID, r.article)
		if err != nil {
			return false, err
		}
		if !alive {
			return true, nil
		}
	}
	return false, nil
}

// flush embed + upsert đợt đang gom rồi ghi checkpoint. Ghi checkpoint SAU khi
// Qdrant báo thành công: thứ tự Qdrant-trước-DB-sau. Lỗi ở giữa thì kỳ sau làm
// lại đúng đợt đó, upsert cùng PointID nên không sinh point trùng.
//
// total KHÔNG cộng thêm ở đây: vòng sinh chunk đã tăng sẵn khi append, cộng
// thêm lần nữa là checkpoint vượt thật.
func (r *indexRun) flush() error {
	if len(r.batch) == 0 {
		return nil
	}
	if err := r.svc.upsertBatch(r.ctx, r.traceID, r.article, r.batch); err != nil {
		return err
	}
	r.batch = r.batch[:0]
	det := worker.Detach(r.ctx)
	defer det.Close()
	if err := r.svc.articleRepo.MarkIndexProgress(det.Ctx, r.article.ID, r.total); err != nil {
		return fmt.Errorf("ghi checkpoint: %w", err)
	}
	// Log mỗi đợt: file lớn chạy hàng chục phút, không có dòng nào thì lúc chết
	// không biết đang ở đâu.
	logs.Infow("index: tiến trình",
		"trace_id", r.traceID, "article_id", r.article.ID, "name", r.article.Name,
		"chunks", r.total, "elapsed_ms", time.Since(r.start).Milliseconds())
	return nil
}

// finish log tổng kết; không trang nào ra chữ là lỗi dữ liệu (ErrInvalid) chứ
// không phải "index xong 0 chunk".
func (r *indexRun) finish() error {
	if r.total == 0 {
		return fmt.Errorf("%w: không có trang nào ra chữ để chunk", extractor.ErrInvalid)
	}
	logs.Infow("index: xong 1 bài", "trace_id", r.traceID, "article_id", r.article.ID,
		"name", r.article.Name, "pieces", r.piecesSeen, "chunks", r.total,
		"elapsed_ms", time.Since(r.start).Milliseconds())
	return nil
}

// resolveTemplate chọn template theo loại tài liệu của bài (laws/table/...),
// bọc baseSplitter; không có template phù hợp thì dùng base (general). DocType
// trống thì đoán từ content-type + tên file. Stateless nên chỉ gọi 1 lần/bài.
func (s *articleIndexService) resolveTemplate(article *domain.Article) chunker.Chunker {
	kind := chunker.NormalizeType(article.DocType)
	if article.DocType == "" {
		kind = chunker.DetectType(article.ContentType, article.Name)
	}
	if tmpl := chunker.NewTemplate(kind, s.baseSplitter); tmpl != nil {
		return tmpl
	}
	return s.baseSplitter
}

// buildCleaner là pass 1 — quét mọi trang theo cửa sổ CHỈ ĐỂ CỘNG SỐ, không
// giữ text. Chính vì vậy RAM của pass này là hằng số dù file có bao nhiêu
// trang, và đây là chỗ duy nhất biết "cả file chữ rải hay sạch, có dòng lặp
// header/footer không".
func (s *articleIndexService) buildCleaner(ctx context.Context, article *domain.Article) (*extractor.Cleaner, error) {
	cleaner := extractor.NewCleaner(article.PageCount, extractor.RepeatOptions{})
	err := eachPageWindow(ctx, s.pageRepo, article.ID, func(pages []*domain.ArticlePage) (int, error) {
		for _, p := range pages {
			cleaner.Scan(p.Text)
		}
		return pages[len(pages)-1].Seq + 1, nil
	})
	if err != nil {
		return nil, err
	}
	return cleaner, nil
}

// upsertBatch gán vector cho 1 đợt chunk rồi upsert Qdrant.
//
// KHÔNG có cache vector: bảng embed_cache đã bỏ (hit rate ~100% với count=1
// nên thực tế không giúp gì), vector nằm trong Qdrant và rebuild dựa vào
// checkpoint index_done_chunks. Vì vậy 1 đợt = 1 call EmbedBatch + 1 call
// embed tên bài (dual embedding), không hỏi cache trước khi gọi model.
func (s *articleIndexService) upsertBatch(ctx context.Context, traceID string, article *domain.Article, batch []*domain.VectorPoint) error {
	texts := make([]string, 0, len(batch))
	for _, p := range batch {
		texts = append(texts, p.Text)
	}
	// Dual embedding: title vector = embed tên bài (1 lần/đợt, tái dùng cho
	// mọi chunk trong đợt). Lỗi embed title không fail job — degrade về
	// content-only (title vector = content vector).
	titleVec, terr := s.embedder.Embed(ctx, article.Name)
	if terr != nil {
		logs.Error(terr, "index: embed title lỗi, dùng content vector thay thế",
			"article_id", article.ID, "name", article.Name)
		titleVec = nil
	}
	// Embed toàn bộ đợt trong 1 call (không cache vector — xem doc đầu hàm).
	// bge-m3 là model của riêng phase index; log gắn tiền tố `index:` để
	// không lẫn với lời gọi LLM của compile/chat.
	logs.Infow("index: gọi embed", "trace_id", traceID, "article_id", article.ID, "name", article.Name,
		"texts", len(texts), "model", s.embedder.ModelName())
	embedStart := time.Now()
	vectors, err := s.embedder.EmbedBatch(ctx, texts)
	if err != nil {
		// retry transient
		if rerr := worker.WithRetry(ctx, constants.INDEX_CALL_ATTEMPTS, constants.INDEX_CALL_BASE_DELAY,
			func(ctx context.Context) error {
				var e error
				vectors, e = s.embedder.EmbedBatch(ctx, texts)
				return e
			}); rerr != nil {
			return fmt.Errorf("gọi embed: %w", rerr)
		}
	}
	logs.Infow("index: embed xong", "trace_id", traceID, "article_id", article.ID, "name", article.Name,
		"texts", len(texts), "elapsed_ms", time.Since(embedStart).Milliseconds())
	if len(vectors) != len(texts) {
		return fmt.Errorf("%w: embed trả %d vector cho %d text", errIndexContract, len(vectors), len(texts))
	}
	for i, p := range batch {
		if len(vectors[i]) != s.embedder.Dimension() {
			return fmt.Errorf("%w: vector chunk %d sai dim", errIndexContract, i)
		}
		p.Vector = vectors[i]
		if titleVec != nil {
			p.TitleVector = titleVec
		} else {
			p.TitleVector = vectors[i]
		}
		// Content enhancement: keywords best-effort, không fail job.
		if kw, err := s.enhancer.Keywords(ctx, p.Text); err == nil && len(kw) > 0 {
			p.Keywords = kw
		}
	}
	if err := worker.WithRetry(ctx, constants.INDEX_CALL_ATTEMPTS, constants.INDEX_CALL_BASE_DELAY,
		func(ctx context.Context) error {
			return s.vectors.UpsertPoints(ctx, batch)
		}); err != nil {
		return fmt.Errorf("upsert qdrant: %w", err)
	}
	return nil
}

// ensureAlive kiểm tra bài còn sống — bài chết thì dọn vector vừa làm rồi trả
// (false, nil) để caller dừng mà không mark done. Lỗi DB (kể cả ctx hủy giữa
// chừng) trả về error để indexClaimed xử lý đúng: cancellation → release
// claim, lỗi thật → MarkIndexError. Trước đây hàm trả bool nên lỗi IsAlive
// (nhất là ctx.Canceled lúc shutdown) bị biến thành "false" rồi indexOne trả
// nil → bài bị mark done dù chưa index xong.
func (s *articleIndexService) ensureAlive(ctx context.Context, traceID string, article *domain.Article) (bool, error) {
	alive, err := s.articleRepo.IsAlive(ctx, article.ID)
	if err != nil {
		return false, fmt.Errorf("kiểm tra bài còn sống: %w", err)
	}
	if !alive {
		det := worker.Detach(ctx)
		defer det.Close()
		logs.Infow("index: bài bị xoá giữa chừng, đã dọn vector vừa làm",
			"trace_id", traceID, "article_id", article.ID)
		if derr := s.vectors.DeleteByArticle(det.Ctx, article.ID); derr != nil {
			logs.Error(derr, "index: không dọn được vector bài đã xoá", "trace_id", traceID, "article_id", article.ID)
		}
		return false, nil
	}
	return true, nil
}

// errIndexContract báo embedder trả thiếu vector (sai contract do service tự
// phát hiện) — retry vô ích nên failed luôn, cùng nhóm với lỗi vĩnh viễn của
// embedder/vector store (hỏi qua domain interface, không import infra).
var errIndexContract = stderrors.New("embed trả thiếu vector")

// ---- CỬA SỔ TRƯỢT ----
//
// pageWindow là vài trang liền nhau đã nối thành văn bản, kèm byte offset của
// từng trang để map offset của block (chunker.SplitBlocks trả về) về đúng số
// trang. Toàn bộ cấu trúc này chỉ dài bằng cửa sổ, KHÔNG bằng cả file — đó
// là điều giữ RAM của phase index không phụ thuộc kích thước tài liệu.
type pageWindow struct {
	sb     strings.Builder
	starts []int // offset đầu mừng trang trong text
	nums   []int // số trang tương ứng
	nChars int
}

// windowSep nối 2 trang trong cửa sổ — 2 dòng trống đúng như PDF thật, để
// findHeadings nhìn đầu dòng sau đó không dính chữ trang trước.
const windowSep = "\n\n"

func newPageWindow() *pageWindow {
	return &pageWindow{}
}

// add nối 1 trang vào cửa sổ và ghi lại offset đầu trang đó.
func (w *pageWindow) add(num int, text string) {
	if len(w.nums) > 0 {
		w.sb.WriteString(windowSep)
		w.nChars += len(windowSep)
	}
	w.starts = append(w.starts, w.sb.Len())
	w.nums = append(w.nums, num)
	w.sb.WriteString(text)
	w.nChars += len(text)
}

func (w *pageWindow) text() string { return w.sb.String() }
func (w *pageWindow) chars() int   { return w.nChars }

// piece là lát cắt của 1 block thuộc trang page.
type piece struct {
	page int
	text string
}

// cuts tách đoạn [lo, hi) của cửa sổ tại ranh giới trang — thay cho ranh giới
// trang của thiết kế cũ (cắt từng trang rời rồi ghép): block Điều trải qua 3
// trang vẫn là MỘT block nên không bị cắt vụn, nhưng mỗi chunk sinh ra vẫn
// nằm gọn trong 1 trang để PageNum chính xác cho trích dẫn.
func (w *pageWindow) cuts(lo, hi int) []piece {
	lo, hi = max(lo, 0), min(hi, w.sb.Len())
	if lo >= hi {
		return nil
	}
	// String() sao chép cả builder — gọi MỘT lần rồi cắt trên string, không
	// gọi trong vòng lặp (mỗi lần là một bản sao cỡ cửa sổ).
	full := w.text()
	var out []piece
	for i, start := range w.starts {
		end := w.sb.Len()
		if i+1 < len(w.starts) {
			end = w.starts[i+1] - len(windowSep)
		}
		if end <= lo {
			continue
		}
		if start >= hi {
			break
		}
		s, e := max(lo, start), min(hi, end)
		if e > s {
			out = append(out, piece{page: w.nums[i], text: full[s:e]})
		}
	}
	return out
}
