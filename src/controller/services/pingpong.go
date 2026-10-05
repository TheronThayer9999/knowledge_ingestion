package services

import (
	"knowledge_ingestion/src/controller/dtos"
	"time"
)

type IPingPongService interface {
	Ping() dtos.Result[dtos.PingPongResponse]
	Pong() dtos.Result[dtos.PingPongResponse]
}

type pingPongService struct{}

func NewPingPongService() IPingPongService {
	return &pingPongService{}
}

func (s *pingPongService) Ping() dtos.Result[dtos.PingPongResponse] {
	return dtos.Ok(dtos.PingPongResponse{Message: "pong", Timestamp: time.Now()})
}

func (s *pingPongService) Pong() dtos.Result[dtos.PingPongResponse] {
	return dtos.Ok(dtos.PingPongResponse{Message: "ping", Timestamp: time.Now()})
}
