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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"istio.io/api/networking/v1alpha3"
	networkingutil "istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry/util/label"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/util"
	"istio.io/istio/pkg/config/analysis/msg"
	configlabels "istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/network"
)

type PodNotSelectedAnalyzer struct{}

var _ analysis.Analyzer = &PodNotSelectedAnalyzer{}

// getNodeLocalityString converts node labels to a locality string using pilot's LocalityToString function
func getNodeLocalityString(nodeLabels map[string]string) string {
	region := nodeLabels[label.LabelTopologyRegion]
	zone := nodeLabels[label.LabelTopologyZone]
	subzone := nodeLabels[label.LabelTopologySubzone]

	// Create core.Locality object from node labels
	locality := &core.Locality{
		Region:  region,
		Zone:    zone,
		SubZone: subzone,
	}

	// Use pilot's LocalityToString function for exact consistency
	return networkingutil.LocalityToString(locality)
}

func (a *PodNotSelectedAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "destinationrule.SubsetSelectPodAnalyzer",
		Description: "Checks if pods are selected by subsets in destination rules.",
		Inputs: []config.GroupVersionKind{
			gvk.DestinationRule,
			gvk.Pod,
			gvk.Service,
			gvk.ServiceEntry,
			gvk.Node,
		},
	}
}

func (a *PodNotSelectedAnalyzer) Analyze(context analysis.Context) {
	type matcher struct {
		selector labels.Selector
		matched  bool
	}

	subsetsMatcher := map[*resource.Instance]map[string]*matcher{}
	services := util.InitServiceEntryHostMap(context)

	// Build an index of node labels by node name for efficient lookup
	nodeLabels := make(map[string]map[string]string)
	context.ForEach(gvk.Node, func(resource *resource.Instance) bool {
		nodeLabels[resource.Metadata.FullName.Name.String()] = resource.Metadata.Labels
		return true
	})

	context.ForEach(gvk.DestinationRule, func(resource *resource.Instance) bool {
		dr := resource.Message.(*v1alpha3.DestinationRule)
		se := util.GetDestinationHost(resource.Metadata.FullName.Namespace, dr.GetExportTo(), dr.GetHost(), services)
		if se == nil {
			msg := msg.NewUnknownDestinationRuleHost(resource, dr.GetHost())
			context.Report(gvk.DestinationRule, msg)
			return true
		}

		if len(dr.GetSubsets()) == 0 {
			return true
		}

		subsetsMatcher[resource] = map[string]*matcher{}
		for _, subset := range dr.GetSubsets() {
			selector := labels.Merge(se.GetWorkloadSelector().GetLabels(), labels.Set(subset.GetLabels())).AsSelector()
			subsetsMatcher[resource][subset.GetName()] = &matcher{
				selector: selector,
				matched:  false,
			}
		}
		return true
	})

	context.ForEach(gvk.Pod, func(resource *resource.Instance) bool {
		podSpec := resource.Message.(*corev1.PodSpec)

		// Use the actual AugmentLabels function that pilot uses in the service registry
		locality := ""
		if nodeLabelsOfPod, exists := nodeLabels[podSpec.NodeName]; exists {
			locality = getNodeLocalityString(nodeLabelsOfPod)
		}

		// Convert pod labels to configlabels.Instance type (which is what AugmentLabels expects)
		podLabelsInstance := configlabels.Instance(resource.Metadata.Labels)

		// Use pilot's AugmentLabels - but with empty cluster/network IDs for as they aren't required for this analyzer
		augmentedLabelsInstance := label.AugmentLabels(
			podLabelsInstance,
			cluster.ID(""),   // Empty cluster ID for analyzer context
			locality,         // Locality string built from node labels
			podSpec.NodeName, // Node name for hostname label
			network.ID(""),   // Empty network ID for analyzer context
		)

		// Convert back to the labels.Set type needed for selector matching
		lbls := labels.Set(map[string]string(augmentedLabelsInstance))

		for _, subsets := range subsetsMatcher {
			for _, matcher := range subsets {
				if matcher.matched {
					continue
				}

				if matcher.selector.Matches(lbls) {
					matcher.matched = true
				}
			}
		}
		return true
	})

	for dr, subsets := range subsetsMatcher {
		for subsetName, matcher := range subsets {
			if !matcher.matched {
				msg := msg.NewDestinationRuleSubsetNotSelectPods(dr, subsetName)
				context.Report(gvk.DestinationRule, msg)
			}
		}
	}
}
