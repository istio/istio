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

	opentracing "github.com/opentracing/opentracing-go"
	jaeger "github.com/uber/jaeger-client-go"
	"github.com/uber/jaeger-client-go/transport"
	"github.com/uber/jaeger-client-go/transport/zipkin"

	"go.uber.org/zap"
	ilog "istio.io/istio/pkg/log"
)

var (
	httpTimeout = 5 * time.Second
	sampler     = jaeger.NewConstSampler(true)
	poolSpans   = jaeger.TracerOptions.PoolSpans(false)
)

type tracingOpts struct {
	serviceName     string
	jaegerURL       string
	zipkinURL       string
	consoleReporter jaeger.Reporter
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

// WithConsoleLogging configures Mixer tracing to log span data to the console.
func WithConsoleLogging() Option {
	return withReporter(conrep)
}

func withReporter(reporter jaeger.Reporter) Option {
	return func(opts *tracingOpts) {
		opts.consoleReporter = reporter
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
		reporters = append(reporters, jaeger.NewRemoteReporter(transport.NewHTTPTransport(opts.jaegerURL, transport.HTTPTimeout(httpTimeout))))
	}

	if opts.consoleReporter != nil {
		reporters = append(reporters, opts.consoleReporter)
	}

	var rep jaeger.Reporter
	if len(reporters) == 1 {
		rep = reporters[0]
	} else {
		rep = jaeger.NewCompositeReporter(reporters...)
	}

	tracer, closer := jaeger.NewTracer(opts.serviceName, sampler, rep, poolSpans)
	return tracer, closer, nil
}

func newZipkinReporter(addr string) (jaeger.Reporter, error) {
	trans, err := zipkin.NewHTTPTransport(addr, zipkin.HTTPLogger(conrep), zipkin.HTTPTimeout(httpTimeout))
	if err != nil {
		return nil, fmt.Errorf("could not build zipkin reporter: %v", err)
	}
	return jaeger.NewRemoteReporter(trans), nil
}

type consoleReporter struct{}

var conrep = consoleReporter{}

// Report implements the Report() method of jaeger.Reporter
func (consoleReporter) Report(span *jaeger.Span) {
	ilog.Info("Reporting span",
		zap.String("operation", span.OperationName()),
		zap.String("span", span.String()))
}

// Close implements the Close() method of jaeger.Reporter.
func (consoleReporter) Close() {}

// Error implements the Error() method of log.Logger.
func (consoleReporter) Error(msg string) {
	ilog.Error(msg)
}

// Infof implements the Infof() method of log.Logger.
func (consoleReporter) Infof(msg string, args ...interface{}) {
	ilog.Infof(msg, args...)
}
