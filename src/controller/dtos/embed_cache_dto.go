package dtos

// EmbedCacheStatsResponse báo số entry + tổng dung lượng cache embed.
type EmbedCacheStatsResponse struct {
	Count int64 `json:"count" example:"32685"`
	Bytes int64 `json:"bytes" example:"134217728"`
}

// EmbedCachePurgeResponse báo số row đã xóa.
type EmbedCachePurgeResponse struct {
	Deleted int64 `json:"deleted" example:"32682"`
}

// EmbedCachePurgeRequest chỉ định số ngày giữ lại entry ít dùng.
type EmbedCachePurgeRequest struct {
	OlderThanDays int `json:"older_than_days" binding:"omitempty,gte=1" example:"30"`
}