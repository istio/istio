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
	"strings"

	"google.golang.org/protobuf/types/known/structpb"
	"k8s.io/apimachinery/pkg/api/meta"
	k8s "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	network "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/kube/gatewaycommon"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/util"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/wellknown"
)

// ExtProcNameCollisionAnalyzer warns when an EnvoyFilter adds a gRPC ext_proc filter named like the
// one Istio generates for InferencePools. Per-route settings are resolved by filter name, so on
// routes to the pool the filter gets the endpoint picker settings, or is disabled.
type ExtProcNameCollisionAnalyzer struct{}

var _ analysis.Analyzer = &ExtProcNameCollisionAnalyzer{}

// Metadata implements analysis.Analyzer
func (*ExtProcNameCollisionAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name: "envoyfilter.ExtProcNameCollisionAnalyzer",
		Description: "Checks for EnvoyFilters that add a gRPC ext_proc filter under the name Istio uses " +
			"for InferencePool ext_proc on a Gateway that serves an InferencePool",
		Inputs: []config.GroupVersionKind{gvk.InferencePool, gvk.HTTPRoute, gvk.EnvoyFilter, gvk.MeshConfig},
	}
}

// Analyze implements analysis.Analyzer
func (*ExtProcNameCollisionAnalyzer) Analyze(c analysis.Context) {
	pools := sets.New[string]()
	c.ForEach(gvk.InferencePool, func(r *resource.Instance) bool {
		pools.Insert(r.Metadata.FullName.String())
		return true
	})
	if pools.IsEmpty() {
		return
	}
	gwPools := gatewayPools(c, pools)
	if len(gwPools) == 0 {
		return
	}

	rootNS := constants.IstioSystemNamespace
	c.ForEach(gvk.MeshConfig, func(r *resource.Instance) bool {
		if mc, ok := r.Message.(*meshconfig.MeshConfig); ok && mc.GetRootNamespace() != "" {
			rootNS = mc.GetRootNamespace()
		}
		return r.Metadata.FullName.Name != util.MeshConfigName
	})

	c.ForEach(gvk.EnvoyFilter, func(r *resource.Instance) bool {
		ef, ok := r.Message.(*network.EnvoyFilter)
		if !ok {
			return true
		}
		for i, cp := range ef.ConfigPatches {
			target, ok := userExtProcTarget(cp)
			if !ok {
				continue
			}
			for _, gw := range extProcGateways(ef, r.Metadata.FullName.Namespace.String(), rootNS, gwPools) {
				message := msg.NewEnvoyFilterExtProcNameCollision(r, target, gw, strings.Join(sets.SortedList(gwPools[gw]), ", "))
				if line, ok := util.ErrorLine(r, fmt.Sprintf(util.EnvoyFilterConfigName, i)); ok {
					message.Line = line
				}
				c.Report(gvk.EnvoyFilter, message)
			}
			break
		}
		return true
	})
}

// gatewayPools returns, for each Gateway (ns/name), the InferencePools its HTTPRoutes send traffic to.
func gatewayPools(c analysis.Context, pools sets.Set[string]) map[string]sets.Set[string] {
	controllers := sets.New[k8s.GatewayController]()
	for _, controller := range gatewaycommon.BuiltinGatewayClasses {
		controllers.Insert(controller)
	}
	res := map[string]sets.Set[string]{}
	c.ForEach(gvk.HTTPRoute, func(r *resource.Instance) bool {
		spec, ok := r.Message.(*k8s.HTTPRouteSpec)
		if !ok {
			return true
		}
		routeNS := r.Metadata.FullName.Namespace.String()
		routePools := sets.New[string]()
		for _, rule := range spec.Rules {
			for _, b := range rule.BackendRefs {
				key := nsOr(b.Namespace, routeNS) + "/" + string(b.Name)
				// istiod drops backends with weight 0
				if pools.Contains(key) && string(ptr.OrEmpty(b.Group)) == gvk.InferencePool.Group &&
					string(ptr.OrEmpty(b.Kind)) == gvk.InferencePool.Kind && ptr.OrDefault(b.Weight, 1) != 0 {
					routePools.Insert(key)
				}
			}
		}
		if routePools.IsEmpty() {
			return true
		}
		var gws []string
		if st, ok := r.Status.(*k8s.HTTPRouteStatus); ok && len(st.Parents) > 0 {
			// istiod builds routes, and the pool's per-route settings, only for parents that accept the route
			// (filteredReferences in route_collections.go). Unlike istiod, this keeps only Gateway parents.
			for _, p := range st.Parents {
				if controllers.Contains(p.ControllerName) && isGatewayRef(p.ParentRef.Group, p.ParentRef.Kind) &&
					!meta.IsStatusConditionFalse(p.Conditions, string(k8s.RouteConditionAccepted)) {
					gws = append(gws, nsOr(p.ParentRef.Namespace, routeNS)+"/"+string(p.ParentRef.Name))
				}
			}
		} else { // no status (manifests usually have none): use the parentRefs; the Gateway's class is not checked here
			for _, p := range spec.ParentRefs {
				if isGatewayRef(p.Group, p.Kind) {
					gws = append(gws, nsOr(p.Namespace, routeNS)+"/"+string(p.Name))
				}
			}
		}
		for _, gw := range gws {
			if res[gw] == nil {
				res[gw] = sets.New[string]()
			}
			res[gw].Merge(routePools)
		}
		return true
	})
	return res
}

