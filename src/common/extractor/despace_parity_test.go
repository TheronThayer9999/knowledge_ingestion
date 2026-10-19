//go:build stress

// Câu hỏi quyết định có giữ được gopdf không: trên PDF tiếng Việt convert từ
// Word, gopdf trích RA ÍT KÝ TỰ HƠN ledongthuc (−7% đến −18%). Chênh đó là
// MẤT NỘI DUNG hay chỉ là KHOẢNG TRẮNG rác (ledongthuc tách từng glyph)?
//
// Cách trả lời: chạy CẢ HAI qua ĐÚNG pipeline khử rải của ta rồi so độ dài
// CUỐI CÙNG. Nếu sau khử rải hai bên bằng nhau thì chênh lúc đầu chỉ là
// khoảng trắng, không mất gì — và gopdf đúng yêu cầu.
//
// Chạy:
//
//	$env:STRESS_VN = "C:\path\a.pdf;C:\path\b.pdf"
//	go test -tags stress -timeout 40m ./src/common/extractor/ -run TestDespaceParity -v
package extractor

import (
	"bytes"
	"os"
	"strings"
	"testing"

	lpdf "github.com/ledongthuc/pdf"
)

// viaGopdf chạy pipeline thật (gopdf + despace hai mức) — dùng luôn
// extractPDFPages để đúng y hành vi production.
func viaGopdf(t *testing.T, raw []byte) (string, int) {
	t.Helper()
	// fromPage=1 (bắt đầu từ trang đầu) rồi DespacePages một lần ở cuối —
	// đúng thứ tự phase extract làm, xem pdf.go.
	all, err := ExtractAll(t.Context(), "uploads/x.pdf", bytes.NewReader(raw), OCRConfig{})
	if err != nil {
		t.Fatalf("ExtractAll lỗi: %v", err)
	}
	var sb strings.Builder
	for _, p := range all {
		sb.WriteString(p.Text)
	}
	return sb.String(), len(all)
}

// viaLedongthuc dựng lại đúng logic cũ: GetTextByRow → nối mảnh bằng space →
// docSpacedAcc quét cả file → joinLine/despaceLine từng dòng.
func viaLedongthuc(t *testing.T, raw []byte) (string, int, bool) {
	t.Helper()
	rd, err := lpdf.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("ledongthuc mở lỗi: %v", err)
	}
	n := rd.NumPage()
	type pg struct{ text string }
	pages := make([]pg, 0, n)
	var acc docSpacedAcc
	for i := 1; i <= n; i++ {
		var text string
		func() {
			defer func() { recover() }() // ledongthuc panic trên PDF lỗi
			p := rd.Page(i)
			if p.V.IsNull() {
				return
			}
			rows, err := p.GetTextByRow()
			if err != nil {
				return
			}
			var b strings.Builder
			for _, row := range rows {
				var parts []string
				for _, w := range row.Content {
					if s := strings.TrimSpace(w.S); s != "" {
						parts = append(parts, s)
					}
				}
				if len(parts) > 0 {
					b.WriteString(strings.Join(parts, " "))
					b.WriteString("\n")
				}
			}
			text = strings.TrimSpace(b.String())
		}()
		for _, l := range strings.Split(text, "\n") {
			acc.addLine(l)
		}
		pages = append(pages, pg{text})
	}
	force := acc.spaced()
	var sb strings.Builder
	for _, p := range pages {
		lines := strings.Split(p.text, "\n")
		for j, l := range lines {
			if force {
				lines[j] = joinLine(l)
			} else {
				lines[j] = despaceLine(l)
			}
		}
		sb.WriteString(strings.Join(lines, "\n"))
	}
	return sb.String(), len(pages), force
}

// flat bỏ mọi khoảng trắng — so độ dài "nội dung thật", không tính layout.
func flat(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r':
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func TestDespaceParity(t *testing.T) {
	list := os.Getenv("STRESS_VN")
	if list == "" {
		t.Skip("set STRESS_VN=<a.pdf;b.pdf> để chạy test này")
	}
	for _, f := range strings.Split(list, ";") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Logf("bỏ qua %s: %v", f, err)
			continue
		}
		t.Logf("\n######## %s (%.1f MB) ########", f, float64(len(raw))/(1<<20))

		gText, gPages := viaGopdf(t, raw)
		lText, lPages, force := viaLedongthuc(t, raw)

		gFlat, lFlat := len(flat(gText)), len(flat(lText))
		t.Logf("  gopdf      : %d trang, %6d ký tự thô, %6d ký tự bỏ whitespace",
			gPages, len(gText), gFlat)
		t.Logf("  ledongthuc : %d trang, %6d ký tự thô, %6d ký tự bỏ whitespace, file rải=%v",
			lPages, len(lText), lFlat, force)

		if lFlat == 0 {
			t.Logf("  ledongthuc không có text (PDF scan) — so sánh bỏ qua")
			continue
		}
		// Ngưỡng: gopdf được phép ít hơn, nhưng không quá 3%. Lệch lớn là
		// mất nội dung, lúc đó thư viện cũ vẫn đúng hơn và phải quay lại.
		diff := 100 * float64(gFlat-lFlat) / float64(lFlat)
		t.Logf("  → gopdf %+.1f%% nội dung so với ledongthuc", diff)
		switch {
		case gFlat >= lFlat:
			t.Logf("  KẾT LUẬN: gopdf không mất nội dung (nhiều hơn hoặc ngang)")
		case diff > -3:
			t.Logf("  KẾT LUẬN: chênh trong dung sai cho — không mất nội dung")
		default:
			t.Errorf("KẾT LUẬN: gopdf MẤT %.1f%% nội dung — không an toàn để thay", -diff)
		}
	}
}
