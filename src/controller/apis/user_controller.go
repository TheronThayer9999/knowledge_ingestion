package apis

import "knowledge_ingestion/src/controller/services"

type UserAPI struct {
	*baseController
	svc services.IPingPongService
}

func NewUserAPI(base *baseController, svc services.IPingPongService) *PingPongAPI {
	return &PingPongAPI{baseController: base, svc: svc}
}
