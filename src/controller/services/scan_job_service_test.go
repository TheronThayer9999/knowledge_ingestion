package services

import (
	"bytes"
	"context"
	stderrors "errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/scanocr"
	"knowledge_ingestion/src/common/storage"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/middlewares"
	"knowledge_ingestion/src/domain"
)

// Fakes chỉ ghi lại những gì luồng tạo job chạm tới; mọi method chưa cài đặt
// PANIC qua interface nhúng — đó là mục đích: test phát hiện ngay khi service bắt
// đầu ăn thêm dữ liệu mà người viết test không biết.
type scanRepoStub struct {
	domain.IOCRJobRepository

	created  *domain.OCRJob
	pages    []*domain.OCRJobPage
	job      *domain.OCRJob
	pagesAll []*domain.OCRJobPage
	deleted  *domain.OCRJob
	createEr error
	cancelEr error
	// resetCalls ghi (job, trang) đã được đưa về hàng đợi.
	resetCalls [][2]int64
	// page/updatedPage phục vụ GetPage: page là trang đã lưu, updatedPage ghi
	// lại lần UpdatePage cuối (RenderKey được ghi sau lần render đầu tiên).
	page        *domain.OCRJobPage
	updatedPage *domain.OCRJobPage
}

func (f *scanRepoStub) Create(_ context.Context, job *domain.OCRJob, pages []*domain.OCRJobPage) error {
	if f.createEr != nil {
		return f.createEr
	}
	job.ID = 77 // INSERT sinh id; service phải dùng id này ở response
	f.created = job
	f.pages = pages
	return nil
}

func (f *scanRepoStub) Get(_ context.Context, jobID, userID int64) (*domain.OCRJob, error) {
	if f.job == nil || f.job.ID != jobID || f.job.UserID != userID {
		return nil, domain.ErrNotFound
	}
	return f.job, nil
}

func (f *scanRepoStub) Cancel(_ context.Context, jobID, userID int64) error {
	return f.cancelEr
}

func (f *scanRepoStub) Delete(_ context.Context, _, _ int64) (*domain.OCRJob, error) {
	return f.deleted, nil
}

func (f *scanRepoStub) ListPages(_ context.Context, _ int64, from, limit int) ([]*domain.OCRJobPage, error) {
	if from > 1 {
		return nil, nil
	}
	if limit > 0 && len(f.pagesAll) > limit {
		f.pagesAll = f.pagesAll[:limit]
	}
	return f.pagesAll, nil
}

func (f *scanRepoStub) ResetPage(_ context.Context, jobID int64, pageNum int) error {
	f.resetCalls = append(f.resetCalls, [2]int64{jobID, int64(pageNum)})
	return nil
}

func (f *scanRepoStub) GetPage(_ context.Context, jobID int64, pageNum int) (*domain.OCRJobPage, error) {
	if f.page == nil || f.page.JobID != jobID || f.page.PageNum != pageNum {
		return nil, domain.ErrNotFound
	}
	return f.page, nil
}

func (f *scanRepoStub) UpdatePage(_ context.Context, page *domain.OCRJobPage) error {
	f.updatedPage = page
	return nil
}

type scanStoreStub struct {
	storage.IStorage

	uploaded    map[string]int64
	deletedKeys []string
	signed      []string
	uploadEr    error
	// download là nội dung blob mà Download trả về (test render ảnh trang).
	download []byte
}

func newStoreStub() *scanStoreStub {
	return &scanStoreStub{uploaded: map[string]int64{}}
}

func (f *scanStoreStub) Upload(_ context.Context, key string, r io.Reader, size int64, _ string) error {
	if f.uploadEr != nil {
		return f.uploadEr
	}
	n, err := io.Copy(io.Discard, r)
	if err != nil {
		return err
	}
	f.uploaded[key] = n
	_ = size
	return nil
}

func (f *scanStoreStub) Delete(_ context.Context, key string) error {
	f.deletedKeys = append(f.deletedKeys, key)
	return nil
}

func (f *scanStoreStub) PresignedGetURL(_ context.Context, key, _ string, _ bool, _ time.Duration) (string, error) {
	f.signed = append(f.signed, key)
	return "https://s3.test/" + key + "?X-Amz-Signature=do-not-log", nil
}

