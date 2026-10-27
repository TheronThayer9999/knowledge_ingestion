package eino

import (
	"context"
	"slices"
	"sync"

	"knowledge_ingestion/src/domain"
)

type ctxKey string

const scopeKey ctxKey = "eino-scope"

// Scope là toàn bộ phạm vi tin cậy của 1 lượt chat, nhét 1 lần ở usecase để
// mọi tool đọc lúc thực thi — tool mới chỉ cần học 1 helper ScopeFrom:
//   - UserID/CategoryIDs/ScoreThreshold: từ token + DTO, KHÔNG tin args LLM
//     (LLM chỉ gợi ý category/limit, DTO thắng).
//   - Collector: gom hit qua các lượt gọi tool để dựng cites.
type Scope struct {
	UserID         int64
	CategoryIDs    []int64
	ScoreThreshold float32
	Collector      *HitCollector
}

// WithScope nhét phạm vi vào ctx — Collector nil thì tự tạo để tool luôn có
// chỗ gom cites.
func WithScope(ctx context.Context, s Scope) context.Context {
	if s.Collector == nil {
		s.Collector = &HitCollector{seen: make(map[string]int)}
	}
	return context.WithValue(ctx, scopeKey, s)
}

// ScopeFrom đọc phạm vi khỏi ctx — false nghĩa là lỗi lập trình (usecase quên
// nhét), tool phải trả lỗi chứ không chạy với phạm vi rỗng.
func ScopeFrom(ctx context.Context) (Scope, bool) {
	s, ok := ctx.Value(scopeKey).(Scope)
	return s, ok
}

// AllowsCategory báo categoryID có nằm trong phạm vi request không.
// Scope rỗng (request không khoanh chủ đề) thì mọi category của user hợp lệ —
// quyền owner vẫn do repo kiểm tra riêng.
func (s Scope) AllowsCategory(categoryID int64) bool {
	if len(s.CategoryIDs) == 0 {
		return true
	}
	return slices.Contains(s.CategoryIDs, categoryID)
}

// maxCollectedHits chặn số hit gom cho cites — agent có thể gọi tool nhiều
// vòng, gom quá nhiều thì response phình mà frontend cũng không hiện hết.
const maxCollectedHits = 30

// HitCollector gom hit qua các lượt gọi tool trong 1 request — n đánh theo
// thứ tự thấy lần đầu, khớp số [^n] tool trả cho LLM và cites trả frontend.
// Có mutex vì Eino gọi parallel tool call (nhiều goroutine cùng gom hit) —
// thiếu thì fatal "concurrent map writes" kill cả process.
type HitCollector struct {
	mu   sync.Mutex
	Hits []*domain.ScoredChunk
	seen map[string]int
	// Charts gom biểu đồ tool chart_from_cite dựng được trong lượt — chat_service
	// map ra DTO từ đây chứ không từ lời model.
	Charts []*CollectedChart
	// Searched đánh dấu agent đã đọc tài liệu (search/info/sections/context),
	// kể cả khi kết quả rỗng không gom được hit nào — nhãn question_type dựa
	// vào cờ này chứ không dựa vào số cites (tool đếm Điều không sinh cites).
	Searched bool
}

// MarkSearched set cờ đã tra cứu — thread-safe cho tool chạy song song.
func (c *HitCollector) MarkSearched() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Searched = true
}

// Snapshot trả bản chốt Hits/Searched sau khi agent xong lượt — copy slice
// header dưới lock để đọc an toàn.
func (c *HitCollector) Snapshot() ([]*domain.ScoredChunk, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Hits, c.Searched
}

// SnapshotCharts trả danh sách biểu đồ đã dựng. Hàm riêng chứ không sửa chữ ký
// Snapshot để không đụng mọi chỗ đang gọi Snapshot.
func (c *HitCollector) SnapshotCharts() []*CollectedChart {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Charts
}

// Cite tra nguồn theo số trích dẫn [^n] — đúng cách Add đánh số (n = index+1
// trong Hits). Tool chỉ được vẽ từ nguồn model đã nhìn thấy; n ngoài phạm vi
// là model bịa, phải trả false để tool từ chối chứ không suy đoán.
func (c *HitCollector) Cite(n int) (*domain.ScoredChunk, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if n < 1 || n > len(c.Hits) {
		return nil, false
	}
	return c.Hits[n-1], true
}

// AddChart ghi một biểu đồ, trả false khi vượt trần. Trùng y hệt (cùng nguồn,
// cùng loại, cùng tiêu đề) coi như đã có và trả true — model gọi lại hai lần
// không được nhân đôi biểu đồ trên màn hình.
func (c *HitCollector) AddChart(chart *CollectedChart) bool {
	if chart == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, old := range c.Charts {
		if old.N == chart.N && old.Kind == chart.Kind && old.Caption == chart.Caption {
			return true
		}
	}
	if len(c.Charts) >= maxCollectedCharts {
		return false
	}
	c.Charts = append(c.Charts, chart)
	return true
}

// Add gom hits, trả n cho từng hit theo thứ tự đầu vào — hit trùng PointID
// giữ n cũ, quá trần thì n=0 (tool bỏ khỏi kết quả trả LLM).
func (c *HitCollector) Add(hits []*domain.ScoredChunk) []int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen == nil {
		c.seen = make(map[string]int)
	}
	ns := make([]int, 0, len(hits))
	for _, h := range hits {
		if h == nil {
			continue
		}
		if n, ok := c.seen[h.PointID]; ok {
			ns = append(ns, n)
			continue
		}
		if len(c.Hits) >= maxCollectedHits {
			ns = append(ns, 0)
			continue
		}
		c.Hits = append(c.Hits, h)
		c.seen[h.PointID] = len(c.Hits)
		ns = append(ns, len(c.Hits))
	}
	return ns
}
