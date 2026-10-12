package tools

import (
	"context"

	"knowledge_ingestion/src/agent/retrieval"
	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/domain"
)

// Tên tool agent được gọi ở vòng 2 — router vòng 1 chỉ chọn trong 2 giá trị
// này. Đặt ở đây (không ở question) để thêm tool mới chỉ chạm package tools +
// router, usecase không phải sửa.
const (
	// KnowledgeSearch tìm đoạn trích trong tài liệu upload (bọc
	// HybridSearcher.SearchHybrid khi port sang Eino InvokableTool).
	KnowledgeSearch = "knowledge_search"
	// None không gọi tool nào (chitchat) — đáp thẳng, đỡ tốn embed+Qdrant.
	None = "none"
)

// Decision là output vòng 1b: tool nào + tham số nào cho vòng 2. DTO của
// caller (limit tự chỉ định) thắng router — router chỉ là default theo loại
// khi caller không nói gì.
type Decision struct {
	Tool string
	// Limit số hit vòng 2 lấy; SkipSearch true thì khỏi search luôn.
	Limit      int
	SkipSearch bool
}

// Args input thuần cho tool search — usecase map từ DTO sang, tool không dính
// envelope HTTP nên test fake được mà không cần DTO.
type Args struct {
	UserID         int64
	Query          string
	CategoryIDs    []int64
	ArticleID      int64
	Limit          int
	ScoreThreshold float32
}

// Registry gom các tool search của agent — hiện chỉ 1 tool thật
// (knowledge_search) + nhánh skip. Thêm tool mới (đọc file, đếm, tính toán)
// thì thêm method + đăng ký ở đây, usecase gọi qua Execute mà không fork
// logic limit/skip mỗi nơi một kiểu.
type Registry struct {
	hybrid *retrieval.HybridSearcher
}

func NewRegistry(hybrid *retrieval.HybridSearcher) *Registry {
	return &Registry{hybrid: hybrid}
}

// ResolveLimit chốt limit cuối: caller ép thì thắng (chặn trần max), không thì
// default theo loại, cuối cùng là default hệ thống.
func ResolveLimit(requested, routeDefault int) int {
	limit := requested
	if limit <= 0 {
		limit = routeDefault
	}
	if limit <= 0 {
		limit = constants.SEARCH_DEFAULT_LIMIT
	}
	if limit > constants.SEARCH_MAX_LIMIT {
		limit = constants.SEARCH_MAX_LIMIT
	}
	return limit
}

// ShouldSkip cho biết có bỏ qua tool không — chitchat không limit ép thì skip,
// caller ép limit vẫn search (ưu tiên ý caller).
func ShouldSkip(d Decision, requestedLimit int) bool {
	return d.SkipSearch && requestedLimit <= 0
}

// Execute chạy tool theo decision: skip thì trả rỗng không chạm embed/Qdrant,
// ngược lại chạy pipeline hybrid với limit đã chốt. Lỗi hạ tầng trả về để
// caller map 500, không nuốt.
func (r *Registry) Execute(ctx context.Context, d Decision, a Args) ([]*domain.ScoredChunk, error) {
	if ShouldSkip(d, a.Limit) {
		return []*domain.ScoredChunk{}, nil
	}
	a.Limit = ResolveLimit(a.Limit, d.Limit)
	return r.hybrid.SearchHybrid(ctx, retrieval.HybridParams{
		UserID:         a.UserID,
		Query:          a.Query,
		CategoryIDs:    a.CategoryIDs,
		ArticleID:      a.ArticleID,
		Limit:          a.Limit,
		ScoreThreshold: a.ScoreThreshold,
	})
}