// Download trả blob test đã chuẩn bị (f.download) — luồng render ảnh trang tải
// nguồn qua đây. Không có blob thì trả lỗi để test thấy ngay thiếu dữ liệu.
func (f *scanStoreStub) Download(_ context.Context, _ string) (io.ReadCloser, error) {
	if f.download == nil {
		return nil, stderrors.New("scanStoreStub: thiếu blob download")
	}
	return io.NopCloser(bytes.NewReader(f.download)), nil
}

// scanFixture chỉ vào thư mục fixture thật của repo (cùng bộ với test scanocr).
func scanFixture(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join("..", "..", "..", "loadtest", dir, name)
	if _, err := os.Stat(p); err != nil {
		t.Skipf("thiếu fixture %s (%v)", p, err)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatalf("đường dẫn fixture: %v", err)
	}
	return abs
}

// Đường dẫn mặc định trên máy dev (AGENTS.md) — chỉ dùng khi env và PATH trắng tay.
const (
	scanTesseractDefault = `C:\Program Files\Tesseract-OCR\tesseract.exe`
	scanPopplerDefault   = `D:\code\tools\poppler\poppler-24.08.0\Library\bin\pdftoppm.exe`
)

// scanTool tìm binary theo thứ tự env → PATH → đường dẫn máy dev. Không có ở cả
// ba thì skip kèm lý do nói rõ đã tìm gì: Create hỏi engine TRƯỚC khi nhận tệp,
// nên trên máy thiếu công cụ test phải nhảy chứ không đỏ.
func scanTool(t *testing.T, env, name, def string) string {
	t.Helper()
	if p := os.Getenv(env); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	if _, err := os.Stat(def); err == nil {
		return def
	}
	t.Skipf("máy không có %s (không có %s, không ở PATH, không có %s) — bỏ qua test OCR thật",
		name, env, def)
	return ""
}

// scanOCRTestConfig dựng config thật cho test: binary thật của máy, ngưỡng text
// layer bật để test "trang đã có chữ" chạy được.
func scanOCRTestConfig(t *testing.T) scanocr.Config {
	t.Helper()
	return scanocr.Config{
		Enabled:           true,
		Binary:            scanTool(t, "SCAN_OCR_BINARY", "tesseract", scanTesseractDefault),
		RenderBinary:      scanTool(t, "SCAN_POPPLER_BINARY", "pdftoppm", scanPopplerDefault),
		RenderDPI:         300,
		TextLayerMinChars: 40,
	}.Normalize()
}

func scanService(t *testing.T, repo *scanRepoStub, store *scanStoreStub) *scanJobService {
	t.Helper()
	return &scanJobService{
		jobs: repo, store: store, ocr: scanOCRTestConfig(t), currentUser: middlewares.NewCurrentUser(),
		renderSem: make(chan struct{}, constants.SCAN_RENDER_CONCURRENCY),
		opt: ScanJobOptions{Enabled: true, Lang: "vie", DPI: 300, PSM: 6, Engine: "tesseract",
			MaxUploadBytes: 20 << 20, MaxPages: 300,
			TempTTL: 24 * time.Hour, PushedTTL: 7 * 24 * time.Hour, PresignTTL: 5 * time.Minute},
	}
}

func scanCtx() context.Context {
	return middlewares.WithUserID(context.Background(), 42)
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		img.Set(y, y/2, color.Gray16{Y: 128})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// tempDirs đếm thư mục "scan-job-*" còn sót trong thư mục tạm của máy. Đếm hiệu
// (không so đường dẫn cụ thể) vì test chạy song song trong cùng package: điều
// kiện đúng là "Create không để lại gì", không phải "không ai khác để lại gì".
func tempDirs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatalf("đọc thư mục tạm: %v", err)
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "scan-job-") {
			n++
		}
	}
	return n
}

// --- Create -------------------------------------------------------------------

