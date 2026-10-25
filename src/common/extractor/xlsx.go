package extractor

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

// extractXlsx trích text từ Excel (.xlsx) chỉ bằng stdlib — xlsx cũng là ZIP
// chứa XML y như docx, nên không thêm dependency nào. Chỉ mở entry trong
// archive, không bung ra đĩa.
//
// Quy ước xuất (khớp docx để chunker xử lý cùng một kiểu):
//   - mỗi sheet 1 khối, mở đầu "## Sheet: <tên>" để chunk mang ngữ cảnh —
//     "mức phạt" ở sheet nào, không phải đoán theo chunk_index;
//   - mỗi hàng 1 dòng pipe "| a | b |" → chunker cắt theo hàng, LLM thấy cột;
//   - ô trống GIỮ CHỖ theo toạ độ cột (A1 + C1 ⇒ "| a |  | c |") vì Excel bỏ
//     hẳn ô rỗng khỏi XML — đếm theo thứ tự xuất hiện là lệch cột ngay, "mức
//     phạt" rơi vào cột "hành vi";
//   - chỉ đọc GIÁ TRỊ đã lưu (<v>): không tính công thức, ô ngày ra serial
//     Excel thay vì ngày. Muốn đúng định dạng phải parse style của từng ô,
//     đắt hơn nhiều so với giá trị mang lại;
//   - ảnh/biểu đồ nhúng không trích (ca đó đi qua tool agent — RAG §6.12).
func extractXlsx(r io.Reader) (string, error) {
	zr, err := openOfficeZip(r, "xlsx")
	if err != nil {
		return "", err
	}
	shared, err := xlsxSharedStrings(zr)
	if err != nil {
		return "", err
	}
	sheets := xlsxSheets(zr)
	if len(sheets) == 0 {
		return "", fmt.Errorf("%w: file xlsx không có sheet nào", ErrInvalid)
	}
	var blocks []string
	for _, sh := range sheets {
		rows, err := xlsxRows(sh.file, shared)
		if err != nil {
			return "", err
		}
		if len(rows) == 0 {
			// Sheet chỉ có ảnh/chưa có giá trị → bỏ, không sinh khối rỗng.
			continue
		}
		blocks = append(blocks, sh.title()+"\n"+strings.Join(rows, "\n"))
	}
	return strings.TrimSpace(strings.Join(blocks, "\n\n")), nil
}

// xlsxSheet là 1 sheet theo đúng thứ tự workbook (thứ tự người dùng thấy),
// kèm entry XML để đọc nội dung. name rỗng nghĩa là không đọc được tên (file
// thiếu workbook.xml) — num giữ số thứ tự để vẫn có tiêu đề phân biệt.
type xlsxSheet struct {
	name string
	num  int
	file *zip.File
}

// title là dòng mở đầu khối. Sheet có tên thì "## Sheet: Biểu phí", không có
// tên thì "## Sheet 2" — không viết "## Sheet: Sheet 2" hai lần chữ Sheet.
func (s xlsxSheet) title() string {
	if s.name == "" {
		return "## Sheet " + strconv.Itoa(s.num)
	}
	return "## Sheet: " + s.name
}

// xlsxSharedStrings đọc bảng chuỗi dùng chung (xl/sharedStrings.xml). Thiếu
// entry này là bình thường — file chỉ có số hoặc dùng inlineStr — nên trả nil
// chứ không báo lỗi.
func xlsxSharedStrings(zr *zip.Reader) ([]string, error) {
	f := findZipEntry(zr, "xl/sharedStrings.xml")
	if f == nil {
		return nil, nil
	}
	data, err := readZipEntry(f)
	if err != nil {
		return nil, err
	}
	var out []string
	var cur strings.Builder
	inSI, inT := false, false
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: parse xl/sharedStrings.xml: %v", ErrInvalid, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "si":
				inSI = true
				cur.Reset()
			case "t":
				// Chuỗi giàu định dạng có nhiều <r><t> trong 1 <si> — nối lại
				// vì text vẫn là một câu liền.
				inT = inSI
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "si":
				out = append(out, cur.String())
				inSI = false
			case "t":
				inT = false
			}
		case xml.CharData:
			if inT {
				cur.WriteString(string(t))
			}
		}
	}
	return out, nil
}

