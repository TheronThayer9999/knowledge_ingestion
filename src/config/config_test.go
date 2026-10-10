package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadProjectConfig khóa cấu hình triển khai hiện tại: LLM lên cloud,
// embedding ở local. Đổi config mà test này đỏ thì kiểm tra lại chủ trương
// trước khi sửa test.
func TestLoadProjectConfig(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "configs", "config.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	ec := cfg.GetEmbedding()
	if ec.Provider != "ollama" {
		t.Errorf("embedding.provider = %q, want ollama", ec.Provider)
	}
	if ec.BaseURL == "" || isCloudURL(ec.BaseURL) {
		t.Errorf("embedding phải ở local, got base_url %q", ec.BaseURL)
	}
	if ec.Dim <= 0 {
		t.Errorf("embedding.dim = %d, want > 0", ec.Dim)
	}

	lc := cfg.GetLLM()
	if lc.Provider != "ollama" {
		t.Errorf("llm.provider = %q, want ollama", lc.Provider)
	}
	if !isCloudURL(lc.BaseURL) {
		t.Errorf("llm phải lên cloud, got base_url %q", lc.BaseURL)
	}
	if lc.Model == "" {
		t.Error("llm.model rỗng — chưa chọn model mặc định")
	}
	// Key để trong file theo convention dev của repo (db/qdrant/s3 đều thế);
	// production đè bằng env OLLAMA_API_KEY. Không assert rỗng.
	if lc.MaxTokens <= 0 {
		t.Errorf("llm.max_tokens = %d, want > 0", lc.MaxTokens)
	}
}

// isCloudURL báo URL có trỏ lên Ollama Cloud không (không phải local).
func isCloudURL(u string) bool {
	return strings.Contains(u, "ollama.com")
}