func TestScanJobCreateFromScanPDFQueuesEveryPage(t *testing.T) {
	repo, store := &scanRepoStub{}, newStoreStub()
	svc := scanService(t, repo, store)
	path := scanFixture(t, "fixtures", "pdf_scan.pdf")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("đọc fixture: %v", err)
	}
	res := svc.Create(scanCtx(), "hop-don.pdf", bytes.NewReader(data), dtos.CreateScanJobRequest{})
	if res.Err != nil {
		t.Fatalf("Create: %v", res.Err)
	}
	job := repo.created
	if job.UserID != 42 || job.Status != domain.QueuePending {
		t.Fatalf("job sai chủ/trạng thái: %+v", job)
	}
	if job.TotalPages == 0 || job.OCRTargetPages != job.TotalPages {
		t.Fatalf("file scan phải OCR ĐỦ mọi trang: total=%d target=%d",
			job.TotalPages, job.OCRTargetPages)
	}
	if len(repo.pages) != job.TotalPages {
		t.Fatalf("số row trang (%d) phải khớp TotalPages (%d)", len(repo.pages), job.TotalPages)
	}
	for i, p := range repo.pages {
		if p.PageNum != i+1 || p.Status != domain.OCRPagePending {
			t.Fatalf("trang %d: số=%d status=%s", i+1, p.PageNum, p.Status)
		}
		if p.Words != domain.OCRWordsEmpty {
			t.Fatalf("words khởi điểm phải là jsonb hợp lệ, nhận %q", p.Words)
		}
	}
	if job.ExpiresAt.Before(time.Now().Add(23 * time.Hour)) {
		t.Fatalf("TTL bản tạm chưa áp: %v", job.ExpiresAt)
	}
	if len(store.uploaded) != 1 {
		t.Fatalf("phải có đúng 1 blob nguồn, nhận %d", len(store.uploaded))
	}
	for key := range store.uploaded {
		if !strings.HasPrefix(key, constants.SCAN_OBJECT_PREFIX) || !strings.HasSuffix(key, "/source.pdf") {
			t.Fatalf("key blob sai khuôn scan/<id>/source.<ext>: %s", key)
		}
	}
	if res.Data.ID != 77 || res.Data.TotalPages != job.TotalPages {
		t.Fatalf("response chưa map đủ: %+v", res.Data)
	}
}

func TestScanJobCreateSkipsPagesWithTextLayer(t *testing.T) {
	repo, store := &scanRepoStub{}, newStoreStub()
	svc := scanService(t, repo, store)
	path := scanFixture(t, "fixtures", "pdf_text_layer.pdf")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("đọc fixture: %v", err)
	}
	res := svc.Create(scanCtx(), "quy-che.pdf", bytes.NewReader(data), dtos.CreateScanJobRequest{})
	if res.Err != nil {
		t.Fatalf("Create: %v", res.Err)
	}
	job := repo.created
	// Đây là điểm autoai làm sai (B.4): file CÓ chữ ở vài trang vẫn phải sinh
	// row cho từng trang, chỉ khác trạng thái.
	if job.OCRTargetPages >= job.TotalPages {
		t.Fatalf("file có text layer phải có trang BỎ QUA OCR: target=%d total=%d",
			job.OCRTargetPages, job.TotalPages)
	}
	skipped := 0
	for _, p := range repo.pages {
		if p.Status == domain.OCRPageSkippedHasText {
			skipped++
			if !p.HasTextLayer || p.CharsExisting <= 0 {
				t.Fatalf("trang bỏ qua phải kèm bằng chứng chữ: %+v", p)
			}
		}
	}
	if skipped == 0 || skipped != job.TotalPages-job.OCRTargetPages {
		t.Fatalf("số trang bỏ qua (%d) không khớp OCRTargetPages (%d/%d)",
			skipped, job.OCRTargetPages, job.TotalPages)
	}
	if !res.Data.HasTextLayer {
		t.Fatal("has_text_layer phải bật để tab Tài liệu báo đúng")
	}
}

func TestScanJobCreateForceOCRRequeuesTextPages(t *testing.T) {
	repo, store := &scanRepoStub{}, newStoreStub()
	svc := scanService(t, repo, store)
	path := scanFixture(t, "fixtures", "pdf_text_layer.pdf")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("đọc fixture: %v", err)
	}
	res := svc.Create(scanCtx(), "quy-che.pdf", bytes.NewReader(data),
		dtos.CreateScanJobRequest{ForceOCR: true})
	if res.Err != nil {
		t.Fatalf("Create: %v", res.Err)
	}
	if repo.created.OCRTargetPages != repo.created.TotalPages {
		t.Fatalf("force_ocr phải OCR mọi trang: %d/%d",
			repo.created.OCRTargetPages, repo.created.TotalPages)
	}
	for _, p := range repo.pages {
		if p.Status != domain.OCRPagePending {
			t.Fatalf("force_ocr mà trang %d vẫn %s", p.PageNum, p.Status)
		}
	}
}

