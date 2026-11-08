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
	"fmt"
	"regexp"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/api/annotation"
	"istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/util"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

// The analyzers in this file check TLS routes to wildcard DYNAMIC_DNS ServiceEntries. istiod builds these routes
// only when ENABLE_WILDCARD_HOST_SERVICE_ENTRIES_FOR_TLS is set, so both analyzers stay silent unless every istiod
// Deployment sets it. When the analyzers cannot tell what istiod does, they stay silent.

// WildcardSNIGatewayAnalyzer checks gateway PASSTHROUGH TLS routes to wildcard DYNAMIC_DNS ServiceEntries.
type WildcardSNIGatewayAnalyzer struct{}

// WildcardSNIWaypointAnalyzer checks waypoint TLS matches for wildcard DYNAMIC_DNS ServiceEntries.
type WildcardSNIWaypointAnalyzer struct{}

var (
	_ analysis.Analyzer = &WildcardSNIGatewayAnalyzer{}
	_ analysis.Analyzer = &WildcardSNIWaypointAnalyzer{}
)

const (
	wildcardTLSFlag             = "ENABLE_WILDCARD_HOST_SERVICE_ENTRIES_FOR_TLS"
	scopeGatewayToNamespaceFlag = "PILOT_SCOPE_GATEWAY_TO_NAMESPACE"
)

// Metadata implements Analyzer
func (a *WildcardSNIGatewayAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "virtualservice.WildcardSNIGatewayAnalyzer",
		Description: "Checks gateway TLS routes to wildcard DYNAMIC_DNS ServiceEntries",
		Inputs: []config.GroupVersionKind{
			gvk.Deployment,
			gvk.Gateway,
			gvk.KubernetesGateway,
			gvk.MeshConfig,
			gvk.MutatingWebhookConfiguration,
			gvk.Namespace,
			gvk.Pod,
			gvk.ServiceEntry,
			gvk.TLSRoute,
			gvk.VirtualService,
		},
	}
}

// Metadata implements Analyzer
func (a *WildcardSNIWaypointAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "virtualservice.WildcardSNIWaypointAnalyzer",
		Description: "Checks waypoint TLS routes to wildcard DYNAMIC_DNS ServiceEntries",
		Inputs: []config.GroupVersionKind{
			gvk.Deployment,
			gvk.GatewayClass,
			gvk.KubernetesGateway,
			gvk.MeshConfig,
			gvk.MutatingWebhookConfiguration,
			gvk.Namespace,
			gvk.ServiceEntry,
			gvk.TLSRoute,
			gvk.VirtualService,
		},
	}
}

// Analyze implements Analyzer
func (a *WildcardSNIGatewayAnalyzer) Analyze(c analysis.Context) {
	istiod := readIstiodFlags(c)
	if !istiod.wildcardTLS || !istiod.gatewayWildcardTLS {
		return
	}
	serviceEntries := indexServiceEntries(c)
	if !serviceEntries.hasWildcard {
		return
	}
	mesh := fetchMeshConfig(c)
	proxyNamespaces := gatewayProxyNamespaces(c, istiod)

	c.ForEach(gvk.VirtualService, func(r *resource.Instance) bool {
		vs := r.Message.(*v1alpha3.VirtualService)
		if len(vs.Tls) == 0 {
			return true
		}
		vsNs := r.Metadata.FullName.Namespace.String()
		reported := sets.New[string]()
		for _, gwName := range vs.Gateways {
			if gwName == util.MeshGateway {
				continue
			}
			gwFullName := resolveGatewayName(gwName, vsNs)
			gwNs, gwLocalName, _ := strings.Cut(gwFullName, "/")
			gwResource := c.Find(gvk.Gateway, resource.NewFullName(resource.Namespace(gwNs), resource.LocalName(gwLocalName)))
			if gwResource == nil {
				continue
			}
			gw := gwResource.Message.(*v1alpha3.Gateway)
			// istiod resolves routes and services from the namespace of the gateway pods, not of the Gateway.
			for _, proxyNs := range proxyNamespaces(gw, gwNs) {
				if !exportedTo(vs.ExportTo, mesh.GetDefaultVirtualServiceExportTo(), vsNs, proxyNs) {
					continue
				}
				for _, server := range gw.Servers {
					if server.GetTls().GetMode() != v1alpha3.ServerTLSSettings_PASSTHROUGH || !serverBindsVirtualService(server, gwNs, vs, vsNs) {
						continue
					}
					for i, tls := range vs.Tls {
						if len(tls.Route) != 1 {
							continue
						}
						seHost := tls.Route[0].GetDestination().GetHost()
						if serviceEntries.wildcardDynamicDNS(seHost, proxyNs, mesh.GetDefaultServiceExportTo()) == nil {
							continue
						}
						for j, match := range tls.Match {
							if !l4Match(match.Port, match.Gateways, server, gwFullName, vsNs) || sniHostsUnder(match.SniHosts, seHost) {
								continue
							}
							// istiod skips this rule (gateway.go, buildGatewayNetworkFiltersFromTLSRoutes).
							if reported.InsertContains(fmt.Sprintf("%s/%d/%d", gwFullName, i, j)) {
								continue
							}
							m := msg.NewGatewayWildcardSNIHostsNotUnderHost(r, gwFullName, i, j, fmt.Sprint(match.SniHosts), seHost)
							if line, ok := util.ErrorLine(r, fmt.Sprintf(util.VSTLSMatchSNIHost, i, j)); ok {
								m.Line = line
							}
							c.Report(gvk.VirtualService, m)
						}
					}
				}
			}
		}
		return true
	})
	analyzeGatewayTLSRoutes(c, serviceEntries, mesh, istiod.revisions)
}

