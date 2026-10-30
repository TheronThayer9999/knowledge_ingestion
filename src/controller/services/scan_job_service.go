package services

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"image"
	_ "image/jpeg" // để đo khổ ảnh JPG bằng image.DecodeConfig
	_ "image/png"  // và ảnh PNG
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/scanocr"
	"knowledge_ingestion/src/common/storage"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/middlewares"
	"knowledge_ingestion/src/domain"
)

// IScanJobService là nghiệp vụ hàng đợi quét của tab Quét (B.4).
//
// BA NGUYÊN TẮC CỦA TẦNG NÀY:
//  1. Không OCR ở đây. Request HTTP chỉ ghi DB + blob rồi trả ngay; OCR nằm ở
//     worker (bài học autoai: OCR trong request làm mọi request tuần tự hoá).
//  2. Không tin client. user_id lấy từ ctx, mọi câu đọc/ghi đều scope theo chủ.
//  3. Không giữ bản scan trên đĩa lâu hơn một request. File tạm chỉ sống để đo
//     số trang, xoá bằng defer — tài liệu là dữ liệu nhạy cảm.
type IScanJobService interface {
	// Create nhận tệp, lưu blob tạm, đo text layer TỪNG trang rồi dựng job.
	Create(ctx context.Context, filename string, r io.Reader, in dtos.CreateScanJobRequest) dtos.Result[*dtos.ScanJobResponse]
	Get(ctx context.Context, jobID int64) dtos.Result[*dtos.ScanJobResponse]
	List(ctx context.Context, status string, limit, offset int) dtos.Result[*dtos.ScanJobListResponse]
	Cancel(ctx context.Context, jobID int64) dtos.Result[*dtos.ScanJobActionResponse]
	Delete(ctx context.Context, jobID int64) dtos.Result[*dtos.ScanJobActionResponse]
	// RetryPage đưa MỘT trang về hàng đợi (nút "Thử lại" của tab Tài liệu).
	RetryPage(ctx context.Context, jobID int64, pageNum int) dtos.Result[*dtos.ScanJobActionResponse]
	// GetPage trả một trang cho tab Xem & sửa: từ + box + confidence, kèm ảnh
	// render presigned (render lazily rồi cache — gặp lần đầu mới tốn pdftoppm).
	GetPage(ctx context.Context, jobID int64, pageNum int) dtos.Result[*dtos.ScanJobPageResponse]
	DownloadURL(ctx context.Context, jobID int64) dtos.Result[*dtos.ScanJobDownloadResponse]
}

// ScanJobOptions là phần núm vận hành của tính năng, map từ config.GetScanOCR()
// trong src/loader/scan.go một lần lúc boot — service không import package
// config (cùng khuôn với PurgeOptions).
type ScanJobOptions struct {
	Enabled bool
	// Mặc định khi client không gửi lang/dpi/psm/engine trong form.
	Lang   string
	DPI    int
	PSM    int
	Engine string
	// MaxUploadBytes/MaxPages là trần của MỘT job (B.8). Job async nên trần không
	// còn là thời gian request mà là dung lượng hàng đợi một tệp có thể chiếm.
	MaxUploadBytes int64
	MaxPages       int
	// TempTTL cho job thường, PushedTTL cho job đã đẩy vào kho (B.11).
	TempTTL   time.Duration
	PushedTTL time.Duration
	// PresignTTL hạn sống URL ảnh/PDF ký cho FE (5 phút theo convention repo).
	PresignTTL time.Duration
}

type scanJobService struct {
	jobs        domain.IOCRJobRepository
	store       storage.IStorage
	ocr         scanocr.Config
	opt         ScanJobOptions
	currentUser middlewares.ICurrentUser
	// renderSem chặn số lượt render ảnh trang chạy đồng thời trong process API.
	// Mỗi lượt là một tiến trình pdftoppm; không chặn thì N người mở tab Xem &
	// sửa cùng lúc = N tiến trình, RAM tỉ lệ với số request chứ không theo cấu
	// hình. Kênh có sức chứa cố định nên phần dư xếp hàng, không sinh thêm.
	renderSem chan struct{}
}

func NewScanJobService(
	jobs domain.IOCRJobRepository,
	store storage.IStorage,
	ocr scanocr.Config,
	opt ScanJobOptions,
	currentUser middlewares.ICurrentUser,
) IScanJobService {
	return &scanJobService{
		jobs: jobs, store: store, ocr: ocr.Normalize(), opt: opt, currentUser: currentUser,
		renderSem: make(chan struct{}, constants.SCAN_RENDER_CONCURRENCY),
	}
}

// scanExts là định dạng tab Quét nhận: ảnh chụp và PDF (kể cả PDF tạo từ ảnh).
// Trước đây nằm ở service OCR đồng bộ; đường đó đã bỏ nên đây là chỗ duy nhất.
//
// WEBP cố ý KHÔNG nhận: kết quả của tab là một tệp PDF, mà fpdf chỉ nhúng được
// JPEG/PNG — nhận webp thì hoặc phải kéo thêm thư viện giải mã, hoặc trả về ảnh
// webp mang tên .pdf. Báo ngay lúc chọn tệp còn hơn để job chạy xong mới lộ.
var scanExts = map[string]bool{
	".pdf":  true,
	".jpg":  true,
	".jpeg": true,
	".png":  true,
}

