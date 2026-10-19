//go:build stress

// Câu hỏi này sinh ra sau khi đổi sang gopdf: soak trên file NHỎ (0.5MB,
// 300 trang) chậm hơn 2.2× so với ledongthuc, trong khi file LỚN thì nhanh
// hơn 57×. Tốc độ nào quan trọng hơn phụ thuộc vào việc file thật trông như
// thế nào — nên đo riêng phần trích text (không tính chunk + ghi DB) trên
// CẢ file nhỏ lẫn file lớn, để biết điểm hòa nằm ở đâu.
//
// Chạy:
//
//	$env:STRESS_PDF = "C:\path\to\big.pdf"   # tuỳ chọn, để so file lớn
//	go test -tags stress -timeout 40m ./src/controller/services/ -run TestExtractOnlyCompare -v
package services

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	lpdf "github.com/ledongthuc/pdf"
	gopdf "github.com/razvandimescu/gopdf/pdf"

	"knowledge_ingestion/src/common/extractor"
)

func TestExtractOnlyCompare(t *testing.T) {
	run := func(name string, raw []byte) {
		// --- gopdf (đang dùng) ---
		gChars, gPeak, gElapsed := func() (int, uint64, time.Duration) {
			var peak uint64
			done := make(chan struct{})
			go func() {
				var ms runtime.MemStats
				for {
					runtime.ReadMemStats(&ms)
					if ms.HeapAlloc > peak {
						peak = ms.HeapAlloc
					}
					select {
					case <-done:
						return
					case <-time.After(5 * time.Millisecond):
					}
				}
			}()
			t0 := time.Now()
			doc, err := gopdf.OpenBytes(raw)
			if err != nil {
				close(done)
				t.Fatalf("gopdf mở lỗi: %v", err)
			}
			n := 0
			for i := range doc.NumPages() {
				lines, err := doc.Page(i).TextLines()
				if err != nil {
					continue
				}
				for _, l := range lines {
					n += len(l.Text)
				}
			}
			el := time.Since(t0)
			close(done)
			return n, peak, el
		}()

		// --- ledongthuc/pdf (thư viện cũ, chỉ để so sánh) ---
		lChars, lPeak, lElapsed := func() (int, uint64, time.Duration) {
			var peak uint64
			done := make(chan struct{})
			go func() {
				var ms runtime.MemStats
				for {
					runtime.ReadMemStats(&ms)
					if ms.HeapAlloc > peak {
						peak = ms.HeapAlloc
					}
					select {
					case <-done:
						return
					case <-time.After(5 * time.Millisecond):
					}
				}
			}()
			t0 := time.Now()
			rd, err := lpdf.NewReader(bytes.NewReader(raw), int64(len(raw)))
			if err != nil {
				close(done)
				t.Fatalf("ledongthuc mở lỗi: %v", err)
			}
			n := 0
			for i := 1; i <= rd.NumPage(); i++ {
				rows, err := rd.Page(i).GetTextByRow()
				if err != nil {
					continue
				}
				for _, r := range rows {
					for _, w := range r.Content {
						n += len(w.S)
					}
				}
			}
			el := time.Since(t0)
			close(done)
			return n, peak, el
		}()

		t.Logf("%s (%.1f MB)", name, float64(len(raw))/(1<<20))
		t.Logf("  gopdf         : %8d ký tự | %7.0f MB heap | %v", gChars, float64(gPeak)/(1<<20), gElapsed.Round(time.Millisecond))
		t.Logf("  ledongthuc/pdf: %8d ký tự | %7.0f MB heap | %v", lChars, float64(lPeak)/(1<<20), lElapsed.Round(time.Millisecond))
		t.Logf("  → gopdf %s nhanh, %s bộ nhớ",
			ratio(lElapsed, gElapsed), ratioNum(lPeak, gPeak))
	}

	// File nhỏ — dùng đúng PDF mà soak dùng để số liệu so được.
	run("PDF nhỏ (soak)", stressPDF(t, 300, 50))
	run("PDF vừa", stressPDF(t, 300, 200))

	if list := os.Getenv("STRESS_PDF"); list != "" {
		for _, p := range strings.Split(list, ";") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			raw, err := os.ReadFile(p)
			if err != nil {
				t.Logf("bỏ qua %s: %v", p, err)
				continue
			}
			run("PDF thật "+filepath.Base(p), raw)
		}
	}

	// Chạy extractor THẬT để xem phần còn lại (despace + callback) tốn bao nhiêu.
	raw := stressPDF(t, 300, 50)
	t0 := time.Now()
	pages := 0
	if err := extractor.ExtractPages(context.Background(), "a.pdf", bytes.NewReader(raw),
		extractor.OCRConfig{}, func(extractor.Page) error {
			pages++
			return nil
		}); err != nil {
		t.Fatalf("ExtractPages lỗi: %v", err)
	}
	t.Logf("extractor thật (PDF nhỏ, %d trang): %v — tức %.1f ms/trang",
		pages, time.Since(t0).Round(time.Millisecond),
		float64(time.Since(t0).Milliseconds())/float64(max(pages, 1)))
}

func ratio(a, b time.Duration) string {
	if b == 0 {
		return "?"
	}
	return fmt.Sprintf("%.1f×", float64(a)/float64(b))
}

func ratioNum(a, b uint64) string {
	if b == 0 {
		return "?"
	}
	return fmt.Sprintf("%.1f×", float64(a)/float64(b))
}
