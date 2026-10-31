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

package destinationrule

import (
	"istio.io/api/label"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/util"
	"istio.io/istio/pkg/config/analysis/msg"
	confighost "istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
)

type WaypointWorkloadSelectorAnalyzer struct{}

var _ analysis.Analyzer = &WaypointWorkloadSelectorAnalyzer{}

func (a *WaypointWorkloadSelectorAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "destinationrule.WaypointWorkloadSelectorAnalyzer",
		Description: "Checks for DestinationRules with workload selectors targeting services that use waypoints.",
		Inputs: []config.GroupVersionKind{
			gvk.DestinationRule,
			gvk.Namespace,
			gvk.Service,
			gvk.ServiceEntry,
		},
	}
}

func (a *WaypointWorkloadSelectorAnalyzer) Analyze(context analysis.Context) {
	waypointNamespaces := map[string]struct{}{}
	context.ForEach(gvk.Namespace, func(ns *resource.Instance) bool {
		waypoint := ns.Metadata.Labels[label.IoIstioUseWaypoint.Name]
		if waypoint != "" && waypoint != "none" {
			waypointNamespaces[ns.Metadata.FullName.Name.String()] = struct{}{}
		}

		return true
	})

	waypointServices := map[string]struct{}{}
	context.ForEach(gvk.Service, func(service *resource.Instance) bool {
		waypoint, hasWaypointLabel := service.Metadata.Labels[label.IoIstioUseWaypoint.Name]
		hasWaypoint := waypoint != "" && waypoint != "none"
		_, nsHasWaypoint := waypointNamespaces[service.Metadata.FullName.Namespace.String()]
		if hasWaypoint || (!hasWaypointLabel && nsHasWaypoint) {
			host := util.ConvertHostToFQDN(
				service.Metadata.FullName.Namespace,
				service.Metadata.FullName.Name.String(),
			)
			waypointServices[host] = struct{}{}
		}

		return true
	})

	context.ForEach(gvk.ServiceEntry, func(se *resource.Instance) bool {
		waypoint, hasWaypointLabel := se.Metadata.Labels[label.IoIstioUseWaypoint.Name]
		hasWaypoint := waypoint != "" && waypoint != "none"
		_, nsHasWaypoint := waypointNamespaces[se.Metadata.FullName.Namespace.String()]
		if !hasWaypoint && (hasWaypointLabel || !nsHasWaypoint) {
			return true
		}

		serviceEntry := se.Message.(*networking.ServiceEntry)
		for _, h := range serviceEntry.Hosts {
			waypointServices[h] = struct{}{}
		}

		return true
	})

	context.ForEach(gvk.DestinationRule, func(dr *resource.Instance) bool {
		rule := dr.Message.(*networking.DestinationRule)
		if rule.WorkloadSelector == nil {
			return true
		}

		host := util.ConvertHostToFQDN(dr.Metadata.FullName.Namespace, rule.Host)

		matchesWaypointService := false
		for serviceHost := range waypointServices {
			// Configured host can be wildcard (e.g. *.default.svc.cluster.local).
			if confighost.Name(host).Matches(confighost.Name(serviceHost)) {
				matchesWaypointService = true
				break
			}
		}
		if !matchesWaypointService {
			return true
		}

		context.Report(
			gvk.DestinationRule,
			msg.NewUnsupportedDestinationRuleWorkloadSelector(
				dr,
				dr.Metadata.FullName.String(),
				host,
			),
		)

		return true
	})
}
