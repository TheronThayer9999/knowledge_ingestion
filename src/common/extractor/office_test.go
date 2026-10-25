package extractor

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"
)

// buildZip gói các entry {tên: nội dung} thành 1 archive trong RAM — dùng cho
// xlsx/pptx, nơi fixture cần nhiều file XML thay vì 1 như docx.
func buildZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("tạo entry %s: %v", name, err)
		}
		if _, err := f.Write([]byte(body)); err != nil {
			t.Fatalf("ghi %s: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("đóng zip: %v", err)
	}
	return buf.Bytes()
}

const xlsxNS = `xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"`

// buildXlsx dựng workbook 2 sheet: sheet 1 dùng sharedStrings (có chuỗi giàu
// định dạng + ô trống ở giữa), sheet 2 dùng inlineStr; tên sheet đọc qua
// workbook.xml + rels (1 Target tương đối, 1 Target tuyệt đối).
func buildXlsx(t *testing.T) []byte {
	t.Helper()
	return buildZip(t, map[string]string{
		"xl/workbook.xml": `<?xml version="1.0"?><workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>` +
			`<sheet name="Biểu phí" sheetId="1" r:id="rId1"/>` +
			`<sheet name="Phụ lục" sheetId="2" r:id="rId2"/>` +
			`</sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Target="worksheets/sheet1.xml"/>` +
			`<Relationship Id="rId2" Target="/xl/worksheets/sheet2.xml"/>` +
			`</Relationships>`,
		"xl/sharedStrings.xml": `<?xml version="1.0"?><sst ` + xlsxNS + `>` +
			`<si><t>Hành vi</t></si>` +
			`<si><t>Mức phạt</t></si>` +
			`<si><r><t>Trễ</t></r><r><t> hạn</t></r></si>` +
			`<si><t>5 triệu</t></si>` +
			`</sst>`,
		"xl/worksheets/sheet1.xml": `<?xml version="1.0"?><worksheet ` + xlsxNS + `><sheetData>` +
			`<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c></row>` +
			`<row r="2"><c r="A2" t="s"><v>2</v></c><c r="C2" t="s"><v>3</v></c></row>` +
			`<row r="3"><c r="A3"><f>SUM(B1:B2)</f><v>42</v></c></row>` +
			`</sheetData></worksheet>`,
		"xl/worksheets/sheet2.xml": `<?xml version="1.0"?><worksheet ` + xlsxNS + `><sheetData>` +
			`<row r="1"><c r="A1" t="inlineStr"><is><t>Ghi chú</t></is></c><c r="B1"><v>12</v></c></row>` +
			`</sheetData></worksheet>`,
	})
}

func TestExtractXlsxSheetsAndSharedStrings(t *testing.T) {
	got := extractOne(t, "uploads/chi_phi.xlsx", buildXlsx(t)).Text
	want := "## Sheet: Biểu phí\n" +
		"| Hành vi | Mức phạt |\n" +
		"| Trễ hạn |  | 5 triệu |\n" +
		"| 42 |\n\n" +
		"## Sheet: Phụ lục\n" +
		"| Ghi chú | 12 |"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// Thiếu workbook/rels (file lạ) vẫn đọc được nội dung, chỉ mất tên sheet.
func TestExtractXlsxFallsBackToSheetNumber(t *testing.T) {
	raw := buildZip(t, map[string]string{
		"xl/worksheets/sheet1.xml": `<?xml version="1.0"?><worksheet ` + xlsxNS + `><sheetData>` +
			`<row r="1"><c r="A1" t="inlineStr"><is><t>Điều 1</t></is></c></row>` +
			`</sheetData></worksheet>`,
		"xl/worksheets/sheet2.xml": `<?xml version="1.0"?><worksheet ` + xlsxNS + `><sheetData>` +
			`<row r="1"><c r="A1" t="inlineStr"><is><t>Điều 2</t></is></c></row>` +
			`</sheetData></worksheet>`,
	})
	got := extractOne(t, "uploads/kho.xlsx", raw).Text
	if !strings.Contains(got, "## Sheet 1\n| Điều 1 |") || !strings.Contains(got, "## Sheet 2\n| Điều 2 |") {
		t.Fatalf("thiếu tên sheet dự phòng: %q", got)
	}
}

// Zip hợp lệ nhưng thiếu entry sheet ⇒ lỗi vĩnh viễn (ErrInvalid), không phải
// "chưa hỗ trợ" và không được trả text rỗng như thành công.
func TestExtractXlsxWithoutSheets(t *testing.T) {
	raw := buildZip(t, map[string]string{"docProps/app.xml": "<x/>"})
	_, err := collectPages("uploads/rong.xlsx", bytes.NewReader(raw), OCRConfig{})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("want ErrInvalid, got %v", err)
	}
}

const pptxNS = `xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" ` +
	`xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"`

// slideXML bọc các đoạn văn vào cấu trúc slide tối thiểu.
func slideXML(paras ...string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><p:sld ` + pptxNS + `><p:cSld><p:spTree><p:sp><p:txBody>`)
	for _, p := range paras {
		b.WriteString(`<a:p><a:r><a:t>` + p + `</a:t></a:r></a:p>`)
	}
	b.WriteString(`</p:txBody></p:sp></p:spTree></p:cSld></p:sld>`)
	return b.String()
}

func TestExtractPptxSlidesSortedByNumber(t *testing.T) {
	raw := buildZip(t, map[string]string{
		"ppt/slides/slide1.xml":  slideXML("Mở đầu"),
		"ppt/slides/slide2.xml":  slideXML("Điều 1", "Nội dung"),
		"ppt/slides/slide3.xml":  slideXML(), // slide chỉ có ảnh/khung rỗng
		"ppt/slides/slide10.xml": slideXML("Kết luận"),
	})
	got := extractOne(t, "uploads/quy_che.pptx", raw).Text
	want := "## Slide 1\nMở đầu\n\n" +
		"## Slide 2\nĐiều 1\n\nNội dung\n\n" +
		"## Slide 10\nKết luận"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestExtractPptxWithoutSlides(t *testing.T) {
	raw := buildZip(t, map[string]string{"docProps/app.xml": "<x/>"})
	_, err := collectPages("uploads/rong.pptx", bytes.NewReader(raw), OCRConfig{})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("want ErrInvalid, got %v", err)
	}
}
