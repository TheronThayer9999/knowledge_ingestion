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
