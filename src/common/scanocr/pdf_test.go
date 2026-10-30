package scanocr

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// pdfinfoOutputThật là output đo được từ `pdfinfo loadtest/fixtures/pdf_scan.pdf`
// trên máy này — viết test theo chuỗi THẬT chứ không theo trí nhớ để parser
// không khớp với định dạng bịa.
const pdfinfoSummary = `Title:           untitled
Subject:         unspecified
Pages:           2
Encrypted:       no
Page size:       595.276 x 841.89 pts (A4)
Page rot:        0
File size:       242166 bytes
`

const pdfinfoPerPage = `Title:           untitled
Pages:           2
Page    1 size:  595.276 x 841.89 pts (A4)
Page    1 rot:   0
Page    2 size:  841.89 x 595.276 pts (A4)
Page    2 rot:   90
`

func TestParsePDFInfoSummary(t *testing.T) {
	info, ok := parsePDFInfoSummary(pdfinfoSummary)
	if !ok {
		t.Fatal("không đọc được số trang")
	}
	if info.pages != 2 || info.encrypted {
		t.Fatalf("summary = %+v", info)
	}
	if info.widthPt != 595.276 || info.heightPt != 841.89 || info.rotation != 0 {
		t.Fatalf("số danh định = %+v", info)
	}
	// File có mật khẩu phải bị từ chối ngay từ lúc dựng job.
	enc, ok := parsePDFInfoSummary("Pages: 3\nEncrypted: yes(aes256)\n")
	if !ok || !enc.encrypted {
		t.Fatalf("cờ mật khẩu không nhận ra: %+v ok=%v", enc, ok)
	}
	if _, ok := parsePDFInfoSummary("Command Line Error: wrong page range"); ok {
		t.Fatal("output rác phải báo không đọc được số trang")
	}
}

func TestParsePDFInfoPages(t *testing.T) {
	pages := parsePDFInfoPages(pdfinfoPerPage)
	if len(pages) != 2 {
		t.Fatalf("số trang đo riêng = %d, muốn 2", len(pages))
	}
	p2 := pages[2]
	// Trang ngang 90° có size ĐẢO so với trang danh định — PDF dựng lại phải lấy
	// đúng chiều này nếu không lớp chữ vô hình tràn ra ngoài khung nhìn.
	if p2.widthPt != 841.89 || p2.heightPt != 595.276 || p2.rotation != 90 {
		t.Fatalf("trang 2 = %+v", p2)
	}
	if got := parsePDFInfoPages(pdfinfoSummary); len(got) != 0 {
		t.Fatalf("bản pdfinfo không in dòng theo trang phải trả map rỗng, nhận %+v", got)
	}
}

func TestParseSize(t *testing.T) {
	w, h := parseSize("595.276 x 841.89 pts (A4)")
	if w != 595.276 || h != 841.89 {
		t.Fatalf("parseSize = %v x %v", w, h)
	}
	if w, h := parseSize("bogus"); w != 0 || h != 0 {
		t.Fatalf("chuỗi rác phải trả 0, nhận %v x %v", w, h)
	}
}

func TestCountCharsPerPage(t *testing.T) {
	// pdftotext nối trang bằng \f và kết thúc bằng một \f nữa → segment rỗng
	// cuối; đếm sai chỗ này là OCR nhầm trang đã có chữ.
	got := countCharsPerPage("abc de\n\fxx\r\n\f\f", 2)
	if len(got) != 2 || got[0] != 5 || got[1] != 2 {
		t.Fatalf("counts = %v, muốn [5 2]", got)
	}
	// Tiếng Việt tính theo rune: " hoà" là 3 ký tự in được chứ không phải 5 byte.
	if n := countPrintableRunes("Cộng hoà"); n != 7 {
		t.Fatalf("countPrintableRunes = %d, muốn 7 (đếm byte sẽ ra 9)", n)
	}
	if got := countCharsPerPage("", 0); len(got) != 0 {
		t.Fatalf("pages=0 phải trả slice rỗng, nhận %v", got)
	}
}

