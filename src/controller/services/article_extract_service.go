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
	"knowledge_ingestion/src/common/extractor"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/storage"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/controller/services/internal/worker"
	"knowledge_ingestion/src/domain"
)

// IArticleExtractService worker phase 0: tải blob, trích text (OCR nếu là PDF
// scan) rồi ghi vào article_pages — chỉ worker gọi, không qua HTTP nên trả
// (int, error) thuần thay vì dtos.Result. Trả số bài đã trích xong.
//
// VÌ SAO TÁCH RIÊNG thay vì gộp vào phase index:
//  1. OCR là phần đắt nhất (~17 phút với file scan 500 trang) và không phụ
//     thuộc cấu hình chunker. Tách ra thì đổi chunker/size/overlap chỉ chạy
//     lại phase index — không tốn OCR cho cả kho.
//  2. Phase index chỉ còn là CPU + gọi Ollama: không ôm blob gốc, không
//     render ảnh, không giữ cả văn bản trong RAM (đọc cửa sổ trượt).
//  3. Trang đã trích nằm trong DB nên rebuild vector (đổi model embed) không
//     tốn OCR lần nào.
type IArticleExtractService interface {
	ExtractPending(ctx context.Context) (int, error)
}

type articleExtractService struct {
	articleRepo domain.ArticleExtractQueue
	pageRepo    domain.IArticlePageRepository
	storage     storage.IStorage
	// ocr cấu hình Tesseract + renderer — build từ config ở loader. Struct
	// value (không phải interface) vì chỉ là tham số chạy, không cần mock ở
	// service (test truyền OCRConfig{} là tắt).
	ocr extractor.OCRConfig
	// opts là các tuỳ chọn bổ sung của lượt trích (hiện: đo bố cục phức tạp),
	// cũng build từ config ở loader. Giữ dạng slice để thêm tuỳ chọn mới mà
	// không phải sửa chữ ký constructor lần nữa.
	opts extractor.ExtractOptions
}

func NewArticleExtractService(
	articleRepo domain.ArticleExtractQueue,
	pageRepo domain.IArticlePageRepository,
	storage storage.IStorage,
	ocr extractor.OCRConfig,
	opts extractor.ExtractOptions) IArticleExtractService {
	return &articleExtractService{
		articleRepo: articleRepo,
		pageRepo:    pageRepo,
		storage:     storage,
		ocr:         ocr,
		opts:        opts,
	}
}

// ExtractPending claim 1 đợt bài rồi trích song song trong pool giới hạn, trả
// số bài xong. Bài nào lỗi cũng chỉ log + MarkExtractError (backoff, quá số
// lần thì failed), không chặn bài khác. Claim bằng SKIP LOCKED nên N worker
// cùng chạy không giẫm nhau — cùng quy ước trace_id với ChunkPending.
func (s *articleExtractService) ExtractPending(ctx context.Context) (int, error) {
	traceID, err := worker.EnsureTraceID(ctx)
	if err != nil {
		return 0, err
	}
	logs.Infow("extract: bắt đầu kỳ quét", "trace_id", traceID)
	articles, err := s.articleRepo.ClaimExtractPending(ctx, constants.EXTRACT_CLAIM_LIMIT, constants.EXTRACT_LEASE)
	if err != nil {
		return 0, err
	}
	var extracted atomic.Int64
	var wg sync.WaitGroup
	sem := make(chan struct{}, constants.EXTRACT_POOL_SIZE)
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
			s.extractClaimed(ctx, traceID, a, &extracted)
		}(article)
	}
	wg.Wait()
	if ctx.Err() != nil && len(pending) > 0 {
		// maps.Keys + slices.Collect thay cho vòng for thủ công; DB chỉ nhận
		// ĐÚNG 1 UPDATE ... WHERE id IN (...) — không N+1.
		ids := slices.Collect(maps.Keys(pending))
		s.releaseClaims(ctx, traceID, ids)
	}
	n := int(extracted.Load())
	logs.Infow("extract: xong 1 kỳ quét", "trace_id", traceID, "found", len(articles), "extracted", n)
	return n, nil
}

