package services

import (
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/common/extractor"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/controller/dtos"
)

// IChatAttachmentService trích text của tệp người dùng gửi kèm MỘT lượt hỏi.
// Tệp đi THẲNG vào prompt của lượt, không vào kho tri thức (không chunk/embed,
// không sống sang lượt sau) — nên câu trả lời có ngay, không phụ thuộc worker
// và Qdrant. Đây là chủ ý: hỏi nhanh nội dung tệp khác với nạp tài liệu.
type IChatAttachmentService interface {
	Extract(ctx context.Context, filename string, data io.Reader) dtos.Result[*dtos.ChatAttachment]
}

type chatAttachmentService struct{}

func NewChatAttachmentService() IChatAttachmentService {
	return &chatAttachmentService{}
}

// Extract đọc text thô của tệp bằng registry extractor hiện có (cùng engine với
// pipeline nạp tài liệu) rồi cắt trần ký tự. OCR không bật ở API nên PDF scan
// ra rỗng → báo lỗi rõ, không trả tệp rỗng cho model.
func (s *chatAttachmentService) Extract(ctx context.Context, filename string, data io.Reader) dtos.Result[*dtos.ChatAttachment] {
	name := attachmentName(filename)
	if !extractor.Supports(name) {
		return dtos.Fail[*dtos.ChatAttachment](errors.NewCustomHttpError(
			http.StatusBadRequest, errors.BadRequest,
			fmt.Sprintf("chưa đọc được tệp %q — hỗ trợ: %s", name, strings.Join(extractor.SupportedExts(), ", ")),
		))
	}
	pages, err := extractor.ExtractAll(ctx, name, data, extractor.OCRConfig{})
	if err != nil {
		return dtos.Fail[*dtos.ChatAttachment](attachmentError(name, err))
	}
	text := clipAttachmentText(pagesText(pages))
	if text == "" {
		return dtos.Fail[*dtos.ChatAttachment](errors.NewCustomHttpError(
			http.StatusBadRequest, errors.BadRequest,
			"tệp không có chữ đọc được (PDF scan cần OCR — hãy nạp vào kho tri thức)",
		))
	}
	return dtos.Ok(&dtos.ChatAttachment{Name: name, Text: text, Chars: len([]rune(text))})
}

// attachmentName lấy tên hiển thị: bỏ đường dẫn client gửi, bỏ xuống dòng (tên
// có newline sẽ phá khối đính kèm trong prompt), chặn độ dài như kho tri thức.
func attachmentName(filename string) string {
	name := filepath.Base(strings.ReplaceAll(filename, "\\", "/"))
	name = strings.TrimSpace(strings.ReplaceAll(name, "\n", " "))
	if name == "" || name == "." {
		return "tep-dinh-kem"
	}
	if rs := []rune(name); len(rs) > constants.MAX_FILENAME_LEN {
		return string(rs[:constants.MAX_FILENAME_LEN])
	}
	return name
}

// pagesText ghép các trang thành khối text liền mạch; trang rỗng bị bỏ để
// không đẩy khoảng trắng vô nghĩa vào prompt.
func pagesText(pages []extractor.Page) string {
	var b strings.Builder
	for _, page := range pages {
		text := strings.TrimSpace(page.Text)
		if text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(text)
	}
	return b.String()
}

// clipAttachmentText cắt theo rune (không xé ký tự Việt) đúng trần prompt.
func clipAttachmentText(text string) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= constants.CHAT_ATTACHMENT_MAX_CHARS {
		return text
	}
	return string(runes[:constants.CHAT_ATTACHMENT_MAX_CHARS]) + "…"
}

// attachmentError chỉ lộ lý do an toàn (không hỗ trợ / tệp hỏng); lỗi khác —
// có thể chứa chi tiết thư viện — chỉ vào log, client nhận câu chung.
func attachmentError(name string, err error) error {
	logs.Warnw("chat: trích tệp đính kèm thất bại",
		"ext", strings.ToLower(filepath.Ext(name)), "error", err)
	switch {
	case stderrors.Is(err, extractor.ErrUnsupported):
		return errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest,
			"loại tệp này chưa đọc được")
	case stderrors.Is(err, extractor.ErrInvalid):
		return errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest,
			"tệp hỏng hoặc không có nội dung đọc được")
	default:
		return errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest,
			"không đọc được tệp đính kèm")
	}
}
