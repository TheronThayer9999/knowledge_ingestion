package services

import (
	"knowledge_ingestion/src/domain/dtos"
	"time"

	"go.uber.org/fx"
)

type IPingPongService interface {
	Ping() dtos.PingPongResponse
	Pong() dtos.PingPongResponse
}

type pingPongService struct{}

func NewPingPongService() IPingPongService {
	return &pingPongService{}
}

func (s *pingPongService) Ping() dtos.PingPongResponse {
	return dtos.PingPongResponse{Message: "pong", Timestamp: time.Now()}
}

func (s *pingPongService) Pong() dtos.PingPongResponse {
	return dtos.PingPongResponse{Message: "ping", Timestamp: time.Now()}
}

var Module = fx.Options(
	fx.Provide(NewPingPongService),
)
