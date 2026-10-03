package dtos

import "time"

type PingPongResponse struct {
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}
