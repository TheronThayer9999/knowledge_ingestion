package dtos

// LLMUsageDailyResponse là 1 dòng tổng hợp chi phí LLM theo tài khoản/ngày —
// admin đọc để quản lý "acc này dùng bao nhiêu LLM, dùng để làm gì".
type LLMUsageDailyResponse struct {
	UserID           int64  `json:"user_id" example:"7"`
	UserName         string `json:"user_name" example:"nguyenvana"`
	Email            string `json:"email" example:"a@example.com"`
	Day              string `json:"day" example:"2026-10-29"`
	Site             string `json:"site" example:"chat"`
	Provider         string `json:"provider" example:"9router"`
	Model            string `json:"model" example:"gpt-4o-mini"`
	Calls            int    `json:"calls" example:"42"`
	PromptTokens     int    `json:"prompt_tokens" example:"120000"`
	CompletionTokens int    `json:"completion_tokens" example:"18500"`
	TotalTokens      int    `json:"total_tokens" example:"138500"`
	Errors           int    `json:"errors" example:"1"`
}

// LLMUsageListQuery lọc báo cáo usage — from/to theo ngày "yyyy-mm-dd",
// user_id lọc đúng 1 tài khoản, site lọc theo phase (chat/compile/classify).
type LLMUsageListQuery struct {
	From   string `form:"from" binding:"omitempty,datetime=2006-01-02" example:"2026-10-01"`
	To     string `form:"to" binding:"omitempty,datetime=2006-01-02" example:"2026-10-29"`
	UserID int64  `form:"user_id" binding:"omitempty,gt=0" example:"7"`
	Site   string `form:"site" binding:"omitempty,oneof=chat compile classify" example:"chat"`
	Limit  int    `form:"limit" binding:"omitempty,gte=1,lte=500" example:"100"`
	Offset int    `form:"offset" binding:"omitempty,gte=0" example:"0"`
}

// LLMUsageListResponse trả danh sách dòng tổng hợp — kèm tổng toàn bộ để
// admin không phải cộng tay.
type LLMUsageListResponse struct {
	Items []LLMUsageDailyResponse `json:"items"`
	Total LLMUsageTotals          `json:"total"`
}

// LLMUsageTotals là tổng số liệu của các dòng trả về — calls/tokens/errors.
type LLMUsageTotals struct {
	Calls            int `json:"calls" example:"128"`
	PromptTokens     int `json:"prompt_tokens" example:"350000"`
	CompletionTokens int `json:"completion_tokens" example:"42000"`
	TotalTokens      int `json:"total_tokens" example:"392000"`
	Errors           int `json:"errors" example:"3"`
}
