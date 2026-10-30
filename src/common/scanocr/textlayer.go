package scanocr

import (
	"context"
	"encoding/xml"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// EngineTextLayer là tên "engine" khi trang KHÔNG được OCR mà lấy chữ có sẵn
// trong PDF. Ghi vào Page.EngineName để tab Lịch sử phân biệt được trang nào máy
// đọc nguyên văn lớp chữ in sẵn, trang nào do Tesseract nhận diện.
const EngineTextLayer = "pdf-text-layer"

// textLayerConf là confidence của chữ lấy từ lớp chữ có sẵn: 100 vì đây là byte
// nguyên văn trong PDF, không phải suy đoán của engine. FE nhờ đó không tô amber
// những trang không cần người xem lại.
const textLayerConf = 100.0

// ExtractTextLayer đọc LỚP CHỮ SẴN CÓ của một trang PDF thành đúng cấu trúc
// Page như engine OCR trả về (từ + box + conf), nhờ `pdftotext -bbox`.
//
// VÌ SAO TỒN TẠI: trang đã có chữ thật (PDF xuất từ Word) không nên OCR — vừa
// chậm vừa kém chính xác hơn bản gốc. Tab Xem & sửa cần box để tô sáng và sửa
// tay, nên "có sẵn chữ" chỉ là tối ưu khi đọc được CẢ vị trí. pdftotext -bbox cho
// đúng thứ đó, miễn phí.
//
// Trang không có text layer trả Page có Lines rỗng chứ không phải lỗi — caller
// quyết định fallback sang OCR theo ngưỡng TextLayerMinChars.
func ExtractTextLayer(ctx context.Context, cfg Config, pdfPath string, pageNum int) (*Page, error) {
	if pageNum < 1 {
		return nil, fmt.Errorf("scanocr: số trang phải >= 1, nhận %d", pageNum)
	}
	cfg = cfg.Normalize()
	// -f/-l giới hạn đúng MỘT trang nên file 300 trang không bị parse cả lượt —
	// cùng nguyên tắc RAM theo trang như RenderPage.
	out, err := runPoppler(ctx, cfg, "pdftotext", "-bbox", "-enc", "UTF-8",
		"-f", strconv.Itoa(pageNum), "-l", strconv.Itoa(pageNum), pdfPath, "-")
	if err != nil {
		return nil, err
	}
	return parseBBox(string(out), pageNum, cfg.RenderDPI)
}

// parseBBox chuyển XML của `pdftotext -bbox` sang Page. Hàm thuần để test không
// cần poppler.
//
// poppler phát <word> theo THỨ TỰ ĐỌC (duyệt block → line → word) nên không phải
// sắp lại; ta chỉ gom word thành dòng theo toạ độ y.
func parseBBox(doc string, pageNum, dpi int) (*Page, error) {
	dec := xml.NewDecoder(strings.NewReader(doc))
	page := &Page{Num: pageNum, DPI: dpi, EngineName: EngineTextLayer}
	var cur *Line
	for {
		tk, err := dec.Token()
		if err != nil {
			break
		}
		el, isStart := tk.(xml.StartElement)
		if !isStart {
			continue
		}
		switch el.Name.Local {
		case "page":
			w, h, ok := pageSizeAttr(el)
			if ok {
				// -bbox trả Pt; đổi sang Px theo DPI render để Box và
				// NormalizedBox dùng chung một hệ với ảnh đã render.
				page.WidthPx = ptToPx(w, dpi)
				page.HeightPx = ptToPx(h, dpi)
			}
		case "word":
			word, text, ok := parseBBoxWord(dec, el, dpi)
			if !ok {
				continue
			}
			word.Text = text
			cur = appendWord(cur, page, word)
		}
	}
	if page.WidthPx <= 0 || page.HeightPx <= 0 {
		return nil, fmt.Errorf("scanocr: pdftotext -bbox không in kích thước trang %d", pageNum)
	}
	return page, nil
}

// parseBBoxWord đọc box của <word> rồi lấy text của nó (token kế tiếp).
//
// Toạ độ poppler phát ra tính từ GỐC TRÊN-TRÁI (khác hệ gốc dưới-trái của PDF)
// — đúng hệ Box khai báo nên chỉ cần đổi Pt sang Px, không lật trục.
func parseBBoxWord(dec *xml.Decoder, el xml.StartElement, dpi int) (Word, string, bool) {
	var x1, y1, x2, y2 float64
	for _, a := range el.Attr {
		v, _ := strconv.ParseFloat(a.Value, 64)
		switch a.Name.Local {
		case "xMin":
			x1 = v
		case "yMin":
			y1 = v
		case "xMax":
			x2 = v
		case "yMax":
			y2 = v
		}
	}
	if x2 <= x1 || y2 <= y1 {
		return Word{}, "", false
	}
	tk, err := dec.Token()
	if err != nil {
		return Word{}, "", false
	}
	cd, isChar := tk.(xml.CharData)
	if !isChar {
		return Word{}, "", false
	}
	text := strings.TrimSpace(string(cd))
	if text == "" {
		return Word{}, "", false
	}
	return Word{
		Box:  Box{X: ptToPx(x1, dpi), Y: ptToPx(y1, dpi), W: ptToPx(x2-x1, dpi), H: ptToPx(y2-y1, dpi)},
		Conf: textLayerConf,
	}, text, true
}

// appendWord nối từ vào dòng đang mở, hoặc mở dòng mới khi nhảy sang hàng khác.
//
// Ngưỡng "khác hàng" đo bằng CHIỀU CAO chữ: cùng dòng thì tâm hai từ lệch dưới
// nửa chiều cao. Dùng chiều cao thay vì số cố định để bản in 8pt và tiêu đề 18pt
// cùng được xử lý đúng.
func appendWord(cur *Line, page *Page, w Word) *Line {
	if cur != nil && cur.Box.H > 0 && math.Abs((w.Box.Y+w.Box.H/2)-(cur.Box.Y+cur.Box.H/2)) < cur.Box.H*0.5 {
		cur.Words = append(cur.Words, w)
		cur.Box = unionBox(cur.Box, w.Box)
		cur.Text = lineText(cur.Words)
		return cur
	}
	page.Lines = append(page.Lines, Line{
		Words: []Word{w},
		Box:   w.Box,
		Text:  w.Text,
		Conf:  w.Conf,
	})
	return &page.Lines[len(page.Lines)-1]
}

func lineText(words []Word) string {
	parts := make([]string, 0, len(words))
	for _, w := range words {
		parts = append(parts, w.Text)
	}
	return strings.Join(parts, " ")
}

func pageSizeAttr(el xml.StartElement) (w, h float64, ok bool) {
	for _, a := range el.Attr {
		v, err := strconv.ParseFloat(a.Value, 64)
		if err != nil {
			continue
		}
		switch a.Name.Local {
		case "width":
			w = v
		case "height":
			h = v
		}
	}
	return w, h, w > 0 && h > 0
}

func ptToPx(pt float64, dpi int) float64 {
	if dpi <= 0 {
		dpi = 300
	}
	return pt * float64(dpi) / 72.0
}
