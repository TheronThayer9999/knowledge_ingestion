package domain

import "context"

// LLMUsage là số liệu tiêu thụ LLM của 1 nhóm lượt gọi — calls = số lượt gọi
// model (lượt LOGIC, không tính lượt thử lại ở tầng HTTP), tokens = token
// provider đã trả, errors = số lượt gọi hỏng.
type LLMUsage struct {
	Calls            int
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	Errors           int
}

// IsZero báo số liệu có trống không — dòng tổng hợp rỗng thì không ghi (đỡ
// rác bảng audit với dòng toàn 0).
func (u LLMUsage) IsZero() bool {
	return u.Calls == 0 && u.PromptTokens == 0 && u.CompletionTokens == 0 &&
		u.TotalTokens == 0 && u.Errors == 0
}

// LLMUsageRecord là 1 dòng tổng hợp theo (tài khoản, ngày, phase, provider,
// model) — đơn vị ghi vào bảng llm_usage_daily.
type LLMUsageRecord struct {
	UserID           int64
	Day              string
	Site             string
	Provider         string
	Model            string
	Calls            int
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	Errors           int
}

// LLMUsageDaily là bảng tổng hợp chi phí LLM theo tài khoản — cơ sở audit
// "acc này dùng bao nhiêu LLM, dùng để làm gì, model nào". Ghi theo NGÀY để
// bảng không phình theo số lượt gọi: 1 tài khoản dùng nhiều vẫn chỉ vài dòng
// mỗi ngày.
//
// Khoá đọc theo (user_id, day, site, provider, model) — cùng khoá thì cộng
// dồn (ON CONFLICT DO UPDATE), nên ghi nhiều lượt trong ngày chỉ tạo 1 dòng.
// Bảng này KHÔNG xoá mềm: dòng tổng hợp giữ trọn lịch sử để đối chiếu hoá
// đơn, nên unique index không partial (khác với các bảng nghiệp vụ).
type LLMUsageDaily struct {
	BaseModel
	UserID   int64  `gorm:"not null;uniqueIndex:idx_llm_usage_daily_key,priority:1"`
	Day      string `gorm:"size:10;not null;uniqueIndex:idx_llm_usage_daily_key,priority:2"`
	Site     string `gorm:"size:32;not null;uniqueIndex:idx_llm_usage_daily_key,priority:3"`
	Provider string `gorm:"size:64;not null;uniqueIndex:idx_llm_usage_daily_key,priority:4"`
	Model    string `gorm:"size:160;not null;uniqueIndex:idx_llm_usage_daily_key,priority:5"`
	// Calls số lượt gọi model; Errors số lượt hỏng (tách riêng vì lượt hỏng
	// vẫn có thể đã tốn token đầu vào).
	Calls            int `gorm:"not null;default:0"`
	PromptTokens     int `gorm:"not null;default:0"`
	CompletionTokens int `gorm:"not null;default:0"`
	TotalTokens      int `gorm:"not null;default:0"`
	Errors           int `gorm:"not null;default:0"`
}

func (LLMUsageDaily) TableName() string {
	return "llm_usage_daily"
}

// LLMUsageFilter lọc báo cáo usage — From/To theo ngày "yyyy-mm-dd", rỗng là
// không lọc. UserID > 0 lọc đúng 1 tài khoản.
type LLMUsageFilter struct {
	From   string
	To     string
	UserID int64
	Site   string
	Limit  int
	Offset int
}

// LLMUsageRow là dòng báo cáo: số liệu tổng hợp + tên/email tài khoản (join
// bảng users) — admin nhìn 1 lần là biết acc nào tốn, không phải tra id.
type LLMUsageRow struct {
	UserID           int64
	UserName         string
	Email            string
	Day              string
	Site             string
	Provider         string
	Model            string
	Calls            int
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	Errors           int
}

// ILLMUsageRepository ghi + đọc bảng tổng hợp usage LLM.
type ILLMUsageRepository interface {
	// Record cộng dồn các dòng tổng hợp — cùng khoá thì cộng vào dòng cũ,
	// không tạo dòng mới (ON CONFLICT DO UPDATE).
	Record(ctx context.Context, recs []LLMUsageRecord) error
	// List trả các dòng tổng hợp theo bộ lọc, ngày mới nhất trước.
	List(ctx context.Context, f LLMUsageFilter) ([]*LLMUsageRow, error)
}
