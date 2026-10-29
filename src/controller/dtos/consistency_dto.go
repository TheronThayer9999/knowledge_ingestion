package dtos

// ConsistencyCheckResponse báo cáo kiểm tra tính toàn vẹn dữ liệu — mọi con số
// 0 là hệ thống lành mạnh. Admin endpoint read-only.
//
// Hai nhóm số khác nguồn: 5 cột đầu chỉ nhìn Postgres, 5 cột `vector_*` nhìn
// thêm Qdrant (qua bộ reconcile) — bài "done" mà kho vector trống thì mọi con
// số Postgres vẫn 0 mà search vẫn thiếu, nên phải kiểm cả hai bên.
type ConsistencyCheckResponse struct {
	OrphanClosureRows  int64 `json:"orphan_closure_rows" example:"0"`
	MissingSelfRows    int64 `json:"missing_self_rows" example:"0"`
	CycleIndicators    int64 `json:"cycle_indicators" example:"0"`
	OrphanArticles     int64 `json:"orphan_articles" example:"0"`
	OrphanArticlePages int64 `json:"orphan_article_pages" example:"0"`
	Healthy            bool  `json:"healthy" example:"true"`
	// VectorPoints là số point ĐẾM ĐƯỢC trong collection; ClaimedChunks là tổng
	// articles.index_done_chunks của đám bài đã quét. Hai số này lệch nhau là
	// dấu hiệu mất vector hàng loạt (collection bị drop/recreate).
	VectorPoints     int64 `json:"vector_points" example:"17299"`
	ClaimedChunks    int64 `json:"claimed_chunks" example:"45533"`
	GhostArticles    int64 `json:"ghost_articles" example:"16"`
	ScannedArticles  int64 `json:"scanned_articles" example:"40"`
	VectorsTruncated bool  `json:"vectors_truncated" example:"false"`
	// VectorError rỗng là kiểm được; có chữ nghĩa là Qdrant không trả lời nên
	// đám số vector ở trên là số 0 VÔ NGHĨA, không phải "sạch" — healthy chỉ
	// tính khi hai bên cùng lên tiếng.
	VectorError string `json:"vector_error,omitempty" example:""`
	// GhostArticleIDs mẫu để kiểm tra tay, cắt ở GHOST_SAMPLE_SIZE.
	GhostArticleIDs []int64 `json:"ghost_article_ids"`
}
