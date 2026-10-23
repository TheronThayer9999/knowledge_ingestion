package dtos

// ChatHistoryItem là 1 lượt hội thoại trước — frontend ReactJS gửi kèm để chat
// nhiều vòng vẫn nhớ ngữ cảnh. Role chỉ nhận "user"/"assistant".
type ChatHistoryItem struct {
	Role    string `json:"role" binding:"required,oneof=user assistant" example:"user"`
	Content string `json:"content" binding:"required,max=4000" example:"Nghị định 123 nói về gì?"`
}

// ChatRequest hỏi đáp tri thức — message bắt buộc; provider/model tùy chọn:
// provider rỗng thì dùng mặc định (ollama), model rỗng thì dùng model mặc
// định của provider đó (openrouter: google/gemma-4-31b-it:free).
type ChatRequest struct {
	Message string `json:"message" binding:"required,min=1,max=2000" example:"Phụ cấp nhân viên đi công tác là bao nhiêu?"`
	// Provider chọn backend LLM (rỗng = mặc định cấu hình) — validate theo
	// registry lúc runtime (Resolve trả 400 kèm danh sách provider đang có),
	// không hardcode oneof ở tag để thêm backend không phải sửa DTO.
	Provider string `json:"provider,omitempty" example:"openrouter"`
	// Model ép model trong provider đó (rỗng = model mặc định theo provider).
	Model string `json:"model,omitempty" example:"apodex/apodex-1.1-mini:free"`
	// Thinking bật chế độ suy luận sâu: request đi qua bản agent đã bật công
	// tắc `think` của backend. Provider không có công tắc đó thì cờ chỉ vô
	// tác dụng (vẫn chạy bản thường), nên không cần validate theo provider.
	Thinking    bool              `json:"thinking,omitempty" example:"true"`
	CategoryIDs []int64           `json:"category_ids,omitempty" binding:"omitempty,max=100,dive,gt=0"`
	Limit       int               `json:"limit,omitempty" binding:"omitempty,gte=0"`
	History     []ChatHistoryItem `json:"history,omitempty"`
}

// ChatCite là 1 nguồn trích dẫn — frontend map thành chip/link, backend đánh
// số [^n] khớp chú thích trong answer markdown.
type ChatCite struct {
	N          int    `json:"n" example:"1"`
	ArticleID  int64  `json:"article_id" example:"8"`
	PageNum    int    `json:"page_num" example:"4"`
	ChunkIndex int    `json:"chunk_index" example:"2"`
	Snippet    string `json:"snippet" example:"Điều 13... phạt 0.05%/ngày..."`
}

// ChatResponse là câu trả lời hoàn chỉnh — answer là Markdown để ReactJS
// render bằng react-markdown, cites để map component trích dẫn.
type ChatResponse struct {
	Answer       string      `json:"answer" example:"Mức phạt là **0.05%/ngày**[^1]."`
	QuestionType string      `json:"question_type" example:"lookup"`
	Cites        []*ChatCite `json:"cites"`
	// Reasoning là đoạn model tự suy nghĩ trước khi đáp (chỉ có khi backend
	// trả reasoning, vd Ollama bật `think`) — omitempty để mặc định response
	// không đổi hình dạng với model không hỗ trợ.
	Reasoning string `json:"reasoning,omitempty" example:"Cần đối chiếu Điều 13..."`
}
