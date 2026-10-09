package extractor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"knowledge_ingestion/src/common/utils"
)

const (
	// maxPlainTextBytes trần text trích ra mỗi file — 20MB ≈ 5 triệu rune
	// ≈ 5000 chunk, quá là truncate (log ở service) chứ không OOM worker.
	// File text thật vượt ngưỡng này gần như luôn là dump máy, không phải
	// tài liệu tri thức.
	maxPlainTextBytes = 20 << 20
	// maxDocxArchiveBytes trần bytes zip đọc vào RAM trước khi giải nén —
	// docx nén tốt nên 100MB zip đã là tài liệu khổng lồ.
	maxDocxArchiveBytes = 100 << 20
)

// ErrUnsupported báo file đúng nhưng loại chưa trích được (ảnh, zip...)
// — service gặp lỗi này thì log + bỏ qua, kỳ sau vẫn thử lại (đợt sau hỗ
// trợ thêm loại nào thì tự ăn theo, không sửa service). So bằng errors.Is.
var ErrUnsupported = errors.New("loại file chưa hỗ trợ trích text")

// ErrInvalid báo file hỏng/xác định (docx/pdf lỗi cấu trúc, quá lớn, ảnh
// trắng không chữ) — blob immutable (key UUID, upload xong không đổi) nên thử
// lại cũng vậy. Khác lỗi transient (S3, DB, ollama, timeout, binary crash) là
// hạ tầng chập chờn, retry có ý nghĩa.
var ErrInvalid = errors.New("file không hợp lệ hoặc không trích được nội dung")

// IsPermanent báo lỗi có thử lại cũng vậy không — service gặp thì failed luôn
// thay vì đốt hết utils.MaxQueueAttempts rồi mới failed.
func IsPermanent(err error) bool {
	return errors.Is(err, ErrUnsupported) || errors.Is(err, ErrInvalid)
}

// Page là 1 đơn vị tiền xử lý — PDF là 1 trang giấy thật (Num từ 1, đúng thứ
// tự file), còn txt/docx không có khái niệm trang nên gom cả file thành 1
// Page Num 0. Service cắt chunk theo từng Page nên chunk không bao giờ tràn
// qua 2 trang, mỗi chunk nhớ được mình thuộc trang nào (trích dẫn chính xác).
type Page struct {
	Num  int
	Text string
}

// ExtractPages trích text theo đơn vị Page — entry duy nhất phase 1 của
// worker chunk gọi. PDF bóc từng trang (xem trang có gì rồi mới chunk),
// txt/md/csv/json/docx gom 1 page, ảnh (jpg/png/webp) chạy OCR Tesseract,
// loại khác trả ErrUnsupported. key là storage key do server sinh (giữ đuôi
// gốc) nên đuôi là nguồn thật.
//
// ctx chỉ để tôn trọng hủy — quá trình toàn CPU/RAM cục bộ, không I/O mạng
// (trừ OCR gọi binary local).
func ExtractPages(ctx context.Context, key string, r io.Reader, ocr OCRConfig) ([]Page, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch strings.ToLower(utils.FileExt(key)) {
	case ".txt", ".md", ".csv", ".json":
		text, err := plainText(r)
		if err != nil {
			return nil, err
		}
		return []Page{{Num: 0, Text: text}}, nil
	case ".docx":
		text, err := extractDocx(r)
		if err != nil {
			return nil, err
		}
		return []Page{{Num: 0, Text: text}}, nil
	case ".pdf":
		return extractPDFPages(ctx, r, ocr)
	case ".jpg", ".jpeg", ".png", ".webp":
		text, err := OCRImage(ctx, ocr, key, r)
		if err != nil {
			return nil, err
		}
		return []Page{{Num: 0, Text: text}}, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupported, utils.FileExt(key))
	}
}

// plainText đọc text thuần, vượt trần thì cắt bớt (cắt đúng biên UTF-8).
func plainText(r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxPlainTextBytes+1))
	if err != nil {
		return "", fmt.Errorf("đọc file text: %w", err)
	}
	if len(data) > maxPlainTextBytes {
		data = data[:maxPlainTextBytes]
		for !utf8.Valid(data) {
			data = data[:len(data)-1]
		}
	}
	return string(data), nil
}
