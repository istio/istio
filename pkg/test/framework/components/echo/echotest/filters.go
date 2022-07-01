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

import (
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/match"
)

type (
	Filter            func(echo.Instances) echo.Instances
	CombinationFilter func(from echo.Instance, to echo.Instances) echo.Instances
)

// From applies each of the filter functions in order to allow removing workloads from the set of clients.
// Example:
//     echotest.New(t, apps).
//       From(echotest.SimplePodServiceAndAllSpecial, echotest.NoExternalServices).
//       Run()
func (t *T) From(filters ...Filter) *T {
	for _, filter := range filters {
		t.sources = filter(t.sources)
	}
	return t
}

func (t *T) FromMatch(m match.Matcher) *T {
	return t.From(FilterMatch(m))
}

// To applies each of the filter functions in order to allow removing workloads from the set of destinations.
// Example:
//     echotest.New(t, apps).
//       To(echotest.SimplePodServiceAndAllSpecial).
//       Run()
func (t *T) To(filters ...Filter) *T {
	for _, filter := range filters {
		t.destinations = filter(t.destinations)
	}
	return t
}

func (t *T) ToMatch(m match.Matcher) *T {
	return t.To(FilterMatch(m))
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
func (t *T) WithDefaultFilters(minimumFrom, minimumTo int) *T {
	return t.
		From(FilterMatch(match.NotExternal)).
		From(SimplePodServiceAndAllSpecial(minimumFrom)).
		ConditionallyTo(ReachableDestinations).
		To(SimplePodServiceAndAllSpecial(minimumTo, t.sources...))
}

func (t *T) applyCombinationFilters(from echo.Instance, to echo.Instances) echo.Instances {
	for _, filter := range t.destinationFilters {
		to = filter(from, to)
	}
	return to
}

// SimplePodServiceAndAllSpecial finds the first Pod deployment that has a sidecar and doesn't use a headless service and removes all
// other "regular" pods that aren't part of the same Service. Pods that are part of the same Service but are in a
// different cluster or revision will still be included.
// Example:
//     The full set of apps is a, b, c, headless, naked, and vm.
//     The plain-pods are a, b and c.
//     This filter would result in a, headless, naked and vm.
// TODO this name is not good
func SimplePodServiceAndAllSpecial(min int, exclude ...echo.Instance) Filter {
	return func(instances echo.Instances) echo.Instances {
		nonRegular := notRegularPods()(instances)
		needed := min - len(nonRegular)
		if needed <= 0 {
			needed = 1
		}

		return nRegularPodPerNamespace(needed, exclude)(instances).Append(nonRegular)
	}
}

func SingleSimplePodServiceAndAllSpecial(exclude ...echo.Instance) Filter {
	return SimplePodServiceAndAllSpecial(1, exclude...)
}

func nRegularPodPerNamespace(needed int, exclude echo.Instances) Filter {
	return func(instances echo.Instances) echo.Instances {
		// Apply the filters.
		regularPods := match.And(
			match.RegularPod,
			match.Not(match.AnyServiceName(exclude.NamespacedNames()))).GetMatches(instances)

		if len(regularPods) == 0 {
			return regularPods
		}

		// Pick regular pods per namespace, up to needed
		namespaces := map[string]int{}
		var outServices echo.Services
		for _, svc := range regularPods.Services() {
			ns := svc.Config().Namespace.Name()
			if namespaces[ns] < needed {
				namespaces[ns]++
				outServices = append(outServices, svc)
			}
		}

		return outServices.Instances()
	}
}

func notRegularPods() Filter {
	return func(instances echo.Instances) echo.Instances {
		return match.NotRegularPod.GetMatches(instances)
	}
}

// FilterMatch returns a filter that simply applies the given matcher.
func FilterMatch(matcher match.Matcher) Filter {
	return func(instances echo.Instances) echo.Instances {
		return matcher.GetMatches(instances)
	}
}

// SameNetwork filters out destinations that are on a different network from the source.
var SameNetwork CombinationFilter = func(from echo.Instance, to echo.Instances) echo.Instances {
	return match.Network(from.Config().Cluster.NetworkName()).GetMatches(to)
}

// NoSelfCalls disallows self-calls where from and to have the same service name. Self-calls can
// by-pass the sidecar, so tests relying on sidecar logic will sent to disable self-calls by default.
var NoSelfCalls CombinationFilter = func(from echo.Instance, to echo.Instances) echo.Instances {
	return match.Not(match.ServiceName(from.NamespacedName())).GetMatches(to)
}

// ReachableDestinations filters out known-unreachable destinations given a source.
// - from a naked pod, we can't reach cross-network endpoints or VMs
// - we can't reach cross-cluster headless endpoints
// - from an injected Pod, only non-naked cross-network endpoints are reachable
var ReachableDestinations CombinationFilter = func(from echo.Instance, to echo.Instances) echo.Instances {
	return match.And(
		reachableFromNaked(from),
		reachableFromVM(from),
		reachableFromProxylessGRPC(from),
		reachableNakedDestinations(from),
		reachableHeadlessDestinations(from)).
		GetMatches(to)
}

// reachableHeadlessDestinations filters out headless services that aren't in the same cluster
// TODO(stevenctl): headless across-networks https://github.com/istio/istio/issues/38327
func reachableHeadlessDestinations(from echo.Instance) match.Matcher {
	excluded := match.And(
		match.Headless,
		match.Not(match.Network(from.Config().Cluster.NetworkName())))
	return match.Not(excluded)
}

// reachableNakedDestinations filters out naked instances that aren't on the same network.
// While External services are considered "naked", we won't see 500s due to different loadbalancing.
func reachableNakedDestinations(from echo.Instance) match.Matcher {
	srcNw := from.Config().Cluster.NetworkName()
	excluded := match.And(
		// Only exclude naked if all subsets are naked. If an echo instance contains a mix of
		// subsets with and without sidecars, we'll leave it up to the test to determine what
		// is reachable.
		match.AllNaked,
		// TODO we probably don't actually reach all external, but for now maintaining what the tests did
		match.NotExternal,
		match.Not(match.Network(srcNw)))
	return match.Not(excluded)
}

// reachableFromVM filters out external services due to issues with ServiceEntry resolution
// TODO https://github.com/istio/istio/issues/27154
func reachableFromVM(from echo.Instance) match.Matcher {
	if !from.Config().IsVM() {
		return match.Any
	}
	return match.NotExternal
}

func reachableFromProxylessGRPC(from echo.Instance) match.Matcher {
	if !from.Config().IsProxylessGRPC() {
		return match.Any
	}
	return match.And(
		match.NotExternal,
		match.NotHeadless)
}

// reachableFromNaked filters out all virtual machines and any instance that isn't on the same network
func reachableFromNaked(from echo.Instance) match.Matcher {
	if !from.Config().IsNaked() {
		return match.Any
	}
	return match.And(
		match.Network(from.Config().Cluster.NetworkName()),
		match.NotVM)
}
