package extractor

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"knowledge_ingestion/src/common/utils"
)

const (
	// maxDocxArchiveBytes trần bytes zip đọc vào RAM trước khi giải nén.
	// docx nén tốt nên 100MB zip đã là tài liệu khổng lồ. KHÁC txt, docx
	// BẮT BUỘC phải đọc hết zip vào RAM rồi mới giải nén được (zip yêu cầu
	// seek), nên đây là giới hạn thật chứ không phải lựa chọn.
	maxDocxArchiveBytes = 100 << 20
	// TextPieceBytes kích thước 1 mảnh khi chia file KHÔNG có khái niệm
	// trang (txt/md/csv/json) — 256KB.
	//
	// VÌ SAO CHIA: trước đây txt 100MB bị cắt cụt ở 20MB, tức là MẤT 80%
	// nội dung mà user không hề biết. Cắt cụt là kiểu hỏng im lặng — search
	// vẫn trả kết quả "đúng" với 1/5 dữ liệu. Nên nay chia thành nhiều Page
	// liên tiếp thay vì cắt: mất hết 100MB thì lấy đủ 100MB, và RAM vẫn
	// hằng số vì phase index đọc theo cửa sổ trượt.
	//
	// 256KB chọn vì nó < INDEX_WINDOW_MAX_CHARS, nên 1 mảnh = đúng 1 cửa sổ
	// và không bao giờ phải gộp. Số mảnh của file 100MB là ~400 row — Postgres
	// và index (article_id, seq) chịu nổi.
	TextPieceBytes = 256 << 10
)

// ErrUnsupported báo file đúng nhưng loại chưa trích được (ảnh, zip...)
// — service gặp lỗi này thì log + bỏ qua, kỳ sau vẫn thử lại (đợt sau hỗ
// trợ thêm loại nào thì tự ăn theo, không sửa service). So bằng errors.Is.
var ErrUnsupported = errors.New("loại file chưa hỗ trợ trích text")

// ErrInvalid báo file hỏng/xác định (docx/pdf lỗi cấu trúc, quá lớn, ảnh
// trắng không chữ) — blob immutable (key UUID, upload xong không đổi) nên thử
// lại cũng vậy. Khác lỗi transient (S3, DB, ollama, timeout, binary crash) là
// hạ tầng chập chờn, retry có ý nghĩa.
var ErrInvalid = errors.New("file không hợp lệ hoặc không trích được nội dung")

// IsPermanent báo lỗi có thử lại cũng vậy không — service gặp thì failed luôn
// thay vì đốt hết utils.MaxQueueAttempts rồi mới failed.
func IsPermanent(err error) bool {
	return errors.Is(err, ErrUnsupported) || errors.Is(err, ErrInvalid)
}

// Page là 1 mảnh text của bài, đơn vị nhỏ nhất phase extract ghi xuống DB.
//
// Num là SỐ TRANG THẬT để trích dẫn: PDF cho Num = số trang giấy (từ 1), còn
// txt/docx không có khái niệm trang nên Num = 0 cho MỌI mảnh. Num giữ nguyên
// suốt hệ thống — nó tới article_pages.page_num rồi tới PageNum của chunk,
// nên "trang N" trong câu trả lời là đúng số trang trong file gốc.
//
// Seq là thứ tự ĐỌC, tách khỏi Num vì 2 lý do:
//   - File txt 100MB bị chia thành ~400 mảnh cùng Num = 0, nên Num không còn
//     làm được con trỏ vị trí.
//   - phase index cần con trỏ "đọc tới đâu" để quét cửa sổ trượt và để resume.
//
// Seq tăng liên tục từ 1 trong suốt bài, bất kể loại file.
type Page struct {
	Num  int
	Seq  int
	Text string
}

// NeedsDespace báo loại file này có cần qua bước khử chữ rải không — chỉ PDF
// (chữ rải do renderer nhúng ký tự rời); txt/docx/ảnh OCR ra text sạch sẵn.
// Caller PHẢI hỏi thay vì tự suy đoán theo đuôi, để quyết định "cả file là
// chữ rải hay sạch" (extractor.Cleaner) nằm đúng 1 chỗ.
func NeedsDespace(key string) bool {
	return strings.EqualFold(utils.FileExt(key), ".pdf")
}

