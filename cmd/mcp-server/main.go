package main

import (
	"context"
	"log/slog"
	stdhttp "net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Stoganet/mcp/internal/config"
	"github.com/Stoganet/mcp/internal/server"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg := config.LoadFromEnv()

	httpSrv := &stdhttp.Server{
		Addr:              cfg.ListenAddr,
		Handler:           server.NewHTTPHandler(cfg),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		logger.Info("mcp-server listening", "addr", cfg.ListenAddr, "name", cfg.ServerName, "version", cfg.Version)
		if err := httpSrv.ListenAndServe(); err != nil && err != stdhttp.ErrServerClosed {
			logger.Error("listen", "err", err)
			cancel()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown", "err", err)
	}
}
