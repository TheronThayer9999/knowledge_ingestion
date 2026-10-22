package dtos

// HealthResponse báo trạng thái các thành phần phụ thuộc — mọi component "ok"
// thì HTTP 200, có thành phần "down" thì 503.
type HealthResponse struct {
	Status     string            `json:"status" example:"ok"`
	Components map[string]string `json:"components"`
}