func TestScanJobCreateFromImageMakesOnePendingPage(t *testing.T) {
	repo, store := &scanRepoStub{}, newStoreStub()
	svc := scanService(t, repo, store)
	data := pngBytes(t, 120, 60)
	res := svc.Create(scanCtx(), "anh-chup.png", bytes.NewReader(data), dtos.CreateScanJobRequest{})
	if res.Err != nil {
		t.Fatalf("Create: %v", res.Err)
	}
	if repo.created.TotalPages != 1 || repo.created.OCRTargetPages != 1 {
		t.Fatalf("ảnh đơn phải là 1 trang cần OCR: %+v", repo.created)
	}
	p := repo.pages[0]
	// Khổ ảnh lưu ngay để worker khỏi mở lại blob chỉ để đo.
	if p.WidthPx != 120 || p.HeightPx != 60 {
		t.Fatalf("khổ ảnh không được đo: %+v", p)
	}
	if p.Status != domain.OCRPagePending {
		t.Fatalf("ảnh upload trực tiếp luôn phải OCR, nhận %s", p.Status)
	}
	for key := range store.uploaded {
		if !strings.HasSuffix(key, "/source.png") {
			t.Fatalf("key blob phải giữ đuôi gốc: %s", key)
		}
	}
}

func TestScanJobCreateRejectsBadInput(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		body     []byte
		want     string
	}{
		{"đuôi không hỗ trợ", "dich.txt", []byte("xin chao"), "chỉ nhận ảnh"},
		{"tệp rỗng", "trong.pdf", nil, "tệp rỗng"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, store := &scanRepoStub{}, newStoreStub()
			svc := scanService(t, repo, store)
			res := svc.Create(scanCtx(), tc.filename, bytes.NewReader(tc.body), dtos.CreateScanJobRequest{})
			if res.Err == nil {
				t.Fatalf("muốn lỗi %q, nhận thành công", tc.want)
			}
			if !strings.Contains(res.Err.Error(), tc.want) {
				t.Fatalf("lỗi = %q, muốn chứa %q", res.Err, tc.want)
			}
			if repo.created != nil || len(store.uploaded) > 0 {
				t.Fatal("từ chối rồi thì không được ghi row hay blob")
			}
		})
	}
}

func TestScanJobCreateDisabledFeature(t *testing.T) {
	repo, store := &scanRepoStub{}, newStoreStub()
	svc := scanService(t, repo, store)
	svc.opt.Enabled = false
	res := svc.Create(scanCtx(), "a.pdf", bytes.NewReader([]byte("%PDF-1.7")), dtos.CreateScanJobRequest{})
	if res.Err == nil || !strings.Contains(res.Err.Error(), "scan_ocr.enabled") {
		t.Fatalf("tính năng tắt phải báo rõ công tắc, nhận %v", res.Err)
	}
}

func TestScanJobCreateWithoutUserInContext(t *testing.T) {
	svc := scanService(t, &scanRepoStub{}, newStoreStub())
	res := svc.Create(context.Background(), "a.pdf", bytes.NewReader([]byte("x")), dtos.CreateScanJobRequest{})
	if res.Err == nil || !strings.Contains(res.Err.Error(), "đăng nhập") {
		t.Fatalf("thiếu user phải 401, nhận %v", res.Err)
	}
}

func TestScanJobCreateRemovesBlobWhenRowFails(t *testing.T) {
	repo, store := &scanRepoStub{}, newStoreStub()
	repo.createEr = stderrors.New("db died")
	svc := scanService(t, repo, store)
	res := svc.Create(scanCtx(), "anh.png", bytes.NewReader(pngBytes(t, 10, 10)), dtos.CreateScanJobRequest{})
	if res.Err == nil {
		t.Fatal("muốn lỗi từ repo")
	}
	// Blob mồ côi không có row trỏ tới thì janitor không bao giờ dọn — service
	// phải tự xoá ngay.
	if len(store.deletedKeys) != 1 {
		t.Fatalf("không dọn blob mồ côi: %v", store.deletedKeys)
	}
}