// Analyze implements Analyzer
func (a *WildcardSNIWaypointAnalyzer) Analyze(c analysis.Context) {
	istiod := readIstiodFlags(c)
	if !istiod.wildcardTLS {
		return
	}
	serviceEntries := indexServiceEntries(c)
	if !serviceEntries.hasWildcard {
		return
	}
	mesh := fetchMeshConfig(c)
	bound := waypointBoundServiceEntries(c, serviceEntries, mesh, istiod.revisions)
	if len(bound) == 0 {
		return
	}
	analyzeWaypointTLSRoutes(c, bound)

	c.ForEach(gvk.VirtualService, func(r *resource.Instance) bool {
		vs := r.Message.(*v1alpha3.VirtualService)
		vsNs := r.Metadata.FullName.Namespace.String()
		seList := bound[vsNs]
		if len(vs.Tls) == 0 || len(seList) == 0 || (len(vs.Gateways) > 0 && !slices.Contains(vs.Gateways, util.MeshGateway)) {
			return true
		}
		for _, s := range seList {
			// The waypoint uses VirtualServices in its own or the ServiceEntry namespace whose hosts match the
			// ServiceEntry host (listener_waypoint.go, getVirtualServiceForWaypoint), if they are visible to it.
			if !exportedTo(vs.ExportTo, mesh.GetDefaultVirtualServiceExportTo(), vsNs, s.waypointNamespace) ||
				!slices.ContainsFunc(vs.Hosts, func(h string) bool { return host.Name(h).Matches(host.Name(s.host)) }) {
				continue
			}
			for i, tls := range vs.Tls {
				// Only rules for traffic to this ServiceEntry: they send to it, or a match names SNI hosts that overlap it.
				// The destination can be another host. Rules only for other hosts of the VirtualService are not meant for it.
				if !slices.ContainsFunc(tls.Route, func(d *v1alpha3.RouteDestination) bool { return d.GetDestination().GetHost() == s.host }) &&
					!slices.ContainsFunc(tls.Match, func(m *v1alpha3.TLSMatchAttributes) bool {
						return slices.ContainsFunc(m.SniHosts, func(h string) bool { return host.Name(h).Matches(host.Name(s.host)) })
					}) {
					continue
				}
				// listener_waypoint.go, getTLSRouteMatch: the waypoint uses the whole rule when one match that applies has
				// sniHosts == [<ServiceEntry host>]; otherwise every match of the rule is ignored.
				var ignored []int
				used := false
				for j, match := range tls.Match {
					if (len(match.Gateways) > 0 && !slices.Contains(match.Gateways, util.MeshGateway)) ||
						(match.Port != 0 && !s.tlsPorts.Contains(match.Port)) {
						continue
					}
					if len(match.SniHosts) == 1 && match.SniHosts[0] == s.host {
						used = true
						break
					}
					ignored = append(ignored, j)
				}
				if used {
					continue
				}
				for _, j := range ignored {
					m := msg.NewWaypointWildcardSNIMatchIgnored(r, s.r.Metadata.FullName.String(), i, j, fmt.Sprint(tls.Match[j].SniHosts), s.host)
					if line, ok := util.ErrorLine(r, fmt.Sprintf(util.VSTLSMatchSNIHost, i, j)); ok {
						m.Line = line
					}
					c.Report(gvk.VirtualService, m)
				}
			}
		}
		return true
	})
}

