package main

import (
	"context"
	"errors"
	"github.com/kryxen/cloud-robot/internal/logbuf"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kryxen/cloud-robot/internal/api"
	"github.com/kryxen/cloud-robot/internal/boxes"
	"github.com/kryxen/cloud-robot/internal/cloud"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Keep recent logs in memory for the admin console as well as stderr.
	logs := logbuf.New(5000)
	slog.SetDefault(slog.New(logbuf.NewHandler(slog.NewTextHandler(os.Stderr, nil), logs)))
	store, err := cloud.New(ctx, cloud.ConfigFromEnv())
	if err != nil {
		slog.Error("configure cloud store", "error", err)
		os.Exit(1)
	}
	if err := ensureCloud(ctx, store); err != nil {
		slog.Error("initialize cloud resources", "error", err)
		os.Exit(1)
	}

	provisioner := boxes.NewClient(envOr("BOX_PROVISIONER_URL", "http://localhost:8090"), os.Getenv("PROVISIONER_TOKEN"))
	app := api.NewServer(store, provisioner)
	app.SetLogs(logs)
	app.RecoverMatches(ctx)
	go app.RunWorker(ctx)
	address := os.Getenv("API_ADDR")
	if address == "" {
		address = ":8080"
	}
	httpServer := &http.Server{Addr: address, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdown)
	}()
	slog.Info("robot arena API ready", "address", address)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("serve API", "error", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func ensureCloud(ctx context.Context, store *cloud.Store) error {
	var err error
	for attempt := 1; attempt <= 30; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err = store.Ensure(attemptCtx)
		cancel()
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return err
}
