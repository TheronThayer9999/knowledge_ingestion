package prompts

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Load đọc 1 prompt file trong dir lúc startup — sửa file, restart app là ăn,
// không cần build lại. File thiếu/không đọc được/nội dung rỗng thì trả lỗi để
// app fail-fast: prompt là bắt buộc, không có bản mặc định trong code.
func Load(dir, file string) (string, error) {
	path := filepath.Join(dir, file)
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("đọc prompt %s: %w (muốn %s)", file, err, path)
	}
	out := strings.TrimSpace(string(raw))
	if out == "" {
		return "", fmt.Errorf("prompt %s rỗng: %s", file, path)
	}
	return out, nil
}
