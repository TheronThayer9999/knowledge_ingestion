package extractor

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/logs"
)

const (
	// maxPDFBytes trần bytes PDF được nhận. File được spool ra đĩa nên trần
	// này không còn là trần RAM — chỉ chặn file quá khổ.
	maxPDFBytes = 100 << 20
	// maxPDFPages trần số trang parse mỗi file — PDF khai gian page tree để
	// treo worker thì lỗi sớm thay vì loop hàng giờ.
	maxPDFPages = 10000
	// pdfTextChunkPages số trang tối đa trong 1 khoảng gửi cho một lần gọi
	// pdftotext. Mỗi lần gọi phải phân tích lại xref nên không đặt quá nhỏ;
	// cũng không quá lớn để tiến trình con và số khoảng song song vẫn cân.
	pdfTextChunkPages = 200
	// pdfProgressEvery log tiến trình mỗi N trang — xem chỗ dùng.
	pdfProgressEvery = 100
)

// pdfRange là khoảng trang liên tiếp [from, to] (1-based, gồm cả hai đầu).
type pdfRange struct{ from, to int }

// extractPDFPages trích text PDF bằng poppler (pdfinfo + pdftotext, cùng gói
// với pdftoppm dùng để render OCR) rồi đẩy từng trang qua visit.
//
// VÌ SAO KHÔNG DÙNG gopdf NỮA: thư viện chỉ nhận []byte nên cả PDF phải nằm
// trong RAM, đo thật trên PDF 90MB (3629 trang) đỉnh ~920MB. pdftotext đọc từ
// file trên đĩa và ghi text ra stdout: đỉnh 79MB, 14s, không NUL.
//
// SONG SONG THEO KHOẢNG: các trang chưa có trong have được gom thành khoảng
// liên tiếp (tối đa pdfTextChunkPages), rồi EXTRACT_PAGE_WORKERS goroutine
// xử lý song song — OCR trang scan là phần đắt nên đây là chỗ được lợi. Mọi
// luồng đọc chung 1 file spool. visit được gọi dưới mutex nên không bao giờ
// đồng thời; thứ tự gọi không cố định, nhưng Seq = số trang nên phase index
// vẫn đọc đúng thứ tự.
//
// Text là TEXT THÔ theo từng trang. Khử chữ rải và bỏ dòng lặp header/footer
// vẫn do phase index đảm nhiệm theo cửa sổ trượt (extractor.Cleaner).
//
// have là tập số trang (= seq) đã lưu ở lần trước; các trang đó không trích lại.
//
// Visit lỗi thì huỷ các khoảng còn lại và trả lỗi đó; phần đã ghi giữ nguyên
// và lần extract sau resume theo tập trang còn thiếu.
func extractPDFPages(ctx context.Context, r io.Reader, cfg extractConfig, have map[int]bool, visit func(Page) error) error {
	start := time.Now()
	ocr := cfg.ocr
	if ocr.RenderBinary == "" {
		return fmt.Errorf("cần ocr.render_binary (poppler) để đọc PDF")
	}
	pdfinfo := popplerTool(ocr.RenderBinary, "pdfinfo")
	pdftotext := popplerTool(ocr.RenderBinary, "pdftotext")

	path, err := spoolPDF(r)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(path) }()

	n, err := pdfPageCount(ctx, pdfinfo, path)
	if err != nil {
		return err
	}
	if n > maxPDFPages {
		return fmt.Errorf("%w: pdf có %d trang, vượt trần %d", ErrInvalid, n, maxPDFPages)
	}
	ranges := pendingPDFRanges(n, have)
	if len(ranges) == 0 {
		return nil
	}
	todo := 0
	for _, rg := range ranges {
		todo += rg.to - rg.from + 1
	}

	// Đo bố cục phải chạy TUẦN TỰ và xong TRƯỚC khi các goroutine trích bắt
	// đầu — chúng chỉ đọc map này (đọc song song map đã ghi xong là an toàn).
	// Lỗi ở bước đo không được làm hỏng extract: điểm bố cục là thông tin phụ,
	// thiếu thì coi như 0 và lần trích sau (khi được bật lại) sẽ đo bù.
	var layoutScores map[int]float64
	if cfg.layout {
		layoutScores = make(map[int]float64, todo)
		for _, rg := range ranges {
			err := streamPDFText(ctx, pdftotext, path, rg.from, rg.to, true, func(num int, text string) error {
				layoutScores[num] = DetectLayout(text).Score()
				return nil
			})
			if err != nil {
				logs.Warnw("pdf: đo bố cục lỗi, bỏ qua bước này",
					"from", rg.from, "to", rg.to, "error", err)
				layoutScores = nil
				break
			}
		}
	}

	var (
		emitMu    sync.Mutex
		extracted atomic.Int64
		ocrPages  atomic.Int64
	)
	runRange := func(rg pdfRange) error {
		return streamPDFText(ctx, pdftotext, path, rg.from, rg.to, false, func(num int, text string) error {
			// Log tiến trình mỗi pdfProgressEvery trang. Bắt buộc về mặt vận
			// hành: job nặng chạy nhiều phút, không có dòng nào thì lúc chết
			// không biết đang ở đâu.
			if d := extracted.Add(1); d%pdfProgressEvery == 0 || d == int64(todo) {
				logs.Infow("pdf: đang trích text",
					"page", num, "pages_done", d, "pages_todo", todo,
					"ocr_pages", ocrPages.Load(),
					"elapsed_ms", time.Since(start).Milliseconds())
			}
			// Điều hướng OCR rẻ: chỉ trang KHÔNG có chữ mới render (trừ khi bật
			// ocr.min_text_chars — xem OCRConfig.needsOCR). Đa số trang có text
			// layer nên vòng này gần như không tốn gì.
			if ocr.Enabled && ocr.needsOCR(text) {
				// Lỗi ở bước nào thì giữ nguyên phần text đã có, không fail cả
				// file vì 1 trang. Khi OCR có chạy trên trang đã có text layer
				// thì MergeOCRText bỏ phần trùng, chỉ nối phần ảnh bổ sung.
				text = MergeOCRText(text, ocrScannedPage(ctx, ocr, path, num))
				if strings.TrimSpace(text) != "" {
					ocrPages.Add(1)
				}
			}
			// PDF có trang thật nên Seq = Num: con trỏ đọc của phase index dùng
			// chung 1 trục với số trang.
			emitMu.Lock()
			defer emitMu.Unlock()
			return visit(Page{Num: num, Seq: num, Text: text, Layout: layoutScores[num]})
		})
	}
	return runPDFRanges(ctx, ranges, constants.EXTRACT_PAGE_WORKERS, runRange)
}

