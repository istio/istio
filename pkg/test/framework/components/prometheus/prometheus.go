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

package prometheus

import (
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	prom "github.com/prometheus/common/model"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/resource"
)

type Instance interface {
	resource.Resource

	// API Returns the core Prometheus APIs.
	API() v1.API
	APIForCluster(cluster cluster.Cluster) v1.API

	// Query Run the provided query against the given cluster
	Query(cluster cluster.Cluster, query Query) (prom.Value, error)

	// QuerySum is a help around Query to compute the sum
	QuerySum(cluster cluster.Cluster, query Query) (float64, error)
}

type Config struct {
	// If true, connect to an existing prometheus rather than creating a new one
	SkipDeploy bool
}

// New returns a new instance of prometheus.
func New(ctx resource.Context, c Config) (i Instance, err error) {
	return newKube(ctx, c)
}

// NewOrFail returns a new Prometheus instance or fails test.
func NewOrFail(t test.Failer, ctx resource.Context, c Config) Instance {
	t.Helper()
	i, err := New(ctx, c)
	if err != nil {
		t.Fatalf("prometheus.NewOrFail: %v", err)
	}

	return i
}