// extProcGateways returns the pool-serving Gateways an EnvoyFilter applies to. A selector
// with labels other than the gateway name cannot be attributed and is skipped.
func extProcGateways(ef *network.EnvoyFilter, ns, rootNS string, gwPools map[string]sets.Set[string]) []string {
	labels := ef.GetWorkloadSelector().GetLabels()
	refs := ef.GetTargetRefs()
	res := sets.New[string]()
	switch {
	// Like istiod's ShouldAttachPolicy: targetRefs, when present, win over the selector.
	case len(refs) > 0:
		for _, ref := range refs {
			if config.CanonicalGroup(ref.GetGroup()) == gvk.KubernetesGateway.CanonicalGroup() &&
				ref.GetKind() == gvk.KubernetesGateway.Kind &&
				(ref.GetNamespace() == "" || ref.GetNamespace() == ns) {
				res.Insert(ns + "/" + ref.GetName())
			}
		}
	case len(labels) > 0:
		name, ok := labels[label.IoK8sNetworkingGatewayGatewayName.Name]
		if !ok || len(labels) > 1 {
			return nil
		}
		if ns != rootNS {
			res.Insert(ns + "/" + name)
			break
		}
		for gw := range gwPools {
			if strings.HasSuffix(gw, "/"+name) {
				res.Insert(gw)
			}
		}
	default: // applies to every workload in its namespace, or in the mesh from the root namespace
		for gw := range gwPools {
			if ns == rootNS || strings.HasPrefix(gw, ns+"/") {
				res.Insert(gw)
			}
		}
	}
	var gws []string
	for gw := range res {
		if gwPools[gw] != nil {
			gws = append(gws, gw)
		}
	}
	return gws
}

// userExtProcTarget returns the gRPC target of a gRPC ext_proc HTTP filter that a patch inserts into a
// Gateway's filter chain under the reserved name. Other filter types, contexts and operations are skipped.
func userExtProcTarget(cp *network.EnvoyFilter_EnvoyConfigObjectPatch) (string, bool) {
	// Gateway proxies apply only GATEWAY and ANY patches (patchListeners in listener_builder.go).
	if ctx := cp.GetMatch().GetContext(); cp.GetApplyTo() != network.EnvoyFilter_HTTP_FILTER ||
		(ctx != network.EnvoyFilter_ANY && ctx != network.EnvoyFilter_GATEWAY) {
		return "", false
	}
	switch cp.GetPatch().GetOperation() {
	case network.EnvoyFilter_Patch_INSERT_BEFORE, network.EnvoyFilter_Patch_INSERT_FIRST:
	case network.EnvoyFilter_Patch_INSERT_AFTER:
		// Without a subFilter, INSERT_AFTER appends like ADD; after the router, it fails the same way.
		sf := cp.GetMatch().GetListener().GetFilterChain().GetFilter().GetSubFilter()
		if sf == nil || sf.GetName() == wellknown.Router {
			return "", false
		}
	default:
		// ADD appends after the router, and Envoy rejects a filter chain whose router is not last.
		// MERGE, REPLACE and REMOVE act on a filter that already exists.
		return "", false
	}
	val := cp.GetPatch().GetValue()
	if field(val, "name").GetStringValue() != wellknown.HTTPExternalProcessing {
		return "", false
	}
	tc := field(val, "typed_config", "typedConfig").GetStructValue()
	typeURL := field(tc, "@type").GetStringValue()
	// Envoy reads a TypedStruct as the type it wraps.
	if t := typeName(typeURL); t == "udpa.type.v1.TypedStruct" || t == "xds.type.v3.TypedStruct" {
		typeURL = field(tc, "type_url", "typeUrl").GetStringValue()
		tc = field(tc, "value").GetStructValue()
	}
	if typeName(typeURL) != "envoy.extensions.filters.http.ext_proc.v3.ExternalProcessor" {
		return "", false
	}
	// Envoy rejects a gRPC service without a target_uri or cluster_name.
	grpc := field(tc, "grpc_service", "grpcService").GetStructValue()
	target := field(field(grpc, "google_grpc", "googleGrpc").GetStructValue(), "target_uri", "targetUri").GetStringValue()
	if target == "" {
		target = field(field(grpc, "envoy_grpc", "envoyGrpc").GetStructValue(), "cluster_name", "clusterName").GetStringValue()
	}
	return target, target != ""
}

// typeName returns the message name in a type URL, the part after the last slash.
func typeName(url string) string {
	return url[strings.LastIndex(url, "/")+1:]
}

// field returns the value of the first key present; protobuf JSON accepts snake_case and lowerCamelCase names.
func field(s *structpb.Struct, keys ...string) *structpb.Value {
	for _, key := range keys {
		if v, ok := s.GetFields()[key]; ok {
			return v
		}
	}
	return nil
}

// isGatewayRef reports whether a parent reference names a Gateway; nil group and kind are the defaults.
func isGatewayRef(group *k8s.Group, kind *k8s.Kind) bool {
	return (group == nil || string(*group) == gvk.KubernetesGateway.Group) &&
		(kind == nil || string(*kind) == gvk.KubernetesGateway.Kind)
}

// nsOr returns ns, or def when ns is unset or empty, as istiod does.
func nsOr(ns *k8s.Namespace, def string) string {
	return ptr.NonEmptyOrDefault(string(ptr.OrEmpty(ns)), def)
}
