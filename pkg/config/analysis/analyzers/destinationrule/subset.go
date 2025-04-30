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
		Name:        "subsetPodNotSelected.Analyzer",
		Description: "Checks if pods are selected by subsets in destination rules.",
		Inputs: []config.GroupVersionKind{
			gvk.DestinationRule,
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
		se := util.GetDestinationHost(resource.Metadata.FullName.Namespace, dr.GetExportTo(), dr.Host, services)
		if se == nil {
			msg := msg.NewUnknownDestinationRuleHost(resource, dr.Host)
			context.Report(gvk.DestinationRule, msg)
			return true
		}

		if len(dr.Subsets) == 0 {
			return true
		}

		subsetsMatcher[resource] = map[string]*matcher{}
		for _, subset := range dr.Subsets {
			selector := labels.Merge(se.GetWorkloadSelector().GetLabels(), labels.Set(subset.Labels)).AsSelector()
			subsetsMatcher[resource][subset.Name] = &matcher{
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
