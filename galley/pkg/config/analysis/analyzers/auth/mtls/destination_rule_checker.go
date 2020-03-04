// Copyright 2019 Istio Authors
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

package mtls

import (
	"sort"

	"istio.io/api/networking/v1alpha3"

	"istio.io/istio/galley/pkg/config/analysis/analyzers/util"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/resource"
)

// DestinationRuleChecker computes whether or not MTLS is used according to
// DestinationRules added to the instance. It handles the complicated logic of
// looking up which DestinationRule takes effect for a given source namespace,
// destination namespace and host name.
//
// This logic matches the logic Pilot uses:
// https://github.com/istio/istio/blob/4a442e9f4cbdedb0accdb33bd8b96a3e59691b0b/pilot/pkg/model/push_context.go#L605
// and what's documented on the istio.io site:
// https://istio.io/docs/ops/traffic-management/deploy-guidelines/#cross-namespace-configuration-sharing
type DestinationRuleChecker struct {
	namespaceToDestinations map[resource.Namespace]destinations
	rootNamespace           resource.Namespace
}

// destination represents a destination specified in a DestinationRule.
type destination struct {
	targetService TargetService
	usesMTLS      bool
	isPrivate     bool
	resource      *resource.Instance
}

// destinations is a list of destinations that supports being sorted by
// hostname. This means that, once sorted, you can iterate over the list going
// from most-specific rules to least-specific. This matches how the API behaves.
type destinations []destination

// destinations implements sort.Interface
var _ sort.Interface = destinations{}

func (d destinations) Len() int {
	return len(d)
}

func (d destinations) Less(i, j int) bool {
	// First, check to see if we have a tie on FQDN. If we do, we want to break
	// ties so that target services with a port specified come before those that
	// don't.
	ts1 := d[i].targetService
	ts2 := d[j].targetService
	if ts1.FQDN() == ts2.FQDN() {
		return ts1.PortNumber() != 0
	}

	// Defer to the sort order for target service hostname
	hosts := []string{d[i].targetService.FQDN(), d[j].targetService.FQDN()}
	return host.NewNames(hosts).Less(0, 1)
}

func (d destinations) Swap(i, j int) {
	d[i], d[j] = d[j], d[i]
}

// NewDestinationRuleChecker creates a new instance with the given config root
// namespace.
func NewDestinationRuleChecker(rootNamespace resource.Namespace) *DestinationRuleChecker {
	return &DestinationRuleChecker{
		namespaceToDestinations: make(map[resource.Namespace]destinations),
		rootNamespace:           rootNamespace,
	}
}

// TargetServices returns the list of TargetServices known to the checker. These
// services are generated from DestinationRules previously added to the checker.
func (dc *DestinationRuleChecker) TargetServices() []TargetService {
	var targetServices []TargetService
	for _, destinations := range dc.namespaceToDestinations {
		for _, destination := range destinations {
			targetServices = append(targetServices, destination.targetService)
		}
	}

	return targetServices
}

// AddDestinationRule adds a DestinationRule to the checker.
func (dc *DestinationRuleChecker) AddDestinationRule(resource *resource.Instance, rule *v1alpha3.DestinationRule) {
	// By default Destination rules are exported publicly.
	isPrivate := false
	for _, export := range rule.ExportTo {
		if export == "." {
			isPrivate = true
		}
	}

	namespace := resource.Metadata.FullName.Namespace
	fqdn := util.ConvertHostToFQDN(namespace, rule.GetHost())
	// By default, we are not using MTLS
	usesMTLS := false
	if rule.TrafficPolicy != nil && rule.TrafficPolicy.Tls != nil && rule.TrafficPolicy.Tls.Mode == v1alpha3.TLSSettings_ISTIO_MUTUAL {
		usesMTLS = true
	}

	dc.namespaceToDestinations[namespace] = append(dc.namespaceToDestinations[namespace], destination{
		targetService: NewTargetService(fqdn),
		usesMTLS:      usesMTLS,
		isPrivate:     isPrivate,
		resource:      resource,
	})

	if rule.TrafficPolicy == nil {
		// No overrides to check
		return
	}
	// TODO Support checking subsets.
	// Now check if we have any overrides
	for _, pls := range rule.TrafficPolicy.PortLevelSettings {
		portUsesMTLS := false
		if pls.Tls != nil && pls.Tls.Mode == v1alpha3.TLSSettings_ISTIO_MUTUAL {
			portUsesMTLS = true
		}

		dc.namespaceToDestinations[namespace] = append(dc.namespaceToDestinations[namespace], destination{
			targetService: NewTargetServiceWithPortNumber(fqdn, pls.Port.GetNumber()),
			usesMTLS:      portUsesMTLS,
			isPrivate:     isPrivate,
		})
	}
}

