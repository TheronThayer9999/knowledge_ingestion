package chunker

import (
	"strings"
	"testing"
)

// Chunk giữa Điều (không chứa tiêu đề) phải thừa hưởng mục từ carry.
func TestAnnotate_CarrySection(t *testing.T) {
	parts := []string{
		"ĐIỀU 5: BẢO HIỂM\nCông ty đóng bảo hiểm xã hội",
		"cho nhân viên chính thức sau khi hết thử việc.",
	}
	got, carry := Annotate(parts, "")
	if len(got) != 2 {
		t.Fatalf("got %d chunks, want 2", len(got))
	}
	if got[0].Section != "ĐIỀU 5: BẢO HIỂM" {
		t.Fatalf("chunk 0 section = %q", got[0].Section)
	}
	if got[1].Section != "ĐIỀU 5: BẢO HIỂM" {
		t.Fatalf("chunk 1 phải thừa hưởng ĐIỀU 5, got %q", got[1].Section)
	}
	if carry != "ĐIỀU 5: BẢO HIỂM" {
		t.Fatalf("carry = %q", carry)
	}
}

// Chunk chứa 2 tiêu đề thì lấy tiêu đề cuối; chữ "điều" trong câu không bắt.
func TestAnnotate_LastHeadingWins(t *testing.T) {
	parts := []string{"đuôi điều 4 cũ\nĐIỀU 5: A\nnội dung\nĐIỀU 6: B"}
	got, _ := Annotate(parts, "ĐIỀU 1: X")
	if got[0].Section != "ĐIỀU 6: B" {
		t.Fatalf("section = %q, want ĐIỀU 6: B", got[0].Section)
	}
}

// Không tiêu đề, không carry thì Section rỗng (không prepend rác).
func TestAnnotate_NoHeading(t *testing.T) {
	got, carry := Annotate([]string{"xin chào thế giới"}, "")
	if got[0].Section != "" || carry != "" {
		t.Fatalf("got %+v carry %q, want rỗng", got, carry)
	}
}

// Metadata đầu văn bản: phiên bản + hiệu lực; thiếu thì rỗng, không đoán.
func TestExtractMeta(t *testing.T) {
	head := "[TÀI LIỆU NỘI BỘ - CÔNG TY ABC] Phiên bản: 2024-Q3\n... hiệu lực từ 01/10/2024 ..."
	v, e := ExtractMeta(head)
	if v != "2024-Q3" || e != "01/10/2024" {
		t.Fatalf("got (%q,%q), want (2024-Q3,01/10/2024)", v, e)
	}
	if v2, e2 := ExtractMeta("văn bản không có metadata"); v2 != "" || e2 != "" {
		t.Fatalf("thiếu metadata phải rỗng, got (%q,%q)", v2, e2)
	}
}

// Version triết học ghi "2025- Alpha"/"2025-Beta" (dấu "-" lẻ rồi xuống dòng)
// — regex không được nuốt dấu "-" lẻ, còn "2024-Q3"/"2025-Alpha" giữ đủ.
func TestExtractMetaVersionDash(t *testing.T) {
	cases := map[string]string{
		"[TÀI LIỆU] Phiên bản: 2025- Alpha Chủ đề": "2025",
		"[TÀI LIỆU] Phiên bản: 2025-Beta Chủ đề":   "2025-Beta",
		"[TÀI LIỆU] Phiên bản: 2024-Q3":            "2024-Q3",
	}
	for in, want := range cases {
		if v, _ := ExtractMeta(in); v != want {
			t.Fatalf("ExtractMeta(%q) version = %q, want %q", in, v, want)
		}
	}
}

// HeaderFor rỗng thì "" để caller khỏi prepend dòng trắng.
func TestHeaderFor(t *testing.T) {
	if HeaderFor("") != "" {
		t.Fatal("section rỗng phải cho header rỗng")
	}
	if !strings.HasPrefix(HeaderFor("ĐIỀU 5"), "[ĐIỀU 5]") {
		t.Fatalf("header sai: %q", HeaderFor("ĐIỀU 5"))
	}
}
