package services

import "knowledge_ingestion/src/controller/dtos"

type IUserService interface {
	Ping() dtos.Result[dtos.PingPongResponse]
	Pong() dtos.Result[dtos.PingPongResponse]
}
type userService struct{}

func NewUserService() IUserService {
	return &userService{}
}

func (u userService) Ping() dtos.Result[dtos.PingPongResponse] {
	//TODO implement me
	panic("implement me")
}

func (u userService) Pong() dtos.Result[dtos.PingPongResponse] {
	//TODO implement me
	panic("implement me")
}