// scanMIME cho phần đuôi tab Quét nhận. Không dùng mime.TypeByExtension vì ánh
// xạ .pdf phụ thuộc /etc/mime.types (trống trên container tối giản) mà
// Content-Type rỗng thì S3 suy ra octet-stream, FE tải về không mở đúng app.
var scanMIME = map[string]string{
	".pdf":  "application/pdf",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
}

// errScanTooLarge/errScanTooManyPages: hai trần người dùng chạm thật (file 1GB,
// PDF 3000 trang). Sentinel để hàm tạo job ngắn lại — thông điệp tiếng Việt soạn
// một chỗ ở scanCreateError.
var (
	errScanTooLarge     = stderrors.New("scan: tệp vượt trần dung lượng")
	errScanTooManyPages = stderrors.New("scan: tệp vượt trần số trang")
	errScanEmptyFile    = stderrors.New("scan: tệp rỗng")
)

// pageGuess là kết quả dò MỘT trang lúc tạo job. Không phải domain model vì nó
// chỉ sống trong một request: từ đây sinh row OCRJobPage rồi bỏ.
type pageGuess struct {
	rotation int
	chars    int
	// needsOCR sai chỉ khi trang đã có đủ chữ thật và người dùng không ép OCR.
	needsOCR bool
	// widthPx/heightPx chỉ có với ảnh upload trực tiếp (không có PDF để render).
	widthPx  int
	heightPx int
}

// --- tạo job -----------------------------------------------------------------

func (s *scanJobService) Create(ctx context.Context, filename string, r io.Reader, in dtos.CreateScanJobRequest) dtos.Result[*dtos.ScanJobResponse] {
	userID, err := s.owner(ctx)
	if err != nil {
		return dtos.Fail[*dtos.ScanJobResponse](err)
	}
	name := attachmentName(filename)
	ext := utils.FileExt(name)
	if !s.opt.Enabled {
		return dtos.Fail[*dtos.ScanJobResponse](s.clientErr(http.StatusBadRequest,
			"tính năng Quét chưa bật trên máy chủ (scan_ocr.enabled)"))
	}
	if !scanExts[ext] {
		return dtos.Fail[*dtos.ScanJobResponse](s.clientErr(http.StatusBadRequest,
			"chỉ nhận ảnh JPG/PNG hoặc PDF"))
	}
	// Hỏi engine TRƯỚC khi nhận tệp: thiếu binary/thiếu traineddata thì MỌI trang
	// sẽ fail sau 3 lượt thử (bài học đã ghi ở loader/worker.go), người dùng chỉ
	// thấy job đỏ. Báo ngay lúc bấm Upload là sớm nhất có thể.
	if err := s.engineGuard(ctx); err != nil {
		return dtos.Fail[*dtos.ScanJobResponse](err)
	}
	dir, path, size, err := s.stageSource(r, ext)
	if err != nil {
		return dtos.Fail[*dtos.ScanJobResponse](scanCreateError(ctx, name, err))
	}
	// Tài liệu nhạy cảm: bản tạm chỉ sống tới hết hàm này.
	defer func() { _ = os.RemoveAll(dir) }()

	guesses, err := s.detect(ctx, path, ext, in.ForceOCR)
	if err != nil {
		return dtos.Fail[*dtos.ScanJobResponse](scanCreateError(ctx, name, err))
	}
	token, err := utils.NewUUIDv7()
	if err != nil {
		return dtos.Fail[*dtos.ScanJobResponse](err)
	}
	key := scanSourceKey(token, ext)
	if err := s.putSource(ctx, path, key, size, ext); err != nil {
		// Kho chết thì nói KHO chết, không đổ tại tệp: người dùng thấy "file hỏng"
		// sẽ đi sửa file trong khi việc cần làm là bấm Thử lại.
		logs.Warnw("scan: ghi blob tạm lỗi", "trace_id", utils.TraceIDFromCtx(ctx),
			"user_id", userID, "ext", ext, "error", err)
		return dtos.Fail[*dtos.ScanJobResponse](errors.NewCustomHttpError(
			http.StatusServiceUnavailable, http.StatusServiceUnavailable,
			"kho lưu tạm đang không ghi được — vui lòng thử lại"))
	}
	job, pages := s.newRows(userID, name, key, in, guesses)
	if err := s.jobs.Create(ctx, job, pages); err != nil {
		// Blob đã ghi mà row không sinh ra thì không còn ai tham chiếu nó — xoá
		// ngay, janitor không biết đường nào mà dọn vì row không tồn tại.
		s.discardSource(ctx, key, err)
		return dtos.Fail[*dtos.ScanJobResponse](err)
	}
	logs.Infow("scan: tạo job",
		"trace_id", utils.TraceIDFromCtx(ctx),
		"job_id", job.ID, "user_id", userID,
		"pages", job.TotalPages, "ocr_pages", job.OCRTargetPages,
		"bytes", size, "ext", ext, "engine", job.Engine)
	return dtos.Ok(toScanJobResponse(job, ""))
}

