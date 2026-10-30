//go:build stress

// Stress test luồng chunk với file PDF nhiều trang — đo bộ nhớ đỉnh và kiểm
// tra treo (deadlock) chứ không chỉ kiểm tra đúng/sai.
//
// Chạy:
//
//	go test -tags stress -timeout 20m ./src/controller/services/ -run TestExtractStress -v
//
// Build tag riêng vì test nặng (dựng PDF 500 trang + chạy cả pipeline), không
// để trong CI mặc định.
//
// VÌ SAO DÙNG fpdf ĐỂ DỰNG PDF: bản viết tay byte-level bị sai xref/đánh số
// object và ledongthuc/pdf trả về Page rỗng — thuật toán trích text không
// hỏng, dữ liệu vào sai. Dùng thư viện cho PDF hợp lệ và giống file thật
// (nhúng font, xuất nhiều dòng/trang) thay vì sửa byte PDF bằng tay.
package services

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-pdf/fpdf"

	"knowledge_ingestion/src/common/extractor"
	"knowledge_ingestion/src/common/storage"
	"knowledge_ingestion/src/domain"
)

// ---- dựng PDF nhiều trang ----

// stressPDF dựng PDF N trang, mỗi trang linesPerPage dòng văn bản hành chính.
func stressPDF(t *testing.T, pages, linesPerPage int) []byte {
	t.Helper()
	const (
		margin = 15.0
		lineH  = 5.0
	)
	doc := fpdf.New("P", "mm", "A4", "")
	doc.SetMargins(margin, margin, margin)
	doc.SetAutoPageBreak(true, margin)

	hasVN := addVNTTF(doc, t)
	for p := 1; p <= pages; p++ {
		doc.AddPage()
		if hasVN {
			doc.SetFont("VN", "", 10)
		} else {
			doc.SetFont("Helvetica", "", 10)
		}
		for l := range linesPerPage {
			doc.CellFormat(180, lineH, stressLine(p, l, hasVN), "", 0, "L", false, 0, "")
		}
	}
	var out bytes.Buffer
	if err := doc.Output(&out); err != nil {
		t.Fatalf("fpdf xuất PDF lỗi: %v", err)
	}
	if doc.Err() {
		t.Fatal("fpdf báo lỗi nội bộ khi dựng PDF")
	}
	return out.Bytes()
}

// addVNTTF nhúng font hệ thống hỗ trợ tiếng Việt, trả false nếu máy không có
// để test chuyển sang ASCII thay vì fail — đo bộ nhớ thì có dấu hay không
// không đổi gì.
func addVNTTF(doc *fpdf.Fpdf, t *testing.T) bool {
	t.Helper()
	for _, path := range []string{
		`C:\Windows\Fonts\arial.ttf`,
		`C:\Windows\Fonts\calibri.ttf`,
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		"/System/Library/Fonts/Supplemental/Arial.ttf",
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		// Tham số là (family, style, bytes) — KHÔNG phải tên file. Family
		// phải khớp đúng thứ SetFont("VN", ...) sẽ tra.
		doc.AddUTF8FontFromBytes("VN", "", data)
		if !doc.Err() {
			return true
		}
	}
	t.Log("không tìm thấy TTF hỗ trợ tiếng Việt — test dùng ASCII, đo bộ nhớ không đổi")
	return false
}

// stressLine dựng 1 dòng văn bản hành chính, xen tiêu đề Điều/Khoản để
// SplitBlocks thật sự tách khối (ASCII thì viết "DIEU"/"KHOAN" vì regex trong
// section.go khớp cả hai dạng).
func stressLine(page, line int, vn bool) string {
	kw, clause := "Điều", "Khoản"
	if !vn {
		kw, clause = "DIEU", "KHOAN"
	}
	amount := (line*7919)%9973 + 120000
	return fmt.Sprintf("%s %d.%d.%d - %s %d. Người lao động làm việc trong quan hệ bảo hiểm xã hội được hưởng mức lương và phụ cấp tối thiểu %d đồng mỗi tháng áp dụng chung.",
		kw, page, (line/6)+1, line%6+1, clause, (line%4)+1, amount)
}

