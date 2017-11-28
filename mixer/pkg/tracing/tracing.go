// Copyright 2017 Istio Authors
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

// Package tracing provides utilities for creating a tracer to use within Mixer
// commands (mixc, mixs). It provides basic methods for configuring export options
// for generated traces.
package tracing

import (
	"fmt"
	"io"
	"time"

	"github.com/golang/glog"
	opentracing "github.com/opentracing/opentracing-go"
	jaeger "github.com/uber/jaeger-client-go"
	"github.com/uber/jaeger-client-go/log"
	"github.com/uber/jaeger-client-go/transport"
	"github.com/uber/jaeger-client-go/transport/zipkin"
)

var (
	httpTimeout = 5 * time.Second
	sampler     = jaeger.NewConstSampler(true)
	poolSpans   = jaeger.TracerOptions.PoolSpans(true)
)

type tracingOpts struct {
	serviceName string
	jaegerURL   string
	zipkinURL   string
	logger      log.Logger
}

// Option is a function that configures Mixer self-tracing options.
type Option func(*tracingOpts)

// NewTracer returns a new tracer for use with Mixer, based on the supplied options.
// The closer returned is used to ensure flushing of all existing span data during
// Mixer shutdown.
func NewTracer(serviceName string, options ...Option) (opentracing.Tracer, io.Closer, error) {
	opts := tracingOpts{serviceName: serviceName}
	for _, opt := range options {
		opt(&opts)
	}
	return newJaegerTracer(opts)
}

// WithZipkinCollector configures Mixer tracing to export span data to a zipkin
// collector at the supplied URL.
func WithZipkinCollector(addr string) Option {
	return func(opts *tracingOpts) {
		opts.zipkinURL = addr
	}
}

// WithJaegerHTTPCollector configures Mixer tracing to export span data to a
// jaeger HTTP collector at the supplied URL using HTTP Basic Authentication.
func WithJaegerHTTPCollector(addr string) Option {
	return func(opts *tracingOpts) {
		opts.jaegerURL = addr
	}
}

// WithLogger configures Mixer tracing to log span data to the console.
func WithLogger() Option {
	return withLogger(gLogger)
}

func withLogger(logger log.Logger) Option {
	return func(opts *tracingOpts) {
		opts.logger = logger
	}
}

func newJaegerTracer(opts tracingOpts) (opentracing.Tracer, io.Closer, error) {
	reporters := make([]jaeger.Reporter, 0, 3)
	if len(opts.zipkinURL) > 0 {
		rep, err := newZipkinReporter(opts.zipkinURL)
		if err != nil {
			return nil, nil, err
		}
		reporters = append(reporters, rep)
	}
	if len(opts.jaegerURL) > 0 {
		reporters = append(reporters, newJaegerReporter(opts.jaegerURL))
	}
	if opts.logger != nil {
		reporters = append(reporters, newLoggingReporter(opts.logger))
	}
	rep := jaeger.NewCompositeReporter(reporters...)
	tracer, closer := jaeger.NewTracer(opts.serviceName, sampler, rep, poolSpans)
	return tracer, closer, nil
}

func newZipkinReporter(addr string) (jaeger.Reporter, error) {
	opts := []zipkin.HTTPOption{zipkin.HTTPLogger(gLogger), zipkin.HTTPTimeout(httpTimeout)}
	trans, err := zipkin.NewHTTPTransport(addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("could not build zipkin reporter: %v", err)
	}
	return jaeger.NewRemoteReporter(trans), nil
}

func newJaegerReporter(addr string) jaeger.Reporter {
	opts := []transport.HTTPOption{transport.HTTPTimeout(httpTimeout)}
	return jaeger.NewRemoteReporter(transport.NewHTTPTransport(addr, opts...))
}

type logReporter struct {
	logger log.Logger
}

func newLoggingReporter(logger log.Logger) jaeger.Reporter {
	return &logReporter{logger}
}

// Report implements Report() method of Reporter by logging the span to the logger.
func (l *logReporter) Report(span *jaeger.Span) {
	l.logger.Infof("Reporting span for operation '%s': %+v", span.OperationName(), span)
}

// Close implements Close() method of jaeger.Reporter.
func (l *logReporter) Close() {}

var gLogger = &glogLogger{}

type glogLogger struct{}

// Error implements the Error() method of log.Logger.
func (g *glogLogger) Error(msg string) {
	glog.Error(msg)
}

// Infof implements the Infof() method of log.Logger.
func (g *glogLogger) Infof(msg string, args ...interface{}) {
	glog.Infof(msg, args...)
}
