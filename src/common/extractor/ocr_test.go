package extractor

// Test OCR Tesseract: tắt → unsupported; ảnh trắng + engine thật → lỗi
// "không ra chữ" (chứng minh plumbing exec chạy tới binary); render PDF
// dùng fake renderer để không cần pdftoppm. Test cần engine thật thì skip
// khi máy không có tesseract.

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// tesseractPath tìm engine thật: PATH trước, rồi đường dẫn cài mặc định
// Windows (file _tesseract.md). Không có thì skip test.
func tesseractPath(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("tesseract"); err == nil {
		return p
	}
	def := `C:\Program Files\Tesseract-OCR\tesseract.exe`
	if _, err := os.Stat(def); err == nil {
		return def
	}
	t.Skip("máy không có tesseract, bỏ qua test OCR thật")
	return ""
}

// TestMissingLangs khoá phần THUẦN của kiểm tra traineddata: config viết
// "vie+eng" nhưng engine chỉ có 1 gói thì phải báo đúng gói còn thiếu, để
// warn lúc boot chỉ ra thứ cần cài thay vì bắt người vận hành tự đoán.
func TestMissingLangs(t *testing.T) {
	tests := []struct {
		name      string
		available []string
		lang      string
		want      []string
	}{
		{name: "đủ cả hai", available: []string{"eng", "osd", "vie"}, lang: "vie+eng", want: nil},
		{name: "thiếu eng", available: []string{"osd", "vie"}, lang: "vie+eng", want: []string{"eng"}},
		{name: "config trống", available: []string{"vie"}, lang: "", want: nil},
		{name: "có khoảng trắng thừa", available: []string{"vie", "eng"}, lang: " vie + eng ", want: nil},
		{name: "trùng lặp chỉ báo một lần", available: []string{"vie"}, lang: "eng+eng", want: []string{"eng"}},
		{name: "khác hoa thường", available: []string{"VIE", "Eng"}, lang: "vie+eng", want: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := MissingLangs(tc.available, tc.lang)
			if len(got) != len(tc.want) {
				t.Fatalf("missing = %v, muốn %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("missing = %v, muốn %v", got, tc.want)
				}
			}
		})
	}
}

// TestListLangs chạy với engine thật để chắc phần parse không vướng dòng
// tiêu đề ("List of available languages in ... (3):") hay đường dẫn Windows.
func TestListLangs(t *testing.T) {
	langs, err := ListLangs(t.Context(), tesseractPath(t))
	if err != nil {
		t.Fatalf("ListLangs lỗi: %v", err)
	}
	if len(langs) == 0 {
		t.Fatal("không parse được ngôn ngữ nào")
	}
	for _, l := range langs {
		if strings.ContainsAny(l, " :") {
			t.Fatalf("parse sai, dính cả dòng tiêu đề: %q", l)
		}
	}
}

// popplerPdftoppm tìm pdftoppm (poppler) để trích PDF thật. Thiếu thì skip.
// Các công cụ khác của poppler (pdfinfo, pdftotext) suy từ thư mục này.
func popplerPdftoppm(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("pdftoppm"); err == nil {
		return p
	}
	def := `D:\code\tools\poppler\poppler-24.08.0\Library\bin\pdftoppm.exe`
	if _, err := os.Stat(def); err == nil {
		return def
	}
	t.Skip("máy không có poppler (pdftoppm/pdftotext), bỏ qua test PDF thật")
	return ""
}

// whitePNG vẽ ảnh trắng PNG trong RAM — OCR phải ra rỗng.
func whitePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 200, 60))
	for y := 0; y < 60; y++ {
		for x := 0; x < 200; x++ {
			img.Set(x, y, color.White)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("vẽ png trắng: %v", err)
	}
	return buf.Bytes()
}

func TestOCRDisabled(t *testing.T) {
	_, err := OCRImage(context.Background(), OCRConfig{}, "uploads/a.png", strings.NewReader("x"))
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("ocr tắt phải báo unsupported, got %v", err)
	}
}

// Engine thật đọc ảnh trắng → lỗi "không ra chữ" (không phải lỗi exec).
func TestOCRBlankImageReal(t *testing.T) {
	bin := tesseractPath(t)
	text, err := OCRImage(context.Background(),
		OCRConfig{Enabled: true, Binary: bin, Lang: "vie"},
		"uploads/blank.png", bytes.NewReader(whitePNG(t)))
	if err == nil || !strings.Contains(err.Error(), "không ra chữ") {
		t.Fatalf("ảnh trắng phải lỗi không-ra-chữ, got text=%q err=%v", text, err)
	}
}

// Trang PDF trắng + fake renderer trả ảnh trắng + engine thật → trang skip
// êm (không fail file), chứng minh đường render→OCR được gọi.
func TestPDFBlankPageRenderFallback(t *testing.T) {
	bin := tesseractPath(t)
	old := pdfPageRenderer
	defer func() { pdfPageRenderer = old }()
	var rendered []int
	pdfPageRenderer = func(ctx context.Context, binary, pdfPath string, page, dpi int) ([]byte, error) {
		rendered = append(rendered, page)
		return whitePNG(t), nil
	}

	raw := buildPDF(t, " ")
	pages, err := collectPages("uploads/scan.pdf", bytes.NewReader(raw),
		OCRConfig{Enabled: true, Binary: bin, Lang: "vie", RenderBinary: popplerPdftoppm(t), RenderDPI: 300})
	if err != nil {
		t.Fatalf("trang scan ocr hỏng phải skip êm, got err %v", err)
	}
	if len(pages) != 1 || pages[0].Text != "" {
		t.Fatalf("trang scan phải rỗng, got %+v", pages)
	}
	if len(rendered) != 1 || rendered[0] != 1 {
		t.Fatalf("render phải được gọi đúng trang 1, got %v", rendered)
	}
}

// OCR tắt → trang trắng skip luôn, không gọi render (dù đã có pdftoppm).
func TestPDFBlankPageSkipsWithoutOCR(t *testing.T) {
	old := pdfPageRenderer
	defer func() { pdfPageRenderer = old }()
	called := false
	pdfPageRenderer = func(ctx context.Context, binary, pdfPath string, page, dpi int) ([]byte, error) {
		called = true
		return nil, nil
	}

	raw := buildPDF(t, " ")
	pages, err := collectPages("uploads/scan.pdf", bytes.NewReader(raw),
		OCRConfig{Enabled: false, RenderBinary: popplerPdftoppm(t)})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(pages) != 1 || pages[0].Text != "" {
		t.Fatalf("trang trắng phải rỗng, got %+v", pages)
	}
	if called {
		t.Fatal("OCR tắt thì không được gọi renderer")
	}
}
