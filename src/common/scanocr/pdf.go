package scanocr

// File này là phần "đo đạc PDF" của tính năng tab Quét: hỏi một PDF có bao nhiêu
// trang, mỗi trang bao nhiêu điểm, xoay bao nhiêu độ, và ĐÃ có chữ sẵn chưa — rồi
// render từng trang thành ảnh đưa vào Engine.
//
// Vì sao dùng poppler thay vì thư viện Go: đây là đường đã kiểm chứng được trên
// máy (configs/config.json trỏ thẳng pdftoppm/pdftotext/pdfinfo cho pipeline nạp
// kho), kết quả đúng từng pixel, và đi theo đúng một cơ chế (os/exec) nên không
// phải thêm dependency C-binding vào build.
//
// Vì sao NẰM Ở ĐÂY chứ không trong src/common/extractor: pipeline nạp kho chỉ
// cần text thô còn tính năng này cần toạ độ + rotation + ngưỡng "trang này đã
// có chữ chưa" theo TỪNG trang. Hai mục tiêu, hai bộ code — xem §0.7 của
// plans/2026-10-30-dot-31.

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Hai trần thời gian tách nhau vì hỏng một binary là hai kiểu hỏng khác nhau:
// pdfinfo/pdftotext chỉ đọc cấu trúc (rất nhanh, treo là do file lỗi hoặc trỏ
// nhầm binary), còn render phải giải mã toàn bộ trang.
const (
	inspectTimeout = 30 * time.Second
	renderTimeout  = 90 * time.Second
)

// Kích thước trang (pts) + rotation + số ký tự đã có trong text layer của MỘT
// trang. TextChars là rune đếm theo ký tự in được (không tính khoảng trắng).
type PageGeometry struct {
	Num       int
	WidthPt   float64
	HeightPt  float64
	Rotation  int
	TextChars int
}

// NeedsOCR trả lời "trang này có đáng OCR không".
//
// Ngưỡng áp theo TỪNG trang — đây là chỗ tính năng hơn autoai: autoai hỏi "cả
// file có trang nào mang chữ không" rồi bỏ qua TOÀN BỘ file, nên một PDF 1 trang
// số + 199 trang scan bị loại sạch (backend/rag/ocr_tools.py:124-136).
func (g PageGeometry) NeedsOCR(minChars int) bool { return g.TextChars < minChars }

// ErrEncryptedPDF: PDF có mật khẩu chủ. poppler từ chối đọc toàn bộ văn bản khi
// chưa có password, nên trang sẽ hiện ra "không có chữ" và bị OCR — trong khi
// thực tế máy không mở nổi file. Báo rõ ràng chứ không âm thầm sinh PDF rỗng.
var ErrEncryptedPDF = errors.New("scanocr: PDF được bảo vệ bằng mật khẩu")

// pdfDocInfo là phần tóm tắt pdfinfo in ra trước các dòng theo trang.
type pdfDocInfo struct {
	pages     int
	widthPt   float64
	heightPt  float64
	rotation  int
	encrypted bool
}

