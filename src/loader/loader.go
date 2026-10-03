package loader

import (
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/apis"
	"knowledge_ingestion/src/controller/routers"
	"knowledge_ingestion/src/controller/services"
	"os"

	"go.uber.org/fx"
)

func ConfigProvider() (config.IConfig, error) {
	path := os.Getenv("APP_CONFIG_PATH")
	if path == "" {
		path = "./configs/config.json"
	}
	return config.Load(path)
}

var Module = fx.Options(
	fx.Provide(ConfigProvider),
	services.Module,
	apis.Module,
	routers.Module,
)

func New(opts ...fx.Option) *fx.App {
	return fx.New(append([]fx.Option{Module}, opts...)...)
}

func Run(opts ...fx.Option) {
	New(opts...).Run()
}
