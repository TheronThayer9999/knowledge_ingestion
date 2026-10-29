// Package llmusage gom số liệu tiêu thụ LLM của 1 lượt rồi đẩy vào bảng
// tổng hợp theo tài khoản — nền tảng của việc audit "acc này dùng bao nhiêu
// LLM".
//
// Cơ chế: collector đi theo ctx, handler của Eino (src/agent/eino) bắt mọi
// lượt gọi chat model rồi cộng vào collector, tầng service đọc ra 1 lần
// rồi ghi 1 dòng tổng hợp. Nhờ vậy không phải sửa chữ ký hàm nào của agent,
// classifier hay summarizer.
package llmusage

import (
	"context"
	"sync"
	"time"

	"knowledge_ingestion/src/domain"
)

// Collector gom số liệu tiêu thụ LLM theo phase (chat/compile/classify) —
// 1 lượt có thể sinh nhiều phase (chat vừa gọi agent vừa gọi classifier)
// nên bóc theo site thay vì 1 con số chung.
type Collector struct {
	mu     sync.Mutex
	bySite map[string]domain.LLMUsage
}

// NewCollector tạo collector rỗng — dùng 1 cái cho 1 lượt (1 lượt chat, 1
// bài compile).
func NewCollector() *Collector {
	return &Collector{bySite: map[string]domain.LLMUsage{}}
}

// Add cộng thêm số liệu của 1 phase vào collector.
func (c *Collector) Add(site string, u domain.LLMUsage) {
	if c == nil || site == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	cur := c.bySite[site]
	cur.Calls += u.Calls
	cur.PromptTokens += u.PromptTokens
	cur.CompletionTokens += u.CompletionTokens
	cur.TotalTokens += u.TotalTokens
	cur.Errors += u.Errors
	c.bySite[site] = cur
}

// Snapshot trả bản sao số liệu theo phase — caller đọc rồi tự ghi, không
// cần biết chi tiết khoá.
func (c *Collector) Snapshot() map[string]domain.LLMUsage {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]domain.LLMUsage, len(c.bySite))
	for k, v := range c.bySite {
		out[k] = v
	}
	return out
}

// Drain lấy số liệu và XOÁ đi — dùng khi ghi theo lô (compile ghi sau mỗi lô
// mục) để lô sau không cộng trùng vào lô trước.
func (c *Collector) Drain() map[string]domain.LLMUsage {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.bySite
	c.bySite = map[string]domain.LLMUsage{}
	return out
}

type ctxKey struct{}

// WithCollector gắn collector vào ctx — tầng trên (service) làm trước khi
// gọi LLM, tầng dưới (handler) đọc ra để cộng.
func WithCollector(ctx context.Context, c *Collector) context.Context {
	if ctx == nil || c == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, c)
}

// FromCtx đọc collector trong ctx — không có thì trả false (lượt gọi LLM
// ngoài phạm vi audit, ví dụ tool nội bộ, không ghi bảng nào).
func FromCtx(ctx context.Context) (*Collector, bool) {
	if ctx == nil {
		return nil, false
	}
	c, ok := ctx.Value(ctxKey{}).(*Collector)
	return c, ok && c != nil
}

// Record đẩy số liệu đã gom vào bảng tổng hợp theo tài khoản — 1 phase 1
// dòng, cùng khoá thì cộng dồn. Dùng chung cho chat và compile nên cách
// ghi chỉ có 1 bản.
func Record(ctx context.Context, repo domain.ILLMUsageRepository, userID int64,
	provider, model string, bySite map[string]domain.LLMUsage) error {
	if repo == nil || len(bySite) == 0 {
		return nil
	}
	day := time.Now().Format("2006-01-02")
	recs := make([]domain.LLMUsageRecord, 0, len(bySite))
	for site, u := range bySite {
		if u.IsZero() {
			continue
		}
		recs = append(recs, domain.LLMUsageRecord{
			UserID:           userID,
			Day:              day,
			Site:             site,
			Provider:         provider,
			Model:            model,
			Calls:            u.Calls,
			PromptTokens:     u.PromptTokens,
			CompletionTokens: u.CompletionTokens,
			TotalTokens:      u.TotalTokens,
			Errors:           u.Errors,
		})
	}
	if len(recs) == 0 {
		return nil
	}
	return repo.Record(ctx, recs)
}
