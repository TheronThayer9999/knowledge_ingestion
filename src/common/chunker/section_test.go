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

// Chunk chứa 2 tiêu đề thì lấy tiêu đề ĐẦU (Điều mà chunk bắt đầu), carry
// mới là tiêu đề cuối để chunk/trang sau nối đúng; chữ "điều" trong câu
// không bắt.
func TestAnnotate_FirstHeadingWins(t *testing.T) {
	parts := []string{"đuôi điều 4 cũ\nĐIỀU 5: A\nnội dung\nĐIỀU 6: B"}
	got, carry := Annotate(parts, "ĐIỀU 1: X")
	if got[0].Section != "ĐIỀU 5: A" {
		t.Fatalf("section = %q, want ĐIỀU 5: A", got[0].Section)
	}
	if carry != "ĐIỀU 6: B" {
		t.Fatalf("carry = %q, want ĐIỀU 6: B", carry)
	}
}

// Text extract PDF dồn một dòng dài ("...ĐIỀU 1: ... ĐIỀU 2: ...") — tiêu đề
// giữa dòng vẫn phải bắt được, mỗi Điều 1 block riêng, tên mục đúng.
func TestSplitBlocks_SingleLine(t *testing.T) {
	text := "[TÀI LIỆU NỘI BỘ] Phiên bản: 2024-Q3 ĐIỀU 1: QUY ĐỊNH LÀM VIỆC TỪ XA " +
		"1. Nhân viên chính thức được làm từ xa tối đa 02 ngày/tuần. 2. Đăng ký trước 24 giờ. " +
		"ĐIỀU 2: CHẾ ĐỘ NGHỈ ỐM 1. Hưởng 100% lương 03 ngày đầu. " +
		"ĐIỀU 3: THƯỞNG DỰ ÁN 1. Chỉ xét nhân viên chính thức."
	blocks := SplitBlocks(text)
	if len(blocks) != 4 {
		t.Fatalf("got %d blocks, want 4 (mở đầu + 3 Điều)", len(blocks))
	}
	if blocks[0].Heading != "" {
		t.Fatalf("block 0 heading = %q, want rỗng (phần mở đầu hưởng carry)", blocks[0].Heading)
	}
	want := []string{
		"ĐIỀU 1: QUY ĐỊNH LÀM VIỆC TỪ XA",
		"ĐIỀU 2: CHẾ ĐỘ NGHỈ ỐM",
		"ĐIỀU 3: THƯỞNG DỰ ÁN",
	}
	for i, w := range want {
		if blocks[i+1].Heading != w {
			t.Fatalf("block %d heading = %q, want %q", i+1, blocks[i+1].Heading, w)
		}
	}
	if !strings.Contains(blocks[1].Text, "02 ngày/tuần") || strings.Contains(blocks[1].Text, "ĐIỀU 2") {
		t.Fatalf("block 1 lẫn Điều khác: %q", blocks[1].Text)
	}
}

// Tên mục không được nuốt nội dung khoản (tái hiện section Qdrant
// "...ĐIỀU 6... 2. Hỗ" do (.*)$ nuốt cả dòng rồi cắt cụt giữa chữ).
func TestFirstHeading_BoundedName(t *testing.T) {
	text := "ĐIỀU 6: CHÍNH SÁCH ĂN TRƯA VÀ ĐI LẠI 1. Công ty hỗ trợ 30.000 VNĐ/ngày làm việc tại văn phòng cho chi phí ăn trưa. 2. Hỗ trợ gửi xe"
	if got := firstHeading(text); got != "ĐIỀU 6: CHÍNH SÁCH ĂN TRƯA VÀ ĐI LẠI" {
		t.Fatalf("section = %q", got)
	}
}

// Viện dẫn chéo trong câu ("Điều 1, Khoản 2", "ĐIỀU 5 và ĐIỀU 6") không phải
// tiêu đề — không được chẻ block hay gán section nhầm.
func TestFindHeadings_SkipsCrossRef(t *testing.T) {
	for _, s := range []string{
		"theo Điều 1, Khoản 2 quy định mức phạt",
		"căn cứ ĐIỀU 5 và ĐIỀU 6 để xử lý vi phạm",
		"như đã nêu ở Điều 3 quy định chung",
	} {
		if got := firstHeading(s); got != "" {
			t.Fatalf("firstHeading(%q) = %q, want rỗng", s, got)
		}
	}
	if len(SplitBlocks("căn cứ ĐIỀU 5 và ĐIỀU 6 để xử lý")) != 1 {
		t.Fatal("viện dẫn chéo không được chẻ block")
	}
}

// Text trước tiêu đề đầu (đuôi Điều trang trước, hoặc cột phải mà extractor
// nhả trước cột trái ở PDF 2 cột) không được rơi mất — thành block riêng
// heading rỗng để caller gán carry. Tái hiện article 10 từng mất "không
// cộng dồn", "dịch bệnh 14 ngày", "ứng trước 2 triệu" vì rớt đúng vùng này.
func TestSplitBlocks_KeepsPreHeadingText(t *testing.T) {
	text := "3. Trường hợp khẩn cấp báo qua Slack trong 48 giờ. 5. Không được cộng dồn. " +
		"ĐIỀU 1: LÀM VIỆC TỪ XA 1. Tối đa 02 ngày/tuần. ĐIỀU 2: NGHỈ ỐM 1. Hưởng 100% lương."
	blocks := SplitBlocks(text)
	if len(blocks) != 3 {
		t.Fatalf("got %d blocks, want 3 (pre + 2 Điều)", len(blocks))
	}
	if blocks[0].Heading != "" {
		t.Fatalf("pre-block heading = %q, want rỗng để caller gán carry", blocks[0].Heading)
	}
	for _, want := range []string{"Slack", "cộng dồn"} {
		if !strings.Contains(blocks[0].Text, want) {
			t.Fatalf("pre-block mất %q", want)
		}
	}
	if blocks[1].Heading != "ĐIỀU 1: LÀM VIỆC TỪ XA" {
		t.Fatalf("block 1 heading = %q", blocks[1].Heading)
	}
	// Toàn pipeline block → split không mất chữ nào.
	joined := ""
	for _, b := range blocks {
		joined += b.Text
	}
	for _, want := range []string{"Slack", "cộng dồn", "02 ngày", "100% lương"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("pipeline mất %q", want)
		}
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
