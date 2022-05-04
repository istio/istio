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

package model

import (
	"strings"

	"github.com/gogo/protobuf/jsonpb"
	"google.golang.org/protobuf/proto"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/util/sets"
)

// SelectVirtualServices selects the virtual services by matching given services' host names.
// This is a common function used by both sidecar converter and http route.
func SelectVirtualServices(virtualServices []config.Config, hosts map[string][]host.Name) []config.Config {
	importedVirtualServices := make([]config.Config, 0)

	vsset := sets.New()
	addVirtualService := func(vs config.Config, hosts host.Names) {
		vsname := vs.Name + "/" + vs.Namespace
		rule := vs.Spec.(*networking.VirtualService)
		for _, ih := range hosts {
			// Check if the virtual service is already processed.
			if vsset.Contains(vsname) {
				break
			}
			for _, h := range rule.Hosts {
				// VirtualServices can have many hosts, so we need to avoid appending
				// duplicated virtualservices to slice importedVirtualServices
				if vsHostMatches(h, ih, vs) {
					importedVirtualServices = append(importedVirtualServices, vs)
					vsset.Insert(vsname)
					break
				}
			}
		}
	}
	for _, c := range virtualServices {
		configNamespace := c.Namespace
		// Selection algorithm:
		// virtualservices have a list of hosts in the API spec
		// if any host in the list matches one service hostname, select the virtual service
		// and break out of the loop.

		// Check if there is an explicit import of form ns/* or ns/host
		if importedHosts, nsFound := hosts[configNamespace]; nsFound {
			addVirtualService(c, importedHosts)
		}

		// Check if there is an import of form */host or */*
		if importedHosts, wnsFound := hosts[wildcardNamespace]; wnsFound {
			addVirtualService(c, importedHosts)
		}
	}

	return importedVirtualServices
}

// vsHostMatches checks if the given VirtualService host matches the importedHost (from Sidecar)
func vsHostMatches(vsHost string, importedHost host.Name, vs config.Config) bool {
	if UseGatewaySemantics(vs) {
		// The new way. Matching logic exactly mirrors Service matching
		// If a route defines `*.com` and we import `a.com`, it will not match
		return host.Name(vsHost).SubsetOf(importedHost)
	}

	// The old way. We check Matches which is bi-directional. This is for backwards compatibility
	return host.Name(vsHost).Matches(importedHost)
}

func resolveVirtualServiceShortnames(rule *networking.VirtualService, meta config.Meta) {
	// resolve top level hosts
	for i, h := range rule.Hosts {
		rule.Hosts[i] = string(ResolveShortnameToFQDN(h, meta))
	}
	// resolve gateways to bind to
	for i, g := range rule.Gateways {
		if g != constants.IstioMeshGateway {
			rule.Gateways[i] = resolveGatewayName(g, meta)
		}
	}
	// resolve host in http route.destination, route.mirror
	for _, d := range rule.Http {
		for _, m := range d.Match {
			for i, g := range m.Gateways {
				if g != constants.IstioMeshGateway {
					m.Gateways[i] = resolveGatewayName(g, meta)
				}
			}
		}
		for _, w := range d.Route {
			if w.Destination != nil {
				w.Destination.Host = string(ResolveShortnameToFQDN(w.Destination.Host, meta))
			}
		}
		if d.Mirror != nil {
			d.Mirror.Host = string(ResolveShortnameToFQDN(d.Mirror.Host, meta))
		}
	}
	// resolve host in tcp route.destination
	for _, d := range rule.Tcp {
		for _, m := range d.Match {
			for i, g := range m.Gateways {
				if g != constants.IstioMeshGateway {
					m.Gateways[i] = resolveGatewayName(g, meta)
				}
			}
		}
		for _, w := range d.Route {
			if w.Destination != nil {
				w.Destination.Host = string(ResolveShortnameToFQDN(w.Destination.Host, meta))
			}
		}
	}
	// resolve host in tls route.destination
	for _, tls := range rule.Tls {
		for _, m := range tls.Match {
			for i, g := range m.Gateways {
				if g != constants.IstioMeshGateway {
					m.Gateways[i] = resolveGatewayName(g, meta)
				}
			}
		}
		for _, w := range tls.Route {
			if w.Destination != nil {
				w.Destination.Host = string(ResolveShortnameToFQDN(w.Destination.Host, meta))
			}
		}
	}
}

