package runtime

import (
	"context"
	"errors"
	"time"

	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

func StartTelemetry(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	var provider *sdktrace.TracerProvider
	if cfg.OTLPEndpoint != "" {
		exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(cfg.OTLPEndpoint))
		if err != nil {
			return nil, err
		}
		res, err := resource.New(ctx, resource.WithAttributes(
			semconv.ServiceName("px1-"+cfg.Role),
			attribute.String("deployment.environment.name", cfg.Environment),
			attribute.String("service.version", cfg.Release),
		))
		if err != nil {
			return nil, err
		}
		provider = sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter), sdktrace.WithResource(res))
		otel.SetTracerProvider(provider)
	}
	if cfg.SentryDSN != "" {
		if err := sentry.Init(sentry.ClientOptions{
			Dsn: cfg.SentryDSN, Environment: cfg.Environment, Release: cfg.Release,
			EnableTracing: true, TracesSampleRate: 0.1,
		}); err != nil {
			if provider != nil {
				_ = provider.Shutdown(ctx)
			}
			return nil, err
		}
	}
	return func(shutdownCtx context.Context) error {
		var err error
		if cfg.SentryDSN != "" && !sentry.Flush(5*time.Second) {
			err = errors.New("timed out flushing Sentry events")
		}
		if provider != nil {
			err = errors.Join(err, provider.Shutdown(shutdownCtx))
		}
		return err
	}, nil
}
