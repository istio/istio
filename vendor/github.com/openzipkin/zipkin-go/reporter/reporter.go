// Copyright 2019 The OpenZipkin Authors
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

/*
Package reporter holds the Reporter interface which is used by the Zipkin
Tracer to send finished spans.

Subpackages of package reporter contain officially supported standard
reporter implementations.
*/
package reporter

import "github.com/openzipkin/zipkin-go/model"

// Reporter interface can be used to provide the Zipkin Tracer with custom
// implementations to publish Zipkin Span data.
type Reporter interface {
	Send(model.SpanModel) // Send Span data to the reporter
	Close() error         // Close the reporter
}

type noopReporter struct{}

func (r *noopReporter) Send(model.SpanModel) {}
func (r *noopReporter) Close() error         { return nil }

// NewNoopReporter returns a no-op Reporter implementation.
func NewNoopReporter() Reporter {
	return &noopReporter{}
}