func TestPageGeometryNeedsOCRPerPage(t *testing.T) {
	g := PageGeometry{Num: 1, TextChars: 348}
	if g.NeedsOCR(40) {
		t.Fatal("trang đã có chữ dày bị OCR lại — lãng phí đúng chỗ đắt nhất")
	}
	scan := PageGeometry{Num: 2, TextChars: 0}
	if !scan.NeedsOCR(40) {
		t.Fatal("trang scan phải cần OCR")
	}
	// Trang chỉ có số trang + tiêu đề in sẵn (dưới ngưỡng) vẫn OCR — dạng PDF
	// export từ Word lẫn ảnh scan trong cùng một file.
	thin := PageGeometry{Num: 3, TextChars: 12}
	if !thin.NeedsOCR(40) {
		t.Fatal("text layer mỏng dưới ngưỡng phải bị OCR")
	}
}

func TestPopplerTool(t *testing.T) {
	// Người vận hành khai MỘT đường dẫn pdftoppm; pdfinfo/pdftotext đi cùng thư
	// mục và mang cùng đuôi .exe trên Windows.
	win := `D:\code\tools\poppler\poppler-24.08.0\Library\bin\pdftoppm.exe`
	if got := popplerTool(win, "pdfinfo"); !strings.EqualFold(filepath.Base(got), "pdfinfo.exe") ||
		!strings.EqualFold(filepath.Dir(got), filepath.Dir(win)) {
		t.Fatalf("popplerTool Windows = %q", got)
	}
	// Đường dẫn không mang đuôi .exe thì anh em của nó cũng không được gắn đuôi
	// (Linux: /usr/bin/pdftoppm → /usr/bin/pdftotext; viết theo filepath để
	// test chạy được trên cả hai OS).
	plain := filepath.Join("nat", "bin", "pdftoppm")
	if got := popplerTool(plain, "pdftotext"); got != filepath.Join("nat", "bin", "pdftotext") {
		t.Fatalf("popplerTool không .exe = %q", got)
	}
	// Không khai báo đường dẫn thì tin vào PATH.
	if got := popplerTool("", "pdfinfo"); got != "pdfinfo" {
		t.Fatalf("popplerTool rỗng = %q, muốn \"pdfinfo\"", got)
	}
}

// --- Test với binary + PDF thật (skip khi máy không có công cụ) --------------

