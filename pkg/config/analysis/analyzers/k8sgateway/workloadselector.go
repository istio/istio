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

package k8sgateway

import (
	"istio.io/api/label"
	typev1beta1 "istio.io/api/type/v1beta1"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/maps"
)

var _ analysis.Analyzer = &SelectorAnalyzer{}

type SelectorAnalyzer struct{}

var policyGVKs = []config.GroupVersionKind{
	gvk.AuthorizationPolicy,
	gvk.RequestAuthentication,
	gvk.Telemetry,
	gvk.WasmPlugin,
}

type policy interface {
	GetSelector() *typev1beta1.WorkloadSelector
	GetTargetRef() *typev1beta1.PolicyTargetReference
}

func (w *SelectorAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "k8sgateway.SelectorAnalyzer",
		Description: "Check that selectors are effective for Kubernetes gateway",
		Inputs: []config.GroupVersionKind{
			gvk.AuthorizationPolicy,
			gvk.RequestAuthentication,
			gvk.Telemetry,
			gvk.WasmPlugin,
			gvk.Pod,
		},
	}
}

// Analyze implements analysis.Analyzer
func (w *SelectorAnalyzer) Analyze(context analysis.Context) {
	pods := gatewayPodsLabelMap(context)

	handleResource := func(r *resource.Instance, gvkType config.GroupVersionKind) {
		spec, ok := r.Message.(policy)
		if spec.GetTargetRef() != nil {
			return
		}
		if !ok || spec.GetSelector() == nil {
			return
		}
		selector := spec.GetSelector()
		for _, pod := range pods[r.Metadata.FullName.Namespace.String()] {
			if maps.Contains(pod, selector.MatchLabels) {
				context.Report(gvkType, msg.NewIneffectiveSelector(r, pod[label.IoK8sNetworkingGatewayGatewayName.Name]))
			}
		}
	}

	for _, gvkType := range policyGVKs {
		context.ForEach(gvkType, func(r *resource.Instance) bool {
			handleResource(r, gvkType)
			return true
		})
	}
}

// gatewayPodsLabelMap returns a map of pod namespaces to labels for all pods with a gateway label
func gatewayPodsLabelMap(context analysis.Context) map[string][]map[string]string {
	pods := make(map[string][]map[string]string)
	context.ForEach(gvk.Pod, func(resource *resource.Instance) bool {
		if _, ok := resource.Metadata.Labels[label.IoK8sNetworkingGatewayGatewayName.Name]; !ok {
			return true
		}
		ns := resource.Metadata.FullName.Namespace.String()
		pods[ns] = append(pods[ns], resource.Metadata.Labels)
		return true
	})
	return pods
}
