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
	pages, err := ExtractPages(context.Background(), "uploads/scan.pdf", bytes.NewReader(raw),
		OCRConfig{Enabled: true, Binary: bin, Lang: "vie", RenderBinary: "fake-pdftoppm", RenderDPI: 300})
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

// Không cấu hình renderer → trang trắng skip luôn, không gọi render.
func TestPDFBlankPageSkipsWithoutRenderer(t *testing.T) {
	old := pdfPageRenderer
	defer func() { pdfPageRenderer = old }()
	called := false
	pdfPageRenderer = func(ctx context.Context, binary, pdfPath string, page, dpi int) ([]byte, error) {
		called = true
		return nil, nil
	}

	raw := buildPDF(t, " ")
	pages, err := ExtractPages(context.Background(), "uploads/scan.pdf", bytes.NewReader(raw), OCRConfig{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(pages) != 1 || pages[0].Text != "" {
		t.Fatalf("trang trắng phải rỗng, got %+v", pages)
	}
	if called {
		t.Fatal("không cấu hình render thì không được gọi renderer")
	}
}
