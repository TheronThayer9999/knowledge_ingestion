package chunker

import "testing"

// TestDetectTypeTheoTenFile khoá chặt lý do "phải giữ đuôi trong tên tài liệu":
// khi doc_type để trống, DetectType dò .xlsx/.csv NGAY TRONG TÊN để chọn template
// table — cắt đuôi ở FE là mất tín hiệu này (chỉ còn content-type đỡ).
func TestDetectTypeTheoTenFile(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		file        string
		want        TemplateType
	}{
		{"xlsx nhận ra nhờ đuôi", "application/octet-stream", "bang_luong.xlsx", TemplateTable},
		{"csv nhận ra nhờ đuôi", "application/octet-stream", "danh_sach.csv", TemplateTable},
		{"mất đuôi thì không còn tín hiệu", "application/octet-stream", "bang_luong", TemplateGeneral},
		{"csv nhận ra nhờ content-type", "text/csv", "danh_sach", TemplateTable},
		{"tên luật tiếng Việt", "application/pdf", "Nghị định 06.pdf", TemplateLaws},
	}
	for _, tc := range cases {
		if got := DetectType(tc.contentType, tc.file); got != tc.want {
			t.Errorf("%s: DetectType(%q, %q) = %q, muốn %q",
				tc.name, tc.contentType, tc.file, got, tc.want)
		}
	}
}
