package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Fixture tối thiểu đủ để Load đi qua hết validate (port, db, embedding,
// qdrant, redis, rabbitmq, jwt). Giữ đúng hình dạng config thật, kể cả model
// 9router có dấu "/" — đó chính là lý do CLI tách provider và model thành 2
// flag riêng.
const llmTestConfigJSON = `{
  "app": {"port": 9001},
  "database": {"host": "127.0.0.1"},
  "embedding": {"provider": "ollama", "model": "bge-m3", "dim": 1024},
  "llms": {
    "default_provider": "9router",
    "providers": {
      "9router": {
        "type": "openai",
        "model": "cl/nvidia/nemotron-3.5-super-vl:free",
        "base_url": "http://localhost:20128/v1",
        "api_key": "nine-router-secret"
      },
      "groq": {
        "type": "openai",
        "model": "qwen/qwen3.8-27b",
        "base_url": "https://api.groq.com/openai/v1",
        "api_key": "groq-secret"
      }
    }
  },
  "qdrant": {"host": "127.0.0.1"},
  "redis": {"host": "127.0.0.1"},
  "rabbitmq": {"host": "127.0.0.1"},
  "jwt": {"secret": "test-secret"}
}`

// writeLLMTestConfig ghi fixture ra thư mục tạm — test không phụ thuộc
// configs/config.local.json của máy đang chạy (file đó gitignored).
func writeLLMTestConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(llmTestConfigJSON), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// clearLLMEnv trung hoà env của máy chạy test — LLM_PROVIDER/LLM_MODEL đặt
// sẵn trong shell sẽ làm mọi test dưới đây đo sai thứ ưu tiên.
func clearLLMEnv(t *testing.T) {
	t.Helper()
	t.Setenv("LLM_PROVIDER", "")
	t.Setenv("LLM_MODEL", "")
}

// TestLoadLLMSelectionFromFile khóa mặc định: không env, không flag thì giữ
// nguyên file, kể cả model 9router có dấu "/" (không bị cắt).
func TestLoadLLMSelectionFromFile(t *testing.T) {
	clearLLMEnv(t)
	cfg, err := Load(writeLLMTestConfig(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	llms := cfg.GetLLMs()
	if llms.DefaultProvider != "9router" {
		t.Fatalf("default provider = %q, muốn 9router", llms.DefaultProvider)
	}
	if got := llms.Providers["9router"].Model; got != "cl/nvidia/nemotron-3.5-super-vl:free" {
		t.Fatalf("model 9router = %q", got)
	}
}

// TestLoadLLMSelectionFromEnv: env là lớp giữa — đổi được provider + model mà
// không sửa file, đúng kiểu Ollama/llm CLI dùng LLM_MODEL.
func TestLoadLLMSelectionFromEnv(t *testing.T) {
	path := writeLLMTestConfig(t)
	t.Setenv("LLM_PROVIDER", "GROQ") // chuẩn hoá hoa/thường như slug trong map
	t.Setenv("LLM_MODEL", "llama-3.1-8b-instant")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	llms := cfg.GetLLMs()
	if llms.DefaultProvider != "groq" {
		t.Fatalf("default provider = %q, muốn groq", llms.DefaultProvider)
	}
	if got := llms.Providers["groq"].Model; got != "llama-3.1-8b-instant" {
		t.Fatalf("model groq = %q", got)
	}
	// Provider không được chọn phải giữ nguyên model trong file.
	if got := llms.Providers["9router"].Model; got != "cl/nvidia/nemotron-3.5-super-vl:free" {
		t.Fatalf("model 9router bị đổi oan: %q", got)
	}
}

// TestLoadLLMSelectionFlagBeatsEnv: flag là lớp cao nhất (12-factor), đè env.
func TestLoadLLMSelectionFlagBeatsEnv(t *testing.T) {
	path := writeLLMTestConfig(t)
	t.Setenv("LLM_PROVIDER", "groq")
	t.Setenv("LLM_MODEL", "from-env")

	cfg, err := Load(path, WithLLMSelection("9router", "cl/nvidia/nemotron-3.5-super-vl:free-pro"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	llms := cfg.GetLLMs()
	if llms.DefaultProvider != "9router" {
		t.Fatalf("default provider = %q, muốn 9router (flag phải thắng env)", llms.DefaultProvider)
	}
	if got := llms.Providers["9router"].Model; got != "cl/nvidia/nemotron-3.5-super-vl:free-pro" {
		t.Fatalf("model = %q, flag phải thắng env", got)
	}
}

// TestLoadLLMSelectionModelOnly: chỉ đổi model thì áp lên provider mặc định —
// người dùng không phải nhớ/gõ lại slug.
func TestLoadLLMSelectionModelOnly(t *testing.T) {
	clearLLMEnv(t)
	cfg, err := Load(writeLLMTestConfig(t), WithLLMSelection("", "custom-model"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	llms := cfg.GetLLMs()
	if llms.DefaultProvider != "9router" {
		t.Fatalf("default provider = %q, muốn giữ 9router", llms.DefaultProvider)
	}
	if got := llms.Providers["9router"].Model; got != "custom-model" {
		t.Fatalf("model = %q, muốn custom-model", got)
	}
}

// TestLoadLLMSelectionUnknownProvider: gõ sai slug phải chết ngay lúc boot kèm
// danh sách slug đang có (kiểu aider --list-models), không để request đầu
// tiên mới 400.
func TestLoadLLMSelectionUnknownProvider(t *testing.T) {
	clearLLMEnv(t)
	_, err := Load(writeLLMTestConfig(t), WithLLMSelection("nope", ""))
	if err == nil {
		t.Fatal("Load phải lỗi khi provider không tồn tại")
	}
	for _, want := range []string{"nope", "9router", "groq"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("lỗi thiếu %q: %v", want, err)
		}
	}
}

// TestLoadLLMSelectionEmptyModel: chọn provider nhưng nó chưa có model thì báo
// ngay, thay vì boot xong rồi mọi lượt chat 400.
func TestLoadLLMSelectionEmptyModel(t *testing.T) {
	clearLLMEnv(t)
	path := filepath.Join(t.TempDir(), "config.json")
	noModel := strings.Replace(llmTestConfigJSON,
		`"model": "qwen/qwen3.8-27b",`, `"model": "",`, 1)
	if err := os.WriteFile(path, []byte(noModel), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, err := Load(path, WithLLMSelection("groq", ""))
	if err == nil || !strings.Contains(err.Error(), "model") {
		t.Fatalf("muốn lỗi thiếu model, được: %v", err)
	}
}

// TestFormatLLMsHidesAPIKey: -list-llms in ra màn hình/CI log nên tuyệt đối
// không được lộ key — chỉ key=set|missing.
func TestFormatLLMsHidesAPIKey(t *testing.T) {
	clearLLMEnv(t)
	cfg, err := Load(writeLLMTestConfig(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	out := FormatLLMs(cfg)
	for _, secret := range []string{"nine-router-secret", "groq-secret"} {
		if strings.Contains(out, secret) {
			t.Fatalf("FormatLLMs lộ api key: %s", out)
		}
	}
	for _, want := range []string{"provider mặc định: 9router", "9router", "groq", "key=set",
		"cl/nvidia/nemotron-3.5-super-vl:free", "http://localhost:20128/v1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("thiếu %q trong:\n%s", want, out)
		}
	}
}