// Return merged virtual services and the root->delegate vs map
func mergeVirtualServicesIfNeeded(
	vServices []config.Config,
	defaultExportTo map[visibility.Instance]bool) ([]config.Config, map[ConfigKey][]ConfigKey) {
	out := make([]config.Config, 0, len(vServices))
	delegatesMap := map[string]config.Config{}
	delegatesExportToMap := map[string]map[visibility.Instance]bool{}
	// root virtualservices with delegate
	var rootVses []config.Config

	// 1. classify virtualservices
	for _, vs := range vServices {
		rule := vs.Spec.(*networking.VirtualService)
		// it is delegate, add it to the indexer cache along with the exportTo for the delegate
		if len(rule.Hosts) == 0 {
			delegatesMap[key(vs.Name, vs.Namespace)] = vs
			if len(rule.ExportTo) == 0 {
				// No exportTo in virtualService. Use the global default
				delegatesExportToMap[key(vs.Name, vs.Namespace)] = defaultExportTo
			} else {
				exportToMap := make(map[visibility.Instance]bool)
				for _, e := range rule.ExportTo {
					if e == string(visibility.Private) {
						exportToMap[visibility.Instance(vs.Namespace)] = true
					} else {
						exportToMap[visibility.Instance(e)] = true
					}
				}
				delegatesExportToMap[key(vs.Name, vs.Namespace)] = exportToMap
			}
			continue
		}

		// root vs
		if isRootVs(rule) {
			rootVses = append(rootVses, vs)
			continue
		}

		// the others are normal vs without delegate
		out = append(out, vs)
	}

	delegatesByRoot := make(map[ConfigKey][]ConfigKey, len(rootVses))

	// 2. merge delegates and root
	for _, root := range rootVses {
		rootConfigKey := ConfigKey{Kind: gvk.VirtualService, Name: root.Name, Namespace: root.Namespace}
		rootVs := root.Spec.(*networking.VirtualService)
		mergedRoutes := []*networking.HTTPRoute{}
		for _, route := range rootVs.Http {
			// it is root vs with delegate
			if delegate := route.Delegate; delegate != nil {
				delegateNamespace := delegate.Namespace
				if delegateNamespace == "" {
					delegateNamespace = root.Namespace
				}
				delegateConfigKey := ConfigKey{Kind: gvk.VirtualService, Name: delegate.Name, Namespace: delegateNamespace}
				delegatesByRoot[rootConfigKey] = append(delegatesByRoot[rootConfigKey], delegateConfigKey)
				delegateVS, ok := delegatesMap[key(delegate.Name, delegateNamespace)]
				if !ok {
					log.Debugf("delegate virtual service %s/%s of %s/%s not found",
						delegateNamespace, delegate.Name, root.Namespace, root.Name)
					// delegate not found, ignore only the current HTTP route
					continue
				}
				// make sure that the delegate is visible to root virtual service's namespace
				exportTo := delegatesExportToMap[key(delegate.Name, delegateNamespace)]
				if !exportTo[visibility.Public] && !exportTo[visibility.Instance(root.Namespace)] {
					log.Debugf("delegate virtual service %s/%s of %s/%s is not exported to %s",
						delegateNamespace, delegate.Name, root.Namespace, root.Name, root.Namespace)
					continue
				}
				// DeepCopy to prevent mutate the original delegate, it can conflict
				// when multiple routes delegate to one single VS.
				copiedDelegate := delegateVS.DeepCopy()
				vs := copiedDelegate.Spec.(*networking.VirtualService)
				merged := mergeHTTPRoutes(route, vs.Http)
				mergedRoutes = append(mergedRoutes, merged...)
			} else {
				mergedRoutes = append(mergedRoutes, route)
			}
		}
		rootVs.Http = mergedRoutes
		if log.DebugEnabled() {
			jsonm := &jsonpb.Marshaler{Indent: "   "}
			vsString, _ := jsonm.MarshalToString(rootVs)
			log.Debugf("merged virtualService: %s", vsString)
		}
		out = append(out, root)
	}

	return out, delegatesByRoot
}

// merge root's route with delegate's and the merged route number equals the delegate's.
// if there is a conflict with root, the route is ignored
func mergeHTTPRoutes(root *networking.HTTPRoute, delegate []*networking.HTTPRoute) []*networking.HTTPRoute {
	root.Delegate = nil

	out := make([]*networking.HTTPRoute, 0, len(delegate))
	for _, subRoute := range delegate {
		merged := mergeHTTPRoute(root, subRoute)
		if merged != nil {
			out = append(out, merged)
		}
	}
	return out
}

