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

// Package tracing provides the canonical tracing functionality used by Go-based
// Istio components.
//
// The package provides direct integration with the Cobra command-line processor which makes it
// easy to build programs that use a consistent interface for tracing. Here's an example
// of a simple Cobra-based program using this tracing package:
//
//		func main() {
//			// get the default tracing options
//			options := tracing.DefaultOptions()
//
//			rootCmd := &cobra.Command{
//				Run: func(cmd *cobra.Command, args []string) {
//
//					// configure the tracing system
//					if _, err := tracing.Configure(options); err != nil {
//                      // print an error and quit
//                  }
//
//					// Generate a trace
//					ot.StartSpan(...)
//				},
//			}
//
//			// add tracing-specific flags to the cobra command
//			options.AttachCobraFlags(rootCmd)
//			rootCmd.SetArgs(os.Args[1:])
//			rootCmd.Execute()
//		}
//
// The point of this package is to configure the global OpenTracing tracer
// for the process as read from ot.GlobalTracer and used by ot.StartSpan.
package tracing

import (
	"fmt"
	"io"
	"time"

	ot "github.com/opentracing/opentracing-go"
	jaeger "github.com/uber/jaeger-client-go"
	"github.com/uber/jaeger-client-go/transport"
	"github.com/uber/jaeger-client-go/transport/zipkin"
	zk "github.com/uber/jaeger-client-go/zipkin"
	"go.uber.org/zap"

	"istio.io/pkg/log"
)

/* TODO:
 *   - Support only tracing when trace context information is already present (mixer)
 *   - Support tracing for some percentage of requests (pilot)
 */

type holder struct {
	closer io.Closer
	tracer ot.Tracer
}

var (
	httpTimeout = 5 * time.Second
	poolSpans   = jaeger.TracerOptions.PoolSpans(false)
	logger      = spanLogger{}
)

// indirection for testing
type newZipkin func(url string, options ...zipkin.HTTPOption) (*zipkin.HTTPTransport, error)

// Configure initializes Istio's tracing subsystem.
//
// You typically call this once at process startup.
// Once this call returns, the tracing system is ready to accept data.
func Configure(serviceName string, options *Options) (io.Closer, error) {
	return configure(serviceName, options, zipkin.NewHTTPTransport)
}

func configure(serviceName string, options *Options, nz newZipkin) (io.Closer, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}

	reporters := make([]jaeger.Reporter, 0, 3)

	sampler, err := jaeger.NewProbabilisticSampler(options.SamplingRate)
	if err != nil {
		return nil, fmt.Errorf("could not build trace sampler: %v", err)
	}

	if options.ZipkinURL != "" {
		trans, err := nz(options.ZipkinURL, zipkin.HTTPLogger(logger), zipkin.HTTPTimeout(httpTimeout))
		if err != nil {
			return nil, fmt.Errorf("could not build zipkin reporter: %v", err)
		}
		reporters = append(reporters, jaeger.NewRemoteReporter(trans))
	}

	if options.JaegerURL != "" {
		reporters = append(reporters, jaeger.NewRemoteReporter(transport.NewHTTPTransport(options.JaegerURL, transport.HTTPTimeout(httpTimeout))))
	}

	if options.LogTraceSpans {
		reporters = append(reporters, logger)
	}

	var rep jaeger.Reporter
	if len(reporters) == 0 {
		// leave the default NoopTracer in place since there's no place for tracing to go...
		return holder{}, nil
	} else if len(reporters) == 1 {
		rep = reporters[0]
	} else {
		rep = jaeger.NewCompositeReporter(reporters...)
	}

	var tracer ot.Tracer
	var closer io.Closer

	if options.ZipkinURL != "" {
		zipkinPropagator := zk.NewZipkinB3HTTPHeaderPropagator()
		injector := jaeger.TracerOptions.Injector(ot.HTTPHeaders, zipkinPropagator)
		extractor := jaeger.TracerOptions.Extractor(ot.HTTPHeaders, zipkinPropagator)
		tracer, closer = jaeger.NewTracer(serviceName, sampler, rep, poolSpans, injector, extractor, jaeger.TracerOptions.Gen128Bit(true))
	} else {
		tracer, closer = jaeger.NewTracer(serviceName, sampler, rep, poolSpans, jaeger.TracerOptions.Gen128Bit(true))
	}

	// NOTE: global side effect!
	ot.SetGlobalTracer(tracer)

	return holder{
		closer: closer,
		tracer: tracer,
	}, nil
}

func (h holder) Close() error {
	if ot.GlobalTracer() == h.tracer {
		ot.SetGlobalTracer(ot.NoopTracer{})
	}

	var err error
	if h.closer != nil {
		err = h.closer.Close()
	}

	return err
}

type spanLogger struct{}

// Report implements the Report() method of jaeger.Reporter
func (spanLogger) Report(span *jaeger.Span) {
	log.Info("Reporting span",
		zap.String("operation", span.OperationName()),
		zap.String("span", span.String()))
}

// Close implements the Close() method of jaeger.Reporter.
func (spanLogger) Close() {}

// Error implements the Error() method of log.Logger.
func (spanLogger) Error(msg string) {
	log.Error(msg)
}

// Infof implements the Infof() method of log.Logger.
func (spanLogger) Infof(msg string, args ...interface{}) {
	log.Infof(msg, args...)
}
