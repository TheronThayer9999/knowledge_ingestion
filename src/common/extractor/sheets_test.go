package extractor

import (
	"testing"
)

// TestParseSheetBlocksFromExtractXlsx khoá CẢ HAI ĐẦU của hợp đồng: text do
// extractXlsx sinh ra phải parse lại được thành đúng sheet/hàng/cột mà tool
// agent sẽ đọc. Đổi định dạng text thì test này gãy trước khi agent trả lời sai.
func TestParseSheetBlocksFromExtractXlsx(t *testing.T) {
	text := extractOne(t, "uploads/chi_phi.xlsx", buildXlsx(t)).Text
	blocks := ParseSheetBlocks(text)
	if len(blocks) != 2 {
		t.Fatalf("want 2 sheet, got %d: %+v", len(blocks), blocks)
	}
	first := blocks[0]
	if first.Index != 1 || first.Name != "Biểu phí" {
		t.Fatalf("sheet 1 sai: %+v", first)
	}
	if first.Header != "| Hành vi | Mức phạt |" {
		t.Fatalf("header sai: %q", first.Header)
	}
	if len(first.Columns) != 2 || first.Columns[0] != "Hành vi" || first.Columns[1] != "Mức phạt" {
		t.Fatalf("columns sai: %v", first.Columns)
	}
	if first.TotalRows() != 2 {
		t.Fatalf("want 2 hàng dữ liệu, got %d: %v", first.TotalRows(), first.Rows)
	}
	// Ô trống phải giữ vị trí — lệch 1 ô là trả lời sai cột.
	if first.Rows[0] != "| Trễ hạn |  | 5 triệu |" {
		t.Fatalf("hàng giữ vị trí cột sai: %q", first.Rows[0])
	}
	if first.Chars == 0 {
		t.Fatal("chars phải > 0 để LLM biết sheet có nội dung")
	}
	second := blocks[1]
	if second.Index != 2 || second.Name != "Phụ lục" || second.TotalRows() != 0 {
		t.Fatalf("sheet 2 sai: %+v", second)
	}
	if second.Header == "" || len(second.Columns) != 2 {
		t.Fatalf("sheet chỉ có header vẫn phải có cột: %+v", second)
	}
}

// File Excel thiếu workbook (không có tên sheet) ghi "## Sheet 2" — parser
// phải nhận dạng dự phòng đó, nếu không agent mất luôn khả năng liệt kê.
func TestParseSheetBlocksFallbackName(t *testing.T) {
	raw := buildZip(t, map[string]string{
		"xl/worksheets/sheet1.xml": `<?xml version="1.0"?><worksheet ` + xlsxNS + `><sheetData>` +
			`<row r="1"><c r="A1" t="inlineStr"><is><t>Điều 1</t></is></c></row>` +
			`</sheetData></worksheet>`,
	})
	blocks := ParseSheetBlocks(extractOne(t, "uploads/kho.xlsx", raw).Text)
	if len(blocks) != 1 || blocks[0].Name != "1" || blocks[0].Index != 1 {
		t.Fatalf("mất tên sheet dự phòng: %+v", blocks)
	}
}

// File không phải Excel (không có dòng tiêu đề sheet) trả nil — tool dựa vào
// đây để nói thật "tài liệu này không có sheet" thay vì bịa sheet rỗng.
func TestParseSheetBlocksNotSpreadsheet(t *testing.T) {
	if blocks := ParseSheetBlocks("Điều 1. Phạm vi áp dụng\n\n1. Văn bản này..."); blocks != nil {
		t.Fatalf("văn bản thường không được có sheet: %+v", blocks)
	}
	if blocks := ParseSheetBlocks(""); blocks != nil {
		t.Fatalf("text rỗng không được có sheet: %+v", blocks)
	}
}

// Dòng rỗng/không phải pipe giữa khối (và CRLF) không được tính là hàng —
// nếu tính, số hàng công bố cho LLM sẽ sai ngay từ file đầu tiên.
func TestParseSheetBlocksIgnoresNoise(t *testing.T) {
	text := "## Sheet: A\r\n| c1 | c2 |\r\n| x | y |\r\n\r\n## Sheet: B\r\n| z |\r\n"
	blocks := ParseSheetBlocks(text)
	if len(blocks) != 2 {
		t.Fatalf("want 2 sheet, got %d: %+v", len(blocks), blocks)
	}
	if blocks[0].TotalRows() != 1 || blocks[0].Rows[0] != "| x | y |" {
		t.Fatalf("sheet A sai: %+v", blocks[0])
	}
	if blocks[1].Header != "| z |" || blocks[1].TotalRows() != 0 {
		t.Fatalf("sheet B sai: %+v", blocks[1])
	}
}