// istiodFlags holds the istiod settings that change what these analyzers expect.
type istiodFlags struct {
	// wildcardTLS is true when there is an istiod Deployment and every istiod Deployment sets the flag. With revisions,
	// a proxy may be served by any of them, so one istiod without the flag makes the result unknown. Without an
	// istiod Deployment (for example, file-only analysis) the flag state is unknown too.
	wildcardTLS bool
	// scopeGatewayToNamespace is true when any istiod Deployment sets the flag. Limiting gateways to their own
	// namespace can only remove warnings, so this is the safe choice when revisions differ.
	scopeGatewayToNamespace bool
	// gatewayWildcardTLS is true when every istiod Deployment runs Istio 1.33 or later, which added gateway wildcard
	// TLS support (not backported). The version comes from the image tag; without one (digest, "latest") it is unknown.
	gatewayWildcardTLS bool
	// revisions are the istio.io/rev values of the istiod Deployments and the revision tags that point to them.
	// istioctl analyze reads one revision, so proxies of other revisions are served by an istiod it did not read.
	revisions sets.Set[string]
}

func readIstiodFlags(c analysis.Context) istiodFlags {
	found, allWildcardTLS, anyScope, gatewaySupport := false, true, false, true
	revisions := sets.New[string]()
	c.ForEach(gvk.Deployment, func(r *resource.Instance) bool {
		if r.Metadata.Labels["app"] != "istiod" {
			return true
		}
		found = true
		revisions.Insert(revisionOf(r.Metadata.Labels))
		d := r.Message.(*appsv1.DeploymentSpec)
		allWildcardTLS = allWildcardTLS && deploymentEnvTrue(d, wildcardTLSFlag)
		anyScope = anyScope || deploymentEnvTrue(d, scopeGatewayToNamespaceFlag)
		for _, container := range d.Template.Spec.Containers {
			if container.Name == "discovery" {
				gatewaySupport = gatewaySupport && imageVersionAtLeast(container.Image, 1, 33)
			}
		}
		return true
	})
	// Revision tags point to a revision through the istio.io/tag and istio.io/rev labels of their injection webhook.
	tags := sets.New[string]()
	c.ForEach(gvk.MutatingWebhookConfiguration, func(r *resource.Instance) bool {
		if tag, ok := r.Metadata.Labels[label.IoIstioTag.Name]; ok && revisions.Contains(revisionOf(r.Metadata.Labels)) {
			tags.Insert(tag)
		}
		return true
	})
	revisions.InsertAll(tags.UnsortedList()...)
	return istiodFlags{
		wildcardTLS:             found && allWildcardTLS,
		scopeGatewayToNamespace: anyScope,
		gatewayWildcardTLS:      gatewaySupport,
		revisions:               revisions,
	}
}

// imageVersion matches the version at the start of an image tag, such as 1.33.0, 1.33.0-distroless or 1.33.0-beta.1.
var imageVersion = regexp.MustCompile(`^(\d+)\.(\d+)\.`)

// imageVersionAtLeast returns true if the image tag starts with a version of at least major.minor. An image without
// such a tag (a digest only, "latest", a custom tag) has an unknown version and returns false.
func imageVersionAtLeast(image string, major, minor int) bool {
	image, _, _ = strings.Cut(image, "@")
	i := strings.LastIndex(image, ":")
	if i < 0 || strings.Contains(image[i:], "/") {
		return false
	}
	m := imageVersion.FindStringSubmatch(image[i+1:])
	if m == nil {
		return false
	}
	gotMajor, _ := strconv.Atoi(m[1])
	gotMinor, _ := strconv.Atoi(m[2])
	return gotMajor > major || (gotMajor == major && gotMinor >= minor)
}

// revisionOf returns the istio.io/rev label, or "default" without it.
func revisionOf(labels map[string]string) string {
	if rev := labels[label.IoIstioRev.Name]; rev != "" {
		return rev
	}
	return "default"
}

func deploymentEnvTrue(d *appsv1.DeploymentSpec, name string) bool {
	for _, container := range d.Template.Spec.Containers {
		for _, e := range container.Env {
			if e.Name == name {
				v, err := strconv.ParseBool(e.Value)
				return err == nil && v
			}
		}
	}
	return false
}

func fetchMeshConfig(c analysis.Context) *meshconfig.MeshConfig {
	var mesh *meshconfig.MeshConfig
	c.ForEach(gvk.MeshConfig, func(r *resource.Instance) bool {
		mesh = r.Message.(*meshconfig.MeshConfig)
		return r.Metadata.FullName.Name != util.MeshConfigName
	})
	return mesh
}

// serviceEntryIndex holds every ServiceEntry by exact host.
type serviceEntryIndex struct {
	byHost      map[string][]*resource.Instance
	hasWildcard bool
}

