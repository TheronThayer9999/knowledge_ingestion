package usecase

import (
	"strings"
	"testing"
)

func TestNormalizeCitationsKhuonMarker(t *testing.T) {
	cases := []struct {
		name        string
		in          string
		nCites      int
		want        string
		wantDropped int
	}{
		{
			name:   "sao y của model: [*2] -> [^2]",
			in:     "Mức phạt là 10%. [*2]",
			nCites: 3,
			want:   "Mức phạt là 10%. [^2]",
		},
		{
			name:   "[n] thuần cũng về khuôn",
			in:     "Quy định tại [3].",
			nCites: 3,
			want:   "Quy định tại [^3].",
		},
		{
			name:   "khoảng trắng trong ngoặc",
			in:     "Xem [ ^ 1 ] nhé",
			nCites: 1,
			want:   "Xem [^1] nhé",
		},
		{
			name:        "số ngoài phạm vi bị xoá",
			in:          "Theo [^9] thì vậy.",
			nCites:      2,
			want:        "Theo thì vậy.",
			wantDropped: 1,
		},
		{
			name:        "n=0 là marker vô nghĩa (hit bị trần cắt)",
			in:          "Có [^0] ở đây.",
			nCites:      2,
			want:        "Có ở đây.",
			wantDropped: 1,
		},
		{
			name:        "không cites thì xoá hết",
			in:          "Câu trả lời [^1] không nguồn.",
			nCites:      0,
			want:        "Câu trả lời không nguồn.",
			wantDropped: 1,
		},
		{
			name:   "gộp trùng trong cùng dãy",
			in:     "Ý đầu [^2][^2] ý tiếp",
			nCites: 2,
			want:   "Ý đầu [^2] ý tiếp",
		},
		{
			name:   "dãy trùng cách nhau một space cũng gộp",
			in:     "A [1] [1] B",
			nCites: 1,
			want:   "A [^1] B",
		},
		{
			name:   "hai số khác nhau giữ nguyên thứ tự",
			in:     "So sánh [^1][^2].",
			nCites: 2,
			want:   "So sánh [^1][^2].",
		},
		{
			name:   "phần chú thích cũng về khuôn",
			in:     "[*1]: quy định nội bộ, trang 3",
			nCites: 1,
			want:   "[^1]: quy định nội bộ, trang 3",
		},
		{
			name:   "ID kỹ thuật bị xoá",
			in:     "Áp dụng theo Article ID 54 và (article_id: 56).",
			nCites: 2,
			want:   "Áp dụng theo và.",
		},
		{
			name:   "cách viết tiếng Việt của cùng lỗi",
			in:     "Xem mã bài viết #12 và nguồn id = 3 nhé",
			nCites: 3,
			want:   "Xem và nhé",
		},
		{
			name:   "ID xen giữa marker vẫn xử lý được",
			in:     "Theo [^1] (Article ID 54) là đúng.",
			nCites: 1,
			want:   "Theo [^1] là đúng.",
		},
		{
			name:   "answer rỗng",
			in:     "   ",
			nCites: 5,
			want:   "   ",
		},
		{
			name:   "link và text có ngoặc vuông không bị đụng",
			in:     "Xem [tài liệu](https://example.com/a) và chọn [ ] nếu đồng ý.",
			nCites: 2,
			want:   "Xem [tài liệu](https://example.com/a) và chọn [ ] nếu đồng ý.",
		},
		{
			name:   "code block giữ nguyên canh lề khi không có gì bị cắt",
			in:     "| Cột | Số |\n|-----|----|\n| A   | 1  |",
			nCites: 3,
			want:   "| Cột | Số |\n|-----|----|\n| A   | 1  |",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, dropped := normalizeCitations(tc.in, tc.nCites)
			if got != tc.want {
				t.Fatalf("answer = %q, muốn %q", got, tc.want)
			}
			if dropped != tc.wantDropped {
				t.Fatalf("số marker bị loại = %d, muốn %d", dropped, tc.wantDropped)
			}
		})
	}
}

// TestNormalizeCitationsKhongInRaSoKhongCoThat: đánh cả hai chiều — không được để
// sót marker ngoài phạm vi (FE bấm vào là vỡ), và không được đánh dấu oan marker
// hợp lệ.
func TestNormalizeCitationsKhongSotMarkerNgoaiPhamVi(t *testing.T) {
	in := "[^1] ok, [^2] ok, [^3] quá, [^7] quá, [*4] quá, [5] quá"
	got, dropped := normalizeCitations(in, 2)
	if dropped != 4 {
		t.Fatalf("dropped = %d, muốn 4 (answer = %q)", dropped, got)
	}
	for _, bad := range []string{"[^3]", "[^7]", "[*4]", "[5]"} {
		if strings.Contains(got, bad) {
			t.Errorf("còn %q trong answer: %q", bad, got)
		}
	}
	for _, good := range []string{"[^1]", "[^2]"} {
		if !strings.Contains(got, good) {
			t.Errorf("mất %q: %q", good, got)
		}
	}
}
