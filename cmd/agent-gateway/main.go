// Binary gRPC riêng cho AI agent (Python) — chạy KnowledgeService trên
// App.GrpcPort, độc lập với binary HTTP (cmd/apis). Dùng chung service +
// repo + interceptor auth/trace với API nên hành vi nhất quán, khác mỗi
// transport. Chạy: go run ./cmd/agent-gateway (cùng flag -configs như API).
package main

import (
	"context"
	"flag"
	"fmt"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/grpchandler"
	"knowledge_ingestion/src/controller/middlewares"
	"knowledge_ingestion/src/loader"
	"net"
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
		fx.Options(loader.LoadGRPC()...),
		fx.Invoke(grpcLifecycle),
	)

	if err := app.Start(context.Background()); err != nil {
		logs.Fatal(err, "Error starting grpc server")
	}
	logs.Infow("grpc server started", "config", configPath)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	if err := app.Stop(context.Background()); err != nil {
		logs.Fatal(err, "Error stopping grpc server")
	}
	logs.Info("grpc server stopped")
	logs.Sync()
}

func grpcLifecycle(lc fx.Lifecycle, srv *grpchandler.Server, auth middlewares.IAuthMiddleware, cfg config.IConfig) {
	addr := fmt.Sprintf(":%d", cfg.GetApp().GrpcPort)
	grpcSrv := grpchandler.NewGRPCServer(srv, auth)

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
			if err != nil {
				return fmt.Errorf("listen %s: %w", addr, err)
			}
			go func() {
				logs.Infow("grpc server serving", "addr", addr)
				if err := grpcSrv.Serve(ln); err != nil {
					logs.Error(err, "grpc server stopped", "addr", addr)
				}
			}()
			return nil
		},
		OnStop: func(context.Context) error {
			logs.Infow("grpc server stopping", "addr", addr)
			grpcSrv.GracefulStop()
			return nil
		},
	})
}