func indexServiceEntries(c analysis.Context) serviceEntryIndex {
	idx := serviceEntryIndex{byHost: map[string][]*resource.Instance{}}
	c.ForEach(gvk.ServiceEntry, func(r *resource.Instance) bool {
		se := r.Message.(*v1alpha3.ServiceEntry)
		for _, h := range se.Hosts {
			idx.byHost[h] = append(idx.byHost[h], r)
			if se.Resolution == v1alpha3.ServiceEntry_DYNAMIC_DNS && host.Name(h).IsWildCarded() {
				idx.hasWildcard = true
			}
		}
		return true
	})
	return idx
}

// wildcardDynamicDNS returns the wildcard DYNAMIC_DNS ServiceEntry that a destination host resolves to from the
// namespace. The lookup is exact, as istiod's sidecar scope lookup. If more than one ServiceEntry with the host is
// visible, istiod picks one by age, so the result is unknown and nil is returned.
func (idx serviceEntryIndex) wildcardDynamicDNS(h, namespace string, defaultExportTo []string) *resource.Instance {
	var found *resource.Instance
	for _, r := range idx.byHost[h] {
		if !exportedTo(r.Message.(*v1alpha3.ServiceEntry).ExportTo, defaultExportTo, r.Metadata.FullName.Namespace.String(), namespace) {
			continue
		}
		if found != nil {
			return nil
		}
		found = r
	}
	if found == nil || found.Message.(*v1alpha3.ServiceEntry).Resolution != v1alpha3.ServiceEntry_DYNAMIC_DNS || !host.Name(h).IsWildCarded() {
		return nil
	}
	return found
}

// exportedTo returns true if a config in ownNamespace with exportTo (or the mesh default) is visible in namespace.
func exportedTo(exportTo, defaultExportTo []string, ownNamespace, namespace string) bool {
	if len(exportTo) == 0 {
		exportTo = defaultExportTo
	}
	if len(exportTo) == 0 {
		return true
	}
	for _, e := range exportTo {
		switch e {
		case util.ExportToAllNamespaces:
			return true
		case util.ExportToNamespaceLocal:
			if ownNamespace == namespace {
				return true
			}
		default:
			if e == namespace {
				return true
			}
		}
	}
	return false
}

// gatewayProxyNamespaces returns a function that lists the namespaces of the pods a Gateway applies to, counting only
// pods of the analyzed istiod revisions. With PILOT_SCOPE_GATEWAY_TO_NAMESPACE, a Gateway applies only to pods in its
// own namespace (push_context.go, mergeGateways).
func gatewayProxyNamespaces(c analysis.Context, istiod istiodFlags) func(gw *v1alpha3.Gateway, gwNamespace string) []string {
	scopeToNamespace := istiod.scopeGatewayToNamespace
	revisions := istiod.revisions
	type pod struct {
		namespace string
		labels    klabels.Set
	}
	var pods []pod
	c.ForEach(gvk.Pod, func(r *resource.Instance) bool {
		if !revisions.Contains(revisionOf(r.Metadata.Labels)) {
			return true
		}
		pods = append(pods, pod{namespace: r.Metadata.FullName.Namespace.String(), labels: r.Metadata.Labels})
		return true
	})
	cache := map[string][]string{}
	return func(gw *v1alpha3.Gateway, gwNamespace string) []string {
		if len(gw.Selector) == 0 {
			return nil
		}
		selector := klabels.SelectorFromSet(gw.Selector)
		key := selector.String()
		if scopeToNamespace {
			key = gwNamespace + "/" + key
		}
		if namespaces, ok := cache[key]; ok {
			return namespaces
		}
		namespaces := sets.New[string]()
		for _, p := range pods {
			if selector.Matches(p.labels) && (!scopeToNamespace || p.namespace == gwNamespace) {
				namespaces.Insert(p.namespace)
			}
		}
		cache[key] = sets.SortedList(namespaces)
		return cache[key]
	}
}

// resolveGatewayName mirrors resolveGatewayName in pilot/pkg/model/config.go: it returns "namespace/name".
func resolveGatewayName(gwName, namespace string) string {
	if before, after, found := strings.Cut(gwName, "/"); found {
		if before == "." {
			return namespace + "/" + after
		}
		return gwName
	}
	name, rest, found := strings.Cut(gwName, ".")
	if !found {
		return namespace + "/" + gwName
	}
	// Legacy FQDN format: name.ns.svc.cluster.local -> ns/name
	ns, _, _ := strings.Cut(rest, ".")
	return ns + "/" + name
}