// stageSource stream tệp lên đĩa tạm với trần dung lượng, trả (thư mục, đường
// dẫn, số byte). io.Copy + LimitReader chứ không ReadAll: PDF 200MB không lên RAM.
func (s *scanJobService) stageSource(r io.Reader, ext string) (string, string, int64, error) {
	dir, err := os.MkdirTemp("", "scan-job-*")
	if err != nil {
		return "", "", 0, fmt.Errorf("tạo thư mục tạm: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(dir)
		}
	}()
	path := filepath.Join(dir, "source"+ext)
	f, err := os.Create(path)
	if err != nil {
		return "", "", 0, fmt.Errorf("ghi file tạm: %w", err)
	}
	// +1 byte để phân biệt "đúng trần" với "vượt trần".
	size, err := io.Copy(f, io.LimitReader(r, s.opt.MaxUploadBytes+1))
	closeErr := f.Close()
	if err != nil {
		return "", "", 0, fmt.Errorf("đọc tệp upload: %w", err)
	}
	if closeErr != nil {
		return "", "", 0, fmt.Errorf("đóng file tạm: %w", closeErr)
	}
	if size > s.opt.MaxUploadBytes {
		return "", "", 0, errScanTooLarge
	}
	if size == 0 {
		return "", "", 0, errScanEmptyFile
	}
	cleanup = false // caller sở hữu dir và chịu trách nhiệm xoá
	return dir, path, size, nil
}

// detect đo text layer THEO TỪNG trang — điểm autoai làm sai (B.4): nó hỏi "cả
// file có trang nào mang chữ không" nên PDF 1 trang số + 199 trang scan bị bỏ
// qua toàn bộ.
func (s *scanJobService) detect(ctx context.Context, path, ext string, force bool) ([]pageGuess, error) {
	if ext != ".pdf" {
		// Ảnh đơn: không có text layer nên luôn OCR. Khổ ảnh đo tại chỗ để worker
		// khỏi phải mở lại blob; định dạng lạ thì bỏ trống (worker đọc từ bytes
		// thật, còn khổ trang PDF chỉ là bằng chứng hiển thị).
		w, h, err := measureImage(path)
		if err != nil {
			return nil, err
		}
		return []pageGuess{{needsOCR: true, widthPx: w, heightPx: h}}, nil
	}
	geom, err := scanocr.InspectPDF(ctx, s.ocr, path)
	if err != nil {
		return nil, err
	}
	if s.opt.MaxPages > 0 && len(geom) > s.opt.MaxPages {
		return nil, errScanTooManyPages
	}
	out := make([]pageGuess, 0, len(geom))
	for _, g := range geom {
		out = append(out, pageGuess{
			rotation: g.Rotation,
			chars:    g.TextChars,
			needsOCR: force || g.NeedsOCR(s.ocr.TextLayerMinChars),
		})
	}
	return out, nil
}

// putSource ghi tệp tạm vào bucket quét. Key theo UUID v7 CHỨ không theo id job:
// id chỉ có sau INSERT, mà blob phải nằm ở đúng key đã ghi trong row (một lần
// INSERT, không có cửa "row trỏ key chưa tồn tại").
func (s *scanJobService) putSource(ctx context.Context, path, key string, size int64, ext string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("mở file tạm để upload: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := s.store.Upload(ctx, key, f, size, scanMIME[ext]); err != nil {
		return fmt.Errorf("ghi blob quét: %w", err)
	}
	return nil
}

// newRows dựng row job + row trang từ kết quả dò. Trang không cần OCR sinh row
// với trạng thái skipped_has_text NGAY tại đây (chứ không phải worker bỏ qua),
// để thanh tiến độ có mẫu số đúng từ giây đầu tiên.
func (s *scanJobService) newRows(userID int64, name, key string, in dtos.CreateScanJobRequest, guesses []pageGuess) (*domain.OCRJob, []*domain.OCRJobPage) {
	now := time.Now()
	target := 0
	for _, g := range guesses {
		if g.needsOCR {
			target++
		}
	}
	job := &domain.OCRJob{
		UserID:         userID,
		SourceName:     name,
		SourceKey:      key,
		Status:         domain.QueuePending,
		TotalPages:     len(guesses),
		OCRTargetPages: target,
		Lang:           s.pickStr(in.Lang, s.opt.Lang),
		DPI:            s.pickInt(in.DPI, s.opt.DPI),
		PSM:            s.pickInt(in.PSM, s.opt.PSM),
		Engine:         s.pickStr(in.Engine, s.opt.Engine),
		ForceOCR:       in.ForceOCR,
		NextRetryAt:    now,
		ExpiresAt:      now.Add(s.opt.TempTTL),
	}
	pages := make([]*domain.OCRJobPage, 0, len(guesses))
	for i, g := range guesses {
		st := domain.OCRPagePending
		if !g.needsOCR {
			st = domain.OCRPageSkippedHasText
		}
		pages = append(pages, &domain.OCRJobPage{
			JobID:         job.ID, // repo.Create ghi id thật vào đây
			PageNum:       i + 1,
			Status:        st,
			Rotation:      g.rotation,
			WidthPx:       g.widthPx,
			HeightPx:      g.heightPx,
			HasTextLayer:  !g.needsOCR,
			CharsExisting: g.chars,
			Words:         domain.OCRWordsEmpty,
			NextRetryAt:   now,
		})
	}
	return job, pages
}

