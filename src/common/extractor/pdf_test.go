package extractor

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// uniquePageTexts sinh nội dung khác nhau cho từng trang. Phải khác cả khi
// chuẩn hoá số (repeatKey đổi số thành "#"), nếu không Cleaner coi chúng là
// header lặp và xoá hết — đúng hành vi của Cleaner, nhưng làm hỏng test.
func uniquePageTexts(n int) []string {
	texts := make([]string, n)
	for i := range texts {
		word := ""
		for k := i + 1; k > 0; k /= 26 {
			word = string(rune('a'+k%26)) + word
		}
		texts[i] = fmt.Sprintf("Trang %d: noi dung %s", i+1, word)
	}
	return texts
}

// PDF nhiều khoảng (nhiều hơn pdfTextChunkPages) trích song song thì số trang
// vẫn đúng ở mọi ranh giới khoảng, và sau khi sắp theo Seq thì đúng thứ tự.
func TestExtractPDFChunksKeepPageNumbers(t *testing.T) {
	const n = pdfTextChunkPages*2 + 50
	texts := uniquePageTexts(n)
	pages, err := collectPages("uploads/big.pdf", bytes.NewReader(buildPDF(t, texts...)),
		OCRConfig{RenderBinary: popplerPdftoppm(t)})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(pages) != n {
		t.Fatalf("want %d pages, got %d", n, len(pages))
	}
	for i, pg := range pages {
		want := fmt.Sprintf("Trang %d:", i+1)
		if pg.Num != i+1 || pg.Seq != i+1 || !strings.HasPrefix(pg.Text, want) {
			t.Fatalf("trang %d sai: num=%d seq=%d text=%q", i+1, pg.Num, pg.Seq, pg.Text)
		}
	}
}

// Trang đã có (have) không được trích lại; phần còn thiếu, kể cả nằm giữa,
// phải được trích đúng 1 lần.
func TestExtractPDFSkipsSavedPages(t *testing.T) {
	const n = 250
	texts := uniquePageTexts(n)
	have := map[int]bool{}
	for p := 1; p <= 50; p++ {
		have[p] = true
	}
	for p := 151; p <= 200; p++ {
		have[p] = true
	}
	seen := map[int]int{}
	err := extractPDFPages(t.Context(), bytes.NewReader(buildPDF(t, texts...)),
		OCRConfig{RenderBinary: popplerPdftoppm(t)}, have, func(p Page) error {
			seen[p.Seq]++
			return nil
		})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(seen) != n-len(have) {
		t.Fatalf("want %d trang còn thiếu, got %d", n-len(have), len(seen))
	}
	for p := 1; p <= n; p++ {
		want := 1
		if have[p] {
			want = 0
		}
		if seen[p] != want {
			t.Fatalf("trang %d được visit %d lần, want %d", p, seen[p], want)
		}
	}
}

// visit không bao giờ chạy đồng thời dù các khoảng trích song song.
func TestExtractPDFVisitIsSerialized(t *testing.T) {
	const n = pdfTextChunkPages*3 + 7
	var (
		mu       sync.Mutex
		inFlight int
		overlap  bool
		count    int
	)
	err := extractPDFPages(t.Context(), bytes.NewReader(buildPDF(t, uniquePageTexts(n)...)),
		OCRConfig{RenderBinary: popplerPdftoppm(t)}, nil, func(p Page) error {
			mu.Lock()
			inFlight++
			if inFlight > 1 {
				overlap = true
			}
			count++
			mu.Unlock()
			mu.Lock()
			inFlight--
			mu.Unlock()
			return nil
		})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if overlap {
		t.Fatal("visit chạy đồng thời")
	}
	if count != n {
		t.Fatalf("want %d trang, got %d", n, count)
	}
}

// visit lỗi thì trả đúng lỗi đó và dừng; không chạy hết file.
func TestExtractPDFVisitErrorStops(t *testing.T) {
	const n = pdfTextChunkPages*3 + 7
	errBoom := errors.New("boom")
	var (
		mu    sync.Mutex
		count int
	)
	err := extractPDFPages(t.Context(), bytes.NewReader(buildPDF(t, uniquePageTexts(n)...)),
		OCRConfig{RenderBinary: popplerPdftoppm(t)}, nil, func(p Page) error {
			mu.Lock()
			defer mu.Unlock()
			count++
			if count == 30 {
				return errBoom
			}
			return nil
		})
	if !errors.Is(err, errBoom) {
		t.Fatalf("want errBoom, got %v", err)
	}
	if count >= n {
		t.Fatalf("visit lỗi nhưng vẫn chạy hết %d trang", n)
	}
}

func TestPendingPDFRanges(t *testing.T) {
	have := map[int]bool{2: true, 3: true, 7: true}
	got := pendingPDFRanges(8, have)
	want := []pdfRange{{1, 1}, {4, 6}, {8, 8}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("pendingPDFRanges = %v, want %v", got, want)
	}
	long := pendingPDFRanges(pdfTextChunkPages*2+1, nil)
	if len(long) != 3 || long[0].to-long[0].from+1 != pdfTextChunkPages {
		t.Fatalf("khoảng phải tối đa %d trang, got %v", pdfTextChunkPages, long)
	}
	if got := pendingPDFRanges(3, map[int]bool{1: true, 2: true, 3: true}); len(got) != 0 {
		t.Fatalf("mọi trang đã có thì không có khoảng nào, got %v", got)
	}
}

// Thiếu ocr.render_binary thì không đọc được PDF (pdfinfo/pdftotext suy từ
// đường dẫn đó) — báo lỗi rõ ràng, không im lặng trả trang rỗng.
func TestExtractPDFNeedsPopplerConfig(t *testing.T) {
	if _, err := collectPages("uploads/d.pdf", bytes.NewReader(buildPDF(t, "x")), OCRConfig{}); err == nil {
		t.Fatal("thiếu render_binary phải báo lỗi")
	}
}

func TestPopplerToolPath(t *testing.T) {
	// Tên trần (tool nằm trên PATH) thì giữ nguyên, không thêm thư mục/đuôi.
	if got := popplerTool("pdftoppm", "pdfinfo"); got != "pdfinfo" {
		t.Errorf("popplerTool(%q,%q) = %q, muốn %q", "pdftoppm", "pdfinfo", got, "pdfinfo")
	}
	// Có thư mục thì suy ra tool cùng thư mục — dựng bằng filepath.Join để
	// đúng cả Windows lẫn Unix (trên CI Linux, `D:\...` không phải path:
	// backslash không phải separator nên filepath.Dir trả về ".").
	dir := t.TempDir()
	bin := filepath.Join(dir, "pdftoppm")
	want := filepath.Join(dir, "pdftotext")
	if got := popplerTool(bin, "pdftotext"); got != want {
		t.Errorf("popplerTool(%q,%q) = %q, muốn %q", bin, "pdftotext", got, want)
	}
	if runtime.GOOS == "windows" {
		// Đuôi .exe phải giữ nguyên khi suy ra tool cùng thư mục.
		const in, wantExe = `D:\tools\bin\pdftoppm.exe`, `D:\tools\bin\pdftotext.exe`
		if got := popplerTool(in, "pdftotext"); got != wantExe {
			t.Errorf("popplerTool(%q,%q) = %q, muốn %q", in, "pdftotext", got, wantExe)
		}
	}
}
