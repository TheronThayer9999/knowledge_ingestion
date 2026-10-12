package synthesis

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Prompts gom prompt load từ file lúc startup — sửa file, restart app là ăn,
// không cần build lại. System là instruction của agent chat (Eino ChatModelAgent)
// và là hợp đồng trích dẫn [^n] với frontend. Không có bản mặc định trong code.
type Prompts struct {
	// System thay system prompt chat (instruction agent).
	System string
}

// chatSystemFile là tên file system prompt chat trong thư mục prompt.
const chatSystemFile = "chat_system.md"

// LoadPrompts đọc prompt từ dir lúc startup. File thiếu/không đọc được/nội
// dung rỗng thì trả lỗi — prompt là bắt buộc nên app fail-fast thay vì chạy
// agent không instruction.
func LoadPrompts(dir string) (Prompts, error) {
	path := filepath.Join(dir, chatSystemFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		return Prompts{}, fmt.Errorf("đọc system prompt chat: %w (muốn %s)", err, path)
	}
	system := strings.TrimSpace(string(raw))
	if system == "" {
		return Prompts{}, fmt.Errorf("system prompt chat rỗng: %s", path)
	}
	return Prompts{System: system}, nil
}
