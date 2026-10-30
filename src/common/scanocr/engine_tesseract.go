package scanocr

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg" // để DecodeConfig nhận ra JPEG (ảnh scan chụp điện thoại)
	_ "image/png"  // và PNG (pdftoppm xuất ra)
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// tesseractEngine là adapter đầu tiên của cổng Engine: gọi binary Tesseract bằng
// os/exec và đọc output dạng TSV.
//
// Vì sao TSV mà không phải "stdout" (text suông) như đường nạp kho: TSV kèm
// level/left/top/width/height/conf — đúng bộ dữ liệu UI cần để tô sáng theo từ,
// vẽ vùng và nhúng lớp chữ vô hình. Đường nạp kho không cần những thứ đó nên vẫn
// dùng lệnh text suông của extractor; hai bên đi riêng là có chủ ý (§0.7).
type tesseractEngine struct {
	cfg Config
}

func newTesseractEngine(cfg Config) *tesseractEngine {
	return &tesseractEngine{cfg: cfg.Normalize()}
}

func (e *tesseractEngine) Name() string { return EngineTesseract }

// Available kiểm tra nhanh: binary chạy được và gói ngôn ngữ đang cấu hình có
// trong tessdata.
//
// Thiếu gói ngôn ngữ là lỗi CẤU TRÚC chứ không phải "OCR kém chính xác":
// tesseract exit != 0 cho MỌI trang. Biết sớm lúc dựng job còn hơn để người dùng
// nhìn 200 trang cùng fail.
func (e *tesseractEngine) Available(ctx context.Context) error {
	langs, err := e.listLangs(ctx)
	if err != nil {
		return err
	}
	missing := MissingLangs(langs, e.cfg.Lang)
	if len(missing) > 0 {
		return fmt.Errorf("%w: thiếu gói ngôn ngữ %v", ErrEngineUnavailable, missing)
	}
	return nil
}

// Recognize chạy OCR trên một ảnh và trả các dòng + từ kèm box pixel.
func (e *tesseractEngine) Recognize(ctx context.Context, img ImageInfo, data []byte) (*Page, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: ảnh rỗng", ErrNoText)
	}
	info, err := e.resolveImage(img, data)
	if err != nil {
		return nil, err
	}
	path, err := writeTempImage(info.Extension, data)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(path) }()

	out, err := e.runTSV(ctx, path)
	if err != nil {
		return nil, err
	}
	page, err := parseTSV(out, info)
	if err != nil {
		return nil, err
	}
	page.EngineName = e.Name()
	page.DPI = info.DPI
	if len(page.Lines) == 0 {
		return page, fmt.Errorf("%w: trang %d", ErrNoText, info.Page)
	}
	return page, nil
}

// resolveImage bổ sung kích thước ảnh khi caller không biết (trường hợp người
// dùng upload thẳng ảnh JPG/PNG, không qua PDF).
func (e *tesseractEngine) resolveImage(img ImageInfo, data []byte) (ImageInfo, error) {
	out := img
	if out.Extension == "" {
		out.Extension = ".png"
	}
	if out.WidthPx > 0 && out.HeightPx > 0 {
		return out, nil
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return out, fmt.Errorf("đo kích thước ảnh OCR: %w", err)
	}
	if out.WidthPx <= 0 {
		out.WidthPx = cfg.Width
	}
	if out.HeightPx <= 0 {
		out.HeightPx = cfg.Height
	}
	if out.DPI <= 0 {
		// Không có metadata DPI thì coi là ảnh A4 @300DPI để tỉ lệ vẫn đúng —
		// box lưu theo tỉ lệ nên con số này chỉ đi kèm để người vận hành đọc log.
		out.DPI = e.cfg.RenderDPI
	}
	return out, nil
}

// writeTempImage ghi ảnh ra file tạm vì Tesseract chỉ nhận đường dẫn, không nhận
// stdin cho ảnh.
func writeTempImage(ext string, data []byte) (string, error) {
	if ext == "" {
		ext = ".png"
	}
	tmp, err := os.CreateTemp("", "scanocr-*"+strings.ToLower(ext))
	if err != nil {
		return "", fmt.Errorf("tạo file tạm cho OCR: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("ghi file tạm cho OCR: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("đóng file tạm cho OCR: %w", err)
	}
	return tmp.Name(), nil
}

// runTSV gọi `tesseract <anh> stdout -l <lang> --psm <n> tsv`.
func (e *tesseractEngine) runTSV(ctx context.Context, path string) (string, error) {
	timeout := e.cfg.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	args := []string{path, "stdout", "-l", e.cfg.Lang, "--psm", strconv.Itoa(e.cfg.PSM), "tsv"}
	cmd := exec.CommandContext(tctx, e.cfg.Binary, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if tctx.Err() != nil {
			return "", fmt.Errorf("ocr trang quá %v: %w", timeout, tctx.Err())
		}
		return "", fmt.Errorf("tesseract lỗi: %w (%s)", err, firstLine(stderr.String()))
	}
	return stdout.String(), nil
}

// listLangs hỏi `tesseract --list-langs` — timeout ngắn vì đây chỉ là lệnh hỏi.
func (e *tesseractEngine) listLangs(ctx context.Context) ([]string, error) {
	if strings.TrimSpace(e.cfg.Binary) == "" {
		return nil, fmt.Errorf("%w: thiếu đường dẫn binary", ErrEngineUnavailable)
	}
	tctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// CombinedOutput: tuỳ bản Tesseract, tiêu đề có thể ra stderr — parseLangs
	// lọc theo hình dạng dòng nên nhận cả hai luồng là an toàn nhất.
	out, err := exec.CommandContext(tctx, e.cfg.Binary, "--list-langs").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: tesseract --list-langs: %v (%s)",
			ErrEngineUnavailable, err, firstLine(string(out)))
	}
	return parseLangs(string(out)), nil
}

// parseLangs tách danh sách ngôn ngữ khỏi output --list-langs. Mỗi ngôn ngữ là
// một token đứng riêng một dòng; dòng tiêu đề có khoảng trắng/dấu hai chấm nên
// bị loại.
func parseLangs(out string) []string {
	var langs []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.ContainsAny(line, " :\t") {
			continue
		}
		langs = append(langs, line)
	}
	return langs
}

// MissingLangs trả những gói trong chuỗi config ("vie+eng") mà engine KHÔNG có.
// Hàm thuần, test offline được.
func MissingLangs(available []string, lang string) []string {
	have := make(map[string]bool, len(available))
	for _, l := range available {
		have[strings.ToLower(strings.TrimSpace(l))] = true
	}
	var missing []string
	seen := map[string]bool{}
	for _, l := range strings.Split(lang, "+") {
		l = strings.ToLower(strings.TrimSpace(l))
		if l == "" || have[l] || seen[l] {
			continue
		}
		seen[l] = true
		missing = append(missing, l)
	}
	return missing
}

// firstLine lấy dòng đầu của stderr để log gọn (Tesseract báo lỗi nhiều dòng).
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