// xlsxSheets trả các sheet theo thứ tự trong xl/workbook.xml. Tên sheet nằm ở
// workbook, còn file XML của sheet nối qua r:id → xl/_rels/workbook.xml.rels.
// Thiếu 1 trong 2 (file lạ) thì rơi về thứ tự entry xl/worksheets/sheetN.xml:
// vẫn đọc đủ nội dung, chỉ mất tên sheet.
func xlsxSheets(zr *zip.Reader) []xlsxSheet {
	numbered := numberedEntries(zr, "xl/worksheets/sheet")
	if len(numbered) == 0 {
		return nil
	}
	names, ids := xlsxWorkbookSheetList(zr)
	if len(names) == 0 {
		out := make([]xlsxSheet, 0, len(numbered))
		for _, n := range numbered {
			out = append(out, xlsxSheet{num: n.num, file: n.file})
		}
		return out
	}
	rels := xlsxWorkbookRels(zr)
	out := make([]xlsxSheet, 0, len(names))
	for i, name := range names {
		var f *zip.File
		if i < len(ids) {
			f = rels[ids[i]]
		}
		// Quan hệ thiếu/hỏng thì lấy sheet thứ i theo số — thà mất tên sheet
		// còn hơn mất cả sheet.
		if f == nil && i < len(numbered) {
			f = numbered[i].file
		}
		if f == nil {
			continue
		}
		out = append(out, xlsxSheet{name: name, num: i + 1, file: f})
	}
	return out
}

// xlsxWorkbookSheetList trả tên + r:id của từng sheet theo thứ tự khai báo
// trong xl/workbook.xml.
func xlsxWorkbookSheetList(zr *zip.Reader) (names, ids []string) {
	f := findZipEntry(zr, "xl/workbook.xml")
	if f == nil {
		return nil, nil
	}
	data, err := readZipEntry(f)
	if err != nil {
		return nil, nil
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "sheet" {
			continue
		}
		var name, relID string
		for _, a := range se.Attr {
			switch a.Name.Local {
			case "name":
				name = a.Value
			case "id":
				// r:id (namespace quan hệ) — sheetId là số nội bộ của Excel,
				// KHÔNG dùng để nối file.
				relID = a.Value
			}
		}
		if name == "" {
			continue
		}
		names = append(names, name)
		ids = append(ids, relID)
	}
	return names, ids
}

// xlsxWorkbookRels đọc xl/_rels/workbook.xml.rels, trả map rId → entry XML.
// Hỏng/thiếu rels chỉ làm mất tên sheet (xem xlsxSheets), không làm hỏng lượt
// trích, nên lỗi ở đây bị nuốt có chủ ý.
func xlsxWorkbookRels(zr *zip.Reader) map[string]*zip.File {
	f := findZipEntry(zr, "xl/_rels/workbook.xml.rels")
	if f == nil {
		return nil
	}
	data, err := readZipEntry(f)
	if err != nil {
		return nil
	}
	out := make(map[string]*zip.File)
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "Relationship" {
			continue
		}
		var id, target string
		for _, a := range se.Attr {
			switch a.Name.Local {
			case "Id":
				id = a.Value
			case "Target":
				target = a.Value
			}
		}
		if id == "" || target == "" {
			continue
		}
		if e := findZipEntry(zr, xlsxRelPath(target)); e != nil {
			out[id] = e
		}
	}
	return out
}