// extractClaimed xử lý 1 bài đã claim: lỗi thì MarkExtractError, xong thì
// MarkExtractDone (ghi text + done trong 1 câu). Ghi bằng detached ctx để được
// dù job hết timeout; job hết timeout (ctx hủy) thì bỏ qua không đánh lỗi oan
// (đánh lỗi oan sẽ đốt hết attempts rồi failed bài đang chỉ cần thử lại).
func (s *articleExtractService) extractClaimed(ctx context.Context, traceID string, article *domain.Article, done *atomic.Int64) {
	// KHÔNG tạo dbCtx dùng chung ở đây: OCR 1 file scan 500 trang tốn hàng
	// chục phút, ctx 30s tạo sớm chết trước khi tới lượt ghi done/error.
	if err := s.extractOneSafe(ctx, traceID, article); err != nil {
		if ctx.Err() != nil {
			// Phân biệt 2 nguyên nhân hủy:
			//   - Canceled: runner dừng (shutdown) → trả bài về pending NGAY,
			//     không tăng attempts; restart là làm tiếp.
			//   - DeadlineExceeded: job vượt trần 45 phút → để lease hết hạn
			//     rồi reclaim, tránh vòng lặp timeout liên tục không backoff.
			if stderrors.Is(ctx.Err(), context.Canceled) {
				s.releaseClaims(ctx, traceID, []int64{article.ID})
			} else {
				logs.Infow("extract: job hết giờ, để lease trả bài",
					"trace_id", traceID, "article_id", article.ID)
			}
			return
		}
		attempts := article.ExtractAttempts + 1
		if extractor.IsPermanent(err) {
			// Blob immutable nên lỗi xác định (loại chưa hỗ trợ, file hỏng,
			// ảnh toàn trắng) thử lại cũng vậy — failed luôn thay vì đốt 10
			// lần retry.
			attempts = utils.MaxQueueAttempts
		}
		logs.Warnw("extract: bỏ qua bài, thử lại theo backoff",
			"trace_id", traceID,
			"article_id", article.ID, "storage_key", article.StorageKey, "error", err)
		det := worker.Detach(ctx)
		defer det.Close()
		if merr := s.articleRepo.MarkExtractError(det.Ctx, article.ID, attempts); merr != nil {
			logs.Error(merr, "extract: không đánh dấu lỗi được", "trace_id", traceID, "article_id", article.ID)
		}
		return
	}
	det := worker.Detach(ctx)
	defer det.Close()
	if err := s.articleRepo.MarkExtractDone(det.Ctx, article.ID); err != nil {
		logs.Error(err, "extract: không đánh dấu done được", "trace_id", traceID, "article_id", article.ID)
		return
	}
	done.Add(1)
}

// extractOne trích text 1 bài rồi lưu từng trang vào bảng article_pages.
//
// TIẾN TRÌNH DẦN: OCR file scan 500 trang mất ~17 phút. Mỗi
// EXTRACT_FLUSH_PAGES trang ghi 1 lô vào bảng article_pages, nên job chết
// giữa chừng chỉ mất tối đa 1 lô thay vì mất trắng cả file — kỳ sau đọc lại
// số trang đã có rồi bảo extractor nhảy qua (fromPage) nên không OCR lại.
//
// Hàm này CỐ TÌNH KHÔNG làm sạch văn bản: bỏ dòng lặp header/footer và khử
// chữ rải đều phải nhìn CẢ file mà lúc trích thì chưa đủ trang, nên giao cho
// phase index chạy theo cửa sổ trượt (extractor.Cleaner — pass 1 chỉ cộng số,
// pass 2 áp từng trang). Bù lại article_pages giữ TEXT THÔ và được GIỮ LẠI
// sau khi trích xong, nên đổi cấu hình chunker không phải OCR lần nào.
// releaseClaims trả 1 lô bài về pending khi job bị hủy do shutdown — 1 câu
// UPDATE, không N+1. Dùng detached ctx vì ctx gốc đã cancel; WHERE ...
// processing trong repo bảo vệ trường hợp job kịp done trước khi hủy.
func (s *articleExtractService) releaseClaims(ctx context.Context, traceID string, ids []int64) {
	if len(ids) == 0 {
		return
	}
	det := worker.Detach(ctx)
	defer det.Close()
	if err := s.articleRepo.ReleaseExtractClaims(det.Ctx, ids); err != nil {
		logs.Error(err, "extract: không trả claim được, bài chờ hết lease",
			"trace_id", traceID, "articles", len(ids))
		return
	}
	logs.Infow("extract: job bị hủy (shutdown), trả bài về pending",
		"trace_id", traceID, "articles", len(ids), "article_ids", ids)
}

