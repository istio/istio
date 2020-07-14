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

package gateway

import (
	v1 "k8s.io/api/core/v1"
	k8s_labels "k8s.io/apimachinery/pkg/labels"

	"istio.io/api/networking/v1alpha3"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

// IngressGatewayPortAnalyzer checks a gateway's ports against the gateway's Kubernetes service ports.
type IngressGatewayPortAnalyzer struct{}

// (compile-time check that we implement the interface)
var _ analysis.Analyzer = &IngressGatewayPortAnalyzer{}

// Metadata implements analysis.Analyzer
func (*IngressGatewayPortAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "gateway.IngressGatewayPortAnalyzer",
		Description: "Checks a gateway's ports against the gateway's Kubernetes service ports",
		Inputs: collection.Names{
			collections.IstioNetworkingV1Alpha3Gateways.Name(),
			collections.K8SCoreV1Pods.Name(),
			collections.K8SCoreV1Services.Name(),
		},
	}
}

// Analyze implements analysis.Analyzer
func (s *IngressGatewayPortAnalyzer) Analyze(c analysis.Context) {
	c.ForEach(collections.IstioNetworkingV1Alpha3Gateways.Name(), func(r *resource.Instance) bool {
		s.analyzeGateway(r, c)
		return true
	})
}

func (*IngressGatewayPortAnalyzer) analyzeGateway(r *resource.Instance, c analysis.Context) {

	gw := r.Message.(*v1alpha3.Gateway)

	// Typically there will be a single istio-ingressgateway service, which will select
	// the same ingress gateway pod workload as the Gateway resource.  If there are multiple
	// Kubernetes services, and they offer different TCP port combinations, this validator will
	// not report a problem if *any* selecting service exposes the Gateway's port.
	servicePorts := map[uint32]bool{}
	gwSelectorMatches := 0

	// For pods selected by gw.Selector, find Services that select them and remember those ports
	gwSelector := k8s_labels.SelectorFromSet(gw.Selector)
	c.ForEach(collections.K8SCoreV1Pods.Name(), func(rPod *resource.Instance) bool {
		pod := rPod.Message.(*v1.Pod)
		podLabels := k8s_labels.Set(pod.ObjectMeta.Labels)
		if gwSelector.Matches(podLabels) {
			gwSelectorMatches++
			c.ForEach(collections.K8SCoreV1Services.Name(), func(rSvc *resource.Instance) bool {
				nsSvc := string(rSvc.Metadata.FullName.Namespace)
				if nsSvc != pod.ObjectMeta.Namespace {
					return true // Services only select pods in their namespace
				}

				service := rSvc.Message.(*v1.ServiceSpec)
				// TODO I want to match service.Namespace to pod.ObjectMeta.Namespace
				svcSelector := k8s_labels.SelectorFromSet(service.Selector)
				if svcSelector.Matches(podLabels) {
					for _, port := range service.Ports {
						if port.Protocol == "TCP" {
							servicePorts[uint32(port.Port)] = true
						}
					}
				}
				return true
			})
		}
		return true
	})

	// Report if we found no pods matching this gateway's selector
	if gwSelectorMatches == 0 {
		c.Report(collections.IstioNetworkingV1Alpha3Gateways.Name(), msg.NewReferencedResourceNotFound(r, "selector", gwSelector.String()))
		return
	}

	// Check each Gateway port against what the workload ingress service offers
	for _, server := range gw.Servers {
		if server.Port != nil {
			_, ok := servicePorts[server.Port.Number]
			if !ok {
				c.Report(collections.IstioNetworkingV1Alpha3Gateways.Name(), msg.NewGatewayPortNotOnWorkload(r, gwSelector.String(), int(server.Port.Number)))
			}
		}
	}
}
