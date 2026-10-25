package extractor

import (
	"strings"
)

// ĐỌC LẠI SHEET TỪ TEXT ĐÃ TRÍCH — dùng cho tool agent (list_sheets/read_sheet).
//
// VÌ SAO KHÔNG MỞ LẠI BLOB: nội dung sheet đã nằm trong article_pages (đúng
// thứ tự, đúng cột) sau phase extract. Mở lại blob phải kéo cả file từ
// SeaweedFS rồi parse lại zip+XML chỉ để trả lời "file có sheet nào" — chậm,
// tốn RAM, và có thể sai lệch với bản đã đánh index.
//
// HỢP ĐỒNG ĐỊNH DẠNG: xlsx.go sinh "## Sheet: <tên>" (hoặc "## Sheet <n>" khi
// thiếu tên) rồi mỗi hàng 1 dòng pipe "| a | b |". Đổi cách sinh text thì phải
// sửa parser này — test ở sheets_test.go khoá cả hai đầu.

// sheetHeaderPrefix là tiền tố dòng tiêu đề sheet do extractXlsx sinh ra.
const sheetHeaderPrefix = "## Sheet"

// SheetBlock là 1 sheet trong text .xlsx đã trích.
type SheetBlock struct {
	// Index là số thứ tự 1-based theo thứ tự trong file — chính là con số
	// trong "Sheet N", dùng để agent trích dẫn và chọn sheet.
	Index int `json:"index"`
	// Name là tên sheet người dùng đặt; dạng dự phòng "2" khi workbook thiếu
	// tên (xlsx.go ghi "## Sheet 2").
	Name string `json:"name"`
	// Header là DÒNG ĐẦU của sheet (thường là tên cột), giữ nguyên dòng pipe.
	// Rỗng khi sheet không có dòng nào.
	Header string `json:"header,omitempty"`
	// Columns là Header đã tách ô — số phần tử đúng bằng số cột của sheet vì
	// xlsx.go giữ vị trí cột (ô trống vẫn có mặt).
	Columns []string `json:"columns,omitempty"`
	// Rows là các dòng dữ liệu SAU header, mỗi phần tử 1 dòng pipe.
	Rows []string `json:"rows,omitempty"`
	// Chars là số ký tự nội dung sheet — để LLM biết sheet to/nhỏ mà chọn
	// cách đọc (read_sheet đọc theo hàng).
	Chars int `json:"chars"`
}

// TotalRows trả số hàng dữ liệu, KHÔNG tính dòng header.
func (s SheetBlock) TotalRows() int { return len(s.Rows) }

// ParseSheetBlocks tách text .xlsx đã trích thành các khối sheet theo thứ tự
// trong file. Trả nil khi text không có dòng tiêu đề nào (không phải Excel,
// hoặc file Excel không có sheet).
//
// Dòng không phải pipe (dòng trống giữa các khối) bị bỏ — chúng không mang
// dữ liệu, và nếu tính là hàng thì số hàng sẽ sai.
func ParseSheetBlocks(text string) []SheetBlock {
	var (
		blocks []SheetBlock
		cur    *SheetBlock
	)
	flush := func() {
		if cur == nil {
			return
		}
		cur.Chars += len(cur.Header) + 1
		blocks = append(blocks, *cur)
		cur = nil
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if name, ok := sheetHeaderName(line); ok {
			flush()
			cur = &SheetBlock{Index: len(blocks) + 1, Name: name}
			continue
		}
		if cur == nil || !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		line = strings.TrimSpace(line)
		if cur.Header == "" {
			cur.Header = line
			cur.Columns = pipeCells(line)
			continue
		}
		cur.Rows = append(cur.Rows, line)
		cur.Chars += len(line) + 1
	}
	flush()
	return blocks
}

// sheetHeaderName nhận cả 2 dạng tiêu đề do extractXlsx sinh ra:
// "## Sheet: Biểu phí" (có tên) và "## Sheet 2" (thiếu tên, dự phòng).
func sheetHeaderName(line string) (string, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(line), sheetHeaderPrefix)
	if !ok {
		return "", false
	}
	rest = strings.TrimSpace(rest)
	rest = strings.TrimSpace(strings.TrimPrefix(rest, ":"))
	return rest, rest != ""
}

// pipeCells tách dòng "| a |  | c |" thành ["a", "", "c"] — ô trống phải giữ
// vị trí, nếu lọc bỏ thì cột bị lệch so với header.
func pipeCells(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	cells := make([]string, len(parts))
	for i, p := range parts {
		cells[i] = strings.TrimSpace(p)
	}
	return cells
}
