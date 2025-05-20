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
	"k8s.io/apimachinery/pkg/labels"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/util"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
)

type PodNotSelectedAnalyzer struct{}

var _ analysis.Analyzer = &PodNotSelectedAnalyzer{}

func (a *PodNotSelectedAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "destinationrule.SubsetSelectPodAnalyzer",
		Description: "Checks if pods are selected by subsets in destination rules.",
		Inputs: []config.GroupVersionKind{
			gvk.DestinationRule,
			gvk.Pod,
			gvk.Service,
			gvk.ServiceEntry,
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
		lbls := labels.Set(resource.Metadata.Labels)
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
