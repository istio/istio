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
	"istio.io/istio/pkg/test/framework/resource"
)

type Instance interface {
	resource.Resource

	// API Returns the core Prometheus APIs.
	API() v1.API

	// WaitForQuiesce runs the provided query periodically until the result gets stable.
	WaitForQuiesce(fmt string, args ...interface{}) (prom.Value, error)
	WaitForQuiesceOrFail(t test.Failer, fmt string, args ...interface{}) prom.Value

	// WaitForOneOrMore runs the provided query and waits until one (or more for vector) values are available.
	WaitForOneOrMore(fmt string, args ...interface{}) (prom.Value, error)
	WaitForOneOrMoreOrFail(t test.Failer, fmt string, args ...interface{}) prom.Value

	// Sum all the samples that has the given labels in the given vector value.
	Sum(val prom.Value, labels map[string]string) (float64, error)
	SumOrFail(t test.Failer, val prom.Value, labels map[string]string) float64
}

type Config struct {
	// Cluster to be used in a multicluster environment
	Cluster resource.Cluster

	// If true, connect to an existing prometheus rather than creating a new one
	SkipDeploy bool
}

// New returns a new instance of echo.
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