// serverBindsVirtualService mirrors pickMatchingGatewayHosts in pilot/pkg/networking/core/gateway.go, with the
// server hosts normalized as in sanitizeServerHostNamespace (pilot/pkg/model/gateway.go).
func serverBindsVirtualService(server *v1alpha3.Server, gatewayNamespace string, vs *v1alpha3.VirtualService, vsNamespace string) bool {
	for _, h := range server.Hosts {
		serverHost := h
		if ns, name, found := strings.Cut(h, "/"); found {
			if ns == "." {
				ns = gatewayNamespace
			}
			if ns != "*" && ns != vsNamespace {
				continue
			}
			serverHost = name
		}
		if slices.ContainsFunc(vs.Hosts, func(vsHost string) bool { return host.Name(serverHost).Matches(host.Name(vsHost)) }) {
			return true
		}
	}
	return false
}

// l4Match mirrors l4SingleMatch in pilot/pkg/networking/core/gateway.go: an empty port or gateway list matches.
func l4Match(port uint32, gateways []string, server *v1alpha3.Server, gateway, vsNamespace string) bool {
	if port != 0 && port != server.GetPort().GetNumber() {
		return false
	}
	return len(gateways) == 0 || slices.ContainsFunc(gateways, func(g string) bool { return resolveGatewayName(g, vsNamespace) == gateway })
}

// sniHostsUnder mirrors sniHostsUnder in pilot/pkg/networking/core/gateway_wildcard_tls.go: true if there are SNI
// hosts and all of them are under the wildcard host.
func sniHostsUnder(sniHosts []string, wildcardHost string) bool {
	return len(sniHosts) > 0 && !slices.ContainsFunc(sniHosts, func(h string) bool { return !host.Name(h).SubsetOf(host.Name(wildcardHost)) })
}

// waypointServiceEntry is a waypoint-bound wildcard DYNAMIC_DNS ServiceEntry host with its TLS ports.
type waypointServiceEntry struct {
	r                 *resource.Instance
	host              string
	tlsPorts          sets.Set[uint32]
	waypointNamespace string
}

// waypointBoundServiceEntries returns the wildcard DYNAMIC_DNS ServiceEntry hosts with TLS ports that are bound to
// a waypoint for service traffic, by the namespaces whose VirtualServices the waypoint uses for them.
func waypointBoundServiceEntries(
	c analysis.Context, idx serviceEntryIndex, mesh *meshconfig.MeshConfig, revisions sets.Set[string],
) map[string][]waypointServiceEntry {
	out := map[string][]waypointServiceEntry{}
	c.ForEach(gvk.ServiceEntry, func(r *resource.Instance) bool {
		se := r.Message.(*v1alpha3.ServiceEntry)
		if se.Resolution != v1alpha3.ServiceEntry_DYNAMIC_DNS {
			return true
		}
		tlsPorts := sets.New[uint32]()
		for _, p := range se.Ports {
			if protocol.Parse(p.Protocol).IsTLS() {
				tlsPorts.Insert(p.Number)
			}
		}
		if tlsPorts.IsEmpty() {
			return true
		}
		waypointNs, ok := serviceWaypointNamespace(c, r, mesh, revisions)
		if !ok {
			return true
		}
		ns := r.Metadata.FullName.Namespace.String()
		for _, h := range se.Hosts {
			// Skip hosts shared with another ServiceEntry: istiod picks one by age.
			if host.Name(h).IsWildCarded() && len(idx.byHost[h]) == 1 {
				s := waypointServiceEntry{r: r, host: h, tlsPorts: tlsPorts, waypointNamespace: waypointNs}
				out[ns] = append(out[ns], s)
				if waypointNs != ns {
					out[waypointNs] = append(out[waypointNs], s)
				}
			}
		}
		return true
	})
	return out
}

