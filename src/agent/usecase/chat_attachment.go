package usecase

import (
	"fmt"
	"strings"

	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/controller/dtos"
)

// Khối đính kèm được chèn vào ĐẦU ngữ cảnh của lượt (sau câu hỏi) — model đọc
// như tài liệu người dùng gửi kèm, không phải tài liệu trong kho. Ranh giới
// <<< >>> giữ nội dung tệp tách khỏi lời dặn của hệ thống; câu dặn cuối nhắc
// rằng tệp KHÔNG có cites [^n] để model không bịa số trích dẫn cho nó.
const (
	attachmentHeader = "\n\n---\nTỆP ĐÍNH KÈM: %s (người dùng gửi cho lượt này, không nằm trong kho tri thức)\n<<<\n"
	attachmentFooter = "\n>>>\n(Trả lời dựa trên tệp khi câu hỏi liên quan tới tệp; không gắn trích dẫn [^n] cho nội dung tệp vì tệp không có trong kho.)"
)

// withAttachment ghép tệp đính kèm vào message gửi agent. Chỉ dùng cho lượt
// hiện tại: bản lưu phiên giữ câu hỏi gốc, nên ngữ cảnh tệp không phình DB và
// không lặng lẽ theo sang lượt sau.
//
// Cắt trần ở đây là chốt chặn thật (không tin client): DTO không giới hạn độ
// dài text để FE gửi đúng tệp đã trích, còn prompt chỉ nhận tối đa
// constants.CHAT_ATTACHMENT_MAX_CHARS ký tự.
func withAttachment(question string, attachment *dtos.ChatAttachment) string {
	if attachment == nil {
		return question
	}
	text := strings.TrimSpace(attachment.Text)
	if runes := []rune(text); len(runes) > constants.CHAT_ATTACHMENT_MAX_CHARS {
		text = string(runes[:constants.CHAT_ATTACHMENT_MAX_CHARS]) + "…"
	}
	if text == "" {
		return question
	}
	name := safeAttachmentName(attachment.Name)
	return question + fmt.Sprintf(attachmentHeader, name) + text + attachmentFooter
}

// safeAttachmentName chặn tên phá khối đính kèm (xuống dòng) và cắt độ dài —
// tên do client gửi nên không tin; tên rỗng vẫn có nhãn chung.
func safeAttachmentName(name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\n", " "))
	if name == "" {
		name = "tệp đính kèm"
	}
	if runes := []rune(name); len(runes) > constants.MAX_FILENAME_LEN {
		name = string(runes[:constants.MAX_FILENAME_LEN])
	}
	return name
}