// DoesNamespaceUseMTLSToService returns true if, according to DestinationRules
// added to the checker, mTLS will be used when communicating to the specified
// TargetService from the source namespace to the destination namespace.
//
// If the TargetService's FQDN has a wildcard, then the set of DestinationRules
// considered for routing are only rules that match a superset of the hosts
// specified by the TargetService FQDN. This means you can check, for example,
// the hostname '*.svc.cluster.local' to see if strict MTLS is enforced globally.
func (dc *DestinationRuleChecker) DoesNamespaceUseMTLSToService(srcNamespace, dstNamespace resource.Namespace, ts TargetService) (bool, *resource.Instance) {
	var matchingDestination *destination
	// First, check for a destination rule for src namespace only if the
	// namespace isn't the root namespace. Pilot has this behavior to ensure that the
	// rules in the root namespace don't override other rules.
	if srcNamespace != dc.rootNamespace {
		matchingDestination = dc.findMatchingRuleInNamespace(srcNamespace, ts, true)
		if matchingDestination != nil {
			return matchingDestination.usesMTLS, matchingDestination.resource
		}
	}

	// Now check destination namespace
	matchingDestination = dc.findMatchingRuleInNamespace(dstNamespace, ts, false)
	if matchingDestination != nil {
		return matchingDestination.usesMTLS, matchingDestination.resource
	}

	// Finally, try the root namespace
	matchingDestination = dc.findMatchingRuleInNamespace(dc.rootNamespace, ts, false)
	if matchingDestination != nil {
		return matchingDestination.usesMTLS, matchingDestination.resource
	}

	// no matches found - just return false
	return false, nil
}

// findMatchingRuleInNamespace looks up a DestinationRule in a namespace for a
// specified target service, optionally including privately-exported rules. Note
// that if target service has a wildcard in it, then matched rules must be a
// strict superset of the target service hostnames.
func (dc *DestinationRuleChecker) findMatchingRuleInNamespace(namespace resource.Namespace, ts TargetService, includePrivate bool) *destination {
	// TODO Port name should be handled at some point.
	// TODO We should really presort these ahead of time.
	var ds destinations
	for _, d := range dc.namespaceToDestinations[namespace] {
		if !includePrivate && d.isPrivate {
			continue
		}
		ds = append(ds, d)
	}
	// Sort our destinations, which allows us to find the first match by iterating.
	sort.Sort(ds)

	for _, d := range ds {
		if !host.Name(ts.FQDN()).SubsetOf(host.Name(d.targetService.FQDN())) {
			continue
		}

		// If a port is specified for ts, then skip if destination also has a
		// port number and it doesn't match.
		if ts.PortNumber() != 0 && d.targetService.PortNumber() != 0 && ts.PortNumber() != d.targetService.PortNumber() {
			continue
		}
		// If target service doesn't specify a port, then skip if destination
		// does specify a port.
		if ts.PortNumber() == 0 && d.targetService.PortNumber() != 0 {
			continue
		}
		return &d
	}

	// No match
	return nil
}
