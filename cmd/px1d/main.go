package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/http"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	hostedruntime "px1/internal/hosted/runtime"
)

func main() {
	role := flag.String("role", "", "hosted process role")
	flag.Parse()
	cfg, err := hostedruntime.LoadConfig(*role)
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	shutdownTelemetry, err := hostedruntime.StartTelemetry(ctx, cfg)
	if err != nil {
		slog.Error("telemetry startup failed", "error", err)
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := shutdownTelemetry(shutdownCtx); err != nil {
			slog.Error("telemetry shutdown failed", "error", err)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	sentryMiddleware := sentryhttp.New(sentryhttp.Options{Repanic: true})
	handler := otelhttp.NewHandler(sentryMiddleware.Handle(mux), "px1.http")
	server := &http.Server{Addr: cfg.Address, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	slog.Info("hosted process ready", "role", cfg.Role, "environment", cfg.Environment, "release", cfg.Release)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		sentry.CaptureException(err)
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