// engineGuard dựng engine theo cấu hình và hỏi nó "máy này chạy được chưa".
//
// Vì sao hỏi ở lúc tạo job chứ không để worker phát hiện: thiếu binary hoặc thiếu
// gói traineddata làm MỌI trang fail sau MaxAttempts lượt — người dùng ngồi chờ
// 10 phút để thấy job đỏ. Một cú kiểm tra (tesseract --list-langs, ~100ms) đổi
// lấy việc báo đúng nguyên nhân ngay khi bấm Upload. Result không cache: engine
// có thể được cài thêm mà không restart API.
func (s *scanJobService) engineGuard(ctx context.Context) error {
	engine, err := scanocr.NewEngine(s.ocr)
	if err != nil {
		logs.Warnw("scan: engine không hợp lệ", "trace_id", utils.TraceIDFromCtx(ctx), "error", err)
		return errors.NewCustomHttpError(http.StatusServiceUnavailable, http.StatusServiceUnavailable,
			"engine OCR chưa được cấu hình đúng trên máy chủ (scan_ocr.engine)")
	}
	if err := engine.Available(ctx); err != nil {
		logs.Warnw("scan: engine chưa chạy được", "trace_id", utils.TraceIDFromCtx(ctx), "error", err)
		return errors.NewCustomHttpError(http.StatusServiceUnavailable, http.StatusServiceUnavailable,
			"engine OCR chưa sẵn sàng trên máy chủ — thiếu phần mềm hoặc gói ngôn ngữ")
	}
	return nil
}

// --- đọc / thao tác ----------------------------------------------------------

func (s *scanJobService) Get(ctx context.Context, jobID int64) dtos.Result[*dtos.ScanJobResponse] {
	job, err := s.readOwned(ctx, jobID)
	if err != nil {
		return dtos.Fail[*dtos.ScanJobResponse](err)
	}
	// FE poll mỗi giây: ký URL thất bại thì VẪN trả tiến độ, chỉ thiếu link.
	// Báo lỗi ở đây sẽ biến sự cố ký URL thành "job hỏng" trong mắt người dùng;
	// nút Tải gọi DownloadURL và ở đó lỗi mới là lỗi thật.
	url, _ := s.signOutput(ctx, job, true)
	return dtos.Ok(toScanJobResponse(job, url))
}

func (s *scanJobService) List(ctx context.Context, status string, limit, offset int) dtos.Result[*dtos.ScanJobListResponse] {
	userID, err := s.owner(ctx)
	if err != nil {
		return dtos.Fail[*dtos.ScanJobListResponse](err)
	}
	limit, offset = utils.NormalizePagination(limit, offset)
	items, total, err := s.jobs.List(ctx, userID, status, limit, offset)
	if err != nil {
		return dtos.Fail[*dtos.ScanJobListResponse](err)
	}
	out := &dtos.ScanJobListResponse{Items: make([]dtos.ScanJobResponse, 0, len(items)), Total: total, Limit: limit, Offset: offset}
	for _, j := range items {
		// Ký URL là HMAC cục bộ (không I/O mạng) nên làm luôn cho tab Lịch sử:
		// bấm "Tải PDF" khỏi phải thêm một round trip.
		url, _ := s.signOutput(ctx, j, true)
		out.Items = append(out.Items, *toScanJobResponse(j, url))
	}
	return dtos.Ok(out)
}

func (s *scanJobService) Cancel(ctx context.Context, jobID int64) dtos.Result[*dtos.ScanJobActionResponse] {
	userID, err := s.owner(ctx)
	if err != nil {
		return dtos.Fail[*dtos.ScanJobActionResponse](err)
	}
	if err := s.jobs.Cancel(ctx, jobID, userID); err != nil {
		return dtos.Fail[*dtos.ScanJobActionResponse](s.mapNotFound(ctx, jobID, err,
			"không huỷ được: job không tồn tại hoặc đã hoàn tất"))
	}
	job, err := s.jobs.Get(ctx, jobID, userID)
	if err != nil {
		return dtos.Fail[*dtos.ScanJobActionResponse](err)
	}
	logs.Infow("scan: huỷ job", "trace_id", utils.TraceIDFromCtx(ctx),
		"job_id", jobID, "user_id", userID, "pages", job.TotalPages)
	return dtos.Ok(&dtos.ScanJobActionResponse{ID: jobID, Status: job.Status})
}

// Delete xoá row TRƯỚC rồi mới xoá object. Thứ tự này là có chủ ý: row là con
// đường duy nhất dẫn tới key, mất row trước thì object còn lại không ai biết
// (janitor quét theo row). Ngược lại, mất object trước mà crash giữa chừng thì
// row còn, janitor vẫn dọn được nốt.
func (s *scanJobService) Delete(ctx context.Context, jobID int64) dtos.Result[*dtos.ScanJobActionResponse] {
	// readOwned kiểm chủ trước: job của người khác phải trả 404 như không tồn tại.
	// userID lấy từ chính row đã đọc (đã được lọc theo chủ) chứ không gọi owner()
	// lần hai — một nguồn sự thật duy nhất cho quyền sở hữu ở luồng này.
	job, err := s.readOwned(ctx, jobID)
	if err != nil {
		return dtos.Fail[*dtos.ScanJobActionResponse](err)
	}
	keys := jobKeys(ctx, s.jobs, jobID)
	deleted, err := s.jobs.Delete(ctx, jobID, job.UserID)
	if err != nil {
		return dtos.Fail[*dtos.ScanJobActionResponse](s.mapNotFound(ctx, jobID, err,
			"job không tồn tại"))
	}
	// `deleted` là row repo trả về sau khi gỡ: key của nó là bản CHÍNH XÁC nhất
	// (worker có thể vừa ghi OutputKey giữa lúc đọc và lúc xoá).
	removed := removeObjects(ctx, s.store, append(keys, deleted.SourceKey, deleted.OutputKey))
	logs.Infow("scan: xoá job", "trace_id", utils.TraceIDFromCtx(ctx),
		"job_id", jobID, "user_id", job.UserID, "objects_removed", removed)
	return dtos.Ok(&dtos.ScanJobActionResponse{ID: jobID, Status: dtos.ScanJobDeleted, PagesAffected: removed})
}

