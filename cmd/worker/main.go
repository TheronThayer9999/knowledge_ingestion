package main

import (
	"context"
	"flag"
	"fmt"
	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/scanocr"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/services"
	"knowledge_ingestion/src/loader"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/fx"
)

var (
	configPath  string
	llmProvider string
	llmModel    string
	listLLMs    bool
)

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
	// Cùng bộ flag với cmd/apis: worker compile tóm tắt mục bằng
	// llms.DefaultProvider nên chọn ở đây cũng đổi luôn model của compile.
	flag.StringVar(&llmProvider, "provider", "", "LLM provider slug (đè llms.default_provider; env LLM_PROVIDER)")
	flag.StringVar(&llmModel, "model", "", "LLM model của provider đã chọn (đè model trong config; env LLM_MODEL)")
	flag.BoolVar(&listLLMs, "list-llms", false, "in danh sách provider/model đã cấu hình rồi thoát")
	flag.Parse()
}

func main() {
	cfg, err := config.Load(configPath, config.WithLLMSelection(llmProvider, llmModel))
	if err != nil {
		logs.Fatal(err, "Error loading config", "path", configPath)
	}
	if listLLMs {
		fmt.Println(config.FormatLLMs(cfg))
		return
	}
	llms := cfg.GetLLMs()
	logs.Infow("llm selected", "provider", llms.DefaultProvider, "model", llms.Providers[llms.DefaultProvider].Model)

	app := fx.New(
		fx.Provide(func() config.IConfig { return cfg }),
		fx.Options(loader.LoadWorkerInfra()...),
		fx.Provide(services.NewArticlePurgeService),
		fx.Provide(services.NewArticleExtractService),
		fx.Provide(services.NewArticleIndexService),
		fx.Provide(services.NewArticleCompileService),
		fx.Provide(services.NewRefreshTokenJanitor),
		// Hang doi OCR quet (tab Quet): service xu ly theo trang.
		fx.Provide(func(cfg config.IConfig) scanocr.Config {
			s := cfg.GetScanOCR()
			return scanocr.Config{
				Enabled:           s.Enabled,
				Engine:            s.Engine,
				Binary:            s.Binary,
				Lang:              s.Lang,
				PSM:               s.PSM,
				Timeout:           time.Duration(s.Timeout) * time.Second,
				PageTimeout:       time.Duration(s.PageTimeout) * time.Second,
				RenderBinary:      s.RenderBinary,
				RenderDPI:         s.RenderDPI,
				PreviewDPI:        s.PreviewDPI,
				TextLayerMinChars: s.ThresholdChar,
				MinConfWord:       s.MinConfWord,
				MaxAttempts:       s.MaxAttempts,
				Parallel:          s.Parallel,
			}.Normalize()
		}),
		fx.Provide(func(cfg config.IConfig) services.ScanJobOptions {
			s := cfg.GetScanOCR()
			ttl := func(hours int, def time.Duration) time.Duration {
				if hours <= 0 {
					return def
				}
				return time.Duration(hours) * time.Hour
			}
			return services.ScanJobOptions{
				Enabled:        s.Enabled,
				Lang:           s.Lang,
				DPI:            s.RenderDPI,
				PSM:            s.PSM,
				Engine:         s.Engine,
				MaxUploadBytes: int64(s.MaxSizeMB) << 20,
				MaxPages:       s.MaxPages,
				TempTTL:        ttl(s.TempTTLHours, constants.SCAN_TMP_TTL),
				PushedTTL:      ttl(s.PushedTTLHours, constants.SCAN_PUSHED_TTL),
				PresignTTL:     constants.PRESIGN_DOWNLOAD_EXPIRY,
			}
		}),
		fx.Provide(services.NewScanJobWorkerService),
		fx.Provide(services.NewScanJobJanitor),
		fx.Provide(services.NewWorkerHeartbeatService),
		// Lưới chắn mất vector — khai báo job nằm ở schedulesFor, không rải
		// trong main.
		fx.Provide(services.NewIndexReconcileService),
		fx.Invoke(attachSchedules),
		// Nhịp sống worker — ghi bảng worker_heartbeats mỗi
		// WORKER_HEARTBEAT_INTERVAL để API/UI biết worker còn chạy (không
		// phải job Schedule: log "job xong" mỗi 15s chỉ tổ nhiễu).
		fx.Invoke(attachHeartbeat),
	)

	if err := app.Start(context.Background()); err != nil {
		logs.Fatal(err, "Error starting worker")
	}
	logs.Infow("worker started", "config", configPath)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	stopCtx, cancel := context.WithTimeout(context.Background(), constants.SHUTDOWN_TIMEOUT)
	defer cancel()
	if err := app.Stop(stopCtx); err != nil {
		logs.Fatal(err, "Error stopping worker")
	}
	logs.Info("worker stopped")
	logs.Sync()
}

// attachHeartbeat chạy nhịp sống worker trên goroutine riêng, gắn vòng đời vào
// fx lifecycle để OnStop cancel trước khi fx đóng DB. Tách khỏi main() vì main
// chỉ nên còn mỗi việc lắp fx (ngân sách hàm, src/quality).
func attachHeartbeat(lc fx.Lifecycle, beat services.IWorkerHeartbeatService) {
	ctx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go beat.Run(ctx)
			return nil
		},
		OnStop: func(context.Context) error {
			cancel()
			return nil
		},
	})
}