// serviceWaypointNamespace mirrors fetchWaypointForService (pilot/pkg/serviceregistry/ambient/waypoints.go): the
// istio.io/use-waypoint label of the ServiceEntry, or else of its namespace, names an existing istio-waypoint Gateway
// for service traffic that accepts the ServiceEntry namespace, served by an analyzed istiod revision. It returns the
// waypoint namespace.
func serviceWaypointNamespace(c analysis.Context, r *resource.Instance, mesh *meshconfig.MeshConfig, revisions sets.Set[string]) (string, bool) {
	seNs := r.Metadata.FullName.Namespace
	nsResource := c.Find(gvk.Namespace, resource.NewFullName("", resource.LocalName(seNs)))
	labels := r.Metadata.Labels
	if _, ok := labels[label.IoIstioUseWaypoint.Name]; !ok {
		if nsResource == nil {
			return "", false
		}
		labels = nsResource.Metadata.Labels
	}
	name, ok := labels[label.IoIstioUseWaypoint.Name]
	if !ok || name == "" || name == "none" {
		return "", false
	}
	waypointNs := seNs.String()
	if override, ok := labels[label.IoIstioUseWaypointNamespace.Name]; ok {
		waypointNs = override
	}
	gw := c.Find(gvk.KubernetesGateway, resource.NewFullName(resource.Namespace(waypointNs), resource.LocalName(name)))
	if gw == nil {
		return "", false
	}
	spec := gw.Message.(*gatewayv1.GatewaySpec)
	if string(spec.GatewayClassName) != constants.WaypointGatewayClassName || !revisions.Contains(revisionOf(gw.Metadata.Labels)) {
		return "", false
	}
	trafficType := constants.ServiceTraffic
	if class := c.Find(gvk.GatewayClass, resource.NewFullName("", constants.WaypointGatewayClassName)); class != nil {
		if tt, ok := class.Metadata.Labels[label.IoIstioWaypointFor.Name]; ok {
			trafficType = tt
		}
	}
	if tt, ok := gw.Metadata.Labels[label.IoIstioWaypointFor.Name]; ok {
		trafficType = tt
	}
	if trafficType != constants.ServiceTraffic && trafficType != constants.AllTraffic {
		return "", false
	}
	// Cross-namespace: ServiceEntry visibility settings can refuse it (crossNamespaceWaypointForbidden), so stay
	// silent when they are configured.
	if waypointNs != seNs.String() && mesh.GetServiceEntryVisibility() != nil {
		return "", false
	}
	// The waypoint listener must accept the ServiceEntry namespace, also in its own namespace
	// (AllowsAttachmentFromNamespace).
	var classAnnotations, nsLabels map[string]string
	if class := c.Find(gvk.GatewayClass, resource.NewFullName("", resource.LocalName(spec.GatewayClassName))); class != nil {
		classAnnotations = class.Metadata.Annotations
	}
	if nsResource != nil {
		nsLabels = nsResource.Metadata.Labels
	}
	if !waypointAllowsNamespace(spec, gw.Metadata.Annotations, classAnnotations, waypointNs, seNs.String(), nsLabels) {
		return "", false
	}
	return waypointNs, true
}

// waypointAllowsNamespace mirrors makeAllowedRoutes and AllowsAttachmentFromNamespace
// (pilot/pkg/serviceregistry/ambient/waypoints.go): the listener named by the inbound binding annotation decides, else
// the HBONE listener on port 15008, else only the waypoint namespace is allowed.
func waypointAllowsNamespace(
	spec *gatewayv1.GatewaySpec, annotations, classAnnotations map[string]string, waypointNs, namespace string, nsLabels map[string]string,
) bool {
	listener, found := boundListener(spec, annotations, classAnnotations)
	if !found {
		for _, l := range spec.Listeners {
			if l.Protocol == "HBONE" && l.Port == 15008 {
				listener, found = l, true
				break
			}
		}
	}
	from := gatewayv1.NamespacesFromSame
	if found && listener.AllowedRoutes != nil && listener.AllowedRoutes.Namespaces != nil && listener.AllowedRoutes.Namespaces.From != nil {
		from = *listener.AllowedRoutes.Namespaces.From
	}
	switch from {
	case gatewayv1.NamespacesFromAll:
		return true
	case gatewayv1.NamespacesFromSelector:
		selector, err := metav1.LabelSelectorAsSelector(listener.AllowedRoutes.Namespaces.Selector)
		return err == nil && nsLabels != nil && selector.Matches(klabels.Set(nsLabels))
	}
	return waypointNs == namespace
}

// boundListener mirrors makeInboundBinding and findBoundListener (pilot/pkg/serviceregistry/ambient/waypoints.go):
// the annotation on the Gateway, else on its GatewayClass, is "<protocol>" or "<protocol>/<port>".
func boundListener(spec *gatewayv1.GatewaySpec, annotations, classAnnotations map[string]string) (gatewayv1.Listener, bool) {
	binding, ok := annotations[annotation.AmbientWaypointInboundBinding.Name]
	if !ok {
		if binding, ok = classAnnotations[annotation.AmbientWaypointInboundBinding.Name]; !ok {
			return gatewayv1.Listener{}, false
		}
	}
	parts := strings.Split(binding, "/")
	if len(parts) > 2 || (parts[0] != "NONE" && parts[0] != "PROXY") {
		return gatewayv1.Listener{}, false
	}
	port := uint64(0)
	if len(parts) == 2 {
		// istiod logs an invalid port and keeps the binding with port 0.
		port, _ = strconv.ParseUint(parts[1], 10, 32)
	}
	for _, l := range spec.Listeners {
		if (port != 0 && uint64(l.Port) == port) ||
			(port == 0 && parts[0] == "PROXY" && l.Protocol == constants.WaypointSandwichListenerProxyProtocol) {
			return l, true
		}
	}
	return gatewayv1.Listener{}, false
}

