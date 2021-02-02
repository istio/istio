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

package opentelemetry

import (
	"net"
	"testing"

	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/resource"
)

// Config represents the configuration for setting up an opentelemetry
// collector.
type Config struct {
	// Cluster to be used in a multicluster environment
	Cluster cluster.Cluster

	// HTTP Address of ingress gateway of the cluster to be used to install open telemetry collector in.
	IngressAddr net.TCPAddr
}

// Instance represents a opencensus collector deployment on kubernetes.
type Instance interface {
	resource.Resource
}

// New creates and returns a new instance of otel.
func New(ctx resource.Context, c Config) (Instance, error) {
	return newCollector(ctx, c)
}

// NewOrFail returns a new otel instance or fails the test.
func NewOrFail(t *testing.T, ctx resource.Context, c Config) Instance {
	t.Helper()
	i, err := New(ctx, c)
	if err != nil {
		t.Fatalf("opentelemetry.NewOrFail: %v", err)
	}
	return i
}
