package tracing

import (
	"context"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	traceapi "go.opentelemetry.io/otel/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"

	"istio.io/istio/pkg/log"
)

func newExporter() (trace.SpanExporter, error) {
	http := otlptracehttp.NewClient()
	_ = http
	grpc := otlptracegrpc.NewClient()
	return otlptrace.New(context.Background(), grpc)
}

// newResource returns a resource describing this application.
func newResource() *resource.Resource {
	r, _ := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName("istio"),
			semconv.ServiceVersion("v0.1.0"),
		),
	)
	return r
}

var tracer traceapi.Tracer

func Initialize(span string) (context.Context, func(), error) {
	exp, err := newExporter()
	if err != nil {
		return nil, nil, err
	}
	tp := trace.NewTracerProvider(
		trace.WithBatcher(exp),
		trace.WithResource(newResource()),
	)
	otel.SetTracerProvider(tp)
	ctx := context.Background()
	tracer = otel.Tracer("istio")
	if p, f := os.LookupEnv("TRACEPARENT"); f {
		pg := propagation.TraceContext{}
		ctx = pg.Extract(ctx, propagation.MapCarrier{"traceparent": p})
	}
	globalCtx, globalSpan := tracer.Start(ctx, span)
	return globalCtx, func() {
		globalSpan.End()
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Warnf("failed to shutdown tracing: %v", err)
		}
	}, nil
}

func Start(ctx context.Context, span string) (context.Context, traceapi.Span) {
	return tracer.Start(ctx, span)
}

func Start1(ctx context.Context, span string) func() {
	_, s := tracer.Start(ctx, span)
	return func() {
		s.End()
	}
}