// acceptedParents returns the parents that istiod accepted the route for, from the route status. A route without
// status (for example, in files) has no known binding.
func acceptedParents(r *resource.Instance) []gatewayv1.ParentReference {
	status, ok := r.Status.(*gatewayv1.TLSRouteStatus)
	if !ok || status == nil {
		return nil
	}
	var out []gatewayv1.ParentReference
	for _, p := range status.Parents {
		if slices.ContainsFunc(p.Conditions, func(c metav1.Condition) bool {
			return c.Type == string(gatewayv1.RouteConditionAccepted) && c.Status == metav1.ConditionTrue
		}) {
			out = append(out, p.ParentRef)
		}
	}
	return out
}

func refIs(group *gatewayv1.Group, kind *gatewayv1.Kind, wantGroup, wantKind string) bool {
	g, k := gatewayv1.GroupName, "Gateway"
	if group != nil {
		g = string(*group)
	}
	if kind != nil {
		k = string(*kind)
	}
	return g == wantGroup && k == wantKind
}

// tlsRouteBackendHost mirrors convertTLSRoute (pilot/pkg/config/kube/gateway/conversion.go) for the one case that
// can name a wildcard host: a single backend with weight > 0 of kind networking.istio.io/Hostname.
func tlsRouteBackendHost(rule gatewayv1.TLSRouteRule) (string, bool) {
	var backends []gatewayv1.BackendRef
	for _, b := range rule.BackendRefs {
		if b.Weight == nil || *b.Weight != 0 {
			backends = append(backends, b)
		}
	}
	if len(backends) != 1 || !refIs(backends[0].Group, backends[0].Kind, gvk.ServiceEntry.Group, "Hostname") {
		return "", false
	}
	return string(backends[0].Name), true
}

// listenerAcceptsRoute mirrors the parts of Gateway API route attachment that select a listener for an accepted
// parent: sectionName and port of the parentRef, a TLS Passthrough listener, and the listener allowedRoutes.
func listenerAcceptsRoute(c analysis.Context, l gatewayv1.Listener, ref gatewayv1.ParentReference, gwNs, routeNs string) bool {
	if ref.SectionName != nil && *ref.SectionName != l.Name {
		return false
	}
	if ref.Port != nil && *ref.Port != l.Port {
		return false
	}
	if l.Protocol != gatewayv1.TLSProtocolType || l.TLS == nil || l.TLS.Mode == nil || *l.TLS.Mode != gatewayv1.TLSModePassthrough {
		return false
	}
	if l.AllowedRoutes != nil && len(l.AllowedRoutes.Kinds) > 0 && !slices.ContainsFunc(l.AllowedRoutes.Kinds, func(k gatewayv1.RouteGroupKind) bool {
		return refIs(k.Group, &k.Kind, gatewayv1.GroupName, "TLSRoute")
	}) {
		return false
	}
	from := gatewayv1.NamespacesFromSame
	if l.AllowedRoutes != nil && l.AllowedRoutes.Namespaces != nil && l.AllowedRoutes.Namespaces.From != nil {
		from = *l.AllowedRoutes.Namespaces.From
	}
	switch from {
	case gatewayv1.NamespacesFromAll:
		return true
	case gatewayv1.NamespacesFromSame:
		return gwNs == routeNs
	case gatewayv1.NamespacesFromSelector:
		ns := c.Find(gvk.Namespace, resource.NewFullName("", resource.LocalName(routeNs)))
		selector, err := metav1.LabelSelectorAsSelector(l.AllowedRoutes.Namespaces.Selector)
		return ns != nil && err == nil && selector.Matches(klabels.Set(ns.Metadata.Labels))
	}
	return false
}

