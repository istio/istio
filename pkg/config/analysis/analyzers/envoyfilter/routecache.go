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

package envoyfilter

import (
	"fmt"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/api/security/v1beta1"
	type_beta "istio.io/api/type/v1beta1"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/util"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
)

// RouteCacheAnalyzer warns when EnvoyFilters may interact with route-dependent authorization.
type RouteCacheAnalyzer struct{}

var _ analysis.Analyzer = &RouteCacheAnalyzer{}

// Metadata implements analysis.Analyzer.
func (*RouteCacheAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "envoyfilter.RouteCacheAnalyzer",
		Description: "Checks for EnvoyFilters used with route-dependent authorization",
		Inputs: []config.GroupVersionKind{
			gvk.EnvoyFilter,
			gvk.AuthorizationPolicy,
		},
	}
}

// Analyze implements analysis.Analyzer.
func (*RouteCacheAnalyzer) Analyze(c analysis.Context) {
	hasHTTPRoutePolicy := false
	c.ForEach(gvk.AuthorizationPolicy, func(r *resource.Instance) bool {
		ap, ok := r.Message.(*v1beta1.AuthorizationPolicy)
		if ok && hasHTTPRouteTargetRef(ap) {
			hasHTTPRoutePolicy = true
			return false
		}
		return true
	})
	if !hasHTTPRoutePolicy {
		return
	}

	c.ForEach(gvk.EnvoyFilter, func(r *resource.Instance) bool {
		ef, ok := r.Message.(*networking.EnvoyFilter)
		if !ok {
			return true
		}
		for i, patch := range ef.ConfigPatches {
			if patch.GetApplyTo() != networking.EnvoyFilter_HTTP_FILTER {
				continue
			}
			switch patch.GetMatch().GetContext() {
			case networking.EnvoyFilter_SIDECAR_INBOUND, networking.EnvoyFilter_SIDECAR_OUTBOUND:
				continue
			}

			message := msg.NewEnvoyFilterMayClearRouteCache(r)
			if line, ok := util.ErrorLine(r, fmt.Sprintf(util.EnvoyFilterConfigPath, i)); ok {
				message.Line = line
			}
			c.Report(gvk.EnvoyFilter, message)
			break
		}
		return true
	})
}

func hasHTTPRouteTargetRef(ap *v1beta1.AuthorizationPolicy) bool {
	if isHTTPRouteTargetRef(ap.GetTargetRef()) {
		return true
	}
	for _, ref := range ap.GetTargetRefs() {
		if isHTTPRouteTargetRef(ref) {
			return true
		}
	}
	return false
}

func isHTTPRouteTargetRef(ref *type_beta.PolicyTargetReference) bool {
	if ref == nil {
		return false
	}
	return ref.GetKind() == gvk.HTTPRoute.Kind &&
		config.CanonicalGroup(ref.GetGroup()) == gvk.HTTPRoute.CanonicalGroup()
}
