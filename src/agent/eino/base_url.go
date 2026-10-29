package eino

import (
	"strings"

	"knowledge_ingestion/src/config"
)

// openAIEndpointSuffixes là các hậu tố endpoint mà người dùng hay DÁN THỪA vào
// ô "Base URL": họ copy nguyên dòng lệnh curl của provider (vd
// "https://api.groq.com/openai/v1/chat/completions"). Base URL phải là GỐC
// của API vì lib eino-ext tự nối "/chat/completions" vào sau — dán thừa
// endpoint thành ".../chat/completions/chat/completions".
var openAIEndpointSuffixes = []string{
	"/chat/completions",
	"/completions",
	"/responses",
	"/models",
}

// NormalizeBaseURL chuẩn hoá base URL của provider trước khi dựng client.
//
// Ba việc, cùng một luật — base URL là GỐC API, không phải endpoint:
//   - bỏ khoảng trắng, "/" cuối, và query/fragment. Query là tham số riêng
//     của provider (Azure: "?api-version=..."), đặt vào base URL sẽ hỏng
//     đường dẫn ngay khi nối "/chat/completions".
//   - bỏ hậu tố endpoint bị dán thừa.
//   - với Ollama (API native /api/chat, /api/tags) bỏ hậu tố /api*.
//
// Nhóm OpenAI-compatible GIỮ NGUYÊN phần đường dẫn vì mỗi provider tự quy
// định (thường kết thúc bằng /v1) — bỏ bớt ở đây sẽ gọi sai endpoint. Phần
// "thiếu /v1" không dò được bằng luật tĩnh nên để đường dò model lo (xem
// modelsEndpoints bên usecase).
func NormalizeBaseURL(kind, raw string) string {
	base := strings.TrimSpace(raw)
	if i := strings.IndexAny(base, "?#"); i >= 0 {
		base = base[:i]
	}
	base = strings.TrimRight(base, "/")
	if base == "" {
		return ""
	}
	if kind == config.LLMTypeOllama {
		return trimAPIURL(base)
	}
	for _, suffix := range openAIEndpointSuffixes {
		if strings.HasSuffix(base, suffix) {
			return strings.TrimRight(strings.TrimSuffix(base, suffix), "/")
		}
	}
	return base
}

// HasVersionPath báo đường dẫn base đã có đoạn phiên bản (v1, v2, v1beta).
// Dùng để biết có nên thử thêm "/v1" vào cuối hay không: base đã có /v1 thì
// thêm nữa thành /v1/v1.
func HasVersionPath(base string) bool {
	path := base
	if i := strings.Index(path, "://"); i >= 0 {
		path = path[i+3:]
	}
	slash := strings.Index(path, "/")
	if slash < 0 {
		return false
	}
	segments := strings.Split(strings.Trim(path[slash+1:], "/"), "/")
	last := segments[len(segments)-1]
	return len(last) > 1 && last[0] == 'v' && last[1] >= '0' && last[1] <= '9'
}

// trimAPIURL cắt hậu tố API khỏi base_url đã cấu hình — lib framework parse
// URL nguyên văn nên phải dọn trước, không thì request thành /api/api/chat.
func trimAPIURL(raw string) string {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	for _, suffix := range []string{"/api/chat", "/api/tags", "/api/embed", "/api"} {
		base = strings.TrimSuffix(base, suffix)
	}
	return base
}
