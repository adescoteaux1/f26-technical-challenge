// Command controltower runs the Generate Cloud Scheduler Control Tower server.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/adescoteaux1/generate-control-tower/internal/api"
	"github.com/adescoteaux1/generate-control-tower/internal/config"
	"github.com/adescoteaux1/generate-control-tower/internal/github"
	"github.com/adescoteaux1/generate-control-tower/internal/store"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("config error", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := store.NewPostgresStore(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	server := &api.Server{Store: db, Log: logger}
	if cfg.GitHubToken != "" && cfg.GitHubOrg != "" {
		server.GitHub = github.NewClient(cfg.GitHubToken, cfg.GitHubOrg)
	} else {
		logger.Warn("GITHUB_TOKEN/GITHUB_ORG not set — POST /apply will report itself unavailable")
	}

	httpServer := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: api.NewRouter(server),
	}

	go func() {
		logger.Info("control tower server listening", "port", cfg.Port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	logger.Info("shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = httpServer.Shutdown(shutdownCtx)
}