// merge the two HTTPRoutes, if there is a conflict with root, the delegate route is ignored
func mergeHTTPRoute(root *networking.HTTPRoute, delegate *networking.HTTPRoute) *networking.HTTPRoute {
	// suppose there are N1 match conditions in root, N2 match conditions in delegate
	// if match condition of N2 is a subset of anyone in N1, this is a valid matching conditions
	merged, conflict := mergeHTTPMatchRequests(root.Match, delegate.Match)
	if conflict {
		log.Debugf("HTTPMatchRequests conflict: root route %s, delegate route %s", root.Name, delegate.Name)
		return nil
	}
	delegate.Match = merged

	if delegate.Name == "" {
		delegate.Name = root.Name
	} else if root.Name != "" {
		delegate.Name = root.Name + "-" + delegate.Name
	}
	if delegate.Rewrite == nil {
		delegate.Rewrite = root.Rewrite
	}
	if delegate.Timeout == nil {
		delegate.Timeout = root.Timeout
	}
	if delegate.Retries == nil {
		delegate.Retries = root.Retries
	}
	if delegate.Fault == nil {
		delegate.Fault = root.Fault
	}
	if delegate.Mirror == nil {
		delegate.Mirror = root.Mirror
	}
	// nolint: staticcheck
	if delegate.MirrorPercent == nil {
		delegate.MirrorPercent = root.MirrorPercent
	}
	if delegate.MirrorPercentage == nil {
		delegate.MirrorPercentage = root.MirrorPercentage
	}
	if delegate.CorsPolicy == nil {
		delegate.CorsPolicy = root.CorsPolicy
	}
	if delegate.Headers == nil {
		delegate.Headers = root.Headers
	}
	return delegate
}

// return merged match conditions if not conflicts
func mergeHTTPMatchRequests(root, delegate []*networking.HTTPMatchRequest) (out []*networking.HTTPMatchRequest, conflict bool) {
	if len(root) == 0 {
		return delegate, false
	}

	if len(delegate) == 0 {
		return root, false
	}

	// each HTTPMatchRequest of delegate must find a superset in root.
	// otherwise it conflicts
	for _, subMatch := range delegate {
		foundMatch := false
		for _, rootMatch := range root {
			if hasConflict(rootMatch, subMatch) {
				log.Debugf("HTTPMatchRequests conflict: root %v, delegate %v", rootMatch, subMatch)
				continue
			}
			// merge HTTPMatchRequest
			out = append(out, mergeHTTPMatchRequest(rootMatch, subMatch))
			foundMatch = true
		}
		if !foundMatch {
			return nil, true
		}
	}
	if len(out) == 0 {
		conflict = true
	}
	return
}

func mergeHTTPMatchRequest(root, delegate *networking.HTTPMatchRequest) *networking.HTTPMatchRequest {
	out := proto.Clone(delegate).(*networking.HTTPMatchRequest)
	if out.Name == "" {
		out.Name = root.Name
	} else if root.Name != "" {
		out.Name = root.Name + "-" + out.Name
	}
	if out.Uri == nil {
		out.Uri = root.Uri
	}
	if out.Scheme == nil {
		out.Scheme = root.Scheme
	}
	if out.Method == nil {
		out.Method = root.Method
	}
	if out.Authority == nil {
		out.Authority = root.Authority
	}
	// headers
	if len(root.Headers) > 0 || len(delegate.Headers) > 0 {
		out.Headers = make(map[string]*networking.StringMatch)
	}
	for k, v := range root.Headers {
		out.Headers[k] = v
	}
	for k, v := range delegate.Headers {
		out.Headers[k] = v
	}
	// withoutheaders
	if len(root.WithoutHeaders) > 0 || len(delegate.WithoutHeaders) > 0 {
		out.WithoutHeaders = make(map[string]*networking.StringMatch)
	}
	for k, v := range root.WithoutHeaders {
		out.WithoutHeaders[k] = v
	}
	for k, v := range delegate.WithoutHeaders {
		out.WithoutHeaders[k] = v
	}
	// queryparams
	if len(root.QueryParams) > 0 || len(delegate.QueryParams) > 0 {
		out.QueryParams = make(map[string]*networking.StringMatch)
	}
	for k, v := range root.QueryParams {
		out.QueryParams[k] = v
	}
	for k, v := range delegate.QueryParams {
		out.QueryParams[k] = v
	}

	if out.Port == 0 {
		out.Port = root.Port
	}

	// SourceLabels
	if len(root.SourceLabels) > 0 || len(delegate.SourceLabels) > 0 {
		out.SourceLabels = make(map[string]string)
	}
	for k, v := range root.SourceLabels {
		out.SourceLabels[k] = v
	}
	for k, v := range delegate.SourceLabels {
		out.SourceLabels[k] = v
	}

	if out.SourceNamespace == "" {
		out.SourceNamespace = root.SourceNamespace
	}

	if len(out.Gateways) == 0 {
		out.Gateways = root.Gateways
	}

	return out
}