// pendingPDFRanges gom các trang 1..n chưa có trong have thành khoảng liên
// tiếp, mỗi khoảng tối đa pdfTextChunkPages trang.
func pendingPDFRanges(n int, have map[int]bool) []pdfRange {
	var out []pdfRange
	for p := 1; p <= n; p++ {
		if have[p] {
			continue
		}
		rg := pdfRange{from: p, to: p}
		for rg.to+1 <= n && !have[rg.to+1] && rg.to-rg.from+1 < pdfTextChunkPages {
			rg.to++
		}
		out = append(out, rg)
		p = rg.to
	}
	return out
}

// runPDFRanges chạy fn trên từng khoảng bằng tối đa `workers` goroutine. Khoảng
// nào lỗi thì huỷ các khoảng còn lại và trả lỗi đầu tiên. ctx bị huỷ từ ngoài
// thì trả ctx.Err().
func runPDFRanges(ctx context.Context, ranges []pdfRange, workers int, fn func(pdfRange) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg       sync.WaitGroup
		once     sync.Once
		firstErr error
	)
	fail := func(err error) {
		once.Do(func() {
			firstErr = err
			cancel()
		})
	}
	jobs := make(chan pdfRange)
	for range min(workers, len(ranges)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Goroutine con không có gin.Recovery: panic ở đây sẽ giết cả
			// process. Biến thành lỗi để cả lô dừng và trả lỗi cho caller.
			defer func() {
				if rec := recover(); rec != nil {
					fail(fmt.Errorf("panic khi trích PDF: %v", rec))
				}
			}()
			for rg := range jobs {
				// ctx hủy (range khác lỗi hoặc job timeout): bỏ các range còn
				// trong queue, không khởi động thêm pdftotext/OCR vô ích.
				if ctx.Err() != nil {
					return
				}
				if err := fn(rg); err != nil {
					fail(err)
				}
			}
		}()
	}
