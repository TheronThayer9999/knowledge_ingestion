// Package scanocr là engine OCR của TÍNH NĂNG tab Quét — OCR chữ in trong ảnh và
// PDF scan.
//
// TÁCH HẲN src/common/extractor (OCR nạp kho) vì mục tiêu ngược nhau: pipeline
// chỉ cần TEXT THÔ từng trang để chunk + embed, còn tính năng này cần TỪ + toạ độ
// + confidence để hiển thị, sửa tay và OCR lại theo vùng. Đi chung code thì mỗi
// lần chỉnh PSM/ngưỡng cho một bên là âm thầm
// đổi bên kia, nên package này tự chứa os/exec của riêng nó (trùng ~100 dòng với
// extractor là cái giá có chủ ý). Xem plans/2026-10-30-dot-31 §0.7.
package scanocr

// Box là hình chữ nhật bao một từ/dòng, đơn vị là PIXEL của ảnh đã render, gốc ở
// trên-trái (cả Tesseract lẫn pdftoppm đều dùng gốc này).
//
// Đây là kiểu NỘI BỘ lúc OCR. Muốn lưu DB hay vẽ lên trình duyệt thì đổi sang
// NormalizedBox: tỉ lệ 0..1 không phụ thuộc DPI, không phụ thuộc ảnh được scale
// thế nào trong CSS — hai điều mà autoai lẫn lộn nên toạ độ dễ lệch.
type Box struct {
	X, Y, W, H float64
}

// NormalizedBox là Box chia theo kích thước trang để ra tỉ lệ 0..1.
//
// Kích thước <= 0 trả box rỗng thay vì panic: trang lỗi giữa job vẫn phải ghi
// được row để người dùng thấy "trang này hỏng", không làm sập cả worker.
func (b Box) NormalizedBox(pageW, pageH float64) NormalizedBox {
	if pageW <= 0 || pageH <= 0 {
		return NormalizedBox{}
	}
	return NormalizedBox{X: b.X / pageW, Y: b.Y / pageH, W: b.W / pageW, H: b.H / pageH}
}

// NormalizedBox là vị trí theo TỈ LỆ 0..1 của trang — đơn vị duy nhất đi qua DB
// và sang FE. Muốn pixel thì nhân lại với kích thước ảnh đang hiển thị.
type NormalizedBox struct {
	X, Y, W, H float64
}

// ToBox ngược về pixel của một ảnh cụ thể (dùng khi cắt vùng để OCR lại).
func (n NormalizedBox) ToBox(pageW, pageH float64) Box {
	return Box{X: n.X * pageW, Y: n.Y * pageH, W: n.W * pageW, H: n.H * pageH}
}

// Word là một từ OCR đọc được, kèm vị trí và độ tự tin 0..100.
type Word struct {
	Text string  `json:"t"`
	Box  Box     `json:"-"`
	Conf float64 `json:"c"`
}

// Line là một dòng: box là hình bao chung của các từ, Text là các từ nối nhau
// bằng dấu cách — thứ người dùng nhìn thấy và sửa được.
type Line struct {
	Text  string  `json:"text"`
	Box   Box     `json:"-"`
	Conf  float64 `json:"conf"`
	Words []Word  `json:"words"`
}

// Page là kết quả OCR một trang ảnh. WidthPx/HeightPx là kích thước ảnh ĐÃ OCR —
// cần nó để đổi Box (px) sang tỉ lệ, nên đi cùng Page chứ không để caller tự nhớ.
type Page struct {
	Num        int     `json:"page"`
	WidthPx    float64 `json:"width_px"`
	HeightPx   float64 `json:"height_px"`
	DPI        int     `json:"dpi"`
	EngineName string  `json:"engine"`
	Lines      []Line  `json:"lines"`
}

// Text trả toàn văn trang theo đúng thứ tự dòng OCR sinh ra — phần FE hiển thị
// ở khung "chữ OCR" và là thứ pipeline PDF dùng khi người dùng sửa xong.
func (p *Page) Text() string {
	if p == nil || len(p.Lines) == 0 {
		return ""
	}
	var sb []byte
	for i, l := range p.Lines {
		if i > 0 {
			sb = append(sb, '\n')
		}
		sb = append(sb, l.Text...)
	}
	return string(sb)
}

// PageStats là thống kê phục vụ UI: bao nhiêu từ, bao nhiêu từ đáng ngờ, trung
// bình cộng confidence.
type PageStats struct {
	Words    int
	LowConf  int
	MeanConf float64
}

// Stats đếm theo TỪ (không theo dòng) vì người dùng sửa từng từ một.
// lowConf < 0 thì coi như không có ngưỡng, trả LowConf = 0.
func (p *Page) Stats(lowConfBelow float64) PageStats {
	if p == nil {
		return PageStats{}
	}
	var st PageStats
	var sum float64
	for _, l := range p.Lines {
		for _, w := range l.Words {
			st.Words++
			sum += w.Conf
			if w.Conf < lowConfBelow {
				st.LowConf++
			}
		}
	}
	if st.Words > 0 {
		st.MeanConf = sum / float64(st.Words)
	}
	return st
}
