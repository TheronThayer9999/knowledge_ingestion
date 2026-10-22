package dtos

// ConsistencyCheckResponse báo cáo kiểm tra tính toàn vẹn dữ liệu — mọi con số
// 0 là hệ thống lành mạnh. Admin endpoint read-only.
type ConsistencyCheckResponse struct {
	OrphanClosureRows   int64 `json:"orphan_closure_rows" example:"0"`
	MissingSelfRows     int64 `json:"missing_self_rows" example:"0"`
	CycleIndicators     int64 `json:"cycle_indicators" example:"0"`
	OrphanArticles      int64 `json:"orphan_articles" example:"0"`
	OrphanArticlePages  int64 `json:"orphan_article_pages" example:"0"`
	Healthy             bool  `json:"healthy" example:"true"`
}