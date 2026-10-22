package domain

import "context"

// ISummarizer hợp đồng tóm tắt 1 mục tài liệu bằng LLM — compile phase 2
// (P2.4 phase 1b) gọi cho từng section. Triển khai ở agent/eino bọc
// ToolCallingChatModel (Eino), service chỉ phụ thuộc interface này nên đổi
// backend LLM chỉ cần viết adapter mới (cùng kiểu Embedder/IVectorStore).
type ISummarizer interface {
	// Summarize trả tóm tắt ngắn (vài câu) của nội dung section — text là
	// chuỗi chunk đã ghép theo tọa độ ListSections/ScrollRange.
	Summarize(ctx context.Context, sectionName, text string) (string, error)
	// ModelID là định danh backend+model ("openrouter/gpt-...") — compile
	// service trộn vào hash cache để đổi model tự mất hiệu lực cache cũ.
	ModelID() string
}
