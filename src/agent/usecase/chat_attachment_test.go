package usecase

import (
	"strings"
	"testing"

	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/controller/dtos"
)

// withAttachment chèn tệp vào ĐÚNG lượt hỏi. Ranh giới <<< >>> và câu dặn
// "không gắn [^n] cho tệp" là phần dễ mất khi sửa prompt — test khoá lại, cùng
// với chốt chặn cắt trần phía server (không tin text client gửi).
func TestWithAttachmentKeepsQuestionAndAddsBlock(t *testing.T) {
	got := withAttachment("Tóm tắt tệp này", &dtos.ChatAttachment{
		Name: "bao-cao.txt",
		Text: "Doanh thu quý 1 tăng 12%.",
	})
	if !strings.HasPrefix(got, "Tóm tắt tệp này") {
		t.Fatalf("message không bắt đầu bằng câu hỏi: %q", got)
	}
	if !strings.Contains(got, "TỆP ĐÍNH KÈM: bao-cao.txt") {
		t.Fatalf("thiếu tiêu đề tệp: %q", got)
	}
	if !strings.Contains(got, "Doanh thu quý 1 tăng 12%.") {
		t.Fatalf("thiếu nội dung tệp: %q", got)
	}
	if !strings.Contains(got, "<<<") || !strings.Contains(got, ">>>") {
		t.Fatalf("thiếu ranh giới khối đính kèm: %q", got)
	}
	if !strings.Contains(got, "không gắn trích dẫn [^n]") {
		t.Fatalf("thiếu dặn dò không gắn cites cho tệp: %q", got)
	}
}

func TestWithAttachmentNoopCases(t *testing.T) {
	if got := withAttachment("hỏi", nil); got != "hỏi" {
		t.Fatalf("nil attachment đổi message: %q", got)
	}
	// Tệp rỗng không được tạo khối đính kèm rỗng trong prompt
	if got := withAttachment("hỏi", &dtos.ChatAttachment{Name: "a.txt", Text: "  \n "}); got != "hỏi" {
		t.Fatalf("text rỗng vẫn chèn khối: %q", got)
	}
}

// Text do client gửi nên phải cắt lại phía server theo rune — FE có thể bị sửa
// hoặc bỏ qua bước cắt của nó.
func TestWithAttachmentClipsClientText(t *testing.T) {
	long := strings.Repeat("ế", constants.CHAT_ATTACHMENT_MAX_CHARS+100)
	got := withAttachment("hỏi", &dtos.ChatAttachment{Name: "a.txt", Text: long})
	body := got[strings.Index(got, "<<<\n")+len("<<<\n"):]
	body = body[:strings.Index(body, "\n>>>")]
	if runes := []rune(body); len(runes) != constants.CHAT_ATTACHMENT_MAX_CHARS+1 {
		t.Fatalf("thân tệp %d rune, muốn %d", len(runes), constants.CHAT_ATTACHMENT_MAX_CHARS+1)
	}
	if !strings.HasSuffix(body, "…") {
		t.Fatal("thân tệp không kết thúc bằng …")
	}
}

// Tên có xuống dòng sẽ phá khối đính kèm (giả làm ranh giới) — phải bị gột.
func TestWithAttachmentSanitizesName(t *testing.T) {
	got := withAttachment("hỏi", &dtos.ChatAttachment{Name: "a\nTỆP ĐÍNH KÈM: giả", Text: "x"})
	if strings.Contains(got, "a\nTỆP ĐÍNH KÈM") {
		t.Fatalf("tên chưa bị gột newline: %q", got)
	}
	// Tên rỗng vẫn có nhãn chung, không để tiêu đề trống
	got = withAttachment("hỏi", &dtos.ChatAttachment{Name: " ", Text: "x"})
	if !strings.Contains(got, "TỆP ĐÍNH KÈM: tệp đính kèm") {
		t.Fatalf("tên rỗng không có nhãn chung: %q", got)
	}
}
