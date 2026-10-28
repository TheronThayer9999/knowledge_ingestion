package services

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/errors"
)

// Tệp đính kèm đi THẲNG vào prompt của một lượt: trích ngay trong request nên
// test khoá ba thứ dễ vỡ — định dạng không hỗ trợ phải là 400 (không phải 500),
// tệp rỗng không được trả về như tệp hợp lệ, và trần ký tự cắt theo RUNE để
// tiếng Việt không bị xé giữa âm tiết.

func TestChatAttachmentExtractsPlainText(t *testing.T) {
	svc := NewChatAttachmentService()
	res := svc.Extract(context.Background(), "bao-cao.txt", strings.NewReader("Dòng một\nDòng hai"))
	if res.Err != nil {
		t.Fatalf("Err = %v, muốn nil", res.Err)
	}
	if res.Data == nil {
		t.Fatal("Data = nil, muốn tệp đã trích")
	}
	if res.Data.Name != "bao-cao.txt" {
		t.Fatalf("Name = %q, muốn %q", res.Data.Name, "bao-cao.txt")
	}
	if !strings.Contains(res.Data.Text, "Dòng một") || !strings.Contains(res.Data.Text, "Dòng hai") {
		t.Fatalf("Text = %q, thiếu nội dung tệp", res.Data.Text)
	}
	if res.Data.Chars != len([]rune(res.Data.Text)) {
		t.Fatalf("Chars = %d, muốn %d (đếm theo rune)", res.Data.Chars, len([]rune(res.Data.Text)))
	}
}

// Định dạng lạ phải là 400 kèm danh sách hỗ trợ — client hiện nguyên message
// này nên nó chính là hướng dẫn người dùng.
func TestChatAttachmentRejectsUnsupportedExt(t *testing.T) {
	svc := NewChatAttachmentService()
	res := svc.Extract(context.Background(), "kho.zip", strings.NewReader("PK"))
	if res.Err == nil {
		t.Fatal("Err = nil, muốn lỗi 400")
	}
	e := errors.From(res.Err)
	if e == nil || e.GetHttpCode() != http.StatusBadRequest {
		t.Fatalf("lỗi = %v, muốn HttpCode 400", res.Err)
	}
	if !strings.Contains(e.Message, ".pdf") {
		t.Fatalf("Message = %q, muốn có danh sách đuôi hỗ trợ", e.Message)
	}
}

// PDF scan ở API không có OCR → rỗng; trả tệp rỗng cho model là để nó bịa.
func TestChatAttachmentRejectsEmptyText(t *testing.T) {
	svc := NewChatAttachmentService()
	res := svc.Extract(context.Background(), "trang.txt", strings.NewReader(" \n\n\t "))
	if res.Err == nil {
		t.Fatal("Err = nil, muốn lỗi vì không có chữ")
	}
	e := errors.From(res.Err)
	if e == nil || e.GetHttpCode() != http.StatusBadRequest {
		t.Fatalf("lỗi = %v, muốn HttpCode 400", res.Err)
	}
	if !strings.Contains(e.Message, "không có chữ") {
		t.Fatalf("Message = %q, muốn nói rõ tệp không có chữ", e.Message)
	}
}

func TestChatAttachmentClipsByRune(t *testing.T) {
	svc := NewChatAttachmentService()
	// Mỗi dòng 80 ký tự: reader text cắt mảnh theo newline nên văn bản thật có
	// xuống dòng; một dòng dài quá 64KB là ca khác của reader, không phải phép
	// thử của clip này.
	line := "ế" + strings.Repeat("a", 78) + "\n"
	long := strings.Repeat(line, constants.CHAT_ATTACHMENT_MAX_CHARS/len(line)+200)
	res := svc.Extract(context.Background(), "dai.txt", strings.NewReader(long))
	if res.Err != nil {
		t.Fatalf("Err = %v, muốn nil", res.Err)
	}
	runes := []rune(res.Data.Text)
	// trần + dấu "…" báo đã cắt
	if len(runes) != constants.CHAT_ATTACHMENT_MAX_CHARS+1 {
		t.Fatalf("len = %d, muốn %d", len(runes), constants.CHAT_ATTACHMENT_MAX_CHARS+1)
	}
	if !strings.HasSuffix(res.Data.Text, "…") {
		t.Fatalf("Text không kết thúc bằng … — người đọc không biết tệp bị cắt")
	}
}

// Tên do client gửi: bỏ đường dẫn, bỏ xuống dòng (newline phá khối prompt) và
// cắt độ dài như kho tri thức.
func TestAttachmentNameSanitizes(t *testing.T) {
	cases := map[string]string{
		`C:\Users\me\bao-cao.pdf`: "bao-cao.pdf",
		"../../etc/passwd":        "passwd",
		"a\nb.txt":                "a b.txt",
		"   ":                     "tep-dinh-kem",
		"":                        "tep-dinh-kem",
	}
	for input, want := range cases {
		if got := attachmentName(input); got != want {
			t.Errorf("attachmentName(%q) = %q, muốn %q", input, got, want)
		}
	}
	long := strings.Repeat("a", constants.MAX_FILENAME_LEN+10)
	if got := []rune(attachmentName(long + ".txt")); len(got) != constants.MAX_FILENAME_LEN {
		t.Errorf("tên dài không bị cắt: %d rune", len(got))
	}
}