// InspectPDF đọc hình dạng từng trang + đo text layer sẵn có của chúng.
//
// Ba lệnh poppler (pdfinfo tóm tắt, pdfinfo theo trang, pdftotext toàn văn) là
// ba process nhưng chỉ chạy MỘT LẦN lúc dựng job — đổi lại là không phải parse
// PDF bằng tay để tìm /Rotate hay /XObject.
//
// Kết quả trả theo thứ tự trang 1..N. trang nào pdftotext không in ra đoạn nào
// thì TextChars = 0 (tức "trang scan") chứ không phải lỗi.
func InspectPDF(ctx context.Context, cfg Config, path string) ([]PageGeometry, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("scanocr: InspectPDF thiếu đường dẫn file")
	}
	summary, err := inspectSummary(ctx, cfg, path)
	if err != nil {
		return nil, err
	}
	if summary.encrypted {
		return nil, ErrEncryptedPDF
	}
	sizes, err := inspectPages(ctx, cfg, path, summary.pages)
	if err != nil {
		return nil, err
	}
	chars, err := textLayerChars(ctx, cfg, path, summary.pages)
	if err != nil {
		return nil, err
	}
	pages := make([]PageGeometry, 0, summary.pages)
	for i := 1; i <= summary.pages; i++ {
		g := PageGeometry{Num: i, WidthPt: summary.widthPt, HeightPt: summary.heightPt,
			Rotation: summary.rotation, TextChars: chars[i-1]}
		// pdfinfo in số đo riêng cho từng trang khi có -f/-l; file không đều
		// (trang dọc xen trang ngang) thì dùng đúng số của trang đó.
		if s, ok := sizes[i]; ok {
			g.WidthPt, g.HeightPt, g.Rotation = s.widthPt, s.heightPt, s.rotation
		}
		pages = append(pages, g)
	}
	return pages, nil
}

// inspectSummary lấy số trang + kích thước danh định + cờ mật khẩu.
func inspectSummary(ctx context.Context, cfg Config, path string) (pdfDocInfo, error) {
	out, err := runPoppler(ctx, cfg, "pdfinfo", path)
	if err != nil {
		return pdfDocInfo{}, err
	}
	info, ok := parsePDFInfoSummary(string(out))
	if !ok {
		return pdfDocInfo{}, fmt.Errorf("scanocr: không đọc được số trang từ pdfinfo (%s)", firstLine(string(out)))
	}
	return info, nil
}

// inspectPages đọc số đo RIÊNG từng trang. Trả map rỗng nếu pdfinfo bản này
// không in dòng nào (khi đó InspectPDF dùng số danh định) — không phải lỗi.
func inspectPages(ctx context.Context, cfg Config, path string, pages int) (map[int]pageSizeInfo, error) {
	if pages <= 0 {
		return nil, nil
	}
	out, err := runPoppler(ctx, cfg, "pdfinfo", "-f", "1", "-l", strconv.Itoa(pages), path)
	if err != nil {
		return nil, err
	}
	return parsePDFInfoPages(string(out)), nil
}

// textLayerChars đếm chữ sẵn có theo trang bằng ĐÚNG MỘT lệnh pdftotext.
//
// pdftotext ngăn cách trang bằng ký tự form-feed \f, nên cắt chuỗi theo \f rẻ
// hơn nhiều so với gọi pdftotext -f N -l N cho từng trang (200 trang = 200
// process khởi động). Segment thừa cuối chuỗi (do \f kết thúc file) bị bỏ.
func textLayerChars(ctx context.Context, cfg Config, path string, pages int) ([]int, error) {
	if pages <= 0 {
		return nil, nil
	}
	out, err := runPoppler(ctx, cfg, "pdftotext", "-enc", "UTF-8", path, "-")
	if err != nil {
		return nil, err
	}
	return countCharsPerPage(string(out), pages), nil
}

