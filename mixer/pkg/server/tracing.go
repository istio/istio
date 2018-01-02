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

package server

import (
	"fmt"
	"io"

	"github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	ot "github.com/opentracing/opentracing-go"
	"google.golang.org/grpc"

	"istio.io/istio/mixer/pkg/tracing"
)

type mixerTracer struct {
	closer io.Closer
	tracer ot.Tracer
}

func startTracer(zipkinURL string, jaegerURL string, logTraceSpans bool) (*mixerTracer, grpc.UnaryServerInterceptor, error) {
	var closer io.Closer
	var tracer ot.Tracer
	var err error
	var interceptor grpc.UnaryServerInterceptor

	opts := make([]tracing.Option, 0, 3)
	if len(zipkinURL) > 0 {
		opts = append(opts, tracing.WithZipkinCollector(zipkinURL))
	}
	if len(jaegerURL) > 0 {
		opts = append(opts, tracing.WithJaegerHTTPCollector(jaegerURL))
	}
	if logTraceSpans {
		opts = append(opts, tracing.WithConsoleLogging())
	}
	tracer, closer, err = tracing.NewTracer("istio-mixer", opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("could not create tracer: %v", err)
	}

	// NOTE: global side effect!
	ot.SetGlobalTracer(tracer)
	interceptor = otgrpc.OpenTracingServerInterceptor(tracer)

	return &mixerTracer{
		closer: closer,
		tracer: tracer,
	}, interceptor, nil
}

func (mt *mixerTracer) Close() error {
	if ot.GlobalTracer() == mt.tracer {
		ot.SetGlobalTracer(nil)
	}

	if mt.closer != nil {
		mt.closer.Close()
	}

	return nil
}
