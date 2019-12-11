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

package destinationrule

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	k8s_labels "k8s.io/apimachinery/pkg/labels"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/util"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/meta/metadata"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/resource"
)

// TargetAnalyzer checks the destination rule hosts for duplicates and host + subsets for missing services / pods
type TargetAnalyzer struct{}

var _ analysis.Analyzer = &TargetAnalyzer{}

type hostAndSubset struct {
	host   resource.Name
	subset string
}

// Metadata implements Analyzer
func (d *TargetAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name: "destinationrule.TargetAnalyzer",
		Inputs: collection.Names{
			metadata.IstioNetworkingV1Alpha3Destinationrules,
			metadata.IstioNetworkingV1Alpha3Serviceentries,
			// Synthetic is not used by this analyzer, but the test requires it because of util package
			metadata.IstioNetworkingV1Alpha3SyntheticServiceentries,
			metadata.K8SCoreV1Services,
			metadata.K8SCoreV1Pods,
		},
		Description: "Checks for missing DestinationRule hosts as well as duplicate destinations (including subsets)",
	}
}

// Analyze implements Analyzer
func (d *TargetAnalyzer) Analyze(ctx analysis.Context) {
	seHosts := util.ServiceEntriesToFqdnMap(ctx)
	services := availableServices(ctx)
	subsets := hostSubsetCombinations(ctx)

	for hs, resources := range subsets {
		hostResourceName := hs.host
		drNamespace, drHost := hostResourceName.InterpretAsNamespaceAndName()
		fqdnName := util.ConvertHostToFQDN(drNamespace, drHost)

		wildCardEntries := []*resource.Entry{}

		// Look for wildcarded DRs also, they're duplicate to this one
		if hs.subset != util.AllSubsets {
			hsWildcardSearch := hs
			hsWildcardSearch.subset = util.AllSubsets

			if wildCardEntriesFound, found := subsets[hsWildcardSearch]; found {
				wildCardEntries = wildCardEntriesFound
			}
		}

		if len(resources) > 1 || len(wildCardEntries) > 0 {
			// Create Report for duplicate host + subset combination
			errorEntries := combineResourceEntryNames(append(resources, wildCardEntries...))
			for _, r := range resources {
				ctx.Report(metadata.IstioNetworkingV1Alpha3Destinationrules,
					msg.NewConflictingDestinationRulesHost(r, errorEntries, fqdnName, hs.subset))
			}
			for _, r := range wildCardEntries {
				// We need to create the duplicate report here of wildcard subsets since searching the other way isn't possible
				ctx.Report(metadata.IstioNetworkingV1Alpha3Destinationrules,
					msg.NewConflictingDestinationRulesHost(r, errorEntries, fqdnName, util.AllSubsets))
			}
		}

		nsScopedFqdn := util.NewScopedFqdn(drNamespace, drNamespace, drHost)
		allNsScopedFqdn := util.NewScopedFqdn(util.ExportToAllNamespaces, drNamespace, drHost)

		_, nsFound := seHosts[nsScopedFqdn]
		_, allFound := seHosts[allNsScopedFqdn]
		targetService, serviceFound := services[nsScopedFqdn]

		if !nsFound && !allFound && !serviceFound {
			// DestinationRule host does not match any ServiceEntry or platform Service
			for _, r := range resources {
				ctx.Report(metadata.IstioNetworkingV1Alpha3Destinationrules,
					msg.NewReferencedResourceNotFound(r, "host", fqdnName))
			}
		}

		if serviceFound {
			// Check subset labels
			for _, r := range resources {
				dr := r.Item.(*v1alpha3.DestinationRule)
			NextSubset:
				for _, ss := range dr.GetSubsets() {
					if ss.Name != hs.subset {
						continue
					}
					subsetSelector := k8s_labels.SelectorFromSet(k8s_labels.Set(ss.GetLabels()))
					for _, targetPodLabels := range targetService {
						if subsetSelector.Matches(targetPodLabels) {
							continue NextSubset
						}
					}
					// No target pods found
					errDest := fmt.Sprintf("%s/subset:%s", fqdnName, ss.Name)
					ctx.Report(metadata.IstioNetworkingV1Alpha3Destinationrules,
						msg.NewReferencedResourceNotFound(r, "subset", errDest))
				}
			}
		}
	}
}

func hostSubsetCombinations(ctx analysis.Context) map[hostAndSubset][]*resource.Entry {
	seenCombinations := map[hostAndSubset][]*resource.Entry{}

	addMatch := func(hs hostAndSubset, r *resource.Entry) {
		if subset, found := seenCombinations[hs]; found {
			subset = append(subset, r)
			seenCombinations[hs] = subset
		} else if !found {
			seenCombinations[hs] = []*resource.Entry{r}
		}
	}

	ctx.ForEach(metadata.IstioNetworkingV1Alpha3Destinationrules, func(r *resource.Entry) bool {
		dr := r.Item.(*v1alpha3.DestinationRule)
		drNamespace, _ := r.Metadata.Name.InterpretAsNamespaceAndName()

		// If there are no subsets, then we need to match ALL the subsets..
		if len(dr.GetSubsets()) < 1 || (len(dr.GetSubsets()) == 1 && dr.GetSubsets()[0].Name == util.AllSubsets) {
			hs := hostAndSubset{
				host:   util.GetResourceNameFromHost(drNamespace, dr.GetHost()),
				subset: util.AllSubsets,
			}
			addMatch(hs, r)
		}
		for _, ss := range dr.GetSubsets() {
			hs := hostAndSubset{
				host:   util.GetResourceNameFromHost(drNamespace, dr.GetHost()),
				subset: ss.GetName(),
			}
			addMatch(hs, r)
		}

		return true
	})

	return seenCombinations
}

func availableServices(ctx analysis.Context) map[util.ScopedFqdn][]k8s_labels.Set {
	podLabelSets := make([]k8s_labels.Set, 0)
	ctx.ForEach(metadata.K8SCoreV1Pods, func(r *resource.Entry) bool {
		pod := r.Item.(*v1.Pod)
		podLabels := k8s_labels.Set(pod.ObjectMeta.Labels)
		podLabelSets = append(podLabelSets, podLabels)
		return true
	})

	services := make(map[util.ScopedFqdn][]k8s_labels.Set)
	ctx.ForEach(metadata.K8SCoreV1Services, func(r *resource.Entry) bool {
		service := r.Item.(*v1.ServiceSpec)
		ns, serviceName := r.Metadata.Name.InterpretAsNamespaceAndName()
		selector := k8s_labels.SelectorFromSet(k8s_labels.Set(service.Selector))

		podLabels := make([]k8s_labels.Set, 0)
		for _, labels := range podLabelSets {
			if selector.Matches(labels) {
				podLabels = append(podLabels, labels)
			}
		}

		services[util.NewScopedFqdn(ns, ns, serviceName)] = podLabels

		return true
	})

	return services
}

func combineResourceEntryNames(rList []*resource.Entry) []string {
	names := []string{}
	for _, r := range rList {
		names = append(names, r.Metadata.Name.String())
	}
	return names
}