// ---- stub chỉ ĐẾM, không giữ trang (giữ sẽ tự loãng phép đo) ----
//
// Đo BỘ NHỚ giờ nhắm phase extract chứ không phải chunk: sau khi tách phase,
// đây mới là chỗ duy nhất ôm TOÀN BỘ văn bản trong RAM (mọi trang + artifact
// đóng khung), nên đây là nơi cần biết con số thật.

type stressPageRepo struct {
	pages  atomic.Int64
	writes atomic.Int64
}

func (r *stressPageRepo) CreateBatch(_ context.Context, pages []*domain.ArticlePage) error {
	r.pages.Add(int64(len(pages)))
	r.writes.Add(1)
	return nil
}
func (r *stressPageRepo) Progress(context.Context, int64) (*domain.PageProgress, error) {
	return &domain.PageProgress{DoneSeq: -1}, nil
}
func (r *stressPageRepo) ListByArticle(context.Context, int64) ([]*domain.ArticlePage, error) {
	return nil, nil
}
func (r *stressPageRepo) ListPagesRange(context.Context, int64, int, int) ([]*domain.ArticlePage, error) {
	return nil, nil
}
func (r *stressPageRepo) DeleteByArticleID(context.Context, int64) error  { return nil }
func (r *stressPageRepo) DeleteByArticles(context.Context, []int64) error { return nil }
func (r *stressPageRepo) DeleteOrphan(context.Context) (int64, error)     { return 0, nil }

type stressArticles struct {
	done atomic.Int64
}

func (a *stressArticles) ClaimExtractPending(context.Context, int, time.Duration) ([]*domain.Article, error) {
	return nil, nil
}
func (a *stressArticles) MarkExtractDone(context.Context, int64) error {
	a.done.Add(1)
	return nil
}
func (a *stressArticles) MarkExtractError(context.Context, int64, int) error  { return nil }
func (a *stressArticles) ReleaseExtractClaims(context.Context, []int64) error { return nil }
func (a *stressArticles) IsAlive(context.Context, int64) (bool, error)        { return true, nil }
func (a *stressArticles) UpdateMetadata(context.Context, int64, int, string, string) error {
	return nil
}

type stressStorage struct{ data []byte }

func (s *stressStorage) Download(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.data)), nil
}
func (s *stressStorage) Delete(context.Context, string) error { return nil }
func (s *stressStorage) Upload(context.Context, string, io.Reader, int64, string) error {
	return nil
}
func (s *stressStorage) Exists(context.Context, string) (bool, error) { return true, nil }
func (s *stressStorage) PresignedURL(context.Context, string, string, time.Duration) (string, error) {
	return "", nil
}
func (s *stressStorage) PresignedGetURL(context.Context, string, string, bool, time.Duration) (string, error) {
	return "", nil
}

// ---- đo bộ nhớ đỉnh ----

