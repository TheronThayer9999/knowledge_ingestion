// @title Knowledge Ingestion API
// @version 1.0
// @description Knowledge ingestion service
// @BasePath /
// @securitydefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and JWT, e.g. "Bearer eyJhbGciOi..."
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	_ "knowledge_ingestion/docs"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/grpchandler"
	"knowledge_ingestion/src/controller/middlewares"
	"knowledge_ingestion/src/controller/routers"
	"knowledge_ingestion/src/loader"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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
		fx.Options(loader.Load()...),
		fx.Invoke(serverLifecycle),
	)

	if err := app.Start(context.Background()); err != nil {
		logs.Fatal(err, "Error starting application")
	}
	logs.Infow("application started", "config", configPath)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	if err := app.Stop(context.Background()); err != nil {
		logs.Fatal(err, "Error stopping application")
	}
	logs.Info("application stopped")
	logs.Sync()
}

func serverLifecycle(lc fx.Lifecycle, router *routers.Router, grpcServer *grpchandler.Server, auth middlewares.IAuthMiddleware, cfg config.IConfig) {
	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.GetApp().Port),
		Handler:           router.Engine,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// gRPC cho AI agent (Python) — cùng service với HTTP, interceptor auth +
	// trace tương đương middleware gin. Chạy cạnh HTTP trong 1 binary để khỏi
	// thêm deploy mới.
	grpcAddr := fmt.Sprintf(":%d", cfg.GetApp().GrpcPort)
	grpcSrv := grpchandler.NewGRPCServer(grpcServer, auth)

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", server.Addr)
			if err != nil {
				return fmt.Errorf("listen %s: %w", server.Addr, err)
			}
			go func() {
				if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
					logs.Error(err, "http server stopped", "addr", server.Addr)
				}
			}()
			gln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", grpcAddr)
			if err != nil {
				return fmt.Errorf("listen %s: %w", grpcAddr, err)
			}
			go func() {
				logs.Infow("grpc server serving", "addr", grpcAddr)
				if err := grpcSrv.Serve(gln); err != nil {
					logs.Error(err, "grpc server stopped", "addr", grpcAddr)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			logs.Infow("http server stopping", "addr", server.Addr)
			grpcSrv.GracefulStop()
			return server.Shutdown(ctx)
		},
	})
}
