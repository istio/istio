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
	klabels "k8s.io/apimachinery/pkg/labels"

	"istio.io/api/networking/v1alpha3"
	"istio.io/api/security/v1beta1"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/util"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/jwt"
)

type JWTClaimRouteAnalyzer struct{}

var _ analysis.Analyzer = &JWTClaimRouteAnalyzer{}

// Metadata implements Analyzer
func (s *JWTClaimRouteAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "virtualservice.JWTClaimRouteAnalyzer",
		Description: "Checks the VirtualService using JWT claim based routing has corresponding RequestAuthentication",
		Inputs: []config.GroupVersionKind{
			gvk.VirtualService,
			gvk.RequestAuthentication,
			gvk.Gateway,
			gvk.Pod,
		},
	}
}

// Analyze implements Analyzer
func (s *JWTClaimRouteAnalyzer) Analyze(c analysis.Context) {
	requestAuthNByNamespace := map[string][]klabels.Selector{}
	c.ForEach(gvk.RequestAuthentication, func(r *resource.Instance) bool {
		ns := r.Metadata.FullName.Namespace.String()
		if _, found := requestAuthNByNamespace[ns]; !found {
			requestAuthNByNamespace[ns] = []klabels.Selector{}
		}
		ra := r.Message.(*v1beta1.RequestAuthentication)
		raSelector := klabels.SelectorFromSet(ra.GetSelector().GetMatchLabels())
		requestAuthNByNamespace[ns] = append(requestAuthNByNamespace[ns], raSelector)
		return true
	})

	c.ForEach(gvk.VirtualService, func(r *resource.Instance) bool {
		s.analyze(r, c, requestAuthNByNamespace)
		return true
	})
}

func (s *JWTClaimRouteAnalyzer) analyze(r *resource.Instance, c analysis.Context, requestAuthNByNamespace map[string][]klabels.Selector) {
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
		gwRes := c.Find(gvk.Gateway, gwFullName)
		if gwRes == nil {
			// The gateway does not exist, this should already be covered by the gateway analyzer.
			continue
		}

		gw := gwRes.Message.(*v1alpha3.Gateway)
		gwSelector := klabels.SelectorFromSet(gw.Selector)

		// Check each pod selected by the gateway.
		c.ForEach(gvk.Pod, func(rPod *resource.Instance) bool {
			podLabels := klabels.Set(rPod.Metadata.Labels)
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
				c.Report(gvk.VirtualService, m)
			}
			return true
		})
	}
}

func routeBasedOnJWTClaimKey(vs *v1alpha3.VirtualService) string {
	for _, httpRoute := range vs.GetHttp() {
		for _, match := range httpRoute.GetMatch() {
			for key := range match.GetHeaders() {
				if jwt.ToRoutingClaim(key).Match {
					return key
				}
			}
			for key := range match.GetWithoutHeaders() {
				if jwt.ToRoutingClaim(key).Match {
					return key
				}
			}
		}
	}
	return ""
}
