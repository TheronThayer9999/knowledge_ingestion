package dtos

import "time"

type PingPongResponse struct {
	Message   string    `json:"message" example:"pong"`
	Timestamp time.Time `json:"timestamp" example:"2026-10-03T10:00:00+07:00"`
}
