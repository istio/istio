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

package virtualservice

import (
	"strings"

	k8s_labels "k8s.io/apimachinery/pkg/labels"

	"istio.io/api/networking/v1alpha3"
	"istio.io/api/security/v1beta1"
	"istio.io/istio/pilot/pkg/util/constant"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/util"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

type JWTClaimRouteAnalyzer struct{}

var _ analysis.Analyzer = &JWTClaimRouteAnalyzer{}

// Metadata implements Analyzer
func (s *JWTClaimRouteAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "virtualservice.JWTClaimRouteAnalyzer",
		Description: "Checks the VirtualService using JWT claim based routing has corresponding RequestAuthentication",
		Inputs: collection.Names{
			collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
			collections.IstioSecurityV1Beta1Requestauthentications.Name(),
			collections.IstioNetworkingV1Alpha3Gateways.Name(),
			collections.K8SCoreV1Pods.Name(),
		},
	}
}

// Analyze implements Analyzer
func (s *JWTClaimRouteAnalyzer) Analyze(c analysis.Context) {
	requestAuthNByNamespace := map[string][]k8s_labels.Selector{}
	c.ForEach(collections.IstioSecurityV1Beta1Requestauthentications.Name(), func(r *resource.Instance) bool {
		ns := r.Metadata.FullName.Namespace.String()
		if _, found := requestAuthNByNamespace[ns]; !found {
			requestAuthNByNamespace[ns] = []k8s_labels.Selector{}
		}
		ra := r.Message.(*v1beta1.RequestAuthentication)
		raSelector := k8s_labels.SelectorFromSet(ra.GetSelector().GetMatchLabels())
		requestAuthNByNamespace[ns] = append(requestAuthNByNamespace[ns], raSelector)
		return true
	})

	c.ForEach(collections.IstioNetworkingV1Alpha3Virtualservices.Name(), func(r *resource.Instance) bool {
		s.analyze(r, c, requestAuthNByNamespace)
		return true
	})
}

func (s *JWTClaimRouteAnalyzer) analyze(r *resource.Instance, c analysis.Context, requestAuthNByNamespace map[string][]k8s_labels.Selector) {
	// Check if the virtual service is using JWT claim based routing.
	vs := r.Message.(*v1alpha3.VirtualService)
	var vsRouteKey string
	if vsRouteKey = routeBasedOnJWTClaimKey(vs); vsRouteKey == "" {
		return
	}
	vsNs := r.Metadata.FullName.Namespace

	// Check if the virtual service is applied to gateway.
	for _, gwName := range vs.Gateways {
		if gwName == util.MeshGateway {
			continue
		}

		gwFullName := resource.NewShortOrFullName(vsNs, gwName)
		gwRes := c.Find(collections.IstioNetworkingV1Alpha3Gateways.Name(), gwFullName)
		if gwRes == nil {
			// The gateway does not exist, this should already be covered by the gateway analyzer.
			continue
		}

		gw := gwRes.Message.(*v1alpha3.Gateway)
		gwSelector := k8s_labels.SelectorFromSet(gw.Selector)

		// Check each pod selected by the gateway.
		c.ForEach(collections.K8SCoreV1Pods.Name(), func(rPod *resource.Instance) bool {
			podLabels := k8s_labels.Set(rPod.Metadata.Labels)
			if !gwSelector.Matches(podLabels) {
				return true
			}

			// Check if there is request authentication applied to the pod.
			var hasRequestAuthNForPod bool

			raSelectors := requestAuthNByNamespace[constants.IstioSystemNamespace]
			raSelectors = append(raSelectors, requestAuthNByNamespace[rPod.Metadata.FullName.Namespace.String()]...)
			for _, raSelector := range raSelectors {
				if raSelector.Matches(podLabels) {
					hasRequestAuthNForPod = true
					break
				}
			}
			if !hasRequestAuthNForPod {
				m := msg.NewJwtClaimBasedRoutingWithoutRequestAuthN(r, vsRouteKey, gwFullName.String(), rPod.Metadata.FullName.Name.String())
				c.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(), m)
			}
			return true
		})
	}
}

func routeBasedOnJWTClaimKey(vs *v1alpha3.VirtualService) string {
	for _, httpRoute := range vs.GetHttp() {
		for _, match := range httpRoute.GetMatch() {
			for key := range match.GetHeaders() {
				if strings.HasPrefix(key, constant.HeaderJWTClaim) {
					return key
				}
			}
			for key := range match.GetWithoutHeaders() {
				if strings.HasPrefix(key, constant.HeaderJWTClaim) {
					return key
				}
			}
		}
	}
	return ""
}
