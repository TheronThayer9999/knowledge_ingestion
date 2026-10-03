package dtos

type ResponseResource struct {
	Status  bool        `json:"status" example:"true"`
	Code    int         `json:"code" example:"200"`
	Message string      `json:"message" example:"success"`
	Data    interface{} `json:"data,omitempty"`
}