// RetryPage đưa một trang về hàng đợi: nút "Thử lại" cạnh trang failed, và cũng
// là đường để OCR lại một trang người dùng thấy đọc kém.
func (s *scanJobService) RetryPage(ctx context.Context, jobID int64, pageNum int) dtos.Result[*dtos.ScanJobActionResponse] {
	job, err := s.readOwned(ctx, jobID)
	if err != nil {
		return dtos.Fail[*dtos.ScanJobActionResponse](err)
	}
	if pageNum < 1 || pageNum > job.TotalPages {
		return dtos.Fail[*dtos.ScanJobActionResponse](s.clientErr(http.StatusBadRequest,
			fmt.Sprintf("trang phải trong khoảng 1..%d", job.TotalPages)))
	}
	if err := s.jobs.ResetPage(ctx, jobID, pageNum); err != nil {
		return dtos.Fail[*dtos.ScanJobActionResponse](s.mapNotFound(ctx, jobID, err,
			"không thấy trang này, hoặc trang đang được xử lý"))
	}
	// Job cancelled không hồi sinh (quy ước của repo): trang về pending nhưng
	// worker chỉ claim job pending/processing — đọc lại job để UI hiện đúng.
	fresh, err := s.jobs.Get(ctx, jobID, job.UserID)
	if err != nil {
		return dtos.Fail[*dtos.ScanJobActionResponse](err)
	}
	logs.Infow("scan: hẹn lại trang", "trace_id", utils.TraceIDFromCtx(ctx),
		"job_id", jobID, "page", pageNum, "user_id", job.UserID)
	return dtos.Ok(&dtos.ScanJobActionResponse{ID: jobID, Status: fresh.Status, PagesAffected: 1})
}

// GetPage trả một trang cho tab Xem & sửa: từ + box + confidence kèm ảnh render.
// Ảnh là best-effort — không render được vẫn trả chữ (FE hiện khung chữ và để
// trống ảnh), chi tiết lỗi vào log chứ không phá cả response vì một tấm ảnh.
func (s *scanJobService) GetPage(ctx context.Context, jobID int64, pageNum int) dtos.Result[*dtos.ScanJobPageResponse] {
	job, err := s.readOwned(ctx, jobID)
	if err != nil {
		return dtos.Fail[*dtos.ScanJobPageResponse](err)
	}
	if pageNum < 1 || pageNum > job.TotalPages {
		return dtos.Fail[*dtos.ScanJobPageResponse](s.clientErr(http.StatusBadRequest,
			fmt.Sprintf("trang phải trong khoảng 1..%d", job.TotalPages)))
	}
	page, err := s.jobs.GetPage(ctx, jobID, pageNum)
	if err != nil {
		return dtos.Fail[*dtos.ScanJobPageResponse](s.mapNotFound(ctx, jobID, err, "không thấy trang này"))
	}
	out := toScanJobPageResponse(page)
	out.RenderURL = s.pageRenderURL(ctx, job, page)
	return dtos.Ok(out)
}

// pageRenderURL ký presigned GET cho ảnh trang. PDF chưa có bản render thì render
// ngay lượt đầu (pdftoppm ở PreviewDPI) rồi ghi vào kho + RenderKey, để các lần
// mở sau chỉ là một cú ký HMAC. Ảnh đơn thì ảnh xem chính là blob gốc.
func (s *scanJobService) pageRenderURL(ctx context.Context, job *domain.OCRJob, page *domain.OCRJobPage) string {
	isPDF := strings.ToLower(filepath.Ext(job.SourceName)) == ".pdf"
	key := page.RenderKey
	switch {
	case isPDF && key == "":
		key = s.renderAndCachePage(ctx, job, page)
	case !isPDF && key == "":
		key = job.SourceKey
	}
	if key == "" {
		return ""
	}
	url, err := s.store.PresignedGetURL(ctx, key, pageImageName(job.SourceName, page.PageNum), true, s.opt.PresignTTL)
	if err != nil {
		logs.Warnw("scan: ký URL ảnh trang lỗi", "trace_id", utils.TraceIDFromCtx(ctx),
			"job_id", job.ID, "page", page.PageNum, "error", err)
		return ""
	}
	return url
}

