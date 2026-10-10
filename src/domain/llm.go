package domain

import "context"

// ChatMessage là một lượt hội thoại gửi cho LLM.
type ChatMessage struct {
	Role    string `json:"role"` // "system", "user", "assistant"
	Content string `json:"content"`
}

// ChatOptions là tham số theo từng request — Model bắt buộc vì config không
// giữ model cố định: user list model từ Ollama rồi truyền vào mỗi lần gọi.
type ChatOptions struct {
	Model       string
	Temperature float64 // <= 0 thì dùng default của client
	MaxTokens   int     // <= 0 thì dùng default của client
}

// ILLM là contract chat cho agent — triển khai ở infrastructure/llm.
type ILLM interface {
	// ListModels trả về tên các model chat đang có trên Ollama (GET /api/tags)
	// để user chọn rồi truyền vào Chat theo request.
	ListModels(ctx context.Context) ([]string, error)
	// Chat gọi model theo request — opts.Model rỗng thì lỗi, không tự đoán.
	Chat(ctx context.Context, messages []ChatMessage, opts ChatOptions) (string, error)
}