func hasConflict(root, leaf *networking.HTTPMatchRequest) bool {
	roots := []*networking.StringMatch{root.Uri, root.Scheme, root.Method, root.Authority}
	leaves := []*networking.StringMatch{leaf.Uri, leaf.Scheme, leaf.Method, leaf.Authority}
	for i := range roots {
		if stringMatchConflict(roots[i], leaves[i]) {
			return true
		}
	}
	// header conflicts
	for key, leafHeader := range leaf.Headers {
		if stringMatchConflict(root.Headers[key], leafHeader) {
			return true
		}
	}

	// without headers
	for key, leafValue := range leaf.WithoutHeaders {
		if stringMatchConflict(root.WithoutHeaders[key], leafValue) {
			return true
		}
	}

	// query params conflict
	for key, value := range leaf.QueryParams {
		if stringMatchConflict(root.QueryParams[key], value) {
			return true
		}
	}

	if root.IgnoreUriCase != leaf.IgnoreUriCase {
		return true
	}
	if root.Port > 0 && leaf.Port > 0 && root.Port != leaf.Port {
		return true
	}

	// sourceNamespace
	if root.SourceNamespace != "" && leaf.SourceNamespace != root.SourceNamespace {
		return true
	}

	// sourceLabels should not conflict, root should have superset of sourceLabels.
	for key, leafValue := range leaf.SourceLabels {
		if v, ok := root.SourceLabels[key]; ok && v != leafValue {
			return true
		}
	}

	// gateways should not conflict, root should have superset of gateways.
	if len(root.Gateways) > 0 && len(leaf.Gateways) > 0 {
		if len(root.Gateways) < len(leaf.Gateways) {
			return true
		}
		rootGateway := sets.New(root.Gateways...)
		for _, gw := range leaf.Gateways {
			if !rootGateway.Contains(gw) {
				return true
			}
		}
	}

	return false
}

func stringMatchConflict(root, leaf *networking.StringMatch) bool {
	// no conflict when root or leaf is not specified
	if root == nil || leaf == nil {
		return false
	}
	// If root regex match is specified, delegate should not have other matches.
	if root.GetRegex() != "" {
		if leaf.GetRegex() != "" || leaf.GetPrefix() != "" || leaf.GetExact() != "" {
			return true
		}
	}
	// If delgate regex match is specified, root should not have other matches.
	if leaf.GetRegex() != "" {
		if root.GetRegex() != "" || root.GetPrefix() != "" || root.GetExact() != "" {
			return true
		}
	}
	// root is exact match
	if exact := root.GetExact(); exact != "" {
		// leaf is prefix match, conflict
		if leaf.GetPrefix() != "" {
			return true
		}
		// both exact, but not equal
		if leaf.GetExact() != exact {
			return true
		}
		return false
	}
	// root is prefix match
	if prefix := root.GetPrefix(); prefix != "" {
		// leaf is prefix match
		if leaf.GetPrefix() != "" {
			// leaf(`/a`) is not subset of root(`/a/b`)
			return !strings.HasPrefix(leaf.GetPrefix(), prefix)
		}
		// leaf is exact match
		if leaf.GetExact() != "" {
			// leaf(`/a`) is not subset of root(`/a/b`)
			return !strings.HasPrefix(leaf.GetExact(), prefix)
		}
	}

	return true
}

func isRootVs(vs *networking.VirtualService) bool {
	for _, route := range vs.Http {
		// it is root vs with delegate
		if route.Delegate != nil {
			return true
		}
	}
	return false
}

// UseIngressSemantics determines which logic we should use for VirtualService
// This allows ingress and VS to both be represented by VirtualService, but have different
// semantics.
func UseIngressSemantics(cfg config.Config) bool {
	return cfg.Annotations[constants.InternalRouteSemantics] == constants.RouteSemanticsIngress
}

// UseGatewaySemantics determines which logic we should use for VirtualService
// This allows gateway-api and VS to both be represented by VirtualService, but have different
// semantics.
func UseGatewaySemantics(cfg config.Config) bool {
	return cfg.Annotations[constants.InternalRouteSemantics] == constants.RouteSemanticsGateway
}
