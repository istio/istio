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

package authz

import (
	"fmt"
	"strings"

	klabels "k8s.io/apimachinery/pkg/labels"

	"istio.io/api/mesh/v1alpha1"
	"istio.io/api/security/v1beta1"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/util"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
)

// AuthorizationPoliciesAnalyzer checks the validity of authorization policies
type AuthorizationPoliciesAnalyzer struct{}

var (
	_          analysis.Analyzer = &AuthorizationPoliciesAnalyzer{}
	meshConfig *v1alpha1.MeshConfig
)

func (a *AuthorizationPoliciesAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "auth.AuthorizationPoliciesAnalyzer",
		Description: "Checks the validity of authorization policies",
		Inputs: []config.GroupVersionKind{
			gvk.MeshConfig,
			gvk.AuthorizationPolicy,
			gvk.Namespace,
			gvk.Pod,
		},
	}
}

func (a *AuthorizationPoliciesAnalyzer) Analyze(c analysis.Context) {
	podLabelsMap := initPodLabelsMap(c)

	c.ForEach(gvk.AuthorizationPolicy, func(r *resource.Instance) bool {
		a.analyzeNoMatchingWorkloads(r, c, podLabelsMap)
		a.analyzeNamespaceNotFound(r, c)
		return true
	})
}

func (a *AuthorizationPoliciesAnalyzer) analyzeNoMatchingWorkloads(r *resource.Instance, c analysis.Context, podLabelsMap map[string][]klabels.Set) {
	ap := r.Message.(*v1beta1.AuthorizationPolicy)
	apNs := r.Metadata.FullName.Namespace.String()

	// If AuthzPolicy is mesh-wide
	if meshWidePolicy(apNs, c) {
		// If it has selector, need further analysis
		if ap.GetSelector() != nil {
			apSelector := klabels.SelectorFromSet(ap.GetSelector().MatchLabels)
			// If there is at least one pod matching the selector within the whole mesh
			if !hasMatchingPodsRunning(apSelector, podLabelsMap) {
				c.Report(gvk.AuthorizationPolicy, msg.NewNoMatchingWorkloadsFound(r, apSelector.String()))
			}
		}

		// If AuthzPolicy is mesh-wide and selectorless,
		// no need to keep the analysis
		return
	}

	// If the AuthzPolicy is namespace-wide and there are present Pods,
	// no messages should be triggered.
	if ap.GetSelector() == nil {
		if len(podLabelsMap[apNs]) == 0 {
			c.Report(gvk.AuthorizationPolicy, msg.NewNoMatchingWorkloadsFound(r, ""))
		}
		return
	}

	// If the AuthzPolicy has Selector, then need to find a matching Pod.
	apSelector := klabels.SelectorFromSet(ap.GetSelector().MatchLabels)
	if !hasMatchingPodsRunningIn(apSelector, podLabelsMap[apNs]) {
		c.Report(gvk.AuthorizationPolicy, msg.NewNoMatchingWorkloadsFound(r, apSelector.String()))
	}
}

// Returns true when the namespace is the root namespace.
// It takes the MeshConfig names istio, if not the last instance found.
func meshWidePolicy(ns string, c analysis.Context) bool {
	mConf := fetchMeshConfig(c)
	return mConf != nil && ns == mConf.GetRootNamespace()
}

func fetchMeshConfig(c analysis.Context) *v1alpha1.MeshConfig {
	if meshConfig != nil {
		return meshConfig
	}

	c.ForEach(gvk.MeshConfig, func(r *resource.Instance) bool {
		meshConfig = r.Message.(*v1alpha1.MeshConfig)
		return r.Metadata.FullName.Name != util.MeshConfigName
	})

	return meshConfig
}

func hasMatchingPodsRunning(selector klabels.Selector, podLabelsMap map[string][]klabels.Set) bool {
	for _, setList := range podLabelsMap {
		if hasMatchingPodsRunningIn(selector, setList) {
			return true
		}
	}
	return false
}

func hasMatchingPodsRunningIn(selector klabels.Selector, setList []klabels.Set) bool {
	hasMatchingPods := false
	for _, labels := range setList {
		if selector.Matches(labels) {
			hasMatchingPods = true
			break
		}
	}
	return hasMatchingPods
}

func (a *AuthorizationPoliciesAnalyzer) analyzeNamespaceNotFound(r *resource.Instance, c analysis.Context) {
	ap := r.Message.(*v1beta1.AuthorizationPolicy)

	for i, rule := range ap.Rules {
		for j, from := range rule.From {
			for k, ns := range append(from.Source.Namespaces, from.Source.NotNamespaces...) {
				if !matchNamespace(ns, c) {
					m := msg.NewReferencedResourceNotFound(r, "namespace", ns)

					nsIndex := k
					if nsIndex >= len(from.Source.Namespaces) {
						nsIndex -= len(from.Source.Namespaces)
					}

					if line, ok := util.ErrorLine(r, fmt.Sprintf(util.AuthorizationPolicyNameSpace, i, j, nsIndex)); ok {
						m.Line = line
					}

					c.Report(gvk.AuthorizationPolicy, m)
				}
			}
		}
	}
}

func matchNamespace(exp string, c analysis.Context) bool {
	match := false
	c.ForEach(gvk.Namespace, func(r *resource.Instance) bool {
		ns := r.Metadata.FullName.String()
		match = namespaceMatch(ns, exp)
		return !match
	})

	return match
}

func namespaceMatch(ns, exp string) bool {
	if strings.EqualFold(exp, "*") {
		return true
	}
	if strings.HasPrefix(exp, "*") {
		return strings.HasSuffix(ns, strings.TrimPrefix(exp, "*"))
	}
	if strings.HasSuffix(exp, "*") {
		return strings.HasPrefix(ns, strings.TrimSuffix(exp, "*"))
	}

	return strings.EqualFold(ns, exp)
}

// Build a map indexed by namespace with in-mesh Pod's labels
func initPodLabelsMap(c analysis.Context) map[string][]klabels.Set {
	podLabelsMap := make(map[string][]klabels.Set)

	c.ForEach(gvk.Pod, func(r *resource.Instance) bool {
		pLabels := klabels.Set(r.Metadata.Labels)

		ns := r.Metadata.FullName.Namespace.String()
		if podLabelsMap[ns] == nil {
			podLabelsMap[ns] = make([]klabels.Set, 0)
		}

		if util.PodInMesh(r, c) {
			podLabelsMap[ns] = append(podLabelsMap[ns], pLabels)
		}

		if util.PodInAmbientMode(r) {
			podLabelsMap[ns] = append(podLabelsMap[ns], pLabels)
		}

		return true
	})

	return podLabelsMap
}
