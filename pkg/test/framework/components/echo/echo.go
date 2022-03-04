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
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/framework/components/cluster"
)

// Builder for a group of collaborating Echo Instances. Once built, all Instances in the
// group:
//
//     1. Are ready to receive traffic, and
//     2. Can call every other Instance in the group (i.e. have received Envoy config
//        from Pilot).
//
// If a test needs to verify that one Instance is NOT reachable from another, there are
// a couple of options:
//
//     1. Build a group while all Instances ARE reachable. Then apply a policy
//        disallowing the communication.
//     2. Build the source and destination Instances in separate groups and then
//        call `source.WaitUntilCallable(destination)`.
type Builder interface {
	// With adds a new Echo configuration to the Builder. Once built, the instance
	// pointer will be updated to point at the new Instance.
	With(i *Instance, cfg Config) Builder

	// WithConfig mimics the behavior of With, but does not allow passing a reference
	// and returns an echoboot builder rather than a generic echo builder.
	// TODO rename this to With, and the old method to WithInstance
	WithConfig(cfg Config) Builder

	// WithClusters will cause subsequent With or WithConfig calls to be applied to the given clusters.
	WithClusters(...cluster.Cluster) Builder

	// Build and initialize all Echo Instances. Upon returning, the Instance pointers
	// are assigned and all Instances are ready to communicate with each other.
	Build() (Instances, error)
	BuildOrFail(t test.Failer) Instances
}

type Caller interface {
	// Call from this Instance to a target Instance.
	Call(options CallOptions) (echo.Responses, error)
	CallOrFail(t test.Failer, options CallOptions) echo.Responses
}

type Callers []Caller

// Instances returns an Instances if all callers are Instance, otherwise returns nil.
func (c Callers) Instances() Instances {
	var out Instances
	for _, caller := range c {
		c, ok := caller.(Instance)
		if !ok {
			return nil
		}
		out = append(out, c)
	}
	return out
}
