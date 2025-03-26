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

package zipkin

import (
	"fmt"
	"strings"
	"testing"

	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/resource"
)

// Instance represents a zipkin deployment on kube
type Instance interface {
	resource.Resource

	// QueryTraces gets at most number of limit most recent available traces from zipkin.
	// spanName filters that only trace with the given span name will be included.
	QueryTraces(limit int, spanName, annotationQuery, hostDomain string) ([]Trace, error)
}

type Config struct {
	// Cluster to be used in a multicluster environment
	Cluster cluster.Cluster

	// HTTP Address of ingress gateway of the cluster to be used to install zipkin in.
	IngressAddr string
}

// Span represents a single span, which includes span attributes for verification
// TODO(bianpengyuan) consider using zipkin proto api https://github.com/istio/istio/issues/13926
type Span struct {
	SpanID       string
	ParentSpanID string
	ServiceName  string
	Name         string
	ChildSpans   []*Span
}

func (s Span) String() string {
	var sb strings.Builder
	sb.WriteString("SpanID: " + s.SpanID + "\n")
	sb.WriteString("ParentSpanID: " + s.ParentSpanID + "\n")
	sb.WriteString("ServiceName: " + s.ServiceName + "\n")
	sb.WriteString("Name: " + s.Name + "\n")

	for i, childSpan := range s.ChildSpans {
		sb.WriteString(fmt.Sprintf("ChildSpans[%d]: \n", i))
		sb.WriteString(childSpan.String())
	}

	return sb.String()
}

// Trace represents a trace by a collection of spans which all belong to that trace
type Trace struct {
	Spans []Span
}

func (t Trace) String() string {
	var sb strings.Builder
	for i, span := range t.Spans {
		sb.WriteString(fmt.Sprintf("Spans[%d]: \n", i))
		sb.WriteString(span.String())
	}
	return sb.String()
}

// New returns a new instance of zipkin.
func New(ctx resource.Context, c Config) (i Instance, err error) {
	return newKube(ctx, c)
}

// NewOrFail returns a new zipkin instance or fails test.
func NewOrFail(t *testing.T, ctx resource.Context, c Config) Instance {
	t.Helper()
	i, err := New(ctx, c)
	if err != nil {
		t.Fatalf("zipkin.NewOrFail: %v", err)
	}

	return i
}
