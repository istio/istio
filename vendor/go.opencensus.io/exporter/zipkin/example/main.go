// Copyright 2018, OpenCensus Authors
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

package main

import (
	"context"
	"log"
	"time"

	openzipkin "github.com/openzipkin/zipkin-go"
	"github.com/openzipkin/zipkin-go/reporter/http"
	"go.opencensus.io/exporter/zipkin"
	"go.opencensus.io/trace"
)

func main() {
	// The localEndpoint stores the name and address of the local service
	localEndpoint, err := openzipkin.NewEndpoint("example-server", "192.168.1.5:5454")
	if err != nil {
		log.Println(err)
	}

	// The Zipkin reporter takes collected spans from the app and reports them to the backend
	// http://localhost:9411/api/v2/spans is the default for the Zipkin Span v2
	reporter := http.NewReporter("http://localhost:9411/api/v2/spans")
	defer reporter.Close()

	// The OpenCensus exporter wraps the Zipkin reporter
	exporter := zipkin.NewExporter(reporter, localEndpoint)
	trace.RegisterExporter(exporter)

	// For example purposes, sample every trace. In a production application, you should
	// configure this to a trace.ProbabilitySampler set at the desired
	// probability.
	trace.ApplyConfig(trace.Config{DefaultSampler: trace.AlwaysSample()})

	ctx := context.Background()
	foo(ctx)
}

func foo(ctx context.Context) {
	// Name the current span "/foo"
	ctx, span := trace.StartSpan(ctx, "/foo")
	defer span.End()

	// Foo calls bar and baz
	bar(ctx)
	baz(ctx)
}

func bar(ctx context.Context) {
	ctx, span := trace.StartSpan(ctx, "/bar")
	defer span.End()

	// Do bar
	time.Sleep(2 * time.Millisecond)
}

func baz(ctx context.Context) {
	ctx, span := trace.StartSpan(ctx, "/baz")
	defer span.End()

	// Do baz
	time.Sleep(4 * time.Millisecond)
}