// analyzeGatewayTLSRoutes checks Gateway API TLSRoutes on istio-class Gateways. istiod converts each rule to a
// VirtualService rule with sniHosts = route hostnames, narrowed by the listener hostname
// (constrainTLSRoutesToListenerHost), so the same skip as for VirtualServices applies.
func analyzeGatewayTLSRoutes(c analysis.Context, serviceEntries serviceEntryIndex, mesh *meshconfig.MeshConfig, revisions sets.Set[string]) {
	c.ForEach(gvk.TLSRoute, func(r *resource.Instance) bool {
		route := r.Message.(*gatewayv1.TLSRouteSpec)
		routeNs := r.Metadata.FullName.Namespace.String()
		reported := sets.New[string]()
		for _, ref := range acceptedParents(r) {
			if !refIs(ref.Group, ref.Kind, gatewayv1.GroupName, "Gateway") {
				continue
			}
			gwNs := routeNs
			if ref.Namespace != nil {
				gwNs = string(*ref.Namespace)
			}
			gwResource := c.Find(gvk.KubernetesGateway, resource.NewFullName(resource.Namespace(gwNs), resource.LocalName(ref.Name)))
			if gwResource == nil {
				continue
			}
			gw := gwResource.Message.(*gatewayv1.GatewaySpec)
			if gw.GatewayClassName != constants.IstioLabel || !revisions.Contains(revisionOf(gwResource.Metadata.Labels)) {
				continue
			}
			gwFullName := gwNs + "/" + string(ref.Name)
			for _, l := range gw.Listeners {
				if !listenerAcceptsRoute(c, l, ref, gwNs, routeNs) {
					continue
				}
				sniHosts := tlsRouteSNIHosts(route)
				if l.Hostname != nil {
					names := host.NewNames(sniHosts).Intersection(host.NewNames([]string{string(*l.Hostname)}))
					sniHosts = slices.Map(names, func(n host.Name) string { return string(n) })
				}
				if len(sniHosts) == 0 {
					// istiod drops the rule for this listener.
					continue
				}
				for i, rule := range route.Rules {
					seHost, ok := tlsRouteBackendHost(rule)
					// The gateway pods run in the Gateway namespace.
					if !ok || serviceEntries.wildcardDynamicDNS(seHost, gwNs, mesh.GetDefaultServiceExportTo()) == nil ||
						sniHostsUnder(sniHosts, seHost) || reported.InsertContains(fmt.Sprintf("%s/%d", gwFullName, i)) {
						continue
					}
					// A TLSRoute rule has one match (buildTLSMatch).
					c.Report(gvk.TLSRoute, msg.NewGatewayWildcardSNIHostsNotUnderHost(r, gwFullName, i, 0, fmt.Sprint(sniHosts), seHost))
				}
			}
		}
		return true
	})
}

// analyzeWaypointTLSRoutes checks Gateway API TLSRoutes attached to waypoint-bound wildcard DYNAMIC_DNS
// ServiceEntries. istiod narrows the hostnames to those matching the ServiceEntry host (compatibleRoutesForHost);
// the waypoint then uses the rule only when they are exactly the ServiceEntry host.
func analyzeWaypointTLSRoutes(c analysis.Context, bound map[string][]waypointServiceEntry) {
	c.ForEach(gvk.TLSRoute, func(r *resource.Instance) bool {
		route := r.Message.(*gatewayv1.TLSRouteSpec)
		routeNs := r.Metadata.FullName.Namespace.String()
		for _, ref := range acceptedParents(r) {
			if !refIs(ref.Group, ref.Kind, gvk.ServiceEntry.Group, gvk.ServiceEntry.Kind) {
				continue
			}
			seNs := routeNs
			if ref.Namespace != nil {
				seNs = string(*ref.Namespace)
			}
			for _, s := range bound[routeNs] {
				if s.r.Metadata.FullName.Namespace.String() != seNs || string(s.r.Metadata.FullName.Name) != string(ref.Name) {
					continue
				}
				if ref.Port != nil && !s.tlsPorts.Contains(uint32(*ref.Port)) {
					continue
				}
				if len(route.Hostnames) == 0 {
					// istiod replaces "*" with the ServiceEntry hosts (augmentTLSPortMatch), so the waypoint uses the rule.
					continue
				}
				sniHosts := tlsRouteSNIHosts(route)
				if len(sniHosts) > 1 {
					sniHosts = slices.FilterInPlace(sniHosts, func(h string) bool { return host.Name(s.host).Matches(host.Name(h)) })
				}
				if len(sniHosts) == 1 && sniHosts[0] == s.host {
					continue
				}
				for i, rule := range route.Rules {
					// A rule whose backends all have weight 0 has no match (convertTLSRoute), and the waypoint always uses it.
					if !slices.ContainsFunc(rule.BackendRefs, func(b gatewayv1.BackendRef) bool { return b.Weight == nil || *b.Weight != 0 }) {
						continue
					}
					c.Report(gvk.TLSRoute, msg.NewWaypointWildcardSNIMatchIgnored(r, s.r.Metadata.FullName.String(), i, 0, fmt.Sprint(sniHosts), s.host))
				}
			}
		}
		return true
	})
}

// tlsRouteSNIHosts mirrors buildTLSMatch (pilot/pkg/config/kube/gateway/conversion.go): the route hostnames, or "*"
// without hostnames.
func tlsRouteSNIHosts(route *gatewayv1.TLSRouteSpec) []string {
	if len(route.Hostnames) == 0 {
		return []string{"*"}
	}
	out := make([]string, 0, len(route.Hostnames))
	for _, h := range route.Hostnames {
		out = append(out, string(h))
	}
	return out
}
