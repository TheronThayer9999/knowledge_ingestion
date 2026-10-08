package extractor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ledongthuc/pdf"
)

const (
	// maxPDFBytes trần bytes PDF đọc vào RAM — bằng trần docx (PDF nén tốt
	// nên 100MB đã là tài liệu khổng lồ, vd 100 file × 500 trang cũng chỉ vài
	// chục MB mỗi file).
	maxPDFBytes = 100 << 20
	// maxPDFPages trần số trang parse mỗi file — PDF khai gian page tree để
	// treo worker thì lỗi sớm thay vì loop hàng giờ.
	maxPDFPages = 10000
)

// extractPDFPages bóc text từng trang PDF bằng ledongthuc/pdf (pure Go, không
// CGO) — tiền xử lý theo trang: xem trang có gì rồi mới chunk, chunk không
// tràn qua 2 trang. Mỗi trang lấy text theo dòng (GetTextByRow giữ ngắt dòng,
// bảng không dính thành 1 cục); trang trắng/scan thì thử render → OCR (cần
// RenderBinary, vd pdftoppm — máy chưa có thì bỏ qua); vẫn trắng thì Text rỗng
// để service skip — OCR thuần text trong ảnh, không đoán layout.
func extractPDFPages(ctx context.Context, r io.Reader, ocr OCRConfig) ([]Page, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxPDFBytes+1))
	if err != nil {
		return nil, fmt.Errorf("đọc file pdf: %w", err)
	}
	if len(raw) > maxPDFBytes {
		return nil, fmt.Errorf("%w: file pdf quá lớn (%d bytes)", ErrInvalid, len(raw))
	}
	rd, err := pdf.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("%w: file pdf không hợp lệ: %v", ErrInvalid, err)
	}
	n := rd.NumPage()
	if n <= 0 {
		return nil, fmt.Errorf("%w: pdf không có trang nào", ErrInvalid)
	}
	if n > maxPDFPages {
		return nil, fmt.Errorf("%w: pdf có %d trang, vượt trần %d", ErrInvalid, n, maxPDFPages)
	}
	// File pdf tạm cho renderer — ghi lười (chỉ khi gặp trang scan đầu tiên),
	// vì 99% trang text không cần tới.
	var pdfPath string
	defer func() {
		if pdfPath != "" {
			_ = os.Remove(pdfPath)
		}
	}()
	pdfPathForRender := func() (string, error) {
		if pdfPath != "" {
			return pdfPath, nil
		}
		f, err := os.CreateTemp("", "pdfocr-*.pdf")
		if err != nil {
			return "", err
		}
		pdfPath = f.Name()
		if _, err := f.Write(raw); err != nil {
			_ = f.Close()
			return "", err
		}
		return pdfPath, f.Close()
	}
	pages := make([]Page, 0, n)
	for i := 1; i <= n; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p := rd.Page(i)
		var text string
		if !p.V.IsNull() {
			text = pageText(p)
		}
		if text == "" && ocr.RenderBinary != "" {
			// Trang scan: render → OCR. Lỗi ở bước nào cũng chỉ giữ trang
			// trắng (skip), không fail cả file vì 1 trang.
			text = ocrScannedPage(ctx, ocr, pdfPathForRender, i)
		}
		pages = append(pages, Page{Num: i, Text: text})
	}
	return pages, nil
}

// ocrScannedPage render 1 trang scan thành PNG rồi OCR — lỗi thì "".
func ocrScannedPage(ctx context.Context, ocr OCRConfig, getPDFPath func() (string, error), page int) string {
	pdfPath, err := getPDFPath()
	if err != nil {
		return ""
	}
	dpi := ocr.RenderDPI
	if dpi <= 0 {
		dpi = 300
	}
	png, err := pdfPageRenderer(ctx, ocr.RenderBinary, pdfPath, page, dpi)
	if err != nil {
		return ""
	}
	text, err := OCRImage(ctx, ocr, "page.png", bytes.NewReader(png))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(text)
}

// pdfPageRenderer render 1 trang PDF thành PNG — package var để test inject
// fake mà không cần renderer thật. Mặc định dùng pdftoppm (chuẩn poppler):
// pdftoppm -r <dpi> -png -f <page> -l <page> in.pdf prefix → prefix-<page>.png.
var pdfPageRenderer = renderPDFPagePdftoppm

func renderPDFPagePdftoppm(ctx context.Context, binary, pdfPath string, page, dpi int) ([]byte, error) {
	dir, err := os.MkdirTemp("", "pdfrender-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	prefix := filepath.Join(dir, "page")
	cmd := exec.CommandContext(ctx, binary,
		"-r", strconv.Itoa(dpi), "-png",
		"-f", strconv.Itoa(page), "-l", strconv.Itoa(page),
		pdfPath, prefix)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("render trang %d: %w (%s)", page, err, firstLine(stderr.String()))
	}
	png, err := os.ReadFile(prefix + "-" + strconv.Itoa(page) + ".png")
	if err != nil {
		return nil, fmt.Errorf("đọc ảnh trang %d: %w", page, err)
	}
	return png, nil
}

// pageText lấy text 1 trang theo dòng — mỗi dòng là các mảnh text nối bằng
// space, các dòng nối bằng \n để chunker cắt đúng biên dòng. Trang lỗi thì
// rỗng (service skip), không fail cả file vì 1 trang hỏng.
func pageText(p pdf.Page) string {
	rows, err := p.GetTextByRow()
	if err != nil {
		return ""
	}
	var sb strings.Builder
	for _, row := range rows {
		var parts []string
		for _, t := range row.Content {
			if s := strings.TrimSpace(t.S); s != "" {
				parts = append(parts, s)
			}
		}
		if len(parts) > 0 {
			sb.WriteString(strings.Join(parts, " "))
			sb.WriteString("\n")
		}
	}
	return strings.TrimSpace(sb.String())
}
