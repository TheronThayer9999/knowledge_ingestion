package synthesis

import (
	"strings"
	"testing"
)

// TestLoadPromptsFromRepo khóa hợp đồng prompt lúc startup: file phải load
// được và phải giữ quy ước trích dẫn [^n] khớp ChatCite/frontend. Sửa prompt
// mà test này đỏ thì kiểm tra lại frontend trước khi đổi quy ước.
func TestLoadPromptsFromRepo(t *testing.T) {
	p, err := LoadPrompts("../../../prompts")
	if err != nil {
		t.Fatalf("LoadPrompts: %v", err)
	}
	if !strings.Contains(p.System, "[^n]") {
		t.Error("system prompt thiếu quy ước trích dẫn [^n]")
	}
}

// TestLoadPromptsLoadsSheetsFragment: fragment luật bảng tính là file riêng,
// phải load được lúc startup (thiếu file thì agent trả lời câu hỏi Excel bằng
// suy đoán thay vì đọc sheet). Nội dung phải nhắc "sheet" — nếu ai đó làm rỗng
// file thì test này đỏ chứ không âm thầm mất luật.
func TestLoadPromptsLoadsSheetsFragment(t *testing.T) {
	p, err := LoadPrompts("../../../prompts")
	if err != nil {
		t.Fatalf("LoadPrompts: %v", err)
	}
	if strings.TrimSpace(p.Sheets) == "" {
		t.Fatal("fragment chat_system_sheets.md rỗng")
	}
	if !strings.Contains(strings.ToLower(p.Sheets), "sheet") {
		t.Error("fragment bảng tính không nhắc tới sheet")
	}
}