func TestScanJobCreateRemovesTempDirWhenStorageFails(t *testing.T) {
	repo, store := &scanRepoStub{}, newStoreStub()
	store.uploadEr = stderrors.New("seaweedfs down")
	svc := scanService(t, repo, store)
	before := tempDirs(t)
	res := svc.Create(scanCtx(), "anh.png", bytes.NewReader(pngBytes(t, 10, 10)), dtos.CreateScanJobRequest{})
	if res.Err == nil {
		t.Fatal("muốn lỗi kho")
	}
	// Bản scan là dữ liệu nhạy cảm: mọi đường thoát của Create phải xoá file tạm.
	if left := tempDirs(t) - before; left != 0 {
		t.Fatalf("còn %d thư mục scan-job-* trên đĩa", left)
	}
}

// --- Get / List / Delete / Retry ----------------------------------------------

func TestScanJobGetSignsOutputWhenDone(t *testing.T) {
	repo, store := &scanRepoStub{}, newStoreStub()
	repo.job = &domain.OCRJob{Status: domain.QueueDone, OutputKey: "scan/abc/output.pdf",
		SourceName: "hop-don.pdf", TotalPages: 3, DonePages: 3}
	repo.job.ID = 5
	repo.job.UserID = 42
	svc := scanService(t, repo, store)
	res := svc.Get(scanCtx(), 5)
	if res.Err != nil {
		t.Fatalf("Get: %v", res.Err)
	}
	if res.Data.Progress != 100 || res.Data.OutputURL == "" {
		t.Fatalf("job done phải có URL + 100%%: %+v", res.Data)
	}
	dl := svc.DownloadURL(scanCtx(), 5)
	if dl.Err != nil || dl.Data.Filename != "hop-don.pdf" {
		t.Fatalf("DownloadURL: %v %+v", dl.Err, dl.Data)
	}
}

func TestScanJobGetOtherUsersJobIsNotFound(t *testing.T) {
	repo, store := &scanRepoStub{}, newStoreStub()
	repo.job = &domain.OCRJob{Status: domain.QueueProcessing}
	repo.job.ID = 5
	repo.job.UserID = 99 // không phải 42
	svc := scanService(t, repo, store)
	if res := svc.Get(scanCtx(), 5); res.Err == nil || !strings.Contains(res.Err.Error(), "không tồn tại") {
		t.Fatalf("job của người khác phải 404 như không có, nhận %v", res.Err)
	}
	if len(store.signed) != 0 {
		t.Fatal("không được ký URL cho job của người khác")
	}
}

func TestScanJobDownloadRequiresBuiltPDF(t *testing.T) {
	repo, store := &scanRepoStub{}, newStoreStub()
	repo.job = &domain.OCRJob{Status: domain.QueueProcessing, SourceKey: "scan/abc/source.pdf"}
	repo.job.ID = 5
	repo.job.UserID = 42
	svc := scanService(t, repo, store)
	res := svc.DownloadURL(scanCtx(), 5)
	if res.Err == nil || !strings.Contains(res.Err.Error(), "chưa sẵn sàng") {
		t.Fatalf("chưa có PDF phải báo đường bấm lại, nhận %v", res.Err)
	}
}

func TestScanJobDeleteRemovesEveryObject(t *testing.T) {
	repo, store := &scanRepoStub{}, newStoreStub()
	repo.job = &domain.OCRJob{Status: domain.QueueDone, SourceKey: "scan/abc/source.pdf",
		OutputKey: "scan/abc/output.pdf"}
	repo.job.ID = 5
	repo.job.UserID = 42
	repo.deleted = repo.job
	repo.pagesAll = []*domain.OCRJobPage{
		{PageNum: 1, RenderKey: "scan/abc/p1.png"},
		{PageNum: 2, RenderKey: ""}, // trang chưa render thì không có gì để xoá
	}
	svc := scanService(t, repo, store)
	res := svc.Delete(scanCtx(), 5)
	if res.Err != nil {
		t.Fatalf("Delete: %v", res.Err)
	}
	want := []string{"scan/abc/p1.png", "scan/abc/source.pdf", "scan/abc/output.pdf"}
	for _, k := range want {
		if !slices.Contains(store.deletedKeys, k) {
			t.Fatalf("thiếu xoá %s trong %v", k, store.deletedKeys)
		}
	}
	if res.Data.PagesAffected != len(want) {
		t.Fatalf("PagesAffected = %d, muốn %d", res.Data.PagesAffected, len(want))
	}
}

