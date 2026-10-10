package dtos

// ChatHistoryItem là 1 lượt hội thoại trước — frontend ReactJS gửi kèm để chat
// nhiều vòng vẫn nhớ ngữ cảnh. Role chỉ nhận "user"/"assistant".
type ChatHistoryItem struct {
	Role    string `json:"role" binding:"required,oneof=user assistant" example:"user"`
	Content string `json:"content" binding:"required,max=4000" example:"Nghị định 123 nói về gì?"`
}

// ChatRequest hỏi đáp tri thức — message bắt buộc, model bắt buộc (chat không
// có model thì không tổng hợp được, khác search còn default lookup).
type ChatRequest struct {
	Message    string            `json:"message" binding:"required,min=1,max=2000" example:"Mức phạt chậm nộp thuế bao nhiêu?"`
	Model      string            `json:"model" binding:"required,min=1" example:"gemma4:31b"`
	CategoryID int64             `json:"category_id,omitempty" binding:"omitempty,gt=0" example:"5"`
	Limit      int               `json:"limit,omitempty" binding:"omitempty,gte=0"`
	History    []ChatHistoryItem `json:"history,omitempty"`
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
	Answer       string     `json:"answer" example:"Mức phạt là **0.05%/ngày**[^1]."`
	QuestionType string     `json:"question_type" example:"lookup"`
	Cites        []*ChatCite `json:"cites"`
}
