package extractor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExtractXlsx_ComplexFixture nối hai đầu ngôn ngữ: `loadtest/
// make_complex_fixtures.py` sinh .xlsx bằng stdlib (zipfile + XML thô, không
// openpyxl), còn file đó sống được hay không thì do extractXlsx quyết định.
// Test này chạy extractor THẬT trên data thật để bảo đảm bộ fixture mang lên
// UI demo không rơi vào nhánh failed/ErrInvalid, và khoá luôn các hành vi mà
// test case trong `docs_test_case_and_data/testcase/` giả định.
//
// Bỏ qua khi file chưa sinh (`python make_complex_fixtures.py`) — extractor
// không có nghĩa vụ tồn tại kèm data, nên gãy ở đây là lỗi data, không phải
// lỗi trích xuất.
func TestExtractXlsx_ComplexFixture(t *testing.T) {
	text := extractFixture(t, "bang_luong_q3.xlsx")

	// Sheet trắng KHÔNG xuất hiện trong text (xlsx.go bỏ sheet không có hàng
	// nào) -> `list_sheets` báo 3 sheet dù workbook khai 4. Test case X-02
	// giả định đúng hành vi này, nên nó phải được khoá ở đây chứ không chỉ
	// mô tả trong tài liệu.
	if n := strings.Count(text, "## Sheet"); n != 3 {
		t.Fatalf("want 3 sheet có dữ liệu, got %d:\n%s", n, text)
	}
	if strings.Contains(text, "So_lieu_cho_cap_nhat") {
		t.Fatal("sheet trống bị bỏ có chủ đích, không được lộ tên trong text")
	}

	blocks := ParseSheetBlocks(text)
	if len(blocks) != 3 {
		t.Fatalf("ParseSheetBlocks = %d khối, want 3", len(blocks))
	}
	luong, phuCap := blocks[0], blocks[1]
	if luong.Name != "Luong" || phuCap.Name != "Phu_cap" || blocks[2].Name != "Ghi_chu" {
		t.Fatalf("tên/thứ tự sheet sai: %s, %s, %s", luong.Name, phuCap.Name, blocks[2].Name)
	}

	// Hàng đầu tiên của sheet là DÒNG TIÊU ĐỀ (1 ô), không phải tên cột. Đây là
	// bẫy có chủ đích: `list_sheets` sẽ trả columns = tiêu đề, nên câu hỏi
	// "bảng có bao nhiêu nhân viên" buộc model phải đọc hàng, không được tin
	// metadata sheet.
	if luong.Header != "| BẢNG LƯƠNG QUÝ 3/2025 - KHỐI VĂN PHÒNG |" {
		t.Fatalf("header sheet Luong phải là dòng tiêu đề, got %q", luong.Header)
	}
	if len(luong.Columns) != 1 {
		t.Fatalf("tiêu đề 1 ô thì columns = 1, got %v", luong.Columns)
	}
	// 8 hàng - 1 hàng header = 7 (header thật, 5 nhân viên, 1 dòng Tổng cộng).
	if luong.TotalRows() != 7 {
		t.Fatalf("Luong want 7 hàng dữ liệu, got %d: %v", luong.TotalRows(), luong.Rows)
	}
	// Ô phụ cấp trống của NV03 phải giữ vị trí cột, nếu không "Phụ cấp" rơi
	// sang cột "Tổng thực nhận" và mọi phép tính theo bảng đều sai.
	wantNV03 := "| NV03 | Lê Minh Ngọc | Chuyên viên | 21.5 | 180000 | 3870000 |  | 3870000 | 193500 | 3676500 |"
	if luong.Rows[3] != wantNV03 {
		t.Fatalf("hàng NV03 giữ chỗ cột sai:\ngot  %q\nwant %q", luong.Rows[3], wantNV03)
	}
	wantTong := "| Tổng cộng |  |  | 113 |  | 25370000 | 4600000 | 29970000 | 2623500 | 27346500 |"
	if luong.Rows[6] != wantTong {
		t.Fatalf("dòng Tổng cộng sai:\ngot  %q\nwant %q", luong.Rows[6], wantTong)
	}

	// Ghi chú trống ở cuối hàng bị cắt (flushRow bỏ ô rỗng đuôi) — hàng NV01
	// của sheet Phu_cap chỉ còn 4 cột, đúng bằng số cột có nội dung.
	wantPC01 := "| NV01 | Phụ cấp ăn trưa | 800000 | Làm việc tại văn phòng từ 22 ngày công trở lên |"
	if phuCap.Rows[0] != wantPC01 {
		t.Fatalf("hàng phụ cấp NV01 sai:\ngot  %q\nwant %q", phuCap.Rows[0], wantPC01)
	}
	if phuCap.TotalRows() != 5 {
		t.Fatalf("Phu_cap want 5 hàng, got %d", phuCap.TotalRows())
	}
}

// Hai file cùng một dịch vụ nhưng mức khác nhau: kịch bản "hai quy định xung
// đột" trong bộ test case chạy trên đúng cặp file này, nên extractor phải đọc
// trọn cả hai.
func TestExtractXlsx_ConflictingVersions(t *testing.T) {
	v1 := extractFixture(t, "quy_che_phi_v1.xlsx")
	v2 := extractFixture(t, "quy_che_phi_v2.xlsx")
	if !strings.Contains(v1, "| PQ-01 | Cấp bản sao văn bản | 5000 |") {
		t.Fatalf("v1 mất mức 5000:\n%s", v1)
	}
	if !strings.Contains(v2, "| PQ-01 | Cấp bản sao văn bản | 8000 |") ||
		!strings.Contains(v2, "Bãi bỏ toàn bộ quy chế phiên bản 1") {
		t.Fatalf("v2 thiếu mức mới hoặc câu bãi bỏ:\n%s", v2)
	}
	if strings.Count(v2, "## Sheet") != 2 {
		t.Fatal("v2 phải có 2 sheet Bieu_phi + Hieu_luc")
	}
}

// Bảng kế toán nhiều sheet: dòng Cộng phải nằm đúng cột Nợ/Có, vì câu hỏi đối
// chiếu ("lệch bao nhiêu") tính từ hiệu hai cột này.
func TestParseSheetBlocks_SheetTotals(t *testing.T) {
	text := extractFixture(t, "bang_ke_toan_multi_sheet.xlsx")
	blocks := ParseSheetBlocks(text)
	if len(blocks) != 3 {
		t.Fatalf("want 3 sheet, got %d", len(blocks))
	}
	soChi := blocks[0]
	last := soChi.Rows[len(soChi.Rows)-2] // kề trước dòng Số dư
	if last != "| Cộng phát sinh |  |  | 58250000 | 103400000 |" {
		t.Fatalf("dòng Cộng phát sinh lệch cột Nợ/Có: %q", last)
	}
	if !strings.Contains(soChi.Rows[len(soChi.Rows)-1], "Có trừ Nợ = 45150000 VNĐ") {
		t.Fatalf("dòng số dư phải ghi rõ hiệu hai cột: %q", soChi.Rows[len(soChi.Rows)-1])
	}
	if blocks[1].Name != "Doi_chieu_ngan_hang" || !strings.Contains(text, "17350000") {
		t.Fatal("sheet đối chiếu ngân hàng thiếu hoặc sai số dư 17350000")
	}
}

// extractFixture chạy extractor thật trên file fixture do Python sinh ra.
func extractFixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "loadtest", "fixtures_complex", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("chưa sinh fixture (%v) — chạy: python loadtest/make_complex_fixtures.py", err)
	}
	page := extractOne(t, "uploads/"+name, raw)
	if strings.TrimSpace(page.Text) == "" {
		t.Fatalf("%s trích ra text rỗng", name)
	}
	return page.Text
}