// renderAndCachePage render một trang PDF ra ảnh xem (JPEG, PreviewDPI), ghi vào
// kho tạm và cập nhật RenderKey. Luôn trả được key rỗng khi thất bại thay vì lỗi
// — chữ là phần quan trọng của tab Xem & sửa, ảnh chỉ phục vụ đối chiếu.
//
// Số lượt render đồng thời bị chặn bởi renderSem: nhiều người mở tab Xem & sửa
// cùng lúc thì phần dư xếp hàng chứ không sinh thêm tiến trình pdftoppm.
func (s *scanJobService) renderAndCachePage(ctx context.Context, job *domain.OCRJob, page *domain.OCRJobPage) string {
	if !s.acquireRender(ctx) {
		return ""
	}
	defer func() { <-s.renderSem }()
	path, cleanup, err := downloadScanTemp(ctx, s.store, job.SourceKey)
	if err != nil {
		logs.Warnw("scan: tải nguồn để render lỗi", "trace_id", utils.TraceIDFromCtx(ctx),
			"job_id", job.ID, "page", page.PageNum, "error", err)
		return ""
	}
	defer cleanup()
	img, err := scanocr.RenderPage(ctx, s.ocr, path, page.PageNum, scanocr.RenderOptions{
		DPI:         s.ocr.PreviewDPI,
		JPEG:        true,
		JPEGQuality: constants.SCAN_PREVIEW_JPEG_QUALITY,
	})
	if err != nil {
		logs.Warnw("scan: render ảnh trang lỗi", "trace_id", utils.TraceIDFromCtx(ctx),
			"job_id", job.ID, "page", page.PageNum, "error", err)
		return ""
	}
	key := fmt.Sprintf("%s%d/p%d.jpg", constants.SCAN_OBJECT_PREFIX, job.ID, page.PageNum)
	if err := s.store.Upload(ctx, key, bytes.NewReader(img), int64(len(img)), "image/jpeg"); err != nil {
		logs.Warnw("scan: ghi ảnh render lỗi", "trace_id", utils.TraceIDFromCtx(ctx),
			"job_id", job.ID, "page", page.PageNum, "error", err)
		return ""
	}
	page.RenderKey = key
	page.RenderDPI = s.ocr.PreviewDPI
	if err := s.jobs.UpdatePage(ctx, page); err != nil {
		// Ảnh đã nằm trong kho nên vẫn dùng được; chỉ tốn một lần render nữa ở
		// lần mở sau (không ai ghi nhớ RenderKey).
		logs.Warnw("scan: ghi RenderKey lỗi", "trace_id", utils.TraceIDFromCtx(ctx),
			"job_id", job.ID, "page", page.PageNum, "error", err)
	}
	return key
}