// xlsxRelPath chuẩn hoá Target của quan hệ thành tên entry trong archive.
// Target thường là "worksheets/sheet1.xml" (tương đối so với xl/) nhưng cũng
// gặp dạng tuyệt đối "/xl/worksheets/sheet1.xml" (tính từ gốc archive).
func xlsxRelPath(target string) string {
	if strings.HasPrefix(target, "/") {
		return strings.TrimPrefix(path.Clean(target), "/")
	}
	return path.Clean(path.Join("xl", target))
}

// xlsxRows đọc 1 sheet thành các dòng pipe.
func xlsxRows(f *zip.File, shared []string) ([]string, error) {
	data, err := readZipEntry(f)
	if err != nil {
		return nil, err
	}
	var rows, cells []string
	var val, inline strings.Builder
	inCell, inV, inT := false, false, false
	cellType := ""
	col := 0

	flushCell := func() {
		if !inCell {
			return
		}
		var s string
		switch cellType {
		case "s":
			// t="s": <v> là CHỈ SỐ vào bảng sharedStrings.
			s = xlsxSharedAt(shared, strings.TrimSpace(val.String()))
		case "inlineStr":
			s = inline.String()
		default:
			s = val.String()
		}
		s = xlsxCellText(s)
		if col <= 0 {
			// Ô không có toạ độ (hiếm) thì nối tiếp — vẫn hơn là làm lệch cả hàng.
			cells = append(cells, s)
		} else {
			for len(cells) < col-1 {
				cells = append(cells, "")
			}
			if len(cells) < col {
				cells = append(cells, s)
			} else {
				cells[col-1] = s
			}
		}
		inCell, cellType, col = false, "", 0
		val.Reset()
		inline.Reset()
	}

	flushRow := func() {
		flushCell()
		for len(cells) > 0 && strings.TrimSpace(cells[len(cells)-1]) == "" {
			cells = cells[:len(cells)-1]
		}
		if len(cells) > 0 {
			rows = append(rows, "| "+strings.Join(cells, " | ")+" |")
		}
		cells = nil
	}

	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: parse %s: %v", ErrInvalid, f.Name, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				cells = nil
			case "c":
				// Phòng XML thiếu </c>: chốt ô trước rồi mở ô mới.
				flushCell()
				inCell = true
				val.Reset()
				inline.Reset()
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "r":
						col = xlsxColIndex(a.Value)
					case "t":
						cellType = a.Value
					}
				}
			case "v":
				inV = inCell
			case "t":
				// Chỉ <is><t> của inlineStr mới có text nội tuyến.
				inT = inCell
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "c":
				flushCell()
			case "row":
				flushRow()
			case "v":
				inV = false
			case "t":
				inT = false
			}
		case xml.CharData:
			if inV {
				val.WriteString(string(t))
			} else if inT {
				inline.WriteString(string(t))
			}
		}
	}
	// File thiếu </row> ở cuối (hiếm) vẫn giữ được hàng đã đọc.
	flushRow()
	return rows, nil
}

// xlsxSharedAt tra chuỗi theo chỉ số trong bảng sharedStrings; chỉ số hỏng
// (file lỗi) thì trả "" thay vì làm hỏng cả sheet.
func xlsxSharedAt(shared []string, idx string) string {
	n, err := strconv.Atoi(idx)
	if err != nil || n < 0 || n >= len(shared) {
		return ""
	}
	return shared[n]
}

// xlsxCellText ép giá trị ô về 1 dòng: ô có Alt+Enter chứa xuống dòng thật,
// mà bảng markdown mỗi hàng phải nằm trên đúng 1 dòng.
func xlsxCellText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

// xlsxColIndex đổi toạ độ ô ("A5", "AA12") thành chỉ số cột 1-based (A=1,
// AA=27). Trả 0 khi ref không có chữ cái nào.
func xlsxColIndex(ref string) int {
	n := 0
	for _, r := range ref {
		switch {
		case r >= 'A' && r <= 'Z':
			n = n*26 + int(r-'A'+1)
		case r >= 'a' && r <= 'z':
			n = n*26 + int(r-'a'+1)
		default:
			return n
		}
	}
	return n
}
