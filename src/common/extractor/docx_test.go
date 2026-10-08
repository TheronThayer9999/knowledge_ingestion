package extractor

// Test trích text .docx/.pdf/.txt: dựng file tối giản trong RAM rồi assert
// đoạn văn / bảng / liệt kê / điều khoản / số trang ra đúng quy ước của
// ExtractPages (docx gom 1 page Num 0, pdf bóc từng trang Num từ 1).

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

// buildDocx gói document.xml thành file docx trong RAM.
func buildDocx(t *testing.T, documentXML string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Create("word/document.xml")
	if err != nil {
		t.Fatalf("tạo entry zip: %v", err)
	}
	if _, err := f.Write([]byte(documentXML)); err != nil {
		t.Fatalf("ghi document.xml: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("đóng zip: %v", err)
	}
	return buf.Bytes()
}

// extractOne là helper test chỉ lấy text khi chắc chắn 1 page (docx/txt).
func extractOne(t *testing.T, key string, raw []byte) Page {
	t.Helper()
	pages, err := ExtractPages(context.Background(), key, bytes.NewReader(raw), OCRConfig{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("want 1 page, got %d", len(pages))
	}
	return pages[0]
}

const docxNS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`

func TestExtractDocxParagraphs(t *testing.T) {
	raw := buildDocx(t, `<?xml version="1.0"?><w:document `+docxNS+`><w:body>`+
		`<w:p><w:r><w:t>Điều 1. Phạm vi áp dụng</w:t></w:r></w:p>`+
		`<w:p><w:r><w:t>Văn bản này áp dụng cho </w:t></w:r><w:r><w:t>mọi thành viên.</w:t></w:r></w:p>`+
		`</w:body></w:document>`)
	got := extractOne(t, "uploads/a.docx", raw)
	if got.Num != 0 {
		t.Fatalf("docx gom 1 page Num 0, got %d", got.Num)
	}
	want := "Điều 1. Phạm vi áp dụng\n\nVăn bản này áp dụng cho mọi thành viên."
	if got.Text != want {
		t.Fatalf("got %q, want %q", got.Text, want)
	}
}

func TestExtractDocxTableAndBullet(t *testing.T) {
	raw := buildDocx(t, `<?xml version="1.0"?><w:document `+docxNS+`><w:body>`+
		`<w:p><w:pPr><w:numPr/></w:pPr><w:r><w:t>Mức phạt 10 triệu</w:t></w:r></w:p>`+
		`<w:tbl><w:tr><w:tc><w:p><w:r><w:t>Hành vi</w:t></w:r></w:p></w:tc>`+
		`<w:tc><w:p><w:r><w:t>Mức phạt</w:t></w:r></w:p></w:tc></w:tr>`+
		`<w:tr><w:tc><w:p><w:r><w:t>Trễ hạn</w:t></w:r></w:p></w:tc>`+
		`<w:tc><w:p><w:r><w:t>5 triệu</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`+
		`</w:body></w:document>`)
	got := extractOne(t, "uploads/b.docx", raw).Text
	if !strings.Contains(got, "- Mức phạt 10 triệu") {
		t.Fatalf("mất tiền tố liệt kê: %q", got)
	}
	if !strings.Contains(got, "| Hành vi | Mức phạt |") || !strings.Contains(got, "| Trễ hạn | 5 triệu |") {
		t.Fatalf("bảng sai định dạng pipe: %q", got)
	}
}

func TestExtractDocxSkipsDeletedAndField(t *testing.T) {
	raw := buildDocx(t, `<?xml version="1.0"?><w:document `+docxNS+`><w:body>`+
		`<w:p><w:r><w:t>Giữ lại</w:t></w:r>`+
		`<w:del><w:r><w:t>Bị xóa track-change</w:t></w:r></w:del>`+
		`<w:r><w:fldChar/></w:r><w:r><w:instrText>PAGEREF _Toc</w:instrText></w:r></w:p>`+
		`</w:body></w:document>`)
	got := extractOne(t, "uploads/c.docx", raw).Text
	if strings.Contains(got, "Bị xóa") || strings.Contains(got, "PAGEREF") {
		t.Fatalf("lọt rác máy vào text: %q", got)
	}
	if !strings.Contains(got, "Giữ lại") {
		t.Fatalf("mất nội dung thật: %q", got)
	}
}

func TestExtractUnsupported(t *testing.T) {
	if _, err := ExtractPages(context.Background(), "uploads/a.png", strings.NewReader("x"), OCRConfig{}); err == nil {
		t.Fatal("ảnh phải báo unsupported")
	}
	if _, err := ExtractPages(context.Background(), "uploads/a.zip", strings.NewReader("x"), OCRConfig{}); err == nil {
		t.Fatal("zip phải báo unsupported")
	}
}

func TestExtractPlainText(t *testing.T) {
	got := extractOne(t, "uploads/a.txt", []byte("xin chào"))
	if got.Text != "xin chào" || got.Num != 0 {
		t.Fatalf("got %+v", got)
	}
}

// buildPDF dựng PDF N trang tối giản trong RAM — text ASCII (Helvetica Type1
// mặc định chỉ chắc WinAnsi, không test dấu tiếng Việt ở đây). Object đánh số
// thưa (page 3.., font 7, stream 10..) nên bảng xref bù free entry cho số thiếu.
func buildPDF(t *testing.T, pageTexts ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	offsets := map[int]int{}
	writeObj := func(n int, body string) {
		offsets[n] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", n, body)
	}
	buf.WriteString("%PDF-1.4\n")
	kids := ""
	for i := range pageTexts {
		kids += fmt.Sprintf("%d 0 R ", 3+i)
	}
	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObj(2, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids, len(pageTexts)))
	for i := range pageTexts {
		writeObj(3+i, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents %d 0 R /Resources << /Font << /F1 7 0 R >> >> >>", 10+i))
	}
	writeObj(7, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	maxObj := 0
	for i, text := range pageTexts {
		content := fmt.Sprintf("BT /F1 12 Tf 72 720 Td (%s) Tj ET", text)
		n := 10 + i
		offsets[n] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n", n, len(content), content)
		if n > maxObj {
			maxObj = n
		}
	}
	xrefPos := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", maxObj+1)
	buf.WriteString("0000000000 65535 f \n")
	for n := 1; n <= maxObj; n++ {
		if off, ok := offsets[n]; ok {
			fmt.Fprintf(&buf, "%010d 00000 n \n", off)
		} else {
			buf.WriteString("0000000000 00000 f \n")
		}
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF", maxObj+1, xrefPos)
	return buf.Bytes()
}

// PDF 2 trang → 2 Page đúng số thứ tự, text không tràn qua nhau.
func TestExtractPDFPages(t *testing.T) {
	raw := buildPDF(t, "Trang 1: Dieu 1", "Trang 2: Dieu 2")
	pages, err := ExtractPages(context.Background(), "uploads/d.pdf", bytes.NewReader(raw), OCRConfig{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("want 2 pages, got %d", len(pages))
	}
	if pages[0].Num != 1 || !strings.Contains(pages[0].Text, "Dieu 1") {
		t.Fatalf("trang 1 sai: %+v", pages[0])
	}
	if pages[1].Num != 2 || !strings.Contains(pages[1].Text, "Dieu 2") {
		t.Fatalf("trang 2 sai: %+v", pages[1])
	}
	if strings.Contains(pages[0].Text, "Dieu 2") || strings.Contains(pages[1].Text, "Dieu 1") {
		t.Fatalf("text tràn qua trang: %+v", pages)
	}
}

// Bytes không phải PDF → lỗi để service skip, kỳ sau thử lại.
func TestExtractPDFInvalid(t *testing.T) {
	if _, err := ExtractPages(context.Background(), "uploads/e.pdf", strings.NewReader("khong phai pdf"), OCRConfig{}); err == nil {
		t.Fatal("pdf hỏng phải lỗi")
	}
}
