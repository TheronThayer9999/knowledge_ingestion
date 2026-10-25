package extractor

import "testing"

func TestCompareExtraction(t *testing.T) {
	tests := []struct {
		name        string
		det         string
		vlm         string
		want        Verdict
		wantOnlyInA []string
		wantOnlyInB []string
		// scoreBelow > 0 thì kiểm thêm Score < ngưỡng này.
		scoreBelow float64
	}{
		{
			name: "trùng hoàn toàn",
			det:  "Điều 7. Mức phạt chậm nộp là 200.000 đồng.",
			vlm:  "Điều 7. Mức phạt chậm nộp là 200.000 đồng.",
			want: VerdictAgree,
		},
		{
			name: "chỉ khác khoảng trắng và xuống dòng",
			det:  "Điều 7. Mức phạt\nchậm nộp là 200.000 đồng.",
			vlm:  "Điều   7.  Mức phạt chậm\n\nnộp là 200.000 đồng.",
			want: VerdictAgree,
		},
		{
			name: "khác hoa thường",
			det:  "ĐIỀU 5 QUY ĐỊNH CHUNG",
			vlm:  "điều 5 quy định chung",
			want: VerdictAgree,
		},
		{
			name: "khác dấu nhóm nghìn",
			det:  "Mức phạt 1.000.000 đồng",
			vlm:  "Mức phạt 1000000 đồng",
			want: VerdictAgree,
		},
		{
			name: "khác dấu thập phân",
			det:  "Tỷ lệ 0,05%",
			vlm:  "Tỷ lệ 0.05%",
			want: VerdictAgree,
		},
		{
			name: "phần trăm tách rời",
			det:  "Tăng thêm 50 %",
			vlm:  "Tăng thêm 50%",
			want: VerdictAgree,
		},
		{
			name:        "lệch một số là xung đột dù chữ trùng hết",
			det:         "Mức phạt 200.000 đồng",
			vlm:         "Mức phạt 2.000.000 đồng",
			want:        VerdictConflict,
			wantOnlyInA: []string{"200000"},
			wantOnlyInB: []string{"2000000"},
		},
		{
			name:        "lệch số điều khoản",
			det:         "Điều 5 quy định",
			vlm:         "Điều 6 quy định",
			want:        VerdictConflict,
			wantOnlyInA: []string{"5"},
			wantOnlyInB: []string{"6"},
		},
		{
			name:        "một bên rỗng",
			det:         "Điều 7 mức phạt",
			vlm:         "",
			want:        VerdictConflict,
			wantOnlyInA: []string{"7"},
			scoreBelow:  0.2,
		},
		{
			name: "cả hai rỗng",
			det:  "",
			vlm:  "",
			want: VerdictAgree,
		},
		{
			name: "bảng phẳng hoá so với markdown",
			det:  "STT Tên hành vi Mức phạt 1 Chậm nộp 200.000 2 Khai sai 500.000",
			vlm:  "| STT | Tên hành vi | Mức phạt |\n|---|---|---|\n| 1 | Chậm nộp | 200.000 |\n| 2 | Khai sai | 500.000 |\n---",
			want: VerdictAgree,
		},
		{
			name:       "mất dấu tiếng Việt là khác từ, không phải khác số",
			det:        "mức phạt chậm nộp thuế",
			vlm:        "muc phat cham nop thue",
			want:       VerdictConflict,
			scoreBelow: 0.2,
		},
		{
			name:       "lệch một từ trên mười là lệch nhẹ",
			det:        "alpha beta gamma delta epsilon zeta eta theta iota kappa",
			vlm:        "alpha beta gamma delta epsilon zeta eta theta iota lambda",
			want:       VerdictMinor,
			scoreBelow: compareAgreeScore,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CompareExtraction(tc.det, tc.vlm)
			if got.Verdict != tc.want {
				t.Fatalf("verdict = %q (reason %q, score %.3f), muốn %q", got.Verdict, got.Reason, got.Score, tc.want)
			}
			if tc.scoreBelow > 0 && got.Score >= tc.scoreBelow {
				t.Errorf("score = %.3f, muốn < %.3f", got.Score, tc.scoreBelow)
			}
			if !equalStrings(got.OnlyInA, tc.wantOnlyInA) {
				t.Errorf("OnlyInA = %v, muốn %v", got.OnlyInA, tc.wantOnlyInA)
			}
			if !equalStrings(got.OnlyInB, tc.wantOnlyInB) {
				t.Errorf("OnlyInB = %v, muốn %v", got.OnlyInB, tc.wantOnlyInB)
			}
		})
	}
}

// TestCompareExtractionTatDinh khoá yêu cầu "chạy lại phải ra đúng một kết quả":
// đây là thứ khiến cross-check giải thích được, khác với việc để LLM so chuỗi.
func TestCompareExtractionTatDinh(t *testing.T) {
	det := "Điều 7. Mức phạt chậm nộp là 200.000 đồng, tăng 50 % từ 01/07/2026."
	vlm := "Điều 7. Mức phạt chậm nộp là 200000 đồng, tăng 50% từ 01/07/2026."
	first := CompareExtraction(det, vlm)
	for i := 0; i < 20; i++ {
		got := CompareExtraction(det, vlm)
		if got.Verdict != first.Verdict || got.Score != first.Score ||
			got.NumbersA != first.NumbersA || got.NumbersB != first.NumbersB ||
			!equalStrings(got.OnlyInA, first.OnlyInA) || !equalStrings(got.OnlyInB, first.OnlyInB) {
			t.Fatalf("lần %d khác lần đầu: %+v vs %+v", i, got, first)
		}
	}
}

func TestCanonicalNumber(t *testing.T) {
	tests := map[string]string{
		"1000000":      "1000000",
		"1.000.000":    "1000000",
		"1,000,000":    "1000000",
		"1.000":        "1000",
		"0,05":         "0.05",
		"0.05":         "0.05",
		"12,5":         "12.5",
		"1000.500":     "1000.500",
		"1,234,567.89": "1234567.89",
		"01":           "01",
	}
	for in, want := range tests {
		if got := canonicalNumber(in); got != want {
			t.Errorf("canonicalNumber(%q) = %q, muốn %q", in, got, want)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
