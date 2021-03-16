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

package echotest

import "istio.io/istio/pkg/test/framework/components/echo"

type (
	simpleFilter      func(echo.Instances) echo.Instances
	combinationFilter func(from echo.Instance, to echo.Instances) echo.Instances
)

// From applies each of the filter funcitons in order to allow removing workloads from the set of clients.
func (t *T) From(filters ...simpleFilter) *T {
	for _, filter := range filters {
		t.sources = filter(t.sources)
	}
	return t
}

// To applies each of the filter funcitons in order to allow removing workloads from the set of destinations.
func (t *T) To(filters ...simpleFilter) *T {
	for _, filter := range filters {
		t.destinations = filter(t.destinations)
	}
	return t
}

// ConditionallyTo appends the given filters which are executed per test. Destination filters may need
// to change behavior based on the client. For example, naked services can't be reached cross-network, so
// the client matters.
func (t *T) ConditionallyTo(filters ...combinationFilter) *T {
	t.destinationFilters = append(t.destinationFilters, filters...)
	return t
}

func (t *T) applyCombinationFilters(from echo.Instance, to echo.Instances) echo.Instances {
	for _, filter := range t.destinationFilters {
		to = filter(from, to)
	}
	return to
}

// SingleSimplePodBasedService finds the first Pod deployment that has a sidecar and doesn't use a headless service and removes all
// other "regular" pods that aren't part of the same Service. Pods that are part of the same Service but are in a
// different cluster or revision will still be included.
var SingleSimplePodBasedService simpleFilter = func(instances echo.Instances) echo.Instances {
	return oneRegularPod(instances)
}

// NoExternalServices filters out external services which are based on
var NoExternalServices simpleFilter = func(instances echo.Instances) echo.Instances {
	return instances.Match(echo.External().Negate())
}

func oneRegularPod(instances echo.Instances) echo.Instances {
	var out echo.Instances
	var key *echo.Deployment
	for _, instance := range instances {
		if key == nil {
			if isRegularPod(instance) {
				k := instance.Config().DeploymentKey()
				key = &k
			}
		} else {
			if isRegularPod(instance) && instance.Config().DeploymentKey() != *key {
				continue
			}
		}
		out = append(out, instance)
	}
	return out
}

// TODO put this on echo.Config?
func isRegularPod(instance echo.Instance) bool {
	c := instance.Config()
	return !c.IsVM() && !c.IsVM() && len(c.Subsets) == 1 && !c.IsNaked() && !c.IsHeadless()
}

// ReachableDestinations filters out known-unreachable destinations given a source.
// - from a naked pod, we can't reach cross-network endpoints or VMs
// - we can't reach cross-cluster headless endpoints
// - from an injected Pod, only non-naked cross-network endpoints are reachable
var ReachableDestinations combinationFilter = func(from echo.Instance, to echo.Instances) echo.Instances {
	if from.Config().IsNaked() {
		to = to.Match(
			echo.
				// we'll only be able to reach same-network without a sidecar
				InNetwork(from.Config().Cluster.NetworkName()).
				// we need a sidecar to reach VMs
				And(echo.IsVM().Negate()),
		)
	}

	return to.Match(func(to echo.Instance) bool {
		if to.Config().IsHeadless() {
			// TODO this _might_ have issues with non-kube clusters (e.g. StaticVM)
			// TODO(landow) incompatibilities with multicluster & headless
			return to.Config().Cluster == from.Config().Cluster
		}
		if to.Config().IsNaked() && !to.Config().IsExternal() {
			// we rely on mTLS for multi-network
			// external service is LoadBalanced differently so we won't see 500s unlike regular naked svc
			// TODO we probably don't actually reach all external, but for now maintaining what the tests did
			return to.Config().Cluster.NetworkName() == from.Config().Cluster.NetworkName()
		}
		return true
	})
}
