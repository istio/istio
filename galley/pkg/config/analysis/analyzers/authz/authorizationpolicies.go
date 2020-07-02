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
	"strings"

	v1 "k8s.io/api/core/v1"
	k8s_labels "k8s.io/apimachinery/pkg/labels"

	"istio.io/api/mesh/v1alpha1"
	"istio.io/api/security/v1beta1"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/util"
	"istio.io/istio/galley/pkg/config/analysis/msg"

	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
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
		Inputs: collection.Names{
			collections.IstioMeshV1Alpha1MeshConfig.Name(),
			collections.IstioSecurityV1Beta1Authorizationpolicies.Name(),
			collections.K8SCoreV1Namespaces.Name(),
			collections.K8SCoreV1Pods.Name(),
		},
	}
}

func (a *AuthorizationPoliciesAnalyzer) Analyze(c analysis.Context) {
	podLabelsMap := initPodLabelsMap(c)

	c.ForEach(collections.IstioSecurityV1Beta1Authorizationpolicies.Name(), func(r *resource.Instance) bool {
		a.analyzeNoMatchingWorkloads(r, c, podLabelsMap)
		a.analyzeNamespaceNotFound(r, c)
		return true
	})
}

func (a *AuthorizationPoliciesAnalyzer) analyzeNoMatchingWorkloads(r *resource.Instance, c analysis.Context, podLabelsMap map[string][]k8s_labels.Set) {
	ap := r.Message.(*v1beta1.AuthorizationPolicy)
	apNs := r.Metadata.FullName.Namespace.String()

	// If AuthzPolicy is mesh-wide
	if meshWidePolicy(apNs, c) {
		// If it has selector, need further analysis
		if ap.Selector != nil {
			apSelector := k8s_labels.SelectorFromSet(ap.Selector.MatchLabels)
			// If there is at least one pod matching the selector within the whole mesh
			if !hasMatchingPodsRunning(apSelector, podLabelsMap) {
				c.Report(collections.IstioSecurityV1Beta1Authorizationpolicies.Name(), msg.NewNoMatchingWorkloadsFound(r, apSelector.String()))
			}
		}

		// If AuthzPolicy is mesh-wide and selectorless,
		// no need to keep the analysis
		return
	}

	// If the AuthzPolicy is namespace-wide and there are present Pods,
	// no messages should be triggered.
	if ap.Selector == nil {
		if len(podLabelsMap[apNs]) == 0 {
			c.Report(collections.IstioSecurityV1Beta1Authorizationpolicies.Name(), msg.NewNoMatchingWorkloadsFound(r, ""))
		}
		return
	}

	// If the AuthzPolicy has Selector, then need to find a matching Pod.
	apSelector := k8s_labels.SelectorFromSet(ap.Selector.MatchLabels)
	if !hasMatchingPodsRunningIn(apSelector, podLabelsMap[apNs]) {
		c.Report(collections.IstioSecurityV1Beta1Authorizationpolicies.Name(), msg.NewNoMatchingWorkloadsFound(r, apSelector.String()))
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

	c.ForEach(collections.IstioMeshV1Alpha1MeshConfig.Name(), func(r *resource.Instance) bool {
		meshConfig = r.Message.(*v1alpha1.MeshConfig)
		return r.Metadata.FullName.Name != util.MeshConfigName
	})

	return meshConfig
}

func hasMatchingPodsRunning(selector k8s_labels.Selector, podLabelsMap map[string][]k8s_labels.Set) bool {
	for _, setList := range podLabelsMap {
		if hasMatchingPodsRunningIn(selector, setList) {
			return true
		}
	}
	return false
}

func hasMatchingPodsRunningIn(selector k8s_labels.Selector, setList []k8s_labels.Set) bool {
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

	for _, rule := range ap.Rules {
		for _, from := range rule.From {
			for _, ns := range append(from.Source.Namespaces, from.Source.NotNamespaces...) {
				if !matchNamespace(ns, c) {
					c.Report(collections.IstioSecurityV1Beta1Authorizationpolicies.Name(), msg.NewReferencedResourceNotFound(r, "namespace", ns))
				}
			}
		}
	}
}

func matchNamespace(exp string, c analysis.Context) bool {
	match := false
	c.ForEach(collections.K8SCoreV1Namespaces.Name(), func(r *resource.Instance) bool {
		ns := r.Metadata.FullName.String()
		match = namespaceMatch(ns, exp)
		return !match
	})

	return match
}

func namespaceMatch(ns, exp string) bool {
	match := false
	if strings.EqualFold(exp, "*") {
		match = true
	} else if strings.HasPrefix(exp, "*") {
		match = strings.HasSuffix(ns, strings.TrimPrefix(exp, "*"))
	} else if strings.HasSuffix(exp, "*") {
		match = strings.HasPrefix(ns, strings.TrimSuffix(exp, "*"))
	} else {
		match = strings.EqualFold(ns, exp)
	}

	return match
}

// Build a map indexed by namespace with in-mesh Pod's labels
func initPodLabelsMap(c analysis.Context) map[string][]k8s_labels.Set {
	podLabelsMap := make(map[string][]k8s_labels.Set)

	c.ForEach(collections.K8SCoreV1Pods.Name(), func(r *resource.Instance) bool {
		p := r.Message.(*v1.Pod)
		pLabels := k8s_labels.Set(p.Labels)

		if podLabelsMap[p.Namespace] == nil {
			podLabelsMap[p.Namespace] = make([]k8s_labels.Set, 0)
		}

		if util.PodInMesh(r, c) {
			podLabelsMap[p.Namespace] = append(podLabelsMap[p.Namespace], pLabels)
		}

		return true
	})

	return podLabelsMap
}