// extractOneSafe bọc extractOne để panic trong goroutine con (không có
// gin.Recovery) không giết process: biến panic thành error để đường xử lý lỗi
// hiện có đánh dấu bài + backoff, thay vì kẹt processing đến hết lease.
func (s *articleExtractService) extractOneSafe(ctx context.Context, traceID string, article *domain.Article) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			logs.Error(fmt.Errorf("panic: %v", rec), "extract: panic khi xử lý bài",
				"trace_id", traceID, "article_id", article.ID, "stack", string(debug.Stack()))
			err = fmt.Errorf("panic khi extract bài %d: %v", article.ID, rec)
		}
	}()
	return s.extractOne(ctx, traceID, article)
}

func (s *articleExtractService) extractOne(ctx context.Context, traceID string, article *domain.Article) error {
	start := time.Now()
	// Trang đã lưu ở lần chạy trước — dùng lại thay vì OCR lại.
	saved, err := s.pageRepo.ListByArticle(ctx, article.ID)
	if err != nil {
		return fmt.Errorf("đọc trang đã trích: %w", err)
	}
	// seen theo SEQ, không theo page_num: file txt bị chia nhiều mảnh cùng
	// page_num = 0 nên page_num không còn định danh được từng mảnh.
	// seen là tập seq đã có: extractor bỏ qua chúng (PDF trích phần còn thiếu,
	// song song theo khoảng nên không còn giả định trang liên tục từ đầu).
	seen := make(map[int]bool, len(saved))
	for _, p := range saved {
		seen[p.Seq] = true
	}
	if len(saved) > 0 {
		logs.Infow("extract: tiếp tục từ mảnh đã trích",
			"trace_id", traceID, "article_id", article.ID,
			"pieces_done", len(saved))
	}

	rc, err := s.storage.Download(ctx, article.StorageKey)
	if err != nil {
		return fmt.Errorf("tải blob: %w", err)
	}
	// Lưới an toàn: nếu panic giữa chừng, recover ở extractOneSafe vẫn không
	// để rò connection S3. Close sớm bên dưới sẽ set rc = nil để defer bỏ qua.
	defer func() {
		if rc != nil {
			_ = rc.Close()
		}
	}()
	// fresh là các trang mới của lần này; sinceFlush đánh dấu đuôi chưa ghi để
	// flush chỉ đúng phần mới — nếu gộp cả danh sách thì mỗi lô lại insert
	// lại các trang đã có (ON CONFLICT giữ nguyên nhưng tốn I/O vô ích).
	fresh := make([]*domain.ArticlePage, 0, constants.EXTRACT_FLUSH_PAGES)
	sinceFlush := 0
	flushTail := func() error {
		if sinceFlush == 0 {
			return nil
		}
		tail := fresh[len(fresh)-sinceFlush:]
		if err := s.pageRepo.CreateBatch(ctx, tail); err != nil {
			return fmt.Errorf("lưu trang: %w", err)
		}
		// Log mỗi lô: job nặng chạy vài chục phút, không có dòng nào thì lúc
		// chết không biết đang ở trang nào.
		logs.Infow("extract: tiến trình",
			"trace_id", traceID, "article_id", article.ID,
			"pages_written", sinceFlush, "elapsed_ms", time.Since(start).Milliseconds())
		sinceFlush = 0
		return nil
	}
	visit := func(pg extractor.Page) error {
		// Chặn trùng theo SEQ — đây là khoá tự nhiên của bảng, nên ON CONFLICT
		// (article_id, seq) cũng chống trùng lần nữa. Vẫn cần chặn ở đây vì
		// nếu không thì mỗi lần resume phải insert lại toàn bộ phần đã lưu.
		if seen[pg.Seq] {
			return nil
		}
		seen[pg.Seq] = true
		fresh = append(fresh, &domain.ArticlePage{
			ArticleID: article.ID,
			Seq:       pg.Seq,
			PageNum:   pg.Num,
			Text:      stripNUL(pg.Text),
			// Layout = 0 khi không bật đo — cột NOT NULL default 0 nên
			// ghi thẳng, không cần phân biệt "chưa đo" ở tầng DB.
			LayoutScore: pg.Layout,
		})
		sinceFlush++
		if sinceFlush >= constants.EXTRACT_FLUSH_PAGES {
			return flushTail()
		}
		return nil
	}
	err = extractor.ExtractRawPages(ctx, article.StorageKey, rc, s.ocr, seen, visit, s.opts...)
	// Close NGAY sau khi trích xong chứ không cuối hàm: OCR 1 trang scan có thể
	// chạy hàng chục giây, giữ rc mở suốt là giữ connection S3 bị giữ vô ích.
	_ = rc.Close()
	rc = nil
	if err != nil {
		if stderrors.Is(err, extractor.ErrUnsupported) {
			return fmt.Errorf("loại file chưa xử lý: %w", err)
		}
		return fmt.Errorf("trích text: %w", err)
	}
	if err := flushTail(); err != nil {
		return err
	}

	if !anyPageHasText(fresh) && len(saved) == 0 {
		// File scan mờ / ảnh trắng: đánh done với text rỗng là done giả (search
		// sẽ trống mà không ai biết vì sao). Trả lỗi để lùi giờ thử thay vì
		// mất bài vĩnh viễn.
		return fmt.Errorf("%w: không có trang nào ra chữ", extractor.ErrInvalid)
	}
	pageCount := len(saved) + len(fresh)

	// Metadata bài trích được ở đây: số trang (đếm thật) + phiên bản/hiệu lực.
	// Phiên bản và hiệu lực luôn nằm ở đầu văn bản — lấy từ `fresh` trước,
	// thiếu thì lấy từ phần lưu ở lần chạy trước. headText dừng ở
	// MaxMetaScanRunes nên không cần gom cả file.
	version, effective := chunker.ExtractMeta(headText(fresh, chunker.MaxMetaScanRunes))
	if version == "" && effective == "" {
		if h := headText(saved, chunker.MaxMetaScanRunes); h != "" {
			version, effective = chunker.ExtractMeta(h)
		}
	}

	// User có thể xoá bài trong lúc đang trích — bài chết rồi thì dọn trang
	// vừa trích ngay (purge chỉ thấy bài xoá mềm, không thấy row mồ côi của
	// bài đã xoá hẳn), đánh done sau đó cũng chỉ là no-op trên row đã mất.
	alive, err := s.articleRepo.IsAlive(ctx, article.ID)
	if err != nil {
		return fmt.Errorf("kiểm tra bài còn sống: %w", err)
	}
	if !alive {
		logs.Infow("extract: bài bị xoá giữa chừng, đã dọn trang vừa trích",
			"trace_id", traceID, "article_id", article.ID)
		det := worker.Detach(ctx)
		defer det.Close()
		if derr := s.pageRepo.DeleteByArticleID(det.Ctx, article.ID); derr != nil {
			return fmt.Errorf("dọn trang bài đã xoá: %w", derr)
		}
		return nil
	}

	// KHÔNG xoá trang: chúng là nguồn sự thật cho phase index đọc cửa sổ
	// trượt, và giữ lại còn giúp đổi config chunker không phải OCR lại.
	det := worker.Detach(ctx)
	defer det.Close()
	if err := s.articleRepo.UpdateMetadata(det.Ctx, article.ID, pageCount, version, effective); err != nil {
		logs.Warnw("extract: không lưu metadata được (bỏ qua)",
			"trace_id", traceID, "article_id", article.ID, "error", err)
	}
	logs.Infow("extract: xong 1 bài",
		"trace_id", traceID, "article_id", article.ID,
		"pages", pageCount, "new_pages", len(fresh),
		"elapsed_ms", time.Since(start).Milliseconds())
	return nil
}

