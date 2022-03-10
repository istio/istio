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
//       From(echotest.SingleSimplePodServiceAndAllSpecial, echotest.NoExternalServices).
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
//       To(echotest.SingleSimplePodServiceAndAllSpecial).
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
func (t *T) WithDefaultFilters() *T {
	return t.
		From(SingleSimplePodServiceAndAllSpecial(), FilterMatch(match.IsNotExternal)).
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
func SingleSimplePodServiceAndAllSpecial(exclude ...echo.Instance) Filter {
	return func(instances echo.Instances) echo.Instances {
		return oneRegularPod(instances, exclude)
	}
}

func oneRegularPod(instances echo.Instances, exclude echo.Instances) echo.Instances {
	regularPods := RegularPod.GetMatches(instances)
	others := NotRegularPod.GetMatches(instances)
	for _, exclude := range exclude {
		regularPods = match.Not(match.SameDeployment(exclude)).GetMatches(regularPods)
	}
	if len(regularPods) == 0 {
		return others
	}
	regularPods = match.SameDeployment(regularPods[0]).GetMatches(regularPods)
	// TODO will the re-ordering end up breaking something or making other filters hard to predict?
	return append(regularPods, others...)
}

// RegularPod matches echos that don't meet any of the following criteria:
// - VM
// - Naked
// - Headless
// - TProxy
// - Multi-Subset
var RegularPod match.Matcher = func(instance echo.Instance) bool {
	c := instance.Config()
	return len(c.Subsets) == 1 && !c.IsVM() && !c.IsTProxy() && !c.IsNaked() && !c.IsHeadless() && !c.IsStatefulSet() && !c.IsProxylessGRPC()
}

var NotRegularPod = match.Not(RegularPod)

// Not includes all workloads that don't match the given filter
func Not(filter Filter) Filter {
	return func(instances echo.Instances) echo.Instances {
		filtered := filter(instances)

		return match.Matcher(func(instance echo.Instance) bool {
			return !filtered.Contains(instance)
		}).GetMatches(instances)
	}
}

// FilterMatch returns a filter that simply applies the given matcher.
func FilterMatch(matcher match.Matcher) Filter {
	return func(instances echo.Instances) echo.Instances {
		return matcher.GetMatches(instances)
	}
}

// ReachableDestinations filters out known-unreachable destinations given a source.
// - from a naked pod, we can't reach cross-network endpoints or VMs
// - we can't reach cross-cluster headless endpoints
// - from an injected Pod, only non-naked cross-network endpoints are reachable
var ReachableDestinations CombinationFilter = func(from echo.Instance, to echo.Instances) echo.Instances {
	return match.And(
		fromNaked(from),
		reachableFromVM(from),
		reachableFromProxylessGRPC(from),
		reachableNakedDestinations(from),
		reachableHeadlessDestinations(from)).
		GetMatches(to)
}

// reachableHeadlessDestinations filters out headless services that aren't in the same cluster
// TODO https://github.com/istio/istio/issues/27342
func reachableHeadlessDestinations(from echo.Instance) match.Matcher {
	excluded := match.And(
		match.IsHeadless,
		match.Not(match.InNetwork(from.Config().Cluster.NetworkName())))
	return match.Not(excluded)
}

// reachableNakedDestinations filters out naked instances that aren't on the same network.
// While External services are considered "naked", we won't see 500s due to different loadbalancing.
func reachableNakedDestinations(from echo.Instance) match.Matcher {
	srcNw := from.Config().Cluster.NetworkName()
	excluded := match.And(
		match.IsNaked,
		// TODO we probably don't actually reach all external, but for now maintaining what the tests did
		match.IsNotExternal,
		match.Not(match.InNetwork(srcNw)))
	return match.Not(excluded)
}

// reachableFromVM filters out external services due to issues with ServiceEntry resolution
// TODO https://github.com/istio/istio/issues/27154
func reachableFromVM(from echo.Instance) match.Matcher {
	if !from.Config().IsVM() {
		return match.Any
	}
	return match.IsNotExternal
}

func reachableFromProxylessGRPC(from echo.Instance) match.Matcher {
	if !from.Config().IsProxylessGRPC() {
		return match.Any
	}
	return match.And(
		match.IsNotExternal,
		match.IsNotHeadless)
}

// fromNaked filters out all virtual machines and any instance that isn't on the same network
func fromNaked(from echo.Instance) match.Matcher {
	if !from.Config().IsNaked() {
		return match.Any
	}
	return match.And(
		match.InNetwork(from.Config().Cluster.NetworkName()),
		match.IsNotVM)
}
