package main

import (
	"context"

	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.10.0"
	"google.golang.org/grpc"
)

type ShutdownFn func(context.Context) error

func InitMeterProvider(ctx context.Context, name string, reader metric.Reader) ShutdownFn {
	res := telemetryResource(ctx, name)
	meterProvider := metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(reader))
	otel.SetMeterProvider(meterProvider)
	return meterProvider.Shutdown
}

func InitTraceProvider(ctx context.Context, name string, spanExporter trace.SpanExporter) ShutdownFn {
	res := telemetryResource(ctx, name)
	bsp := trace.NewBatchSpanProcessor(spanExporter)
	tracerProvider := trace.NewTracerProvider(
		trace.WithSampler(trace.TraceIDRatioBased(1)),
		trace.WithResource(res),
		trace.WithSpanProcessor(bsp),
	)
	otel.SetTracerProvider(tracerProvider)
	return tracerProvider.Shutdown
}

func telemetryResource(ctx context.Context, serviceName string) *resource.Resource {
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithAttributes(
			// the service name used to display traces in backend
			semconv.ServiceNameKey.String(serviceName),
		),
	)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to initialize telemetry resource")
	}
	return res
}

func NewPrometheusExporter(ctx context.Context) *prometheus.Exporter {
	exporter, err := prometheus.New()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialize prometheus exporter")
	}
	return exporter
}

func NewOTLPTraceExporter(ctx context.Context, otlpEndpoint string) *otlptrace.Exporter {
	traceClient := otlptracegrpc.NewClient(
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithEndpoint(otlpEndpoint),
		otlptracegrpc.WithDialOption(grpc.WithBlock()))
	traceExp, err := otlptrace.New(ctx, traceClient)
	if err != nil {
		log.Fatal().Err(err).Msgf("Failed to create the collector trace exporter")
	}
	return traceExp
}
