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

package echo

import (
	"context"
	"sort"
	"strings"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/framework/components/cluster"
)

// WorkloadContainer is container for a number of Workload objects.
type WorkloadContainer interface {
	// Workloads retrieves the list of all deployed workloads for this Echo service.
	// Guarantees at least one workload, if error == nil.
	Workloads() (Workloads, error)
	WorkloadsOrFail(t test.Failer) Workloads
	MustWorkloads() Workloads

	// Clusters where the workloads are deployed.
	Clusters() cluster.Clusters
}

// Workload provides an interface for a single deployed echo server.
type Workload interface {
	// PodName gets the original pod name for the workload.
	PodName() string

	// Address returns the network address of the endpoint.
	Address() string

	// Sidecar if one was specified.
	Sidecar() Sidecar

	// Cluster where this Workload resides.
	Cluster() cluster.Cluster

	// ForwardEcho executes specific call from this workload.
	// TODO(nmittler): Instead of this, we should just make Workload implement Caller.
	ForwardEcho(context.Context, *proto.ForwardEchoRequest) (echo.Responses, error)

	// Logs returns the logs for the app container
	Logs() (string, error)
	LogsOrFail(t test.Failer) string
}

type Workloads []Workload

func (ws Workloads) Len() int {
	return len(ws)
}

// Addresses returns the list of addresses for all workloads.
func (ws Workloads) Addresses() []string {
	out := make([]string, 0, len(ws))
	for _, w := range ws {
		out = append(out, w.Address())
	}
	return out
}

func (ws Workloads) Clusters() cluster.Clusters {
	clusters := make(map[string]cluster.Cluster)
	for _, w := range ws {
		if c := w.Cluster(); c != nil {
			clusters[c.Name()] = c
		}
	}
	out := make(cluster.Clusters, 0, len(clusters))
	for _, c := range clusters {
		out = append(out, c)
	}

	// Sort the clusters by name.
	sort.SliceStable(out, func(i, j int) bool {
		return strings.Compare(out[i].Name(), out[j].Name()) < 0
	})

	return out
}