// headText ghép n ký tự đầu của danh sách trang — dùng cho metadata (phiên bản,
// hiệu lực luôn nằm ở đầu văn bản). Dừng CỐT theo từng trang, không gom cả file.
func headText(pages []*domain.ArticlePage, n int) string {
	if n <= 0 {
		return ""
	}
	var b strings.Builder
	for _, p := range pages {
		if b.Len() >= n {
			break
		}
		b.WriteString(p.Text)
		b.WriteString("\n")
	}
	s := b.String()
	rs := []rune(s)
	if len(rs) > n {
		s = string(rs[:n])
	}
	return s
}

// anyPageHasText báo có trang nào ra chữ không.
func anyPageHasText(pages []*domain.ArticlePage) bool {
	for _, p := range pages {
		if strings.TrimSpace(p.Text) != "" {
			return true
		}
	}
	return false
}

// stripNUL bỏ byte \x00. Postgres từ chối text chứa NUL (lỗi
// "invalid byte sequence ... 0x00"), mà gopdf đôi khi trả NUL trong text
// trang PDF thật — đo được ở trang 1443, 1444, 2740, 2788 của file 3629 trang.
// Không bỏ thì cả lô 20 trang bị rollback và job retry mãi ở cùng chỗ.
func stripNUL(s string) string {
	if !strings.ContainsRune(s, 0) {
		return s
	}
	return strings.ReplaceAll(s, "\x00", "")
}
