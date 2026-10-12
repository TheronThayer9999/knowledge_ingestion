package extractor

import "testing"

// Dòng rải chữ (PDF kết xuất lạ) phải nối lại đúng từ, tách đúng âm tiết.
func TestDespaceLine_SpacedPDF(t *testing.T) {
	got := despaceLine("Đ ạ o đ ứ c nô l ệ xu ấ t phát t ừ s ự oán gi ậ n")
	if got != "Đạo đức nô lệ xuất phát từ sự oán giận" {
		t.Fatalf("got %q", got)
	}
}

// Token nhiều ký tự xen giữa vẫn giữ đúng vị trí, không dồn dòng.
func TestDespaceLine_MixedTokens(t *testing.T) {
	got := despaceLine("2. Con ng ườ i ném mình vào th ế gi ớ i r ộ ng l ớ n v ô t ậ n")
	if got != "2. Con người ném mình vào thế giới rộng lớn vô tận" {
		t.Fatalf("got %q", got)
	}
}

// Dòng sạch (ít token 1 ký tự) đi qua nguyên vẹn — kể cả có "ở" lẻ.
func TestDespaceLine_CleanUntouched(t *testing.T) {
	clean := "1. Nhân viên chính thức được phép ở lại tối đa 02 ngày/tuần."
	if got := despaceLine(clean); got != clean {
		t.Fatalf("dòng sạch bị sửa: %q", got)
	}
}

// Câu ngắn toàn từ 2 chữ là câu sạch thường gặp — strict không được nối oan
// (hồi quy: ngưỡng cũ đếm cả token 2 chữ nên "Cô ấy đi ra." suýt bị nối).
func TestDespaceLine_ShortCleanUntouched(t *testing.T) {
	for _, clean := range []string{"Cô ấy đi ra.", "Tôi đi ăn cơm.", "Ai đi đâu?"} {
		if got := despaceLine(clean); got != clean {
			t.Fatalf("câu ngắn bị sửa: %q → %q", clean, got)
		}
	}
}

// Dòng ngắn rải chữ không qua nổi ngưỡng strict — file rải thì vòng force
// (joinLine) phải nối được, dấu câu bám giữ đúng chỗ.
func TestJoinLine_ShortSpaced(t *testing.T) {
	cases := map[string]string{
		"ế kẻ mạ nh.":                "ế kẻ mạnh.",
		"yế u, đề cao sự khiêm nh ườ ng": "yếu, đề cao sự khiêm nhường",
		"Phiên b ả n: 2025- Alpha":      "Phiên bản: 2025- Alpha",
	}
	for in, want := range cases {
		if got := joinLine(in); got != want {
			t.Fatalf("joinLine(%q) = %q, want %q", in, got, want)
		}
	}
}

// docSpaced: file nhiều dòng rải thì true, file sạch thì false.
func TestDocSpaced(t *testing.T) {
	spaced := "Đ ạ o đ ứ c nô l ệ xu ấ t phát oán gi ậ n\n" +
		"2. Con ng ườ i ném mình vào th ế gi ớ i r ộ ng l ớ n\n" +
		"ế kẻ mạ nh và yế u đuối trong xã hội hiện nay\n" +
		"ĐIỀU 1: QUY ĐỊNH LÀM VIỆC TỪ XA BẮT BUỘC\n" +
		"1. Nhân viên chính thức được phép ở lại tối đa 02 ngày\n" +
		"2. Bắt buộc phải đăng ký trên hệ thống HRM trước giờ\n"
	if !docSpaced(spaced) {
		t.Fatalf("file rải mà docSpaced=false")
	}
	clean := "ĐIỀU 1: QUY ĐỊNH LÀM VIỆC TỪ XA\n" +
		"1. Nhân viên chính thức được phép làm việc từ xa tối đa 02 ngày/tuần.\n" +
		"2. Bắt buộc phải đăng ký trên hệ thống HRM trước ít nhất 24 giờ.\n" +
		"3. Trường hợp khẩn cấp, nhân viên có thể báo cáo qua Slack.\n" +
		"Phiên bản: 2024-Q3 do Phòng Nhân sự ban hành rộng rãi\n"
	if docSpaced(clean) {
		t.Fatalf("file sạch mà docSpaced=true")
	}
}

// Tách âm tiết: cụm phụ âm trước nguyên âm có dấu thì tách, từ ngoại lai
// không dấu và số giữ nguyên.
func TestSplitSyllables(t *testing.T) {
	cases := map[string]string{
		"Đạođức": "Đạo đức",
		"tâmhồn": "tâm hồn",
		"xuất":   "xuất",
		"người":  "người",
		"không":  "không",
		"Camus":  "Camus",
		"2024":   "2024",
	}
	for in, want := range cases {
		got := splitSyllables(in)
		joined := ""
		for i, s := range got {
			if i > 0 {
				joined += " "
			}
			joined += s
		}
		if joined != want {
			t.Fatalf("splitSyllables(%q) = %q, want %q", in, joined, want)
		}
	}
}