// acquireRender xếp hàng chờ một suất render đồng thời. ctx hết hạn thì trả
// false để request nhận ảnh rỗng (chữ vẫn hiện) thay vì giữ kết nối chờ vô hạn.
func (s *scanJobService) acquireRender(ctx context.Context) bool {
	select {
	case s.renderSem <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (s *scanJobService) DownloadURL(ctx context.Context, jobID int64) dtos.Result[*dtos.ScanJobDownloadResponse] {
	job, err := s.readOwned(ctx, jobID)
	if err != nil {
		return dtos.Fail[*dtos.ScanJobDownloadResponse](err)
	}
	if job.OutputKey == "" {
		return dtos.Fail[*dtos.ScanJobDownloadResponse](s.clientErr(http.StatusBadRequest,
			"tệp kết quả chưa sẵn sàng — chờ OCR hoàn tất"))
	}
	url, err := s.signOutput(ctx, job, false)
	if err != nil {
		return dtos.Fail[*dtos.ScanJobDownloadResponse](err)
	}
	logs.Infow("scan: ký URL tải PDF", "trace_id", utils.TraceIDFromCtx(ctx),
		"job_id", jobID, "user_id", job.UserID, "expires_ms", s.opt.PresignTTL.Milliseconds())
	return dtos.Ok(&dtos.ScanJobDownloadResponse{
		URL:       url,
		ExpiresAt: time.Now().Add(s.opt.PresignTTL),
		Filename:  scanOutputName(job.SourceName),
	})
}

// --- helpers ------------------------------------------------------------------

// readOwned kiểm chủ ở đây chứ không ở SQL của từng thao tác: repo đã lọc theo
// user_id nhưng service cần một chỗ duy nhất trả 404 "không tồn tại" (đọc job của
// người khác phải giống hệt đọc job không có).
func (s *scanJobService) readOwned(ctx context.Context, jobID int64) (*domain.OCRJob, error) {
	userID, err := s.owner(ctx)
	if err != nil {
		return nil, err
	}
	job, err := s.jobs.Get(ctx, jobID, userID)
	if err != nil {
		if stderrors.Is(err, domain.ErrNotFound) {
			return nil, s.clientErr(http.StatusNotFound, "job không tồn tại")
		}
		return nil, err
	}
	return job, nil
}

func (s *scanJobService) owner(ctx context.Context) (int64, error) {
	userID, ok := s.currentUser.UserID(ctx)
	if !ok || userID <= 0 {
		return 0, s.clientErr(http.StatusUnauthorized, "phiên đăng nhập không hợp lệ")
	}
	return userID, nil
}

// signOutput ký presigned GET cho PDF xuất. Job chưa có output thì trả chuỗi
// rỗng chứ không phải lỗi — đó là trạng thái bình thường lúc đang OCR.
func (s *scanJobService) signOutput(ctx context.Context, job *domain.OCRJob, inline bool) (string, error) {
	if job == nil || job.OutputKey == "" {
		return "", nil
	}
	url, err := s.store.PresignedGetURL(ctx, job.OutputKey, scanOutputName(job.SourceName), inline, s.opt.PresignTTL)
	if err != nil {
		logs.Warnw("scan: ký URL PDF lỗi", "trace_id", utils.TraceIDFromCtx(ctx),
			"job_id", job.ID, "user_id", job.UserID, "error", err)
		return "", err
	}
	return url, nil
}

// jobKeys gom mọi object của job (ảnh đã render từng trang) để xoá row là xoá
// sạch dấu vết. Đọc trang theo cửa sổ nên job 500 trang không lên RAM.
//
// Hàm package-level (không phải method) vì janitor cũng dùng: cùng một định
// nghĩa "object thuộc job" cho cả đường người dùng xoá và đường hệ thống dọn.
func jobKeys(ctx context.Context, repo domain.IOCRJobRepository, jobID int64) []string {
	var keys []string
	for from := 1; ; from += constants.SCAN_PAGE_WINDOW {
		pages, err := repo.ListPages(ctx, jobID, from, constants.SCAN_PAGE_WINDOW)
		if err != nil || len(pages) == 0 {
			return keys
		}
		for _, p := range pages {
			if p.RenderKey != "" {
				keys = append(keys, p.RenderKey)
			}
		}
		if len(pages) < constants.SCAN_PAGE_WINDOW {
			return keys
		}
	}
}

// removeObjects xoá best-effort từng object, trả số xoá thành công. Lỗi xoá một
// object không được làm hỏng thao tác của người dùng: row đã mất thì job biến mất
// khỏi UI, object còn lại có janitor lo tiếp.
func removeObjects(ctx context.Context, store storage.IStorage, keys []string) int {
	seen := make(map[string]bool, len(keys))
	removed := 0
	for _, k := range keys {
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		if err := store.Delete(ctx, k); err != nil {
			logs.Warnw("scan: xoá object lỗi", "trace_id", utils.TraceIDFromCtx(ctx), "error", err)
			continue
		}
		removed++
	}
	return removed
}

// discardSource dọn blob vừa ghi khi INSERT thất bại.
func (s *scanJobService) discardSource(ctx context.Context, key string, cause error) {
	logs.Warnw("scan: ghi row lỗi, xoá blob vừa upload",
		"trace_id", utils.TraceIDFromCtx(ctx), "error", cause)
	if err := s.store.Delete(ctx, key); err != nil {
		logs.Warnw("scan: xoá blob cứu lỗi", "trace_id", utils.TraceIDFromCtx(ctx), "error", err)
	}
}

func (s *scanJobService) pickStr(v, def string) string {
	if t := strings.TrimSpace(v); t != "" {
		return t
	}
	return def
}

func (s *scanJobService) pickInt(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}

// clientErr soạn lỗi tiếng Việt cho người dùng. Render chỉ log lỗi không rõ
// nguyên nhân, nên chủ động dùng nó cho mọi lỗi 4xx thay để tầng 500 in ra.
func (s *scanJobService) clientErr(httpCode int, message string) error {
	return errors.NewCustomHttpError(httpCode, httpCode, message)
}

// mapNotFound dịch domain.ErrNotFound thành 404 với thông điệp riêng, lỗi khác
// giữ nguyên để Render log.
func (s *scanJobService) mapNotFound(ctx context.Context, jobID int64, err error, message string) error {
	if stderrors.Is(err, domain.ErrNotFound) {
		logs.Infow("scan: không thấy bản ghi", "trace_id", utils.TraceIDFromCtx(ctx),
			"job_id", jobID, "reason", message)
		return s.clientErr(http.StatusNotFound, message)
	}
	return err
}

// scanSourceKey là key blob gốc trong bucket tạm. scan/<uuid>/… thay vì
// scan/<job_id>/… để một lần INSERT là đủ: key phải ghi TRƯỚC khi biết id.
func scanSourceKey(token, ext string) string {
	return constants.SCAN_OBJECT_PREFIX + token + "/source" + ext
}

// scanOutputName đặt tên file tải về: bỏ đuôi cũ, luôn kết thúc bằng .pdf vì
// output của mọi job là PDF (nguồn PDF giữ nguyên, nguồn ảnh được bọc một trang).
func scanOutputName(source string) string {
	base := strings.TrimSuffix(source, filepath.Ext(source))
	if base == "" {
		base = "scan"
	}
	return base + ".pdf"
}

// scanOutputKey là key blob kết quả. Chỉ job nguồn ẢNH mới có key riêng: job
// nguồn PDF trỏ thẳng SourceKey (output chính là tệp gốc), nên không sinh bản sao.
func scanOutputKey(jobID int64) string {
	return fmt.Sprintf("%s%d/output.pdf", constants.SCAN_OBJECT_PREFIX, jobID)
}

// measureImage đọc khổ ảnh bằng image.DecodeConfig — không giải mã toàn ảnh nên
// rẻ. ErrFormat (định dạng stdlib không có decoder, hoặc ảnh người dùng đổi tay
// phần mở rộng) trả (0,0,nil) vì hai cột này chỉ là BẰNG CHỨNG hiển thị; worker
// vẫn đo lại từ bytes thật lúc OCR. Lỗi I/O thì phải báo, đừng để job mở ra rồi chết.
func measureImage(path string) (int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, fmt.Errorf("mở ảnh để đo khổ: %w", err)
	}
	defer func() { _ = f.Close() }()
	cfg, _, err := image.DecodeConfig(f)
	if stderrors.Is(err, image.ErrFormat) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, fmt.Errorf("đọc khổ ảnh: %w", err)
	}
	return cfg.Width, cfg.Height, nil
}

// toScanJobPageResponse map row trang sang DTO tab Xem & sửa.
func toScanJobPageResponse(page *domain.OCRJobPage) *dtos.ScanJobPageResponse {
	return &dtos.ScanJobPageResponse{
		JobID:         page.JobID,
		Page:          page.PageNum,
		Status:        page.Status,
		Words:         toPageWords(page.Words),
		Text:          page.Text,
		MeanConf:      page.MeanConf,
		LowConfWords:  page.LowConfWords,
		CharsExisting: page.CharsExisting,
		HasTextLayer:  page.HasTextLayer,
		LastError:     page.LastError,
	}
}