func TestScanJobCancelPropagatesNotFound(t *testing.T) {
	repo, store := &scanRepoStub{}, newStoreStub()
	repo.cancelEr = domain.ErrNotFound
	svc := scanService(t, repo, store)
	res := svc.Cancel(scanCtx(), 5)
	if res.Err == nil || !strings.Contains(res.Err.Error(), "không huỷ được") {
		t.Fatalf("job đã xong phải nói rõ vì sao không huỷ, nhận %v", res.Err)
	}
}

func TestScanJobRetryPageRejectsPageOutOfRange(t *testing.T) {
	repo, store := &scanRepoStub{}, newStoreStub()
	repo.job = &domain.OCRJob{Status: domain.QueueDone, TotalPages: 4}
	repo.job.ID = 5
	repo.job.UserID = 42
	svc := scanService(t, repo, store)
	if res := svc.RetryPage(scanCtx(), 5, 9); res.Err == nil {
		t.Fatal("trang ngoài khoảng phải bị chặn trước khi chạm repo")
	}
	if res := svc.RetryPage(scanCtx(), 5, 2); res.Err != nil {
		t.Fatalf("RetryPage(2): %v", res.Err)
	}
	if len(repo.resetCalls) != 1 || repo.resetCalls[0][1] != 2 {
		t.Fatalf("ResetPage không gọi đúng trang: %v", repo.resetCalls)
	}
}

// --- GetPage ------------------------------------------------------------------

// Trang đã có ảnh render trong kho → chỉ ký URL, words JSONB phải parse đúng
// sang DTO và job của người khác phải 404.
func TestScanJobGetPageReturnsWordsAndSignsCachedRender(t *testing.T) {
	repo := &scanRepoStub{job: &domain.OCRJob{TotalPages: 3}}
	repo.job.ID = 7
	repo.job.UserID = 42
	repo.page = &domain.OCRJobPage{
		JobID: 7, PageNum: 2, Status: domain.OCRPageDone,
		RenderKey: "scan/7/p2.jpg",
		Words:     `[{"t":"Phiên","x":0.25,"y":0.04,"w":0.05,"h":0.02,"c":96.5}]`,
		Text:      "Phiên", MeanConf: 96.5, LowConfWords: 1,
	}
	store := newStoreStub()
	svc := scanService(t, repo, store)

	res := svc.GetPage(scanCtx(), 7, 2)
	if res.Err != nil {
		t.Fatalf("GetPage: %v", res.Err)
	}
	page := res.Data
	if page.RenderURL == "" || len(store.signed) != 1 || store.signed[0] != "scan/7/p2.jpg" {
		t.Fatalf("phải ký đúng RenderKey đã cache: url=%q signed=%v", page.RenderURL, store.signed)
	}
	if len(page.Words) != 1 || page.Words[0].T != "Phiên" || page.Words[0].C != 96.5 {
		t.Fatalf("words parse sai: %+v", page.Words)
	}
	if page.MeanConf != 96.5 || page.LowConfWords != 1 || page.Status != domain.OCRPageDone {
		t.Fatalf("thiếu chỉ số trang: %+v", page)
	}

	otherJob := &domain.OCRJob{TotalPages: 1}
	otherJob.ID = 7
	otherJob.UserID = 43
	other := &scanRepoStub{job: otherJob, page: repo.page}
	if res := scanService(t, other, newStoreStub()).GetPage(scanCtx(), 7, 2); res.Err == nil {
		t.Fatal("job của người khác phải trả lỗi như không tồn tại")
	}
	if res := svc.GetPage(scanCtx(), 7, 99); res.Err == nil {
		t.Fatal("số trang ngoài khoảng phải bị chặn trước khi chạm repo")
	}
}

