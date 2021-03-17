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
	SimpleFilter      func(echo.Instances) echo.Instances
	CombinationFilter func(from echo.Instance, to echo.Instances) echo.Instances
)

// From applies each of the filter functions in order to allow removing workloads from the set of clients.
// Example:
//     echotest.New(t, apps).
//       From(echotest.SingleSimplePodServiceAndAllSpecial, echotest.NoExternalServices).
//       Run()
func (t *T) From(filters ...SimpleFilter) *T {
	for _, filter := range filters {
		t.sources = filter(t.sources)
	}
	return t
}

// To applies each of the filter functions in order to allow removing workloads from the set of destinations.
// Example:
//     echotest.New(t, apps).
//       To(echotest.SingleSimplePodServiceAndAllSpecial).
//       Run()
func (t *T) To(filters ...SimpleFilter) *T {
	for _, filter := range filters {
		t.destinations = filter(t.destinations)
	}
	return t
}

// ConditionallyTo appends the given filters which are executed per test. Destination filters may need
// to change behavior based on the client. For example, naked services can't be reached cross-network, so
// the client matters.
// Example:
//     echotest.New(t, apps).
//       ConditionallyTo(echotest.ReachableDestinations).
//       Run()
func (t *T) ConditionallyTo(filters ...CombinationFilter) *T {
	t.destinationFilters = append(t.destinationFilters, filters...)
	return t
}

// WithDefaultFilters applies common filters that work for most tests.
// Example:
//   The full set of apps is a, b, c, headless, naked, and vm (one simple pod).
//   Only a, headless, naked and vm are used as sources.
//   Subtests are generated only for reachable destinations.
//   Pod a will not be in destinations, but b will (one simpe pod not in sources)
func (t *T) WithDefaultFilters() *T {
	return t.
		From(SingleSimplePodServiceAndAllSpecial(), Not(ExternalServices)).
		ConditionallyTo(ReachableDestinations).
		To(SingleSimplePodServiceAndAllSpecial(t.sources...))
}

func (t *T) applyCombinationFilters(from echo.Instance, to echo.Instances) echo.Instances {
	for _, filter := range t.destinationFilters {
		to = filter(from, to)
	}
	return to
}

// SingleSimplePodServiceAndAllSpecial finds the first Pod deployment that has a sidecar and doesn't use a headless service and removes all
// other "regular" pods that aren't part of the same Service. Pods that are part of the same Service but are in a
// different cluster or revision will still be included.
// Example:
//     The full set of apps is a, b, c, headless, naked, and vm.
//     The plain-pods are a, b and c.
//     This filter would result in a, headless, naked and vm.
// TODO this name is not good
func SingleSimplePodServiceAndAllSpecial(exclude ...echo.Instance) SimpleFilter {
	return func(instances echo.Instances) echo.Instances {
		return oneRegularPod(instances, exclude)
	}
}

func oneRegularPod(instances echo.Instances, exclude echo.Instances) echo.Instances {
	regularPods := instances.Match(isRegularPod)
	others := instances.Match(echo.Not(isRegularPod))
	for _, exclude := range exclude {
		regularPods = regularPods.Match(echo.Not(echo.SameDeployment(exclude)))
	}
	if len(regularPods) == 0 {
		return others
	}
	regularPods = regularPods.Match(echo.SameDeployment(regularPods[0]))
	// TODO will the re-ordering end up breaking something or making other filters hard to predict?
	return append(regularPods, others...)
}

// TODO put this on echo.Config?
func isRegularPod(instance echo.Instance) bool {
	c := instance.Config()
	return !c.IsVM() && len(c.Subsets) == 1 && !c.IsNaked() && !c.IsHeadless()
}

// Not includes all workloads that don't match the given filter
func Not(filter SimpleFilter) SimpleFilter {
	return func(instances echo.Instances) echo.Instances {
		filtered := filter(instances)
		inverted := instances.Match(func(instance echo.Instance) bool {
			return !filtered.Contains(instance)
		})
		return inverted
	}
}

// VirtualMachines includes VM deployments
func VirtualMachines(instances echo.Instances) echo.Instances {
	return instances.Match(echo.IsVirtualMachine())
}

// ExternalServices includes services that are based on naked pods with a custom DefaultHostHeader
func ExternalServices(instances echo.Instances) echo.Instances {
	return instances.Match(echo.IsExternal())
}

// ReachableDestinations filters out known-unreachable destinations given a source.
// - from a naked pod, we can't reach cross-network endpoints or VMs
// - we can't reach cross-cluster headless endpoints
// - from an injected Pod, only non-naked cross-network endpoints are reachable
var ReachableDestinations CombinationFilter = func(from echo.Instance, to echo.Instances) echo.Instances {
	return to.Match(fromNaked(from).
		And(fromVM(from)).
		And(toSameNetworkNaked(from)).
		And(toInClusterHeadless(from)))
}

func toInClusterHeadless(from echo.Instance) echo.Matcher {
	excluded := echo.IsHeadless().
		And(echo.Not(echo.InCluster(from.Config().Cluster)))
	return excluded.Negate()
}

// toSameNetworkNaked filters out naked instances that aren't on the same network.
// While External services are considered "naked", we won't see 500s due to different loadbalancing.
func toSameNetworkNaked(from echo.Instance) echo.Matcher {
	srcNw := from.Config().Cluster.NetworkName()
	excluded := echo.IsNaked().
		// TODO we probably don't actually reach all external, but for now maintaining what the tests did
		And(echo.Not(echo.IsExternal())).
		And(echo.Not(echo.InNetwork(srcNw)))
	return excluded.Negate()
}

// fromVM filters out external services
func fromVM(from echo.Instance) echo.Matcher {
	if !from.Config().IsVM() {
		return echo.Any
	}
	return echo.IsExternal().Negate()
}

// fromNaked filters out all virtual machines and any instance that isn't on the same network
func fromNaked(from echo.Instance) echo.Matcher {
	if !from.Config().IsNaked() {
		return echo.Any
	}
	return echo.InNetwork(from.Config().Cluster.NetworkName()).And(echo.IsVirtualMachine().Negate())
}
