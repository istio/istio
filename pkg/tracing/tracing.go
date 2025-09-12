// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tracing

import (
	"context"
	"fmt"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	traceapi "go.opentelemetry.io/otel/trace"

	"istio.io/istio/pkg/log"
)

func Enabled() bool {
	return os.Getenv("OTEL_TRACES_EXPORTER") == "otlp" || os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" || os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != ""
}

// Inspired by https://github.com/moby/buildkit/blob/d9a6afdf089a7c4b97cac704a60ad70c21086f12/util/tracing/detect/otlp.go#L18
// Most OTLP aspects are configured by Environment variables, but the actual client we use needs to be explicitly defined.
// So we can parse the env vars ourselves and set up the correct client.
func newExporter() (trace.SpanExporter, error) {
	if !Enabled() {
		return nil, nil
	}

	proto := os.Getenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL")
	if proto == "" {
		proto = os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")
	}
	if proto == "" {
		proto = "grpc"
	}

	var c otlptrace.Client

	switch proto {
	case "grpc":
		c = otlptracegrpc.NewClient()
	case "http/protobuf":
		c = otlptracehttp.NewClient()
	// case "http/json": // unsupported by library
	default:
		return nil, fmt.Errorf("unsupported otlp protocol %v", proto)
	}
	return otlptrace.New(context.Background(), c)
}

// newResource returns a resource describing this application.
func newResource(name string) *resource.Resource {
	r, _ := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(name),
			// TODO: consider adding attributes here.
			// Component, hostname, version are all possibly useful
		),
	)
	return r
}

const (
	// instrumentationScope is the name of OpenTelemetry instrumentation scope
	instrumentationScope = "istio.io/istio"
)

func tracer() traceapi.Tracer {
	return otel.Tracer(instrumentationScope)
}

func newPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}

// Initialize starts the tracing provider. This must be called before any traces are created or traces will be discarded.
// Returned is a shutdown function that should be called to ensure graceful shutdown.
func Initialize(name string) (func(), error) {
	exp, err := newExporter()
	if err != nil {
		return nil, err
	}
	prop := newPropagator()
	otel.SetTextMapPropagator(prop)
	tp := trace.NewTracerProvider(
		trace.WithBatcher(exp),
		trace.WithResource(newResource(name)),
	)
	otel.SetTracerProvider(tp)
	return func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Warnf("failed to shutdown tracing: %v", err)
		}
	}, nil
}

// InitializeFullBinary is a specialized variant of Initialize for uses with binaries who are tracing their entire execution
// as a single trace span. Not for use with long running services.
func InitializeFullBinary(name string, rootSpan string) (context.Context, func(), error) {
	shutdown, err := Initialize(name)
	if err != nil {
		return nil, nil, err
	}
	ctx := context.Background()
	if p, f := os.LookupEnv("TRACEPARENT"); f {
		pg := propagation.TraceContext{}
		ctx = pg.Extract(ctx, propagation.MapCarrier{"traceparent": p})
	}
	globalCtx, globalSpan := Start(ctx, rootSpan)
	return globalCtx, func() {
		globalSpan.End()
		shutdown()
	}, nil
}

func Start(ctx context.Context, span string) (context.Context, traceapi.Span) {
	return tracer().Start(ctx, span)
}