feed:
	for _, rg := range ranges {
		select {
		case jobs <- rg:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

// popplerTool suy đường dẫn công cụ poppler khác (pdfinfo, pdftotext) từ
// ocr.render_binary (pdftoppm) — cùng thư mục, cùng đuôi (.exe trên Windows).
// Nếu render_binary chỉ là tên (nằm trên PATH) thì trả về tên không kèm thư mục.
func popplerTool(renderBinary, name string) string {
	ext := filepath.Ext(renderBinary)
	if !strings.ContainsAny(renderBinary, `/\`) {
		return name + ext
	}
	return filepath.Join(filepath.Dir(renderBinary), name+ext)
}

// spoolPDF ghi stream PDF xuống file tạm, không giữ trong RAM. Vượt maxPDFBytes
// thì từ chối (ErrInvalid), không cắt bớt.
func spoolPDF(r io.Reader) (string, error) {
	f, err := os.CreateTemp("", "pdfextract-*.pdf")
	if err != nil {
		return "", fmt.Errorf("tạo file tạm cho pdf: %w", err)
	}
	path := f.Name()
	n, err := io.Copy(f, io.LimitReader(r, maxPDFBytes+1))
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("ghi file tạm pdf: %w", err)
	}
	if n > maxPDFBytes {
		_ = os.Remove(path)
		return "", fmt.Errorf("%w: file pdf quá lớn (>%d bytes)", ErrInvalid, maxPDFBytes)
	}
	return path, nil
}

// pdfPageCount đọc dòng "Pages:" của pdfinfo. Lỗi thì phân biệt được file có
// mật khẩu với file hỏng để log nói rõ nguyên nhân.
func pdfPageCount(ctx context.Context, pdfinfo, path string) (int, error) {
	out, err := exec.CommandContext(ctx, pdfinfo, path).Output()
	if err != nil {
		return 0, pdfToolError("pdfinfo", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		key, val, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) != "Pages" {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(val))
		if err != nil || n <= 0 {
			break
		}
		return n, nil
	}
	return 0, fmt.Errorf("%w: pdf không có trang nào", ErrInvalid)
}

// streamPDFText chạy pdftotext cho khoảng trang [from, to] và đẩy từng trang
// qua visit với số trang thật. pdftotext ngăn các trang bằng ký tự form feed
// (\f), nên đọc stdout theo từng \f: RAM chỉ giữ 1 trang text.
//
// layout=true thêm cờ `-layout`: giữ khoảng trắng canh cột của bảng. Dùng cho
// lượt ĐO bố cục; lượt trích chính vẫn chạy không -layout vì text đã canh cột
// sẽ làm chunker cắt sai và làm phình token (nhiều khoảng trắng vô nghĩa).
func streamPDFText(ctx context.Context, pdftotext, path string, from, to int, layout bool, visit func(num int, text string) error) error {
	args := []string{"-enc", "UTF-8"}
	if layout {
		args = append(args, "-layout")
	}
	args = append(args, "-f", strconv.Itoa(from), "-l", strconv.Itoa(to), path, "-")
	cmd := exec.CommandContext(ctx, pdftotext, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("pipe pdftotext: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return pdfToolError("pdftotext", err)
	}

	stop := func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
	br := bufio.NewReaderSize(stdout, 64<<10)
	num := from
	for {
		chunk, rerr := br.ReadString('\f')
		if chunk != "" {
			if num > to {
				stop()
				return fmt.Errorf("pdftotext trả nhiều hơn %d trang (khoảng %d-%d)", to-from+1, from, to)
			}
			if err := visit(num, cleanPageText(strings.TrimSuffix(chunk, "\f"))); err != nil {
				stop()
				return err
			}
			num++
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			stop()
			return fmt.Errorf("đọc đầu ra pdftotext: %w", rerr)
		}
	}
	if err := cmd.Wait(); err != nil {
		return pdfToolError("pdftotext", err, stderr.String())
	}
	if num != to+1 {
		return fmt.Errorf("pdftotext trả %d/%d trang (khoảng %d-%d)", num-from, to-from+1, from, to)
	}
	return nil
}

// pdfToolError gói lỗi chạy poppler. Thông báo về mật khẩu được tách riêng vì
// thử lại cũng vô ích (blob immutable), cần người xử lý file.
func pdfToolError(tool string, err error, stderr ...string) error {
	msg := ""
	if len(stderr) > 0 {
		msg = firstLine(stderr[0])
	}
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("không chạy được %s (cần poppler-utils): %w", tool, err)
	}
	if strings.Contains(strings.ToLower(msg), "password") {
		return fmt.Errorf("%w: pdf được bảo vệ bằng mật khẩu, không đọc được nội dung", ErrInvalid)
	}
	return fmt.Errorf("%w: %s lỗi: %v %s", ErrInvalid, tool, err, msg)
}

// ocrScannedPage render 1 trang scan thành PNG rồi OCR — lỗi thì "".
func ocrScannedPage(ctx context.Context, ocr OCRConfig, pdfPath string, page int) string {
	dpi := ocr.RenderDPI
	if dpi <= 0 {
		dpi = 300
	}
	png, err := pdfPageRenderer(ctx, ocr.RenderBinary, pdfPath, page, dpi)
	if err != nil {
		return ""
	}
	text, err := OCRImage(ctx, ocr, "page.png", bytes.NewReader(png))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(text)
}

// pdfPageRenderer render 1 trang PDF thành PNG — package var để test inject
// fake mà không cần renderer thật. Mặc định dùng pdftoppm (chuẩn poppler):
// pdftoppm -r <dpi> -png -f <page> -l <page> in.pdf prefix → prefix-<page>.png.
var pdfPageRenderer = renderPDFPagePdftoppm

func renderPDFPagePdftoppm(ctx context.Context, binary, pdfPath string, page, dpi int) ([]byte, error) {
	dir, err := os.MkdirTemp("", "pdfrender-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	prefix := filepath.Join(dir, "page")
	cmd := exec.CommandContext(ctx, binary,
		"-r", strconv.Itoa(dpi), "-png",
		"-f", strconv.Itoa(page), "-l", strconv.Itoa(page),
		pdfPath, prefix)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("render trang %d: %w (%s)", page, err, firstLine(stderr.String()))
	}
	png, err := os.ReadFile(prefix + "-" + strconv.Itoa(page) + ".png")
	if err != nil {
		return nil, fmt.Errorf("đọc ảnh trang %d: %w", page, err)
	}
	return png, nil
}

// cleanPageText bỏ dòng trống và khoảng thừa đầu/cuối từng dòng, giữ ngắt dòng
// để chunker cắt đúng biên dòng. Khử chữ rải làm ở mức cả file (phase index).
func cleanPageText(s string) string {
	var sb strings.Builder
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			sb.WriteString(t)
			sb.WriteString("\n")
		}
	}
	return strings.TrimSpace(sb.String())
}
