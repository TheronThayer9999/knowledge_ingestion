//go:build stress

// Các PDF thật trong dự án đều là tiếng Việt convert từ Word — đúng loại
// mà ledongthuc/pdf trả về chữ rải ("S ystems", "N e tw o r k") và là lý do
// dựng cả despace.go. Hai câu hỏi bắt buộc phải trả lời TRƯỚC khi chốt
// thư viện:
//
//  1. gopdf có giữ đúng dấu tiếng Việt không? Dấu là ký tự tổ hợp, trích
//     text sai dấu thì chunk rác — nhanh cũng vô nghĩa.
//  2. Sau khi đổi thư viện, despace còn cần không? Nếu docSpaced không còn
//     kích hoạt thì 400 dòng despace.go chỉ còn là lưới an toàn.
//
// Chạy:
//
//	$env:STRESS_VN = "C:\path\a.pdf;C:\path\b.pdf"
//	go test -tags stress -timeout 40m ./src/controller/services/ -run TestVietnamesePDF -v
package services

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	lpdf "github.com/ledongthuc/pdf"
	gopdf "github.com/razvandimescu/gopdf/pdf"

	"knowledge_ingestion/src/common/extractor"
)

// vnFiles đo — toàn bộ đều là tài liệu tiếng Việt export từ Word.
// Đổi danh sách này để đo trên file của mình.
func vnFiles() []string {
	if v := os.Getenv("STRESS_VN"); v != "" {
		return strings.Split(v, ";")
	}
	return []string{}
}

func TestVietnamesePDF(t *testing.T) {
	files := vnFiles()
	if len(files) == 0 {
		t.Skip("set STRESS_VN=<a.pdf;b.pdf> để chạy test này")
	}
	for _, f := range files {
		f = strings.TrimSpace(f)
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Logf("bỏ qua %s: %v", f, err)
			continue
		}
		t.Logf("\n################ %s (%.1f MB) ################", f, float64(len(raw))/(1<<20))

		// --- gopdf: chất lượng tiếng Việt ---
		t0 := time.Now()
		doc, err := gopdf.OpenBytes(raw)
		if err != nil {
			t.Logf("  gopdf mở lỗi: %v", err)
			continue
		}
		var gChars int
		var gSample strings.Builder
		for i := range doc.NumPages() {
			lines, err := doc.Page(i).TextLines()
			if err != nil {
				continue
			}
			for _, l := range lines {
				gChars += len(l.Text)
			}
			if gSample.Len() == 0 {
				for _, l := range lines {
					if strings.TrimSpace(l.Text) != "" {
						gSample.WriteString(l.Text)
						gSample.WriteString("\n")
					}
					if gSample.Len() > 400 {
						break
					}
				}
			}
		}
		gElapsed := time.Since(t0)

		// --- ledongthuc: cùng thứ, để so ---
		var lChars int
		var lSample strings.Builder
		lElapsed := time.Duration(0)
		if rd, err := lpdf.NewReader(bytes.NewReader(raw), int64(len(raw))); err == nil {
			t0 = time.Now()
			for i := 1; i <= rd.NumPage(); i++ {
				func() {
					defer func() { recover() }()
					p := rd.Page(i)
					if p.V.IsNull() {
						return
					}
					rows, err := p.GetTextByRow()
					if err != nil {
						return
					}
					var sb strings.Builder
					for _, r := range rows {
						for _, w := range r.Content {
							sb.WriteString(w.S)
							sb.WriteString(" ")
						}
						sb.WriteString("\n")
					}
					lChars += len(sb.String())
					if lSample.Len() == 0 {
						lSample.WriteString(sb.String())
					}
				}()
			}
			lElapsed = time.Since(t0)
		}

		t.Logf("  gopdf        : %8d ký tự | %v", gChars, gElapsed.Round(time.Millisecond))
		t.Logf("  ledongthuc   : %8d ký tự | %v", lChars, lElapsed.Round(time.Millisecond))
		t.Logf("  mẫu gopdf     : %s", firstLines(gSample.String(), 6))

		// --- extractor THẬT: despace còn kích hoạt không, dấu còn đúng không ---
		var pages int
		var text strings.Builder
		var errVN error
		t0 = time.Now()
		errVN = extractor.ExtractPages(t.Context(), f, bytes.NewReader(raw), extractor.OCRConfig{},
			func(p extractor.Page) error {
				pages++
				text.WriteString(p.Text)
				return nil
			})
		pipelineElapsed := time.Since(t0)
		if errVN != nil {
			t.Logf("  extractor lỗi: %v", errVN)
			continue
		}
		full := text.String()
		t.Logf("  extractor THẬT: %d trang, %d ký tự sau khử rải, %v",
			pages, len(full), pipelineElapsed.Round(time.Millisecond))

		// Dấu tiếng Việt còn nguyên? Đếm rune có dấu.
		withTone := 0
		for _, r := range full {
			if strings.ContainsRune("àáảãạăằắẳẵặâầấẩẫậèéẻẽẹêềếểễệìíỉĩịòóỏõọôồốổỗộơờớởỡợùúủũụưừứửữựỳýỷỹỵđ", r) {
				withTone++
			}
		}
		t.Logf("  rune có dấu: %d (%.2f%% tổng) — nếu 0%% là mất dấu",
			withTone, 100*float64(withTone)/float64(max(len([]rune(full)), 1)))
		t.Logf("  mẫu sau khử rải: %s", firstLines(full, 6))
	}
}

func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, "    "+l)
		}
	}
	return strings.Join(out, "\n")
}
