package dtos

// ChatHistoryItem là 1 lượt hội thoại trước — frontend ReactJS gửi kèm để chat
// nhiều vòng vẫn nhớ ngữ cảnh. Role chỉ nhận "user"/"assistant".
type ChatHistoryItem struct {
	Role    string `json:"role" binding:"required,oneof=user assistant" example:"user"`
	Content string `json:"content" binding:"required,max=4000" example:"Nghị định 123 nói về gì?"`
}

// ChatAttachment là tệp người dùng đính kèm cho MỘT lượt hỏi. Server trích
// text ở POST /chat/attachments rồi FE gửi lại kèm câu hỏi — tệp KHÔNG vào kho
// tri thức (không chunk/embed), nên câu trả lời dựa trên tệp không có trích dẫn
// [^n] và không sống sang lượt sau. Text bị chặn trần ở service, không tin
// kích thước client gửi.
type ChatAttachment struct {
	Name  string `json:"name" binding:"required,max=255" example:"bao-cao-quy.pdf"`
	Text  string `json:"text" binding:"required" example:"Nội dung đã trích từ tệp…"`
	Chars int    `json:"chars,omitempty" example:"15230"`
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
	// Attachment là tệp đính kèm của RIÊNG lượt này (rỗng = không có); nội dung
	// được chèn vào ngữ cảnh của lượt, không lưu vào phiên.
	Attachment *ChatAttachment `json:"attachment,omitempty"`
	// ConversationID gắn lượt chat vào một phiên đã lưu (S1): có id thì LỊCH SỬ
	// NẠP TỪ DB và `history` ở trên bị bỏ qua — hai nguồn cùng lúc là hai sự thật.
	// 0/không gửi = hành vi cũ: tin history client, không lưu lượt nào.
	ConversationID int64 `json:"conversation_id,omitempty" binding:"omitempty,gt=0" example:"12"`
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

// ChatChart là một biểu đồ backend tự dựng từ chunk đã dẫn (Đợt 26). Model chỉ
// được chọn nguồn/loại/tiêu đề/tên cột — `spec` là ECharts option do code sinh
// ra và đã bị cổng validate đối chiếu từng con số với series, nên FE cứ vẽ
// thẳng, không cần (và không được) sửa số.
type ChatChart struct {
	// N là số trích dẫn nguồn của biểu đồ — khớp đúng [^n] trong answer, FE bấm
	// chip "Nguồn: [n]" là nhảy sang source panel.
	N int `json:"n" example:"1"`
	// Kind là loại THỰC vẽ (bar|line|pie), có thể khác loại model xin khi dữ
	// liệu không hợp với line và rơi về bar.
	Kind string `json:"kind" example:"bar"`
	// Spec là option ECharts nguyên văn cho component chart.
	Spec map[string]any `json:"spec"`
	// Caption là chú thích đã kèm khuôn nguồn, để hiển thị dưới biểu đồ.
	Caption string `json:"caption" example:"Nguồn: [^1] · Phụ cấp theo nhân viên"`
}

// ChatResponse là câu trả lời hoàn chỉnh — answer là Markdown để ReactJS
// render bằng react-markdown, cites để map component trích dẫn.
type ChatResponse struct {
	Answer       string `json:"answer" example:"Mức phạt là **0.05%/ngày**[^1]."`
	QuestionType string `json:"question_type" example:"lookup"`
	// Provider/Model là lựa chọn THỰC đã chạy (đã resolve default) — FE gắn
	// badge dưới câu trả lời mà không phải đoán lại từ state chọn.
	Provider string      `json:"provider" example:"9router"`
	Model    string      `json:"model" example:"kc/nvidia/nemotron-3.5-super-vl:free"`
	Cites    []*ChatCite `json:"cites"`
	// Charts là biểu đồ của lượt này — dựng từ collector chứ không từ lời model,
	// buildCharts luôn trả mảng (kể cả rỗng) nên FE không gặp null.
	Charts []*ChatChart `json:"charts"`
	// Reasoning là đoạn model tự suy nghĩ trước khi đáp (chỉ có khi backend
	// trả reasoning, vd Ollama bật `think`) — omitempty để mặc định response
	// không đổi hình dạng với model không hỗ trợ.
	Reasoning string `json:"reasoning,omitempty" example:"Cần đối chiếu Điều 13..."`
}