// ExtractRawPages trích text THÔ của từng trang rồi đẩy qua visit, KHÔNG khử
// chữ rải — vì sao tách vậy ở DespacePages.
//
// have là tập SEQ đã lưu ở lần trước (nil = chưa có gì). Với PDF, seq = số
// trang nên các trang trong have được BỎ QUA, còn lại trích song song theo
// khoảng (xem extractPDFPages). Phase extract ghi tiến trình vào bảng
// article_pages, nên job chết giữa file 500 trang thì kỳ sau chỉ trích phần
// còn thiếu — đây là thứ biến "mất 17 phút OCR" thành "mất tối đa một khoảng
// đang chạy". Loại file txt/docx không có khái niệm trang nên bỏ qua have và
// dựa vào dedupe theo seq trong visit.
//
// Text trả ra là THÔ. Bỏ dòng lặp + khử chữ rải để phase index lo khi đọc
// theo cửa sổ trượt (extractor.Cleaner), vì 2 việc đó cần nhìn CẢ file mà
// worker thì không ôm cả file trong RAM.
//
// visit không bao giờ được gọi đồng thời, nhưng thứ tự gọi không đảm bảo theo
// trang với PDF (dùng Seq để sắp). visit trả lỗi thì dừng và trả luôn lỗi đó
// (ctx hủy, DB chết...). ctx chỉ để tôn trọng hủy — quá trình to CPU/RAM cục
// bộ, không I/O mạng (trừ OCR gọi binary local).
// ctxReader kiểm tra ctx trước mỗi lần đọc — io.ReadAll/io.Copy trên blob lớn
// dừng ngay khi job bị hủy thay vì đọc hết file rồi mới thấy lỗi. Cần cho
// docx (đọc cả archive vào RAM) và cho phần spool PDF từ S3.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

func ExtractRawPages(ctx context.Context, key string, r io.Reader, ocr OCRConfig, have map[int]bool, visit func(Page) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Bọc reader để mọi đường đọc (text stream, docx, spool PDF) đều tôn
	// trọng hủy giữa chừng, không chỉ các lệnh exec bên dưới.
	r = &ctxReader{ctx: ctx, r: r}
	switch strings.ToLower(utils.FileExt(key)) {
	case ".txt", ".md", ".csv", ".json":
		// Loại này đọc được TUẦN TỰ nên stream thẳng ra các mảnh, không
		// gom cả file vào RAM rồi mới chia — file 100MB RAM vẫn chỉ 256KB.
		return plainTextPieces(r, visit)
	case ".docx":
		text, err := extractDocx(r)
		if err != nil {
			return err
		}
		return visitPieces(0, text, visit)
	case ".pdf":
		return extractPDFPages(ctx, r, ocr, have, visit)
	case ".jpg", ".jpeg", ".png", ".webp":
		text, err := OCRImage(ctx, ocr, key, r)
		if err != nil {
			return err
		}
		return visit(Page{Num: 0, Seq: 1, Text: text})
	default:
		return fmt.Errorf("%w: %q", ErrUnsupported, utils.FileExt(key))
	}
}

// ExtractAll trích HẾT text của file rồi mới trả về — dùng khi không cần ghi
// tiến trình từng lô (test, công cụ tay). Phase extract của worker không dùng
// hàm này: nó gọi ExtractRawPages để flush tiến trình, và việc khử chữ rải /
// bỏ dòng lặp đã chuyển sang phase index chạy theo cửa sổ trượt (xem Cleaner).
//
// VÌ SAO ĐỂ ĐÓNG KHUNG TOÀN FILE TRONG RAM: chi phí là vài MB text (đo thật ở
// pdf.go: 6M ký tự cho file 5644 trang) so với heap đỉnh 589MB mà thư viện
// parse PDF đã dùng. Chỉ dùng ở test — worker không đi đường này.
func ExtractAll(ctx context.Context, key string, r io.Reader, ocr OCRConfig) ([]Page, error) {
	var pages []Page
	if err := ExtractRawPages(ctx, key, r, ocr, nil, func(p Page) error {
		pages = append(pages, p)
		return nil
	}); err != nil {
		return nil, err
	}
	// PDF trích song song nên visit không theo thứ tự trang; sắp theo Seq để
	// người gọi (test, công cụ tay) luôn nhận đúng thứ tự đọc.
	slices.SortFunc(pages, func(a, b Page) int { return cmp.Compare(a.Seq, b.Seq) })
	if NeedsDespace(key) {
		c := NewCleaner(len(pages), RepeatOptions{})
		for _, p := range pages {
			c.Scan(p.Text)
		}
		for i := range pages {
			pages[i].Text = c.Apply(pages[i].Text)
		}
	}
	return pages, nil
}