func tesseractBinary(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("SCAN_OCR_BINARY"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("tesseract"); err == nil {
		return p
	}
	def := `C:\Program Files\Tesseract-OCR\tesseract.exe`
	if _, err := os.Stat(def); err == nil {
		return def
	}
	t.Skip("máy không có Tesseract — bỏ qua test OCR thật")
	return ""
}

func pdftoppmBinary(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("pdftoppm"); err == nil {
		return p
	}
	def := `D:\code\tools\poppler\poppler-24.08.0\Library\bin\pdftoppm.exe`
	if _, err := os.Stat(def); err == nil {
		return def
	}
	t.Skip("máy không có poppler (pdftoppm/pdfinfo/pdftotext) — bỏ qua test PDF thật")
	return ""
}

func fixture(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join("..", "..", "..", "loadtest", dir, name)
	if _, err := os.Stat(p); err != nil {
		t.Skipf("thiếu fixture %s — bỏ qua (%v)", p, err)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatalf("đường dẫn fixture: %v", err)
	}
	return abs
}

func testConfig(t *testing.T) Config {
	t.Helper()
	return Config{Enabled: true, Binary: tesseractBinary(t), RenderBinary: pdftoppmBinary(t),
		Lang: "vie", RenderDPI: 300}.Normalize()
}

// TestInspectScanFixtureBlocked proves phần detect theo từng trang chạy trên file
// scan thật: pdfinfo + pdftotext phải báo cả hai trang đều "không chữ".
func TestInspectScanFixtureBlocked(t *testing.T) {
	cfg := testConfig(t)
	pages, err := InspectPDF(context.Background(), cfg,
		fixture(t, "fixtures", "pdf_scan.pdf"))
	if err != nil {
		t.Fatalf("InspectPDF lỗi: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("số trang = %d, muốn 2", len(pages))
	}
	for _, g := range pages {
		if g.TextChars != 0 {
			t.Fatalf("trang %d có %d ký tự — fixture này phải là ảnh thuần", g.Num, g.TextChars)
		}
		if !g.NeedsOCR(cfg.TextLayerMinChars) {
			t.Fatalf("trang %d bị bỏ qua dù không có chữ", g.Num)
		}
		if g.WidthPt <= 0 || g.HeightPt <= 0 {
			t.Fatalf("trang %d thiếu kích thước: %+v", g.Num, g)
		}
	}
}

// TestInspectMixedFixturePerPage là bài học autoai: nó chỉ hỏi "cả file có chữ
// không" nên file 1 trang text + 1 trang scan bị bỏ qua TOÀN BỘ. Đây là test khoá
// hành vi phát hiện theo TỪNG trang.
func TestInspectMixedFixturePerPage(t *testing.T) {
	cfg := testConfig(t)
	pages, err := InspectPDF(context.Background(), cfg,
		fixture(t, "fixtures_complex", "scan_vua_text_vua_anh.pdf"))
	if err != nil {
		t.Fatalf("InspectPDF lỗi: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("số trang = %d, muốn 2", len(pages))
	}
	if pages[0].NeedsOCR(cfg.TextLayerMinChars) {
		t.Fatalf("trang 1 đã có %d ký tự mà vẫn bị OCR", pages[0].TextChars)
	}
	if !pages[1].NeedsOCR(cfg.TextLayerMinChars) {
		t.Fatal("trang 2 là ảnh scan nhưng không bị OCR — đúng lỗi của autoai")
	}
}

// TestRenderThenRecognize chạy cả chuỗi: render trang 1 @300DPI, đưa sang
// Tesseract qua cổng Engine, nhận về Page có dòng + box. Đây là bằng chứng
// package tách riêng tự chạy end-to-end mà không đụng src/common/extractor.
func TestRenderThenRecognize(t *testing.T) {
	cfg := testConfig(t)
	ctx := context.Background()
	path := fixture(t, "fixtures", "pdf_scan.pdf")

	img, err := RenderPage(ctx, cfg, path, 1, RenderOptions{DPI: cfg.RenderDPI})
	if err != nil {
		t.Fatalf("RenderPage lỗi: %v", err)
	}
	if len(img) < 10_000 {
		t.Fatalf("ảnh render quá nhỏ (%d bytes) — khó lòng là trang A4 300DPI", len(img))
	}
	eng, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine lỗi: %v", err)
	}
	if err := eng.Available(ctx); err != nil {
		t.Fatalf("engine không sẵn sàng: %v", err)
	}
	page, err := eng.Recognize(ctx, ImageInfo{Page: 1, DPI: cfg.RenderDPI, Extension: ".png"}, img)
	if err != nil {
		t.Fatalf("Recognize lỗi: %v", err)
	}
	if len(page.Lines) == 0 || strings.TrimSpace(page.Text()) == "" {
		t.Fatalf("OCR không ra chữ: %+v", page)
	}
	if page.WidthPx <= 0 || page.HeightPx <= 0 || page.EngineName != EngineTesseract {
		t.Fatalf("siêu dữ liệu trang sai: %+v", page)
	}
	// Box phải nằm trong ảnh — sai một ly là lớp chữ vô hình lệch khỏi chữ nhìn
	// thấy, và đây là lỗi khó thấy nhất khi duyệt PDF.
	st := page.Stats(0)
	for _, l := range page.Lines {
		if l.Box.X < 0 || l.Box.Y < 0 || l.Box.X+l.Box.W > page.WidthPx+1 || l.Box.Y+l.Box.H > page.HeightPx+1 {
			t.Fatalf("box ngoài trang: %+v (trang %.0fx%.0f)", l.Box, page.WidthPx, page.HeightPx)
		}
	}
	t.Logf("fixture scan: %d dòng / %d từ / TB conf %.1f — %q",
		len(page.Lines), st.Words, st.MeanConf, clip(page.Text(), 80))
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