// peakHeapMB lấy mẫu HeapAlloc mỗi 10ms trong lúc fn chạy và trả đỉnh.
// ReadMemStats dừng thế giới thoáng qua nên 10ms là đủ thô mà không nghẽn.
// Bổ sung TotalAlloc để biết tổng đã cấp phát (con số này không bị GC giảm).
func peakHeapMB(fn func()) (peakMB, totalMB float64, elapsed time.Duration) {
	var peak, total atomic.Uint64
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		var ms runtime.MemStats
		for {
			runtime.ReadMemStats(&ms)
			if ms.HeapAlloc > peak.Load() {
				peak.Store(ms.HeapAlloc)
			}
			total.Store(ms.TotalAlloc)
			select {
			case <-stop:
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()
	start := time.Now()
	fn()
	elapsed = time.Since(start)
	close(stop)
	wg.Wait()
	return float64(peak.Load()) / (1 << 20), float64(total.Load()) / (1 << 20), elapsed
}

// TestExtractStressDumpPDF in ra extractor thực sự trả gì — dùng để phân
// biệt lỗi ở dữ liệu vào với lỗi ở extractor/chunker. Chạy riêng:
//
//	go test -tags stress ./src/controller/services/ -run TestExtractStressDumpPDF -v
func TestExtractStressDumpPDF(t *testing.T) {
	raw := stressPDF(t, 3, 8)
	var pages []extractor.Page
	err := extractor.ExtractPages(context.Background(), "uploads/dump.pdf", bytes.NewReader(raw),
		extractor.OCRConfig{}, func(p extractor.Page) error {
			pages = append(pages, p)
			return nil
		})
	if err != nil {
		t.Fatalf("ExtractPages lỗi: %v", err)
	}
	t.Logf("PDF %d bytes → %d trang", len(raw), len(pages))
	for _, p := range pages {
		snippet := p.Text
		if len(snippet) > 140 {
			snippet = snippet[:140] + "..."
		}
		t.Logf("  trang %d: %d ký tự, %d dòng | %q", p.Num, len(p.Text),
			strings.Count(p.Text, "\n")+1, snippet)
	}
	if len(pages) > 0 && pages[0].Text == "" {
		t.Fatal("trang đầu rỗng — dữ liệu vào sai, KHÔNG phải lỗi pipeline")
	}
}

// TestExtractStressRealPDF chạy pipeline trên PDF THẬT của máy, không dựng
// data giả — để đo bộ nhớ với file thực tế (font nhúng, nén, ảnh, bảng) thay
// vì PDF tối giản.
//
// Cách chạy (Windows):
//
//	$env:STRESS_PDF = "C:\path\to\file.pdf"
//	go test -tags stress -timeout 30m ./src/controller/services/ -run TestExtractStressRealPDF -v
//
// Không set STRESS_PDF thì test skip chứ không fail — đường dẫn cá nhân không
// nằm trong repo.
func TestExtractStressRealPDF(t *testing.T) {
	path := os.Getenv("STRESS_PDF")
	if path == "" {
		t.Skip("set STRESS_PDF=<đường dẫn PDF> để chạy test này")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("đọc %s: %v", path, err)
	}
	t.Logf("file: %s (%.1f MB)", filepath.Base(path), float64(len(raw))/(1<<20))

	// Xem extractor lấy được bao nhiêu trang / bao nhiêu ký tự TRƯỚC, để phân
	// biệt "hỏng vì tài liệu" với "hỏng vì pipeline".
	var pages, chars int
	if err := extractor.ExtractPages(context.Background(), "uploads/real.pdf", bytes.NewReader(raw),
		extractor.OCRConfig{}, func(p extractor.Page) error {
			pages++
			chars += len(p.Text)
			return nil
		}); err != nil {
		t.Fatalf("ExtractPages lỗi: %v", err)
	}
	t.Logf("extractor: %d trang, %d ký tự (%.1f MB text)", pages, chars, float64(chars)/(1<<20))

	repo := &stressPageRepo{}
	arts := &stressArticles{}
	svc := NewArticleExtractService(arts, repo, &stressStorage{data: raw},
		extractor.OCRConfig{}, nil).(*articleExtractService)
	article := &domain.Article{BaseModel: domain.BaseModel{ID: 1}, StorageKey: "uploads/real.pdf"}

	peakMB, totalMB, elapsed := peakHeapMB(func() {
		if err := svc.extractOne(context.Background(), "real", article); err != nil {
			t.Errorf("extractOne lỗi: %v", err)
		}
	})
	t.Logf("KẾT QUẢ: %d trang / %d lô ghi | heap đỉnh %.0f MB, tổng cấp %.0f MB | %v",
		repo.pages.Load(), repo.writes.Load(), peakMB, totalMB, elapsed.Round(time.Millisecond))
}

// TestExtractStressRealPDFConcurrent chạy N bản CÙNG file thật song song —
// đây là con số quyết định cho câu hỏi "tràn RAM không", vì CHUNK_POOL_SIZE
// trong worker = 3 và không ai kiểm tra file nặng. Nhân bản bộ nhớ một bài là
// con số ngoài suy luận.
func TestExtractStressRealPDFConcurrent(t *testing.T) {
	path := os.Getenv("STRESS_PDF")
	if path == "" {
		t.Skip("set STRESS_PDF=<đường dẫn PDF> để chạy test này")
	}
	workers := 3
	if v := os.Getenv("STRESS_WORKERS"); v != "" {
		if _, err := fmt.Sscanf(v, "%d", &workers); err != nil || workers < 1 {
			t.Fatalf("STRESS_WORKERS=%q không phải số nguyên dương", v)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("đọc %s: %v", path, err)
	}
	// Nhân bản buffer ra N bản riêng — nếu dùng chung 1 slice thì cả N worker
	// chỉ đọc, không phản ánh việc mỗi bài giữ buffer riêng như ngoài đời
	// (S3 trả 1 reader riêng mỗi lần Download).
	buffers := make([][]byte, workers)
	for i := range buffers {
		buffers[i] = make([]byte, len(raw))
		copy(buffers[i], raw)
	}
	t.Logf("file %.1f MB × %d goroutine (mỗi bài giữ buffer riêng)",
		float64(len(raw))/(1<<20), workers)

	repo := &stressPageRepo{}
	arts := &stressArticles{}

	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			svc := NewArticleExtractService(arts, repo, &stressStorage{data: buffers[id]},
				extractor.OCRConfig{}, nil).(*articleExtractService)
			a := &domain.Article{BaseModel: domain.BaseModel{ID: int64(id + 1)}, StorageKey: "uploads/real.pdf"}
			if err := svc.extractOne(context.Background(), "real", a); err != nil {
				t.Errorf("bài %d lỗi: %v", id, err)
			}
		}(i)
	}
	peakMB, totalMB, elapsed := peakHeapMB(func() { wg.Wait() })

	t.Logf("KẾT QUẢ %d bài song song: %d trang / %d lô ghi | heap đỉnh %.0f MB, tổng cấp %.0f MB | %v",
		workers, repo.pages.Load(), repo.writes.Load(), peakMB, totalMB, elapsed.Round(time.Millisecond))
	t.Logf("SUY RA: ~%.0f MB mỗi bài nặng", peakMB/float64(workers))
}

// TestExtractStressSoak là câu hỏi "service có chết sau nhiều bài không" được
// trả lời trực tiếp: chạy N bài LIÊN TIẾP, đo heap sau từng bài (đã ép GC),
// rồi so bình quân 5 bài đầu với 5 bài cuối.
//
//   - Heap đi ngang  → không rò, service chạy vô hạn.
//   - Heap tăng đều  → rò bộ nhớ, đây mới là thứ giết service.
//
// Dùng bài vừa phải (300 trang) chứ không dùng file 85MB vì cần đủ số vòng
// để thấy xu hướng trong thời gian chấp nhận được; xu hướng rò không đổi bản
// chất khi đổi kích thước bài.
func TestExtractStressSoak(t *testing.T) {
	const (
		rounds    = 40
		pagesPer  = 300
		linesPer  = 50
		workers   = 3
		poolLimit = 6
	)
	raw := stressPDF(t, pagesPer, linesPer)
	t.Logf("mỗi bài: %d trang, %.1f MB PDF; %d bài × %d goroutine",
		pagesPer, float64(len(raw))/(1<<20), rounds, workers)

	var heaps []uint64
	start := time.Now()
	var totalPages atomic.Int64
	for r := range rounds {
		repo := &stressPageRepo{}
		arts := &stressArticles{}
		var wg sync.WaitGroup
		sem := make(chan struct{}, workers) // mô phỏng CHUNK_POOL_SIZE
		for i := range workers {
			wg.Add(1)
			sem <- struct{}{}
			go func(id int) {
				defer wg.Done()
				defer func() { <-sem }()
				svc := NewArticleExtractService(arts, repo, &stressStorage{data: raw},
					extractor.OCRConfig{}, nil).(*articleExtractService)
				a := &domain.Article{BaseModel: domain.BaseModel{ID: int64(r*10 + id)}, StorageKey: "uploads/soak.pdf"}
				if err := svc.extractOne(context.Background(), "soak", a); err != nil {
					t.Errorf("bài %d lỗi: %v", id, err)
				}
			}(i)
		}
		wg.Wait()
		totalPages.Add(repo.pages.Load())
		// Ép GC rồi đọc — nếu không ép, heap sống sẽ bị đo bẩn bởi rác chưa
		// thu gom và tăng giả theo thời gian chứ không phải rò.
		runtime.GC()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		heaps = append(heaps, ms.HeapAlloc)
	}
	elapsed := time.Since(start)

	// Log đường cong heap để nhìn ngay: thẳng = sạch, dốc = rò.
	t.Log("heap sau từng vòng (MB):")
	for i, h := range heaps {
		if i%5 == 0 || i == len(heaps)-1 {
			t.Logf("  vòng %2d: %6.0f MB", i+1, float64(h)/(1<<20))
		}
	}

	mean := func(v []uint64) float64 {
		var s uint64
		for _, x := range v {
			s += x
		}
		return float64(s) / float64(len(v)) / (1 << 20)
	}
	head, tail := mean(heaps[:5]), mean(heaps[len(heaps)-5:])
	t.Logf("KẾT QUẢ: %d trang | %v (%v/bài) | heap đầu %.0f MB → cuối %.0f MB",
		totalPages.Load(), elapsed.Round(time.Second),
		(elapsed / rounds).Round(time.Millisecond), head, tail)

	// Cho phép trần 64MB: pool goroutine, cache của splitter, biến tĩnh của
	// test... đều gây dao động nhỏ. Vượt ngưỡng này mới là rò thật.
	const growthMB = 64
	if tail > head+growthMB {
		t.Errorf("RÒ BỘ NHỚ: heap trung bình 5 vòng đầu %.0f MB → 5 vòng cuối %.0f MB "+
			"(+%0.f MB, trần cho phép %d MB)", head, tail, tail-head, growthMB)
	}
	if totalPages.Load() == 0 {
		t.Fatal("không trích trang nào")
	}
	_ = poolLimit
}

func TestExtractStressPDF(t *testing.T) {
	const (
		pages       = 500
		linesPerPag = 60 // ~5 ký tự dòng × 60 ≈ 5.4 ký tự/trang, gần file thật
	)
	raw := stressPDF(t, pages, linesPerPag)
	t.Logf("PDF: %d trang, %.1f MB bytes", pages, float64(len(raw))/(1<<20))

	repo := &stressPageRepo{}
	arts := &stressArticles{}
	svc := NewArticleExtractService(arts, repo, &stressStorage{data: raw},
		extractor.OCRConfig{}, nil).(*articleExtractService)

	article := &domain.Article{BaseModel: domain.BaseModel{ID: 1}, StorageKey: "uploads/big.pdf"}

	// -timeout của go test là chốt chặn deadlock: nếu pipeline treo (semaphore
	// không nhả, WaitGroup không về) test sẽ panic kèm stack đầy đủ.
	peakMB, totalMB, elapsed := peakHeapMB(func() {
		if err := svc.extractOne(context.Background(), "stress", article); err != nil {
			t.Errorf("extractOne lỗi: %v", err)
		}
	})

	t.Logf("KẾT QUẢ: %d trang / %d lô ghi | heap đỉnh %.0f MB, tổng cấp %.0f MB | %v",
		repo.pages.Load(), repo.writes.Load(), peakMB, totalMB, elapsed.Round(time.Millisecond))

	if repo.pages.Load() == 0 {
		t.Fatal("không trích trang nào — pipeline hỏng")
	}
	// Trần rất rộng có chủ đích: mục tiêu là BẮT hồi quy, không phải chặt
	// ngưỡng chính xác (kích thước thay đổi theo phiên bản Eino/máy).
	const limitMB = 400
	if peakMB > limitMB {
		t.Errorf("heap đỉnh %.0f MB vượt trần %d MB", peakMB, limitMB)
	}
}

// TestExtractStressConcurrent mô phỏng CHUNK_POOL_SIZE = 3 bài song song —
// đây là kịch bản OOM thật vì 3 bài cùng giữ buffer.
func TestExtractStressConcurrent(t *testing.T) {
	const pool = 3
	const pages = 500
	raw := stressPDF(t, pages, 60)
	t.Logf("PDF dùng chung cho %d goroutine: %.1f MB", pool, float64(len(raw))/(1<<20))

	repo := &stressPageRepo{}
	arts := &stressArticles{}
	svc := NewArticleExtractService(arts, repo, &stressStorage{data: raw},
		extractor.OCRConfig{}, nil).(*articleExtractService)

	peakMB, totalMB, elapsed := peakHeapMB(func() {
		var wg sync.WaitGroup
		for i := range pool {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				a := &domain.Article{BaseModel: domain.BaseModel{ID: int64(id + 1)}, StorageKey: "uploads/big.pdf"}
				if err := svc.extractOne(context.Background(), "stress", a); err != nil {
					t.Errorf("bài %d lỗi: %v", id, err)
				}
			}(i)
		}
		wg.Wait()
	})

	total := repo.pages.Load()
	t.Logf("KẾT QUẢ: %d bài → %d trang / %d lô ghi | heap đỉnh %.0f MB, tổng cấp %.0f MB | %v",
		pool, total, repo.writes.Load(), peakMB, totalMB, elapsed.Round(time.Millisecond))
	if total == 0 {
		t.Fatal("không trích trang nào — pool chết")
	}
	const limitMB = 1200
	if peakMB > limitMB {
		t.Errorf("heap đỉnh %.0f MB vượt trần %d MB cho %d bài song song", peakMB, limitMB, pool)
	}
}