// ExtractPages trích text rồi đẩy từng trang qua visit — wrapper mỏng giữ
// hợp đồng cũ cho test/công cụ đọc file. Phase extract dùng ExtractRawPages
// (xem file này) vì cần ghi tiến trình từng lô trước khi khử chữ rải.
func ExtractPages(ctx context.Context, key string, r io.Reader, ocr OCRConfig, visit func(Page) error) error {
	pages, err := ExtractAll(ctx, key, r, ocr)
	if err != nil {
		return err
	}
	for _, pg := range pages {
		if err := visit(pg); err != nil {
			return err
		}
	}
	return nil
}

// plainTextPieces đọc text thuần theo dòng và đẩy ra từng mảnh TextPieceBytes,
// KHÔNG cắt cụt — mất dữ liệu là không chấp nhận được, nên cách giới hạn
// chuyển từ "cắt bỏ phần thừa" sang "chia nhỏ để RAM vẫn hằng số".
//
// Đọc theo bufio.Reader từng khối: RAM đỉnh = buffer đọc + 1 mảnh, không phụ
// thuộc kích thước file.
//
// Cắt ranh giới mảnh ở CHỮ XUỐNG DÒNG gần nhất chứ không cắt cứng 256KB —
// cắt giữa rune làm hỏng text, cắt giữa từ làm mất nghĩa câu. Mảnh hơi vượt
// 256KB là chấp nhận được; đổi lại không bao giờ sinh ký tự hỏng.
func plainTextPieces(r io.Reader, visit func(Page) error) error {
	br := bufio.NewReaderSize(r, 64<<10)
	var seq int
	var buf []byte
	seq = 1
	emit := func() error {
		if len(buf) == 0 {
			return nil
		}
		p := Page{Num: 0, Seq: seq, Text: string(buf)}
		seq++
		buf = buf[:0]
		return visit(p)
	}
	for {
		// Đọc tới vùng đệm, nhưng cắt tại newline gần nhất trước đó: tìm từ
		// CUỐI buffer trở lại để không phải giữ cả mảnh trong RAM.
		chunk, err := br.ReadSlice('\n')
		if len(buf)+len(chunk) > TextPieceBytes && len(buf) > 0 {
			cut := lastNewline(buf)
			if cut >= 0 {
				// Giữ luôn newline vào mảnh hiện tại: bỏ nó là mất 1 byte mỗi lần cắt.
				if verr := visit(Page{Num: 0, Seq: seq, Text: string(buf[:cut+1])}); verr != nil {
					return verr
				}
				seq++
				buf = append(buf[:0], buf[cut+1:]...)
			}
		}
		buf = append(buf, chunk...)
		if err == nil {
			continue
		}
		if err == io.EOF {
			break
		}
		return fmt.Errorf("đọc file text: %w", err)
	}
	return emit()
}

// lastNewline trả vị trí newline CUỐI CÙNG trong buf, hoặc -1. Dùng '\n' làm
// mốc chứ không phải bất kỳ khoảng trắng nào: cắt ở ranh giới từ vẫn để lại
// câu đứt dở, cắt ở newline thì mất nhiều nhất 1 ký tự cuối dòng.
func lastNewline(buf []byte) int {
	for i := len(buf) - 1; i >= 0; i-- {
		if buf[i] == '\n' {
			return i
		}
	}
	return -1
}

// visitPieces chia text đã nằm sẵn trong RAM (docx, ảnh OCR) thành các Page
// Num = 0, Seq tăng dần. Cùng kết quả với plainTextPieces nhưng nguồn đã
// trong RAM rồi nên không cần stream.
func visitPieces(num int, text string, visit func(Page) error) error {
	if text == "" {
		return visit(Page{Num: num, Seq: 1, Text: text})
	}
	var seq int
	for len(text) > TextPieceBytes {
		cut := lastNewlineString(text[:TextPieceBytes])
		if cut <= 0 {
			cut = TextPieceBytes
		} else {
			cut++ // giữ luôn newline vào mảnh hiện tại
		}
		seq++
		if err := visit(Page{Num: num, Seq: seq, Text: text[:cut]}); err != nil {
			return err
		}
		text = text[cut:]
	}
	seq++
	return visit(Page{Num: num, Seq: seq, Text: text})
}

func lastNewlineString(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '\n' {
			return i
		}
	}
	return -1
}
