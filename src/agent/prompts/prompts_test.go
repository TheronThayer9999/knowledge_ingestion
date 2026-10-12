package prompts

import (
	"os"
	"path/filepath"
	"testing"
)

// Load là 1 điểm đọc prompt duy nhất — question + synthesis cùng ủy quyền,
// khỏi mỗi package một bản os.ReadFile.
func TestLoad(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("  hello  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir, "a.md")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != "hello" {
		t.Fatalf("phải trim space, got %q", got)
	}
	if _, err := Load(dir, "thieu.md"); err == nil {
		t.Fatal("file thiếu phải lỗi để fail-fast")
	}
	if err := os.WriteFile(filepath.Join(dir, "rong.md"), []byte("  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, "rong.md"); err == nil {
		t.Fatal("nội dung rỗng phải lỗi")
	}
}

// Khóa hợp đồng với repo thật: 2 prompt file agent phải load được.
func TestLoadRepoPrompts(t *testing.T) {
	for _, f := range []string{"chat_system.md", "classify_system.md"} {
		if _, err := Load("../../../prompts", f); err != nil {
			t.Fatalf("Load(%s): %v", f, err)
		}
	}
}
