package extractor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"knowledge_ingestion/src/common/utils"
)

const (
	// maxOCRBytes trần bytes ảnh đọc vào RAM mỗi lần OCR — ảnh upload tối đa
	// 500MB nhưng OCR ảnh đó vừa chậm vừa tốn RAM, 50MB là đủ cho scan A4
	// 300DPI. Vượt thì lỗi để service retry/backoff rồi failed, không OOM.
	maxOCRBytes = 50 << 20
	// defaultOCRTimeout khi service quên set — engine local vài giây/trang là
	// cùng, 120s là trần an toàn.
	defaultOCRTimeout = 120 * time.Second
)

// OCRConfig giữ công tắc + đường dẫn engine Tesseract — service build từ
// config.GetOCR() rồi truyền vào ExtractPages. Nằm ở extractor (không phải
// config) để package này không phụ thuộc config. Struct này cũng là contract
// nếu sau này tách OCR thành API riêng / tool cho agent (input bytes → text).
type OCRConfig struct {
	Enabled bool
	Binary  string
	Lang    string
	Timeout time.Duration
	// RenderBinary renderer PDF→ảnh cho trang scan (vd "pdftoppm") — rỗng thì
	// trang scan bỏ qua. RenderDPI <= 0 thì dùng 300.
	RenderBinary string
	RenderDPI    int
	// MinTextChars là ngưỡng "text layer mỏng" để vẫn chạy OCR. MẶC ĐỊNH 0 =
	// TẮT, và đó là chủ ý: đa số PDF không phải ảnh, OCR mỗi trang tốn ~2s
	// render + OCR nên bật rộng sẽ thành cả giờ máy cho vài trang có ích.
	// Bật (vd 150) chỉ để bắt trang export từ Word có text layer (tiêu đề, số
	// hiệu chữ thật) nhưng phần lớn nội dung là ảnh scan — dạng đó text KHÔNG
	// rỗng nên nếu chỉ OCR trang rỗng thì mất sạch phần ảnh.
	// Đếm theo RUNE (tiếng Việt 2 byte/ký tự). Càng cao càng tiên chọn, đắt
	// hơn là mọi trang đều OCR.
	MinTextChars int
	// PSM là Page Segmentation Mode truyền cho Tesseract — 6 = một khối chữ
	// đồng nhất (thân bản), 3 = tự dò bố cục (bảng, cột). <= 0 thì dùng 6.
	PSM int
}

// needsOCR báo trang này có đáng render để OCR không.
//
// ĐÂY LÀ TOÀN BỘ QUY TẮC ĐIỀU HƯỚNG, giữ cho rẻ và dễ đoán:
//   - Trang không có chữ nào → chắc chắn là trang scan, OCR.
//   - Trang đã có chữ → KHÔNG OCR, trừ khi bật MinTextChars và chữ quá mỏng.
//
// Không dò "trang có ảnh nhúng không" vì thư viện PDF không expose cái đó —
// để biết phải tự parse /XObject trong Resources của trang, phức tạp vừa
// đắt vừa dễ sai (ảnh có thể nằm trong Form XObject, pattern, hoặc chữ được
// vector hoá thành đường nét chứ không phải ảnh). Ngưỡng MinTextChars là cách
// rẻ để người vận hành tự bật khi thấy file của mình bị thiếu phần ảnh.
func (c OCRConfig) needsOCR(text string) bool {
	r := utf8.RuneCountInString(strings.TrimSpace(text))
	if r == 0 {
		return true
	}
	return c.MinTextChars > 0 && r < c.MinTextChars
}

// OCRImage đọc chữ trong file ảnh bằng engine Tesseract — gọi trực tiếp binary
// qua os/exec (KHÔNG qua Python trung gian, đúng file _tesseract.md chốt engine
// + model "vie"). Tắt OCR thì trả ErrUnsupported để service skip như trước khi
// có OCR. Ảnh trắng/không ra chữ thì lỗi (retry/backoff rồi failed), không đánh
// done giả. PSM 6 = khối chữ đồng nhất — hợp văn bản scan nhiều dòng tiếng Việt.
func OCRImage(ctx context.Context, cfg OCRConfig, key string, r io.Reader) (string, error) {
	if !cfg.Enabled {
		return "", fmt.Errorf("%w: ocr tắt (ext %q)", ErrUnsupported, utils.FileExt(key))
	}
	if strings.TrimSpace(cfg.Binary) == "" {
		return "", fmt.Errorf("ocr bật nhưng thiếu đường dẫn binary")
	}
	data, err := io.ReadAll(io.LimitReader(r, maxOCRBytes+1))
	if err != nil {
		return "", fmt.Errorf("đọc file ảnh: %w", err)
	}
	if len(data) > maxOCRBytes {
		return "", fmt.Errorf("%w: file ảnh quá lớn (%d bytes)", ErrInvalid, len(data))
	}
	// Giữ đuôi gốc để Tesseract nhận diện định dạng (png/jpg/webp...).
	tmp, err := os.CreateTemp("", "ocr-*"+strings.ToLower(utils.FileExt(key)))
	if err != nil {
		return "", fmt.Errorf("tạo file tạm cho ocr: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("ghi file tạm cho ocr: %w", err)
	}
	_ = tmp.Close()

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultOCRTimeout
	}
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	lang := cfg.Lang
	if lang == "" {
		lang = "vie"
	}
	var stdout, stderr bytes.Buffer
	// PSM <= 0 thì dùng 6 — xem OCRConfig.PSM để biết 6 và 3 khác nhau ở
	// đâu (bảng/cột thì 3 mới giữ được thứ tự đọc).
	psm := cfg.PSM
	if psm <= 0 {
		psm = 6
	}
	cmd := exec.CommandContext(tctx, cfg.Binary, tmpName, "stdout", "-l", lang, "--psm", strconv.Itoa(psm))
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if tctx.Err() != nil {
			return "", fmt.Errorf("ocr quá %v: %w", timeout, tctx.Err())
		}
		return "", fmt.Errorf("tesseract lỗi: %w (%s)", err, firstLine(stderr.String()))
	}
	text := strings.TrimSpace(stdout.String())
	if text == "" {
		return "", fmt.Errorf("%w: ocr không ra chữ (ảnh trắng/scan mờ?)", ErrInvalid)
	}
	return text, nil
}

// firstLine lấy dòng đầu stderr (tesseract báo lỗi nhiều dòng) để log gọn.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
