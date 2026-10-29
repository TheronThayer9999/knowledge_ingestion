package domain

import "context"

// SectionContent là 1 mục tài liệu cần tóm tắt — tên mục + text đã ghép từ
// các chunk của mục đó.
type SectionContent struct {
	Name string
	Text string
}

// ISummarizer hợp đồng tóm tắt mục tài liệu bằng LLM — compile phase 2
// (P2.4 phase 1b) gọi cho từng lô mục. Triển khai ở agent/eino bọc
// ToolCallingChatModel (Eino), service chỉ phụ thuộc interface này nên đổi
// backend LLM chỉ cần viết adapter mới (cùng kiểu Embedder/IVectorStore).
type ISummarizer interface {
	// SummarizeBatch tóm tắt N mục trong 1 lượt gọi LLM, trả về N tóm tắt
	// CÙNG THỨ TỰ với items. Gộp lô là bắt buộc để giảm số request: bài 500
	// trang có hàng nghìn mục, gọi 1 mục/lượt thì tốn hàng nghìn request.
	//
	// items rỗng → trả slice rỗng, không lỗi.
	SummarizeBatch(ctx context.Context, items []SectionContent) ([]string, error)
	// ModelID là định danh model ("gpt-4o-mini") — compile service trộn vào
	// hash cache để đổi model tự mất hiệu lực cache cũ.
	ModelID() string
	// ProviderID là slug provider ("9router") — ghi vào bảng audit usage để
	// quản lý biết chi phí rơi vào backend nào.
	ProviderID() string
}
