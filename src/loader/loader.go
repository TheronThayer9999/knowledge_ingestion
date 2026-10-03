package loader

import (
	"knowledge_ingestion/src/controller/apis"
	"knowledge_ingestion/src/controller/routers"
	"knowledge_ingestion/src/controller/services"
	"knowledge_ingestion/src/infrastructure/seaweedfs"

	"github.com/go-playground/validator/v10"
	"go.uber.org/fx"
)

func Load() []fx.Option {
	return []fx.Option{
		fx.Options(loadAdapter()...),
		fx.Options(loadService()...),
		fx.Options(loadValidator()...),
		fx.Options(loadEngine()...),
	}
}

func loadAdapter() []fx.Option {
	return []fx.Option{
		fx.Provide(seaweedfs.NewStorage),
	}
}

func loadService() []fx.Option {
	return []fx.Option{
		fx.Provide(services.NewPingPongService),
	}
}

func loadValidator() []fx.Option {
	return []fx.Option{
		fx.Provide(validator.New),
	}
}

func loadEngine() []fx.Option {
	return []fx.Option{
		fx.Provide(apis.NewBaseController),
		fx.Provide(apis.NewPingPongAPI),
		fx.Provide(routers.NewRouter),
	}
}
