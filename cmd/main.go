// @title Knowledge Ingestion API
// @version 1.0
// @description Knowledge ingestion service
// @BasePath /
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	_ "knowledge_ingestion/docs"
	"knowledge_ingestion/src/config"
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
		fmt.Fprintln(os.Stderr, "Error starting application:", err)
		os.Exit(1)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	if err := app.Stop(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "Error stopping application:", err)
		os.Exit(1)
	}
}

func serverLifecycle(lc fx.Lifecycle, router *routers.Router, cfg config.IConfig) {
	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.GetApp().Port),
		Handler:           router.Engine,
		ReadHeaderTimeout: 5 * time.Second,
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			ln, err := net.Listen("tcp", server.Addr)
			if err != nil {
				return fmt.Errorf("listen %s: %w", server.Addr, err)
			}
			go func() {
				if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
					fmt.Fprintln(os.Stderr, "http server stopped:", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return server.Shutdown(ctx)
		},
	})
}