// runPoppler chạy một lệnh poppler và trả stdout.
//
// Binary suy ra từ scan_ocr.render_binary: pdftoppm, pdftotext và pdfinfo nằm
// cùng thư mục trong mọi bản poppler (kể cả bản Windows trong config hiện tại),
// nên người vận hành chỉ khai MỘT đường dẫn. Không có thư mục kèm theo (máy Linux
// cài hệ thống) thì tin vào PATH.
func runPoppler(ctx context.Context, cfg Config, tool string, args ...string) ([]byte, error) {
	bin := popplerTool(cfg.RenderBinary, tool)
	tctx, cancel := context.WithTimeout(ctx, inspectTimeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(tctx, bin, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if tctx.Err() != nil {
			return nil, fmt.Errorf("%s quá %v: %w", tool, inspectTimeout, tctx.Err())
		}
		return nil, fmt.Errorf("%s lỗi: %w (%s)", tool, err, firstLine(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// popplerTool chỉ vào binary cùng thư mục với renderer.
//
// Đuôi .exe nhân theo render_binary chứ không theo runtime.GOOS: trên Windows
// người dùng khai "…\pdftoppm.exe" nên mọi anh em cùng thư mục cũng phải mang
// đuôi đó (Go không tự thêm .exe khi đường dẫn tuyệt đối có đuôi).
func popplerTool(renderBinary, tool string) string {
	renderBinary = strings.TrimSpace(renderBinary)
	if renderBinary == "" {
		return tool
	}
	name := tool
	if strings.EqualFold(filepath.Ext(renderBinary), ".exe") {
		name += ".exe"
	}
	return filepath.Join(filepath.Dir(renderBinary), name)
}

// RenderOptions tả ảnh render ra cho một trang.
type RenderOptions struct {
	// DPI là độ phân giải. 300 cho OCR (điểm ngọt Tesseract), 150 cho ảnh FE vẽ
	// vùng — box đã chuẩn hoá 0..1 nên hai ảnh dùng chung một bộ toạ độ.
	DPI int
	// JPEG nén ảnh để giảm băng thông (PNG 300DPI một trang A4 đo được ~0,9MB).
	JPEG        bool
	JPEGQuality int
}

// RenderPage đổi một trang PDF thành bytes ảnh, dùng đĩa tạm rồi xoá ngay bằng
// defer nên không để lại bản scan của người dùng trên máy. Lỗi process/lỗi đĩa
// trả kèm stderr dòng đầu để worker ghi đúng nguyên nhân vào row của trang.
//
// Ảnh đã render LUÔN mang rotation thật của trang (pdftoppm áp /Rotate trước khi
// in ra pixel), nên box của Tesseract và ảnh hiển thị luôn khớp nhau — tính năng
// này dựng PDF lại TỪ ẢNH nên không cần nhân tiện đảo lại toạ độ như viewer.
func RenderPage(ctx context.Context, cfg Config, pdfPath string, pageNum int, opts RenderOptions) ([]byte, error) {
	if pageNum < 1 {
		return nil, fmt.Errorf("scanocr: số trang phải >= 1, nhận %d", pageNum)
	}
	dpi := opts.DPI
	if dpi <= 0 {
		dpi = cfg.RenderDPI
	}
	if dpi <= 0 {
		dpi = 300
	}
	dir, err := os.MkdirTemp("", "scanocr-render-*")
	if err != nil {
		return nil, fmt.Errorf("scanocr: tạo thư mục tạm render: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	base := filepath.Join(dir, "page")
	args := []string{formatFlag(opts.JPEG), "-r", strconv.Itoa(dpi),
		"-f", strconv.Itoa(pageNum), "-l", strconv.Itoa(pageNum), "-singlefile"}
	if opts.JPEG && opts.JPEGQuality > 0 {
		args = append(args, "-jpegopt", "quality="+strconv.Itoa(clampQuality(opts.JPEGQuality)))
	}
	args = append(args, pdfPath, base)

	tctx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()
	var stderr bytes.Buffer
	cmd := exec.CommandContext(tctx, popplerTool(cfg.RenderBinary, "pdftoppm"), args...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if tctx.Err() != nil {
			return nil, fmt.Errorf("scanocr: render trang %d quá %v: %w", pageNum, renderTimeout, tctx.Err())
		}
		return nil, fmt.Errorf("scanocr: render trang %d lỗi: %w (%s)", pageNum, err, firstLine(stderr.String()))
	}
	data, err := os.ReadFile(base + imageExt(opts.JPEG))
	if err != nil {
		return nil, fmt.Errorf("scanocr: pdftoppm không ghi ra ảnh trang %d: %w", pageNum, err)
	}
	return data, nil
}

func formatFlag(jpeg bool) string {
	if jpeg {
		return "-jpeg"
	}
	return "-png"
}

func imageExt(jpeg bool) string {
	if jpeg {
		return ".jpg"
	}
	return ".png"
}

// clampQuality giữ chất lượng JPEG trong khoảng poppler nhận (0..100).
func clampQuality(q int) int {
	if q < 1 {
		return 1
	}
	if q > 100 {
		return 100
	}
	return q
}

// parsePDFInfoSummary đọc các dòng "Pages:", "Page size:", "Page rot:",
// "Encrypted:" từ output pdfinfo không tham số.
func parsePDFInfoSummary(out string) (pdfDocInfo, bool) {
	var info pdfDocInfo
	var havePages bool
	for _, line := range strings.Split(out, "\n") {
		key, val, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch key {
		case "Pages":
			n, err := strconv.Atoi(val)
			if err == nil && n > 0 {
				info.pages, havePages = n, true
			}
		case "Encrypted":
			info.encrypted = strings.HasPrefix(strings.ToLower(val), "yes")
		case "Page size":
			info.widthPt, info.heightPt = parseSize(val)
		case "Page rot":
			info.rotation, _ = strconv.Atoi(val)
		}
	}
	return info, havePages
}

// pageSizeInfo là số đo RIÊNG một trang, chỉ dùng trong lúc đọc pdfinfo.
type pageSizeInfo struct {
	num      int
	widthPt  float64
	heightPt float64
	rotation int
}

// parsePDFInfoPages đọc các dòng theo trang:
//
//	Page    1 size:  595.276 x 841.89 pts (A4)
//	Page    1 rot:   0
//
// Trả map rỗng khi không có dòng nào để caller rơi về số danh định.
func parsePDFInfoPages(out string) map[int]pageSizeInfo {
	pages := map[int]pageSizeInfo{}
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 4 || f[0] != "Page" {
			continue
		}
		num, err := strconv.Atoi(f[1])
		if err != nil || num < 1 {
			continue
		}
		p := pages[num]
		p.num = num
		switch strings.TrimSuffix(f[2], ":") {
		case "size":
			p.widthPt, p.heightPt = parseSize(strings.Join(f[3:], " "))
		case "rot":
			p.rotation, _ = strconv.Atoi(f[3])
		default:
			continue
		}
		pages[num] = p
	}
	return pages
}

// parseSize đọc "595.276 x 841.89 pts (A4)" — poppler luôn in đơn vị pts trước
// tên giấy trong ngoặc, nhưng để an toàn vẫn bỏ mọi phần chữ phía sau.
func parseSize(s string) (w, h float64) {
	s = strings.ReplaceAll(s, " x ", "x")
	if i := strings.IndexByte(s, '('); i >= 0 {
		s = s[:i]
	}
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == 'x' || r == ' ' || r == 'X' })
	if len(parts) < 2 {
		return 0, 0
	}
	w, _ = strconv.ParseFloat(parts[0], 64)
	h, _ = strconv.ParseFloat(parts[1], 64)
	return w, h
}

// countCharsPerPage cắt output pdftotext theo form-feed thành N trang rồi đếm
// ký tự in được. Segment cuối sau \f kết thúc thường rỗng nên chỉ lấy N trang.
func countCharsPerPage(text string, pages int) []int {
	counts := make([]int, pages)
	if pages <= 0 {
		return counts
	}
	segments := strings.Split(text, "\f")
	for i := 0; i < pages && i < len(segments); i++ {
		counts[i] = countPrintableRunes(segments[i])
	}
	return counts
}

// countPrintableRunes đếm rune nhìn thấy được (bỏ khoảng trắng), theo RUNE chứ
// không byte: tiếng Việt 2 byte/ký tự nên đếm byte sẽ nâng khống text layer và
// bỏ sót trang cần OCR.
func countPrintableRunes(s string) int {
	n := 0
	for _, r := range s {
		if !unicode.IsSpace(r) {
			n++
		}
	}
	return n
}
