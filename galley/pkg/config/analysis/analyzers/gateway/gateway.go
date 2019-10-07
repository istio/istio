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

package gateway

import (
	v1 "k8s.io/api/core/v1"
	k8s_labels "k8s.io/apimachinery/pkg/labels"

	"istio.io/api/networking/v1alpha3"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/meta/metadata"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/resource"
)

// IngressGatewayPortAnalyzer checks a Gateway's ports against the gateway's K8s Service ports.
type IngressGatewayPortAnalyzer struct{}

var (
	// The ports from install/kubernetes/istio.yaml's istio-ingressgateway service.
	// Use this only if we validate a Gateway that selects the system gateway and
	// the user doesn't supply it.
	defaultIngressGatewayPorts = map[uint32]bool{
		80:    true,
		443:   true,
		31400: true,
		15443: true,
	}

	// (compile-time check that we implement the interface)
	_ analysis.Analyzer = &IngressGatewayPortAnalyzer{}
)

// Metadata implements analysis.Analyzer
func (*IngressGatewayPortAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name: "gateway.IngressGatewayPortAnalyzer",
		Inputs: collection.Names{
			metadata.IstioNetworkingV1Alpha3Gateways,
			metadata.K8SCoreV1Pods,
			metadata.K8SCoreV1Services,
		},
	}
}

// Analyze implements analysis.Analyzer
func (s *IngressGatewayPortAnalyzer) Analyze(c analysis.Context) {
	c.ForEach(metadata.IstioNetworkingV1Alpha3Gateways, func(r *resource.Entry) bool {
		s.analyzeGateway(r, c)
		return true
	})
}

func (*IngressGatewayPortAnalyzer) analyzeGateway(r *resource.Entry, c analysis.Context) {

	gw := r.Item.(*v1alpha3.Gateway)

	// Typically there will be a single istio-ingressgateway service, which will select
	// the same ingress gateway pod workload as the Gateway resource.  If there are multiple
	// Kubernetes services, and they offer different TCP port combinations, this validator will
	// not report a problem if *any* selecting service exposes the Gateway's port.
	servicePorts := map[uint32]bool{}
	gwSelectorMatches := 0

	// For pods selected by gw.Selector, find Services that select them and remember those ports
	gwSelector := k8s_labels.SelectorFromSet(gw.Selector)
	c.ForEach(metadata.K8SCoreV1Pods, func(rPod *resource.Entry) bool {
		pod := rPod.Item.(*v1.Pod)
		podLabels := k8s_labels.Set(pod.ObjectMeta.Labels)
		if gwSelector.Matches(podLabels) {
			gwSelectorMatches++
			c.ForEach(metadata.K8SCoreV1Services, func(rSvc *resource.Entry) bool {
				nsSvc, _ := rSvc.Metadata.Name.InterpretAsNamespaceAndName()
				if nsSvc != pod.ObjectMeta.Namespace {
					return true // Services only select pods in their namespace
				}

				service := rSvc.Item.(*v1.ServiceSpec)
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

	if gwSelectorMatches == 0 {
		// We found no service for the Gateway's workload selector.  If the Gateway doesn't select
		// the Istio system ingress gateway complain about a missing referenced resource.  (We
		// don't want to complain about missing system resources, because a user may want to analyze
		// only his own application files.)
		if len(gw.Selector) != 1 || gw.Selector["istio"] != "ingressgateway" {
			c.Report(metadata.IstioNetworkingV1Alpha3Gateways, msg.NewReferencedResourceNotFound(r, "selector", gwSelector.String()))
			return
		}
		// The unreferenced Ingress is the System ingress, pretend we have found it.
		servicePorts = defaultIngressGatewayPorts
	}

	// Check each Gateway port against what the workload ingress service offers
	for _, server := range gw.Servers {
		if server.Port != nil {
			_, ok := servicePorts[server.Port.Number]
			if !ok {
				c.Report(metadata.IstioNetworkingV1Alpha3Gateways, msg.NewGatewayPortNotOnWorkload(r, gwSelector.String(), int(server.Port.Number)))
			}
		}
	}
}