// Lần mở đầu trang PDF chưa render → render bằng pdftoppm thật, ghi ảnh vào kho
// và lưu RenderKey; lần sau không render lại (blob nguồn đã bỏ đi vẫn ký URL được).
func TestScanJobGetPageRendersAndCachesImage(t *testing.T) {
	repo := &scanRepoStub{job: &domain.OCRJob{
		TotalPages: 1, SourceName: "quy-dinh.pdf", SourceKey: "scan/uuid/source.pdf",
	}}
	repo.job.ID = 9
	repo.job.UserID = 42
	repo.page = &domain.OCRJobPage{JobID: 9, PageNum: 1, Status: domain.OCRPagePending, Words: domain.OCRWordsEmpty}
	store := newStoreStub()
	data, err := os.ReadFile(scanFixture(t, "fixtures", "pdf_scan.pdf"))
	if err != nil {
		t.Fatalf("đọc fixture: %v", err)
	}
	store.download = data
	svc := scanService(t, repo, store)

	res := svc.GetPage(scanCtx(), 9, 1)
	if res.Err != nil {
		t.Fatalf("GetPage: %v", res.Err)
	}
	const want = "scan/9/p1.jpg"
	if res.Data.RenderURL == "" {
		t.Fatal("trang PDF chưa render phải render ngay ở lượt mở đầu")
	}
	if store.uploaded[want] == 0 {
		t.Fatalf("phải ghi ảnh xem vào kho đúng key %s, đã ghi: %v", want, store.uploaded)
	}
	if repo.updatedPage == nil || repo.updatedPage.RenderKey != want {
		t.Fatalf("RenderKey phải được lưu lại, nhận: %+v", repo.updatedPage)
	}
	if repo.updatedPage.RenderDPI <= 0 {
		t.Fatalf("RenderDPI phải ghi theo ảnh thật, nhận %d", repo.updatedPage.RenderDPI)
	}

	// Lần hai: RenderKey đã lưu nên không đụng tới blob nguồn nữa.
	store.download = nil
	if res := svc.GetPage(scanCtx(), 9, 1); res.Err != nil || res.Data.RenderURL == "" {
		t.Fatalf("lần mở sau phải ký từ RenderKey đã cache: err=%v url=%q", res.Err, res.Data.RenderURL)
	}
}

// --- các hàm thuần -----------------------------------------------------------

func TestToScanJobResponseProgressAndFlags(t *testing.T) {
	mk := func(total, done, failed, target int) *domain.OCRJob {
		j := &domain.OCRJob{Status: domain.QueueProcessing, TotalPages: total,
			DonePages: done, FailedPages: failed, OCRTargetPages: target}
		j.ID = 9
		return j
	}
	cases := []struct {
		name     string
		job      *domain.OCRJob
		wantProg int
		wantText bool
	}{
		{"0 trang thì 0%", mk(0, 0, 0, 0), 0, false},
		{"1/4 trang", mk(4, 1, 0, 4), 25, false},
		{"toàn bộ có chữ sẵn", mk(3, 3, 0, 0), 100, true},
		{"done vượt total không được quá 100", mk(2, 5, 0, 2), 100, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := toScanJobResponse(tc.job, "")
			if res.Progress != tc.wantProg {
				t.Fatalf("progress = %d, muốn %d", res.Progress, tc.wantProg)
			}
			if res.HasTextLayer != tc.wantText {
				t.Fatalf("has_text_layer = %v, muốn %v", res.HasTextLayer, tc.wantText)
			}
		})
	}
}

func TestScanOutputNameAndSourceKey(t *testing.T) {
	if got := scanOutputName("hop don.pdf"); got != "hop don.pdf" {
		t.Fatalf("scanOutputName = %q", got)
	}
	if got := scanOutputName("anh chup.JPG"); got != "anh chup.pdf" {
		t.Fatalf("ảnh phải đổi sang .pdf, nhận %q", got)
	}
	if got := scanOutputName(".pdf"); got != "scan.pdf" {
		t.Fatalf("tên cụt phải có mặc định, nhận %q", got)
	}
	key := scanSourceKey("019abcv7", ".png")
	if key != constants.SCAN_OBJECT_PREFIX+"019abcv7/source.png" {
		t.Fatalf("scanSourceKey = %q", key)
	}
	if out := scanOutputKey(9); out != constants.SCAN_OBJECT_PREFIX+"9/output.pdf" {
		t.Fatalf("scanOutputKey = %q", out)
	}
}

func TestScanCreateErrorMessages(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		cause error
		want  string
	}{
		{errScanTooLarge, "quá dung lượng"},
		{errScanEmptyFile, "tệp rỗng"},
		{errScanTooManyPages, "quá nhiều trang"},
		{scanocr.ErrEncryptedPDF, "mật khẩu"},
		{stderrors.New("khác"), "không đọc được cấu trúc tệp"},
	} {
		res := scanCreateError(ctx, "x.pdf", tc.cause)
		if res == nil || !strings.Contains(res.Error(), tc.want) {
			t.Fatalf("%v -> %q, muốn chứa %q", tc.cause, res, tc.want)
		}
	}
}
