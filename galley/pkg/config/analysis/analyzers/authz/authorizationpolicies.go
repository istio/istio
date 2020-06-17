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

package authz

import (
	"strings"

	v1 "k8s.io/api/core/v1"
	k8s_labels "k8s.io/apimachinery/pkg/labels"

	"istio.io/api/annotation"
	"istio.io/api/mesh/v1alpha1"
	"istio.io/api/networking/v1alpha3"
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

var _ analysis.Analyzer = &AuthorizationPoliciesAnalyzer{}
var meshConfig *v1alpha1.MeshConfig

func (a *AuthorizationPoliciesAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "auth.AuthorizationPoliciesAnalyzer",
		Description: "Checks the validity of authorization policies",
		Inputs: collection.Names{
			collections.IstioMeshV1Alpha1MeshConfig.Name(),
			collections.IstioNetworkingV1Alpha3Serviceentries.Name(),
			collections.IstioSecurityV1Beta1Authorizationpolicies.Name(),
			collections.K8SCoreV1Namespaces.Name(),
			collections.K8SCoreV1Pods.Name(),
			collections.K8SCoreV1Services.Name(),
		},
	}
}

func (a *AuthorizationPoliciesAnalyzer) Analyze(c analysis.Context) {
	serviceEntryHosts := util.InitServiceEntryHostMap(c)
	podLabelsMap := initPodLabelsMap(c)

	c.ForEach(collections.IstioSecurityV1Beta1Authorizationpolicies.Name(), func(r *resource.Instance) bool {
		a.analyzeNoMatchingWorkloads(r, c, podLabelsMap)
		a.analyzeNamespaceNotFound(r, c)
		a.analyzeHostNotFound(r, c, serviceEntryHosts)
		return true
	})
}

func (a *AuthorizationPoliciesAnalyzer) analyzeNoMatchingWorkloads(r *resource.Instance, c analysis.Context, podLabelsMap map[string][]k8s_labels.Set) {
	ap := r.Message.(*v1beta1.AuthorizationPolicy)
	apNs := r.Metadata.FullName.Namespace.String()

	// If AuthzPolicy is mesh-wide
	if meshWidePolicy(apNs, c) {
		// If it has selector, need further analysis
		if !selectorLess(ap) {
			apSelector := k8s_labels.SelectorFromSet(ap.Selector.MatchLabels)
			// If there is at least one pod matching the selector within the whole mesh
			if !hasMatchingPodsRunning(apSelector, podLabelsMap) {
				c.Report(collections.IstioSecurityV1Beta1Authorizationpolicies.Name(), msg.NewNoMatchingWorkloadsFound(r, ""))
			}
		}

		// If AuthzPolicy is mesh-wide and selectorless,
		// no need to keep the analysis
		return
	}

	// If the AuthzPolicy is namespace-wide and there are present Pods,
	// no messages should be triggered.
	if ap.Selector == nil {
		if !hasAnyPodRunning(apNs, podLabelsMap) {
			c.Report(collections.IstioSecurityV1Beta1Authorizationpolicies.Name(), msg.NewNoMatchingWorkloadsFound(r, ""))
		}
		return
	}

	// If the AuthzPolicy has Selector, then need to find a matching Pod.
	apSelector := k8s_labels.SelectorFromSet(ap.Selector.MatchLabels)
	if !hasMatchingPodsRunning(apSelector, podLabelsMap) {
		c.Report(collections.IstioSecurityV1Beta1Authorizationpolicies.Name(), msg.NewNoMatchingWorkloadsFound(r, apSelector.String()))
	}
}

// Returns true when the selector is nil
func selectorLess(ap *v1beta1.AuthorizationPolicy) bool {
	return ap.Selector == nil
}

// Returns true when the namespace is the root namespace
// It takes the MeshConfig names istio, if not the last instance found.
func meshWidePolicy(ns string, c analysis.Context) bool {
	mConf := fetchMeshConfig(c)
	return (mConf != nil && ns == mConf.GetRootNamespace()) || false
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

func hasAnyPodRunning(ns string, podLabelsMap map[string][]k8s_labels.Set) bool {
	return len(podLabelsMap[ns]) > 0
}

func (a *AuthorizationPoliciesAnalyzer) analyzeNamespaceNotFound(r *resource.Instance, c analysis.Context) {
	ap := r.Message.(*v1beta1.AuthorizationPolicy)

	for _, rule := range ap.Rules {
		for _, from := range rule.From {
			for _, ns := range from.Source.Namespaces {
				if !c.Exists(collections.K8SCoreV1Namespaces.Name(), resource.NewFullName("", resource.LocalName(ns))) {
					c.Report(collections.IstioSecurityV1Beta1Authorizationpolicies.Name(), msg.NewReferencedResourceNotFound(r, "namespace", ns))
				}
			}
		}
	}
}

func (a *AuthorizationPoliciesAnalyzer) analyzeHostNotFound(r *resource.Instance, c analysis.Context,
	serviceEntryHosts map[util.ScopedFqdn]*v1alpha3.ServiceEntry) {
	ap := r.Message.(*v1beta1.AuthorizationPolicy)
	for _, rule := range ap.Rules {
		for _, to := range rule.To {
			for _, host := range to.Operation.Hosts {
				// Check if the host is either a Service or a Service Entry
				if se := util.GetDestinationHost(r.Metadata.FullName.Namespace, host, serviceEntryHosts); se == nil {
					c.Report(collections.IstioSecurityV1Beta1Authorizationpolicies.Name(), msg.NewNoHostFound(r, host))
				}
			}
		}
	}
}

// Whether the pod is part of the mesh or not
func podInMesh(r *resource.Instance) bool {
	p := r.Message.(*v1.Pod)

	if val := p.GetAnnotations()[annotation.SidecarInject.Name]; strings.EqualFold(val, "false") {
		return false
	}

	proxyImage := ""
	for _, container := range p.Spec.Containers {
		if container.Name == util.IstioProxyName {
			proxyImage = container.Image
			break
		}
	}

	return proxyImage != ""
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

		if podInMesh(r) {
			podLabelsMap[p.Namespace] = append(podLabelsMap[p.Namespace], pLabels)
		}

		return true
	})

	return podLabelsMap
}
