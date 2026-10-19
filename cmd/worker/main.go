package main

import (
	"context"
	"flag"
	"knowledge_ingestion/src/common/constants"
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
	// Ưu tiên config.local.json (gitignored, chứa key thật) khi chạy local;
	// env/flag vẫn ép được như cũ.
	if p := os.Getenv("APP_CONFIG_PATH"); p != "" {
		defaultPath = p
	} else if _, err := os.Stat("./configs/config.local.json"); err == nil {
		defaultPath = "./configs/config.local.json"
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
		fx.Provide(services.NewArticleExtractService),
		fx.Provide(services.NewArticleIndexService),
		// Thêm job mới = inject service ở tham số + append 1 dòng Schedule:
		// {Name: "...", Interval: ..., Run: asJob(svc.Method)}.
		fx.Invoke(func(lc fx.Lifecycle, purge services.IArticlePurgeService, extract services.IArticleExtractService, index services.IArticleIndexService, opts services.PurgeOptions) {
			NewRunner([]Schedule{
				{Name: "purge-articles", Interval: constants.PURGE_GRACE_PERIOD, Run: func(ctx context.Context) error {
					_, err := purge.PurgeDeleted(ctx)
					return err
				}},
				{Name: "trim-orphan-pages", Interval: opts.TrimInterval, Run: func(ctx context.Context) error {
					_, err := purge.TrimOrphanPages(ctx)
					return err
				}},
				// Timeout riêng vì bài 500 trang OCR mất ~17 phút — cancel giữa
				// chừng thì mất công. Nhờ EXTRACT_FLUSH_PAGES nên hết giờ chỉ
				// mất tối đa 1 lô trang, kỳ sau nhảy tiếp.
				{Name: "extract-articles", Interval: constants.EXTRACT_INTERVAL, Timeout: constants.EXTRACT_JOB_TIMEOUT, Run: func(ctx context.Context) error {
					_, err := extract.ExtractPending(ctx)
					return err
				}},
				// Phase index không còn OCR nên nhẹ hơn hẳn, nhưng bài lớn vẫn
				// gọi Ollama ~47 lần nên vẫn cần timeout riêng.
				{Name: "index-articles", Interval: constants.INDEX_INTERVAL, Timeout: constants.INDEX_JOB_TIMEOUT, Run: func(ctx context.Context) error {
					_, err := index.IndexPending(ctx)
					return err
				}},
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
