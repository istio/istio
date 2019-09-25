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
	"istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/processor/metadata"
	"istio.io/istio/galley/pkg/config/resource"
)

// GatewayAnalyzer checks the gateways associated with each virtual service
type Analyzer struct{}

var _ analysis.Analyzer = &Analyzer{}

// Metadata implements analysis.Analyzer
func (*Analyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name: "gateway.GatewayAnalyzer",
		Inputs: collection.Names{
			metadata.IstioNetworkingV1Alpha3Gateways,
			metadata.K8SCoreV1Pods,
		},
	}
}

// Analyze implements Analyzer
func (s *Analyzer) Analyze(c analysis.Context) {
	c.ForEach(metadata.IstioNetworkingV1Alpha3Gateways, func(r *resource.Entry) bool {
		s.analyzeGateway(r, c)
		return true
	})
}

func (*Analyzer) analyzeGateway(r *resource.Entry, c analysis.Context) {

	gw := r.Item.(*v1alpha3.Gateway)

	// pod container port exposed by the gateway workload selector.  All the istio
	// ingress pods matching a selector SHOULD offer the same ports.  This validator
	// does not check for the case where they don't.
	podPorts := map[uint32]bool{}
	selectorMatches := 0

	// Find pods selected by gw.Selector and extract their container ports
	gwSelector := k8s_labels.SelectorFromSet(gw.Selector)
	c.ForEach(metadata.K8SCoreV1Pods, func(r *resource.Entry) bool {
		pod := r.Item.(*v1.Pod)
		podLabels := k8s_labels.Set(pod.ObjectMeta.Labels)
		if gwSelector.Matches(podLabels) {
			selectorMatches++
			for _, container := range pod.Spec.Containers {
				for _, port := range container.Ports {
					podPorts[uint32(port.ContainerPort)] = true
				}
			}
		}
		return true
	})

	if selectorMatches == 0 {
		c.Report(metadata.IstioNetworkingV1Alpha3Gateways, msg.NewReferencedResourceNotFound(r, "selector", gwSelector.String()))
		return
	}

	// Check each Gateway port against what the pod offers
	for _, server := range gw.Servers {
		if server.Port != nil {
			_, ok := podPorts[server.Port.Number]
			if !ok {
				c.Report(metadata.IstioNetworkingV1Alpha3Gateways, msg.NewGatewayPortNotOnWorkload(r, gwSelector.String(), int(server.Port.Number)))
			}
		}
	}
}