// TestExtractStressUnderMemoryLimit là câu hỏi "tràn RAM không" trả lời trực
// tiếp: đặt trần cứng cho heap rồi chạy. Nếu pipeline cần nhiều hơn trần,
// Go không crash ngay mà GC phải liên tục thu gom rác → chậm hàng trăm lần.
// Vì vậy test chỉ PASS khi chạy xong TRONG trần, và ta cảnh báo nếu phải vượt.
func TestExtractStressUnderMemoryLimit(t *testing.T) {
	const limit = 256 << 20
	debug.SetMemoryLimit(limit)
	defer debug.SetMemoryLimit(-1)

	raw := stressPDF(t, 500, 60)
	repo := &stressPageRepo{}
	arts := &stressArticles{}
	svc := NewArticleExtractService(arts, repo, &stressStorage{data: raw},
		extractor.OCRConfig{}, nil).(*articleExtractService)
	article := &domain.Article{BaseModel: domain.BaseModel{ID: 1}, StorageKey: "uploads/big.pdf"}

	start := time.Now()
	err := svc.extractOne(context.Background(), "stress", article)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("extractOne lỗi dưới trần %d MB: %v", limit>>20, err)
	}
	t.Logf("dưới trần %d MB: %d trang / %d lô ghi trong %v",
		limit>>20, repo.pages.Load(), repo.writes.Load(), elapsed.Round(time.Millisecond))
	// Ngưỡng "GC quá tay": vượt trần là pipeline cần >256MB thật.
	if elapsed > 3*time.Minute {
		t.Errorf("mất %v dưới trần %d MB — GC phải gồng quá mức, tức là pipeline cần vượt trần",
			elapsed.Round(time.Second), limit>>20)
	}
}

// compile-time: stub phải đúng interface thật.
var (
	_ domain.IArticlePageRepository = (*stressPageRepo)(nil)
	_ domain.ArticleExtractQueue    = (*stressArticles)(nil)
	_ storage.IStorage              = (*stressStorage)(nil)
)
