package main

import (
	"context"
	"flag"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/services"
	"knowledge_ingestion/src/loader"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/fx"
)

var configPath string

func init() {
	logs.LoadLogger()
	defaultPath := "./configs/config.json"
	if p := os.Getenv("APP_CONFIG_PATH"); p != "" {
		defaultPath = p
	}
	flag.StringVar(&configPath, "configs", defaultPath, "path config")
	flag.Parse()
}

func main() {
	app := fx.New(
		fx.Provide(func() (config.IConfig, error) {
			return config.Load(configPath)
		}),
		fx.Options(loader.LoadWorkerInfra()...),
		fx.Provide(services.NewArticlePurgeService),
		// Thêm job mới = inject service ở tham số + append 1 dòng Schedule:
		// {Name: "...", Interval: ..., Run: asJob(svc.Method)}.
		fx.Invoke(func(lc fx.Lifecycle, purge services.IArticlePurgeService) {
			NewRunner([]Schedule{
				{Name: "purge-articles", Interval: services.PurgeGracePeriod, Run: asJob(purge.PurgeDeleted)},
			}).Attach(lc)
		}),
	)

	if err := app.Start(context.Background()); err != nil {
		logs.Fatal(err, "Error starting worker")
	}
	logs.Infow("worker started", "config", configPath)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	if err := app.Stop(context.Background()); err != nil {
		logs.Fatal(err, "Error stopping worker")
	}
	logs.Info("worker stopped")
	logs.Sync()
}
