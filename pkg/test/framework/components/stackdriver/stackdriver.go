//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package stackdriver

import (
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/environment"

	loggingpb "google.golang.org/genproto/googleapis/logging/v2"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
)

// Instance represents a deployed Stackdriver app instance in a Kubernetes cluster.
type Instance interface {
	// Gets the namespace in which stackdriver is deployed.
	GetStackdriverNamespace() string
	ListTimeSeries() ([]*monitoringpb.TimeSeries, error)
	ListLogEntries() ([]*loggingpb.LogEntry, error)
}

type Config struct {
	// Cluster to be used in a multicluster environment
	Cluster resource.Cluster
}

// New returns a new instance of stackdriver.
func New(ctx resource.Context, c Config) (i Instance, err error) {
	err = resource.UnsupportedEnvironment(ctx.Environment())
	ctx.Environment().Case(environment.Kube, func() {
		i, err = newKube(ctx, c)
	})
	return
}

// NewOrFail returns a new Stackdriver instance or fails test.
func NewOrFail(t test.Failer, ctx resource.Context, c Config) Instance {
	t.Helper()
	i, err := New(ctx, c)
	if err != nil {
		t.Fatalf("stackdriver.NewOrFail: %v", err)
	}

	return i
}