// toPageWords parse JSONB words (cấu trúc savedScanWord worker đã ghi) sang DTO.
// JSON hỏng coi như trang chưa có từ nào — một row lỗi không được phá cả response.
func toPageWords(raw string) []dtos.ScanJobPageWord {
	var saved []savedScanWord
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &saved)
	}
	out := make([]dtos.ScanJobPageWord, 0, len(saved))
	for _, w := range saved {
		out = append(out, dtos.ScanJobPageWord{T: w.T, X: w.X, Y: w.Y, W: w.W, H: w.H, C: w.C})
	}
	return out
}

// downloadScanTemp tải blob về tệp tạm, trả (đường dẫn, cleanup). Một chỗ duy
// nhất chịu trách nhiệm xoá bản tạm: worker (OCR + dựng PDF) và service (render
// ảnh trang) đều đi qua đây.
func downloadScanTemp(ctx context.Context, store storage.IStorage, key string) (string, func(), error) {
	rc, err := store.Download(ctx, key)
	if err != nil {
		return "", nil, err
	}
	// Đã copy hết xuống tệp tạm trước khi đóng, nên lỗi Close ở đây không đổi
	// kết quả; vẫn phải gọi để nhả kết nối của kho.
	defer func() { _ = rc.Close() }()
	tmp, err := os.CreateTemp("", "scan-src-*")
	if err != nil {
		return "", nil, err
	}
	if _, err := io.Copy(tmp, rc); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", nil, err
	}
	// Close lỗi = dữ liệu có thể chưa xuống đĩa hết, không được đưa đường dẫn
	// cho bước OCR đọc tiếp.
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", nil, err
	}
	cleanup := func() { _ = os.Remove(tmp.Name()) }
	return tmp.Name(), cleanup, nil
}

// pageImageName đặt tên file ảnh trang khi FE tải về — presign cần một downloadAs
// tường minh, tên đúng gốc còn giúp thư mục tải của người dùng dễ nhận ra.
//
// Nguồn PDF: ảnh do BE render nên luôn là .jpg. Nguồn ảnh: trang 1 CHÍNH LÀ tệp
// gốc (không render), nên giữ đúng đuôi gốc — đặt tên .jpg cho một tệp PNG là
// nói dối định dạng.
func pageImageName(source string, page int) string {
	base := strings.TrimSuffix(source, filepath.Ext(source))
	if base == "" {
		base = "scan"
	}
	ext := strings.ToLower(filepath.Ext(source))
	if ext != ".pdf" && ext != "" {
		return fmt.Sprintf("%s-p%d%s", base, page, ext)
	}
	return fmt.Sprintf("%s-p%d.jpg", base, page)
}

// toScanJobResponse map row job sang DTO. outputURL do caller truyền vì chỉ
// service biết mình có ký được hay không; Progress tính một chỗ để mọi màn hình
// dùng chung một cách làm tròn.
func toScanJobResponse(job *domain.OCRJob, outputURL string) *dtos.ScanJobResponse {
	progress := 0
	if job.TotalPages > 0 {
		progress = min(100, job.DonePages*100/job.TotalPages)
	}
	return &dtos.ScanJobResponse{
		ID:             job.ID,
		Name:           job.SourceName,
		Status:         job.Status,
		TotalPages:     job.TotalPages,
		DonePages:      job.DonePages,
		FailedPages:    job.FailedPages,
		OCRTargetPages: job.OCRTargetPages,
		HasTextLayer:   job.TotalPages > 0 && job.OCRTargetPages < job.TotalPages,
		ForceOCR:       job.ForceOCR,
		Lang:           job.Lang,
		DPI:            job.DPI,
		PSM:            job.PSM,
		Engine:         job.Engine,
		Progress:       progress,
		ErrorCode:      job.ErrorCode,
		ErrorMessage:   job.ErrorMessage,
		ExpiresAt:      job.ExpiresAt,
		CreatedAt:      job.CreatedAt,
		OutputURL:      outputURL,
	}
}

// scanCreateError dịch lỗi của tầng dò/đọc thành câu tiếng Việt có hướng xử lý.
// Nội dung chi tiết đi vào log, không ra response (convention của repo).
func scanCreateError(ctx context.Context, name string, err error) error {
	logs.Warnw("scan: dựng job thất bại",
		"trace_id", utils.TraceIDFromCtx(ctx), "ext", utils.FileExt(name), "error", err)
	switch {
	case stderrors.Is(err, errScanTooLarge):
		return errors.NewCustomHttpError(http.StatusRequestEntityTooLarge, http.StatusRequestEntityTooLarge,
			"tệp quá dung lượng cho phép của một lần quét")
	case stderrors.Is(err, errScanEmptyFile):
		return errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "tệp rỗng")
	case stderrors.Is(err, errScanTooManyPages):
		return errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest,
			"tệp có quá nhiều trang cho một job — tách nhỏ rồi quét lại")
	case stderrors.Is(err, scanocr.ErrEncryptedPDF):
		return errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest,
			"PDF có mật khẩu — mở khoá rồi upload lại")
	default:
		return errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest,
			"không đọc được cấu trúc tệp — kiểm tra file có hỏng không")
	}
}
