package telemetry

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.uber.org/zap"

	"temporal-utility/config"
)

type SDK struct {
	tracerProvider *trace.TracerProvider
	meterProvider  *metric.MeterProvider
}

func Setup(ctx context.Context, cfg *config.OTELConfig, logger *zap.Logger) (*SDK, error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
		),
	)
	if err != nil {
		return nil, err
	}

	tp, err := newTracerProvider(ctx, cfg, res, logger)
	if err != nil {
		return nil, err
	}

	mp, err := newMeterProvider(ctx, cfg, res, logger)
	if err != nil {
		return nil, err
	}

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return &SDK{tracerProvider: tp, meterProvider: mp}, nil
}

func (s *SDK) Shutdown(ctx context.Context) error {
	return errors.Join(
		s.tracerProvider.Shutdown(ctx),
		s.meterProvider.Shutdown(ctx),
	)
}

func newTracerProvider(ctx context.Context, cfg *config.OTELConfig, res *resource.Resource, logger *zap.Logger) (*trace.TracerProvider, error) {
	var exporter trace.SpanExporter
	var err error

	if cfg.ExporterEndpoint != "" {
		exporter, err = otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(cfg.ExporterEndpoint),
			otlptracegrpc.WithInsecure(),
		)
		logger.Info("OTEL trace exporter: OTLP gRPC", zap.String("endpoint", cfg.ExporterEndpoint))
	} else {
		exporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
		logger.Info("OTEL trace exporter: stdout")
	}
	if err != nil {
		return nil, err
	}

	return trace.NewTracerProvider(
		trace.WithResource(res),
		trace.WithBatcher(exporter, trace.WithBatchTimeout(5*time.Second)),
		trace.WithSampler(trace.AlwaysSample()),
	), nil
}

func newMeterProvider(ctx context.Context, cfg *config.OTELConfig, res *resource.Resource, logger *zap.Logger) (*metric.MeterProvider, error) {
	var exporter metric.Exporter
	var err error

	if cfg.ExporterEndpoint != "" {
		exporter, err = otlpmetricgrpc.New(ctx,
			otlpmetricgrpc.WithEndpoint(cfg.ExporterEndpoint),
			otlpmetricgrpc.WithInsecure(),
		)
		logger.Info("OTEL metric exporter: OTLP gRPC", zap.String("endpoint", cfg.ExporterEndpoint))
	} else {
		exporter, err = stdoutmetric.New()
		logger.Info("OTEL metric exporter: stdout")
	}
	if err != nil {
		return nil, err
	}

	return metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(metric.NewPeriodicReader(exporter, metric.WithInterval(15*time.Second))),
	), nil
}
