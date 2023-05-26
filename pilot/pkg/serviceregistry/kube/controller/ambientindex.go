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

package controller

import (
	"net/netip"
	"strconv"
	"strings"
	"sync"

	"google.golang.org/protobuf/proto"
	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/api/security/v1beta1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/config/schema/kind"
	kubeutil "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kubetypes"
	kubelabels "istio.io/istio/pkg/kube/labels"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
	"istio.io/istio/pkg/workloadapi/security"
)

// AmbientIndex maintains an index of ambient WorkloadInfo objects by various keys.
// These are intentionally pre-computed based on events such that lookups are efficient.
type AmbientIndex struct {
	mu sync.RWMutex

	// podIPIndex maintains an index of IP -> Pod
	podIPIndex *kclient.Index[string, *v1.Pod]
	// serviceVipIndex maintains an index of IP -> Service
	serviceVipIndex *kclient.Index[string, *v1.Service]
	// serviceHostnameIndex maintains and index of Hostname -> Service
	serviceHostnameIndex   *kclient.Index[string, *v1.Service]
	sliceIPIndex           *kclient.Index[string, *discoveryv1.EndpointSlice]
	waypointsIndex         *kclient.Index[model.WaypointScope, *gateway.Gateway]
	services               kclient.Client[*v1.Service]
	controller             *Controller
	queue                  controllers.Queue
	workloadEntriesIPIndex *kclient.Index[string, *config.Config]

	// addresses              *kclient.DerivedCollection[*model.AddressInfo]
	workloads *kclient.DerivedCollection[*model.WorkloadInfo]
	wservices *kclient.DerivedCollection[*model.ServiceInfo]
}

func workloadToAddressInfo(w *workloadapi.Workload) *model.AddressInfo {
	return &model.AddressInfo{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: w,
			},
		},
	}
}

func serviceToAddressInfo(s *workloadapi.Service) *model.AddressInfo {
	return &model.AddressInfo{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Service{
				Service: s,
			},
		},
	}
}

// Lookup finds the list of AddressInfos for a given key.
// network/IP -> return associated pod Workload or the Service and its corresponding Workloads
// namespace/hostname -> return the Service and its corresponding Workloads
func (a *AmbientIndex) Lookup(key string) []*model.AddressInfo {
	//a.mu.RLock()
	//defer a.mu.RUnlock()
	//network, ip, found := strings.Cut(key, "/")
	//if !found {
	//	log.Warnf(`key (%v) did not contain the expected "/" character`, key)
	//	return nil
	//}
	//if _, err := netip.ParseAddr(ip); err != nil {
	// lookup Service and any Workloads for that Service for each of the network addresses
	// TODO: addresses is only by IP. Should we add an index on our derived collection??
	//for _, svc := range a.serviceVipIndex.Lookup(ip) {
	//	sc := a.controller.constructService(svc)
	//	res = append(res, serviceToAddressInfo(sc))
	//	// TODO: we need an index here
	//	a.ad
	//	for _, addr := range sc.Addresses {
	//		ii, _ := netip.AddrFromSlice(addr.Address)
	//		networkAddr := networkAddress{network: addr.Network, ip: ii.String()}
	//		for _, wl := range a.byService[networkAddr] {
	//			res = append(res, workloadToAddressInfo(wl.Workload))
	//		}
	//	}
	//}
	//}

	w := a.workloads.Get(key, "")
	if w != nil {
		return []*model.AddressInfo{workloadToAddressInfo(w.Workload)}
	}
	svc := a.wservices.Get(key, "")
	if svc != nil {
		// TODO: if its a service, find all associate pods
		return []*model.AddressInfo{serviceToAddressInfo(svc.Service)}
	}
	return nil
}

// All return all known workloads. Result is un-ordered
func (a *AmbientIndex) All() []*model.AddressInfo {
	res := []*model.AddressInfo{}
	// byPod will not have any duplicates, so we can just iterate over that.
	for _, wl := range a.workloads.List("", klabels.Everything()) {
		res = append(res, workloadToAddressInfo(wl.Workload))
	}
	for _, s := range a.wservices.List("", klabels.Everything()) {
		res = append(res, serviceToAddressInfo(s.Service))
	}
	return res
}

func isWaypoint(labels map[string]string) bool {
	return labels[constants.ManagedGatewayLabel] == constants.ManagedGatewayMeshControllerLabel
}

func (c *Controller) WorkloadsForWaypoint(scope model.WaypointScope) []*model.WorkloadInfo {
	var res []*model.WorkloadInfo
	// TODO: try to precompute
	for _, wl := range c.ambientIndex.workloads.List("", klabels.Everything()) {
		if c.ambientIndex.matchesScope(scope, wl) {
			res = append(res, wl)
		}
	}
	return res
}

// Waypoint finds all waypoint IP addresses for a given scope.  Performs first a Namespace+ServiceAccount
// then falls back to any Namespace wide waypoints
func (c *Controller) Waypoint(scope model.WaypointScope) []netip.Addr {
	a := c.ambientIndex
	wp := slices.Head(a.waypointsIndex.Lookup(scope))
	if wp == nil {
		// Now look for namespace-wide
		scope.ServiceAccount = ""
		wp = slices.Head(a.waypointsIndex.Lookup(scope))
	}
	if wp == nil {
		return nil
	}
	for _, addr := range (*wp).Status.Addresses {
		var ips []string
		switch ptr.OrDefault(addr.Type, gateway.IPAddressType) {
		case gateway.IPAddressType:
			ips = []string{addr.Value}
		case gateway.HostnameAddressType:
			// TODO: this is a little wonky, should be zero one one
			for _, svc := range c.ambientIndex.serviceHostnameIndex.Lookup(addr.Value) {
				ips = getVIPs(svc)
			}
		}
		return slices.Map(ips, netip.MustParseAddr)
	}
	return nil
}

func (a *AmbientIndex) matchesScope(scope model.WaypointScope, w *model.WorkloadInfo) bool {
	if len(scope.ServiceAccount) == 0 {
		// We are a namespace wide waypoint. SA scope take precedence.
		// Check if there is one for this workloads service account
		if len(a.waypointsIndex.Lookup(model.WaypointScope{Namespace: scope.Namespace, ServiceAccount: w.ServiceAccount})) > 0 {
			return false
		}
	}
	if scope.ServiceAccount != "" && (w.ServiceAccount != scope.ServiceAccount) {
		return false
	}
	if w.Namespace != scope.Namespace {
		return false
	}
	// Filter out waypoints.
	if w.Labels[constants.ManagedGatewayLabel] == constants.ManagedGatewayMeshControllerLabel {
		return false
	}
	return true
}

func PickBestPod(pods []*v1.Pod) *v1.Pod {
	var best *v1.Pod
	for _, p := range pods {
		if !IsPodRunning(p) {
			continue
		}
		if best == nil || p.CreationTimestamp.After(best.CreationTimestamp.Time) {
			best = p
		}
	}
	return best
}

func (a *AmbientIndex) Recompute(name types.NamespacedName) *model.WorkloadInfo {
	ip := name.Name
	log := log.WithLabels("controller", "ambient", "ip", ip)
	log.Debugf("reconcile")

	// Should be 0 or 1 pod, unless we are in a weird state
	pod := PickBestPod(a.podIPIndex.Lookup(ip))

	// Should be 0 or 1 entry, unless we are in a weird state
	var entry *networking.WorkloadEntry
	var configEntry *config.Config
	if a.workloadEntriesIPIndex != nil {
		entries := a.workloadEntriesIPIndex.Lookup(ip)
		if len(entries) > 0 {
			configEntry = entries[0]
			entry = serviceentry.ConvertWorkloadEntry(*entries[0])
		}
	}

	// We can have many slices (part of many services). Note each slice contains other IPs as well; we need to filter
	epSlices := a.sliceIPIndex.Lookup(ip)
	slices.SortFunc(epSlices, func(a, b *discoveryv1.EndpointSlice) bool {
		// Newer slices win, could be an address moving from 1 to another
		return a.CreationTimestamp.After(b.CreationTimestamp.Time)
	})

	if len(epSlices) == 0 && pod == nil && entry == nil {
		log.Debugf("no endpoints or pod found")
		return nil
	}

	// find the namespace for this workload
	var namespace string
	if pod != nil {
		namespace = pod.Namespace
	} else if entry != nil {
		namespace = configEntry.Namespace
	} else {
		for _, slice := range epSlices {
			namespace = slice.Namespace
			break
		}
	}

	vips := a.buildVIPsFromSlice(epSlices, namespace)
	a.buildVIPsFromWorkloadEntry(entry, namespace, vips)

	wl := &workloadapi.Workload{
		Address:    parseIP(ip),
		Namespace:  namespace,
		Network:    a.controller.network.String(),
		ClusterId:  a.controller.Cluster().String(),
		VirtualIps: vips,
	}
	if td := spiffe.GetTrustDomain(); td != "cluster.local" {
		wl.TrustDomain = td
	}

	var labels, annotations map[string]string
	if pod != nil {
		labels = pod.Labels
		annotations = pod.Annotations
		wl.Name = pod.Name
		wl.ServiceAccount = pod.Spec.ServiceAccountName
		wl.Node = pod.Spec.NodeName
		if !IsPodReady(pod) {
			wl.Status = workloadapi.WorkloadStatus_UNHEALTHY
		}

		wl.WorkloadName, wl.WorkloadType = workloadNameAndType(pod)
		wl.CanonicalName, wl.CanonicalRevision = kubelabels.CanonicalService(pod.Labels, wl.WorkloadName)
	} else if entry != nil {
		labels = entry.Labels
		annotations = configEntry.Annotations
		wl.Name = configEntry.Name
		wl.ServiceAccount = entry.ServiceAccount

		// TODO: support WorkloadGroup
		// wl.WorkloadName, wl.WorkloadType = workloadNameAndType(pod)
		wl.CanonicalName, wl.CanonicalRevision = kubelabels.CanonicalService(labels, wl.WorkloadName)
		if !serviceentry.IsHealthy(*configEntry) {
			wl.Status = workloadapi.WorkloadStatus_UNHEALTHY
		}

		// Replace current network, if set.
		wl.Network = model.GetOrDefault(entry.Network, wl.Network)
	}
	wl.AuthorizationPolicies = a.controller.selectorAuthorizationPolicies(namespace, labels)
	if annotations[constants.AmbientRedirection] == constants.AmbientRedirectionEnabled {
		// Configured for override
		wl.TunnelProtocol = workloadapi.TunnelProtocol_HBONE
	}
	// Otherwise supports tunnel directly
	if model.SupportsTunnel(labels, model.TunnelHTTP) {
		wl.TunnelProtocol = workloadapi.TunnelProtocol_HBONE
		wl.NativeTunnel = true
	}

	// Setup waypoints.
	a.buildWaypointAddresses(labels, wl)

	return &model.WorkloadInfo{Workload: wl, Labels: labels}
}

func (a *AmbientIndex) buildWaypointAddresses(labels map[string]string, wl *workloadapi.Workload) {
	if isWaypoint(labels) {
		// Waypoints do not have waypoints themselves, so filter them.
		return
	}
	waypoints := a.controller.Waypoint(model.WaypointScope{Namespace: wl.Namespace, ServiceAccount: wl.ServiceAccount})
	// If we have a waypoint, configure it
	if len(waypoints) > 0 {
		wp := waypoints[0]
		wl.Waypoint = &workloadapi.GatewayAddress{
			Destination: &workloadapi.GatewayAddress_Address{
				Address: &workloadapi.NetworkAddress{
					Network: a.controller.Network(wp.String(), nil).String(),
					Address: wp.AsSlice(),
				},
			},
			// TODO: look up the HBONE port instead of hardcoding it
			Port: 15008,
		}
	}
}

// buildVIPsFromWorkloadEntry adds vips to `vips` based on WorkloadEntry
// Note: this mutates `vips`!
func (a *AmbientIndex) buildVIPsFromWorkloadEntry(entry *networking.WorkloadEntry, namespace string, vips map[string]*workloadapi.PortList) {
	if entry != nil {
		// TODO: can we derive this from ServiceInstances from the various existing stores instead of redoing everything here?
		// find the workload entry's service by label selector
		// rather than scanning through our internal map of model.services, get the services via the k8s apis
		dummyPod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Labels: entry.Labels},
		}
		// find the services that map to this workload entry, fire off eds updates if the service is of type client-side lb
		allServices := a.controller.services.List(namespace, klabels.Everything())
		if k8sServices := getPodServices(allServices, dummyPod); len(k8sServices) > 0 {
			for _, k8sSvc := range k8sServices {
				if k8sSvc.Spec.Selector == nil {
					continue
				}
				// Get the updated list of endpoints that includes k8s pods and the workload entries for this service
				// and then notify the EDS server that endpoints for this service have changed.
				// We need one endpoint object for each service port
				pl := &workloadapi.PortList{}
				for _, port := range k8sSvc.Spec.Ports {
					if port.Protocol == v1.ProtocolUDP {
						continue
					}
					pn := uint32(port.TargetPort.IntVal)
					portName := port.Name
					if port.TargetPort.StrVal != "" {
						portName = port.TargetPort.StrVal
					}
					if portName != "" {
						// This is a named port, find the corresponding port in the port map
						matchedPort := entry.Ports[portName]
						if matchedPort != 0 {
							pn = matchedPort
						} else if port.TargetPort.StrVal != "" { // We only have a named targetPort, so we needed a match
							continue
						}
					}

					pl.Ports = append(pl.Ports, &workloadapi.Port{
						ServicePort: uint32(port.Port),
						TargetPort:  pn,
					})
				}
				for _, vip := range getVIPs(k8sSvc) {
					if _, f := vips[vip]; f {
						// Service already handled
						continue
					}
					vips[vip] = pl
				}
			}
		}
	}
}

func (a *AmbientIndex) buildVIPsFromSlice(epSlices []*discoveryv1.EndpointSlice, namespace string) map[string]*workloadapi.PortList {
	vips := map[string]*workloadapi.PortList{}
	for _, slice := range epSlices {
		if slice.Namespace != namespace {
			// IP is moving
			continue
		}
		svcName, f := slice.Labels[discoveryv1.LabelServiceName]
		if !f {
			// This isn't for a service, just a naked endpoint slice.
			continue
		}
		svc := a.services.Get(svcName, slice.Namespace)
		if svc == nil {
			// Service is gone, nothing we can do
			continue
		}
		pl := &workloadapi.PortList{}
		for _, epPort := range slice.Ports {
			if epPort.Name == nil {
				continue
			}
			if epPort.Port == nil {
				continue
			}
			if epPort.Protocol != nil && *epPort.Protocol != v1.ProtocolTCP {
				continue
			}
			for _, svcPort := range svc.Spec.Ports {
				if *epPort.Name == svcPort.Name {
					pl.Ports = append(pl.Ports, &workloadapi.Port{
						ServicePort: uint32(svcPort.Port),
						TargetPort:  uint32(*epPort.Port),
					})
				}
			}
		}
		if len(pl.Ports) > 0 {
			for _, vip := range getVIPs(svc) {
				if _, f := vips[vip]; f {
					// Service already handled
					continue
				}
				vips[vip] = pl
			}
		}
	}
	return vips
}

func (c *Controller) Policies(requested sets.Set[model.ConfigKey]) []*security.Authorization {
	if !c.configCluster {
		return nil
	}
	cfgs := c.configController.List(gvk.AuthorizationPolicy, metav1.NamespaceAll)
	l := len(cfgs)
	if len(requested) > 0 {
		l = len(requested)
	}
	res := make([]*security.Authorization, 0, l)
	for _, cfg := range cfgs {
		k := model.ConfigKey{
			Kind:      kind.AuthorizationPolicy,
			Name:      cfg.Name,
			Namespace: cfg.Namespace,
		}
		if len(requested) > 0 && !requested.Contains(k) {
			continue
		}
		pol := convertAuthorizationPolicy(c.meshWatcher.Mesh().GetRootNamespace(), cfg)
		if pol == nil {
			continue
		}
		res = append(res, pol)
	}
	return res
}

func (c *Controller) selectorAuthorizationPolicies(ns string, lbls map[string]string) []string {
	global := c.configController.List(gvk.AuthorizationPolicy, c.meshWatcher.Mesh().GetRootNamespace())
	local := c.configController.List(gvk.AuthorizationPolicy, ns)
	res := sets.New[string]()
	matches := func(c config.Config) bool {
		sel := c.Spec.(*v1beta1.AuthorizationPolicy).Selector
		if sel == nil {
			return false
		}
		return labels.Instance(sel.MatchLabels).SubsetOf(lbls)
	}

	for _, pl := range [][]config.Config{global, local} {
		for _, p := range pl {
			if matches(p) {
				res.Insert(p.Namespace + "/" + p.Name)
			}
		}
	}
	return sets.SortedList(res)
}

func (c *Controller) AuthorizationPolicyHandler(old config.Config, obj config.Config, ev model.Event) {
	getSelector := func(c config.Config) map[string]string {
		if c.Spec == nil {
			return nil
		}
		pol := c.Spec.(*v1beta1.AuthorizationPolicy)
		return pol.Selector.GetMatchLabels()
	}
	// Normal flow for AuthorizationPolicy will trigger XDS push, so we don't need to push those. But we do need
	// to update any relevant workloads and push them.
	sel := getSelector(obj)
	oldSel := getSelector(old)

	switch ev {
	case model.EventUpdate:
		if maps.Equal(sel, oldSel) {
			// Update event, but selector didn't change. No workloads to push.
			return
		}
	default:
		if sel == nil {
			// We only care about selector policies
			return
		}
	}

	pods := sets.New[types.NamespacedName]()
	for _, p := range c.getPodsInPolicy(obj.Namespace, sel) {
		for _, ip := range p.Status.PodIPs {
			pods.Insert(types.NamespacedName{Name: ip.IP})
		}
	}
	if oldSel != nil {
		for _, p := range c.getPodsInPolicy(obj.Namespace, oldSel) {
			for _, ip := range p.Status.PodIPs {
				pods.Insert(types.NamespacedName{Name: ip.IP})
			}
		}
	}
	for p := range pods {
		c.ambientIndex.queue.Add(p)
	}
}

func (c *Controller) getPodsInPolicy(ns string, sel map[string]string) []*v1.Pod {
	if ns == c.meshWatcher.Mesh().GetRootNamespace() {
		ns = metav1.NamespaceAll
	}
	return c.podsClient.List(ns, klabels.ValidatedSetSelector(sel))
}

func (c *Controller) constructService(svc *v1.Service) *workloadapi.Service {
	ports := make([]*workloadapi.Port, 0, len(svc.Spec.Ports))
	for _, p := range svc.Spec.Ports {
		ports = append(ports, &workloadapi.Port{
			ServicePort: uint32(p.Port),
			TargetPort:  uint32(p.TargetPort.IntVal),
		})
	}

	// TODO this is only checking one controller - we may be missing service vips for instances in another cluster
	vips := getVIPs(svc)
	addrs := make([]*workloadapi.NetworkAddress, 0, len(vips))
	for _, vip := range vips {
		addrs = append(addrs, &workloadapi.NetworkAddress{
			Network: c.Network(vip, make(labels.Instance, 0)).String(),
			Address: netip.MustParseAddr(vip).AsSlice(),
		})
	}

	return &workloadapi.Service{
		Name:      svc.Name,
		Namespace: svc.Namespace,
		Hostname: string(model.ResolveShortnameToFQDN(svc.Name, config.Meta{
			Namespace: svc.Namespace,
			Domain:    spiffe.GetTrustDomain(),
		})),
		Addresses: addrs,
		Ports:     ports,
	}
}

func convertAuthorizationPolicy(rootns string, obj config.Config) *security.Authorization {
	pol := obj.Spec.(*v1beta1.AuthorizationPolicy)

	scope := security.Scope_WORKLOAD_SELECTOR
	if pol.Selector == nil {
		scope = security.Scope_NAMESPACE
		// TODO: TDA
		if rootns == obj.Namespace {
			scope = security.Scope_GLOBAL // TODO: global workload?
		}
	}
	action := security.Action_ALLOW
	switch pol.Action {
	case v1beta1.AuthorizationPolicy_ALLOW:
	case v1beta1.AuthorizationPolicy_DENY:
		action = security.Action_DENY
	default:
		return nil
	}
	opol := &security.Authorization{
		Name:      obj.Name,
		Namespace: obj.Namespace,
		Scope:     scope,
		Action:    action,
		Groups:    nil,
	}

	for _, rule := range pol.Rules {
		rules := handleRule(action, rule)
		if rules != nil {
			rg := &security.Group{
				Rules: rules,
			}
			opol.Groups = append(opol.Groups, rg)
		}
	}

	return opol
}

func anyNonEmpty[T any](arr ...[]T) bool {
	for _, a := range arr {
		if len(a) > 0 {
			return true
		}
	}
	return false
}

func handleRule(action security.Action, rule *v1beta1.Rule) []*security.Rules {
	toMatches := []*security.Match{}
	for _, to := range rule.To {
		op := to.Operation
		if action == security.Action_ALLOW && anyNonEmpty(op.Hosts, op.NotHosts, op.Methods, op.NotMethods, op.Paths, op.NotPaths) {
			// L7 policies never match for ALLOW
			// For DENY they will always match, so it is more restrictive
			return nil
		}
		match := &security.Match{
			DestinationPorts:    stringToPort(op.Ports),
			NotDestinationPorts: stringToPort(op.NotPorts),
		}
		// if !emptyRuleMatch(match) {
		toMatches = append(toMatches, match)
		//}
	}
	fromMatches := []*security.Match{}
	for _, from := range rule.From {
		op := from.Source
		if action == security.Action_ALLOW && anyNonEmpty(op.RemoteIpBlocks, op.NotRemoteIpBlocks, op.RequestPrincipals, op.NotRequestPrincipals) {
			// L7 policies never match for ALLOW
			// For DENY they will always match, so it is more restrictive
			return nil
		}
		match := &security.Match{
			SourceIps:     stringToIP(op.IpBlocks),
			NotSourceIps:  stringToIP(op.NotIpBlocks),
			Namespaces:    stringToMatch(op.Namespaces),
			NotNamespaces: stringToMatch(op.NotNamespaces),
			Principals:    stringToMatch(op.Principals),
			NotPrincipals: stringToMatch(op.NotPrincipals),
		}
		// if !emptyRuleMatch(match) {
		fromMatches = append(fromMatches, match)
		//}
	}

	rules := []*security.Rules{}
	if len(toMatches) > 0 {
		rules = append(rules, &security.Rules{Matches: toMatches})
	}
	if len(fromMatches) > 0 {
		rules = append(rules, &security.Rules{Matches: fromMatches})
	}
	for _, when := range rule.When {
		l4 := l4WhenAttributes.Contains(when.Key)
		if action == security.Action_ALLOW && !l4 {
			// L7 policies never match for ALLOW
			// For DENY they will always match, so it is more restrictive
			return nil
		}
		positiveMatch := &security.Match{
			Namespaces:       whenMatch("source.namespace", when, false, stringToMatch),
			Principals:       whenMatch("source.principal", when, false, stringToMatch),
			SourceIps:        whenMatch("source.ip", when, false, stringToIP),
			DestinationPorts: whenMatch("destination.port", when, false, stringToPort),
			DestinationIps:   whenMatch("destination.ip", when, false, stringToIP),

			NotNamespaces:       whenMatch("source.namespace", when, true, stringToMatch),
			NotPrincipals:       whenMatch("source.principal", when, true, stringToMatch),
			NotSourceIps:        whenMatch("source.ip", when, true, stringToIP),
			NotDestinationPorts: whenMatch("destination.port", when, true, stringToPort),
			NotDestinationIps:   whenMatch("destination.ip", when, true, stringToIP),
		}
		rules = append(rules, &security.Rules{Matches: []*security.Match{positiveMatch}})
	}
	return rules
}

var l4WhenAttributes = sets.New(
	"source.ip",
	"source.namespace",
	"source.principal",
	"destination.ip",
	"destination.port",
)

func whenMatch[T any](s string, when *v1beta1.Condition, invert bool, f func(v []string) []T) []T {
	if when.Key != s {
		return nil
	}
	if invert {
		return f(when.NotValues)
	}
	return f(when.Values)
}

func stringToMatch(rules []string) []*security.StringMatch {
	res := make([]*security.StringMatch, 0, len(rules))
	for _, v := range rules {
		var sm *security.StringMatch
		switch {
		case v == "*":
			sm = &security.StringMatch{MatchType: &security.StringMatch_Presence{}}
		case strings.HasPrefix(v, "*"):
			sm = &security.StringMatch{MatchType: &security.StringMatch_Suffix{
				Suffix: strings.TrimPrefix(v, "*"),
			}}
		case strings.HasSuffix(v, "*"):
			sm = &security.StringMatch{MatchType: &security.StringMatch_Prefix{
				Prefix: strings.TrimSuffix(v, "*"),
			}}
		default:
			sm = &security.StringMatch{MatchType: &security.StringMatch_Exact{
				Exact: v,
			}}
		}
		res = append(res, sm)
	}
	return res
}

func stringToPort(rules []string) []uint32 {
	res := make([]uint32, 0, len(rules))
	for _, m := range rules {
		p, err := strconv.ParseUint(m, 10, 32)
		if err != nil || p > 65535 {
			continue
		}
		res = append(res, uint32(p))
	}
	return res
}

func stringToIP(rules []string) []*security.Address {
	res := make([]*security.Address, 0, len(rules))
	for _, m := range rules {
		if len(m) == 0 {
			continue
		}

		var (
			ipAddr        netip.Addr
			maxCidrPrefix uint32
		)

		if strings.Contains(m, "/") {
			ipp, err := netip.ParsePrefix(m)
			if err != nil {
				continue
			}
			ipAddr = ipp.Addr()
			maxCidrPrefix = uint32(ipp.Bits())
		} else {
			ipa, err := netip.ParseAddr(m)
			if err != nil {
				continue
			}

			ipAddr = ipa
			maxCidrPrefix = uint32(ipAddr.BitLen())
		}

		res = append(res, &security.Address{
			Address: ipAddr.AsSlice(),
			Length:  maxCidrPrefix,
		})
	}
	return res
}

func (c *Controller) setupIndex() *AmbientIndex {
	idx := AmbientIndex{}

	// Workloads is our core resource. This is a derived collection
	idx.workloads = kclient.NewDerivedCollection[*model.WorkloadInfo](
		idx.Recompute,
		func(a, b *model.WorkloadInfo) bool {
			// If the result doesn't change, suppress event handlers
			return proto.Equal(a, b)
		})
	idx.workloads.AddEventHandler(controllers.FromEventHandler(func(o controllers.Event[*model.WorkloadInfo]) {
		// Workload changed, send update for it
		cu := sets.New(model.ConfigKey{Kind: kind.Address, Name: o.Latest().ResourceName()})
		c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
			ConfigsUpdated: cu,
			Reason:         []model.TriggerReason{model.AmbientUpdate},
		})
	}))

	idx.queue = controllers.NewQueue("ambient", controllers.WithReconciler(idx.workloads.RecomputeE))

	// Build up a variety of indexes to help compute workloads. Each index has a delegate so we can ensure
	// the index is populated before the handlers that depend on them.
	// In general, any object that *may* impact a Workload must enqueue that workload.
	// It's OK if it ends up being a NOP change - they will be filtered out.

	// Lookup service by pod
	idx.serviceVipIndex = kclient.CreateIndexWithDelegate[string, *v1.Service](c.services, getVIPs, controllers.AllObjectHandler(func(svc *v1.Service) {
		if svc.Spec.Selector == nil {
			// services with nil selectors match nothing, not everything.
			return
		}

		// Enqueue all pods selected by the Service.
		// This is probably overkill in most cases.
		// A service change would almost always result in an EndpointSlice change; but not in all cases.
		pods := c.podsClient.List(svc.Namespace, klabels.ValidatedSetSelector(svc.Spec.Selector))
		for _, p := range pods {
			for _, ip := range p.Status.PodIPs {
				idx.queue.Add(types.NamespacedName{Name: ip.IP})
			}
		}

		// WorkloadEntries can also be selected by Service...
		if c.configController != nil {
			entries := c.configController.List(gvk.WorkloadEntry, svc.Namespace)
			for _, e := range entries {
				we := serviceentry.ConvertWorkloadEntry(e)
				if labels.Instance(svc.Spec.Selector).Match(we.Labels) {
					idx.queue.Add(types.NamespacedName{Name: we.Address})
				}
			}
		}
	}))

	idx.serviceVipIndex = kclient.CreateIndexWithDelegate[string, *v1.Service](c.services, getVIPs, controllers.AllObjectHandler(func(svc *v1.Service) {
		if svc.Spec.Selector == nil {
			// services with nil selectors match nothing, not everything.
			return
		}

		// Enqueue all pods selected by the Service.
		// This is probably overkill in most cases.
		// A service change would almost always result in an EndpointSlice change; but not in all cases.
		pods := c.podsClient.List(svc.Namespace, klabels.ValidatedSetSelector(svc.Spec.Selector))
		for _, p := range pods {
			for _, ip := range p.Status.PodIPs {
				idx.queue.Add(types.NamespacedName{Name: ip.IP})
			}
		}

		// WorkloadEntries can also be selected by Service...
		if c.configController != nil {
			entries := c.configController.List(gvk.WorkloadEntry, svc.Namespace)
			for _, e := range entries {
				we := serviceentry.ConvertWorkloadEntry(e)
				if labels.Instance(svc.Spec.Selector).Match(we.Labels) {
					idx.queue.Add(types.NamespacedName{Name: we.Address})
				}
			}
		}
	}))

	// TODO: lack of delegate is broken...
	idx.serviceHostnameIndex = kclient.CreateIndex[string, *v1.Service](c.services, func(svc *v1.Service) []string {
		return []string{string(kube.ServiceHostname(svc.Name, svc.Namespace, c.opts.DomainSuffix))}
	})

	// Pod is fairly straightforward - each pod creates a Workload by its own IP(s).
	// Note: currently multiple IPs would create many Workloads.
	idx.podIPIndex = kclient.CreateIndexWithDelegate[string, *v1.Pod](c.podsClient, func(p *v1.Pod) []string {
		res := make([]string, 0, len(p.Status.PodIPs))
		for _, v := range p.Status.PodIPs {
			res = append(res, v.IP)
		}
		return res
	}, controllers.AllObjectHandler(func(p *v1.Pod) {
		for _, ip := range p.Status.PodIPs {
			idx.queue.Add(types.NamespacedName{Name: ip.IP})
		}
	}))

	// For slices we also just enqueue each address. This can be a bit wasteful as a slice holds
	// multiple IPs, but we will filter out the NOP changes anyways.
	epSlices := kclient.NewFiltered[*discoveryv1.EndpointSlice](c.client, kclient.Filter{ObjectFilter: c.opts.GetFilter()})
	idx.sliceIPIndex = kclient.CreateIndexWithDelegate[string, *discoveryv1.EndpointSlice](epSlices, func(o *discoveryv1.EndpointSlice) []string {
		res := []string{}
		for _, ep := range o.Endpoints {
			res = append(res, ep.Addresses...)
		}
		return res
	}, controllers.AllObjectHandler(func(p *discoveryv1.EndpointSlice) {
		for _, ep := range p.Endpoints {
			for _, ip := range ep.Addresses {
				idx.queue.Add(types.NamespacedName{Name: ip})
			}
		}
	}))

	// WorkloadEntry is just like pod, enqueue its own address.
	if c.configController != nil {
		idx.workloadEntriesIPIndex = kclient.CreateIndexFromStore[string](gvk.WorkloadEntry, c.configController,
			func(o *config.Config) []string {
				addr := o.Spec.(*networking.WorkloadEntry).Address
				if len(addr) != 0 {
					return []string{addr}
				}
				return nil
			}, controllers.AllObjectHandler[*config.Config](func(c *config.Config) {
				ip := c.Spec.(*networking.WorkloadEntry).Address
				if len(ip) != 0 {
					idx.queue.Add(types.NamespacedName{Name: ip})
				}
			}))
	}

	gateways := kclient.NewDelayedInformer[*gateway.Gateway](c.client, gvr.KubernetesGateway, kubetypes.StandardInformer, kclient.Filter{ObjectFilter: c.opts.GetFilter()})
	idx.waypointsIndex = kclient.CreateIndexWithDelegate[model.WaypointScope, *gateway.Gateway](gateways, func(p *gateway.Gateway) []model.WaypointScope {
		if p.Spec.GatewayClassName != constants.WaypointGatewayClassName {
			// not a waypoint
			return nil
		}
		return []model.WaypointScope{{Namespace: p.Namespace, ServiceAccount: p.Annotations[constants.WaypointServiceAccount]}}
	}, controllers.AllObjectHandler(func(p *gateway.Gateway) {
		// Note: This must be NamespaceAll as the workloads index abuses Namespace to mean network.
		// Here we *actually* want namespace to mean namespace.
		for _, wl := range idx.workloads.List(metav1.NamespaceAll, klabels.Everything()) {
			if wl.Namespace != p.Namespace {
				continue
			}
			ip, _ := netip.AddrFromSlice(wl.Address)
			idx.queue.Add(types.NamespacedName{Name: ip.String()})
		}
	}))

	idx.services = c.services
	idx.controller = c
	return &idx
}

// AddressInformation returns all AddressInfo's in the cluster.
// This may be scoped to specific subsets by specifying a non-empty addresses field
func (c *Controller) AddressInformation(addresses sets.Set[types.NamespacedName]) ([]*model.AddressInfo, []string) {
	if len(addresses) == 0 {
		// Full update
		return c.ambientIndex.All(), nil
	}
	var wls []*model.AddressInfo
	var removed []string
	for p := range addresses {
		wname := p.Name
		// GenerateDeltas has the formatted wname from the xds request, but not sure if other callers
		// have the format enforced
		if _, _, found := strings.Cut(p.Name, "/"); !found {
			cNetwork := c.Network(p.Name, make(labels.Instance, 0)).String()
			wname = cNetwork + "/" + p.Name
		}
		wl := c.ambientIndex.Lookup(wname)
		if len(wl) == 0 {
			removed = append(removed, p.Name)
		} else {
			wls = append(wls, wl...)
		}
	}
	return wls, removed
}

func parseIP(ip string) []byte {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return nil
	}
	return addr.AsSlice()
}

// internal object used for indexing in ambientindex maps
type networkAddress struct {
	network string
	ip      string
}

func (n *networkAddress) String() string {
	return n.network + "/" + n.ip
}

func getVIPs(svc *v1.Service) []string {
	res := []string{}
	if svc.Spec.ClusterIP != "" && svc.Spec.ClusterIP != v1.ClusterIPNone {
		res = append(res, svc.Spec.ClusterIP)
	}
	for _, ing := range svc.Status.LoadBalancer.Ingress {
		res = append(res, ing.IP)
	}
	return res
}

func (c *Controller) AdditionalPodSubscriptions(
	proxy *model.Proxy,
	allAddresses sets.Set[types.NamespacedName],
	currentSubs sets.Set[types.NamespacedName],
) sets.Set[types.NamespacedName] {
	shouldSubscribe := sets.New[types.NamespacedName]()

	// First, we want to handle VIP subscriptions. Example:
	// Client subscribes to VIP1. Pod1, part of VIP1, is sent.
	// The client wouldn't be explicitly subscribed to Pod1, so it would normally ignore it.
	// Since it is a part of VIP1 which we are subscribe to, add it to the subscriptions
	for s := range allAddresses {
		cNetwork := c.Network(s.Name, make(labels.Instance, 0)).String()
		for _, wl := range c.ambientIndex.Lookup(cNetwork + "/" + s.Name) {
			// We may have gotten an update for Pod, but are subscribe to a Service.
			// We need to force a subscription on the Pod as well
			switch addr := wl.Address.Type.(type) {
			case *workloadapi.Address_Workload:
				for vip := range addr.Workload.VirtualIps {
					t := types.NamespacedName{Name: vip}
					if currentSubs.Contains(t) {
						shouldSubscribe.Insert(types.NamespacedName{Name: wl.ResourceName()})
						break
					}
				}
			case *workloadapi.Address_Service:
				// ignore, results in duplicate entries pushed to proxies
			}
		}
	}

	// Next, as an optimization, we will send all node-local endpoints
	if nodeName := proxy.Metadata.NodeName; nodeName != "" {
		for _, wl := range c.ambientIndex.All() {
			switch addr := wl.Address.Type.(type) {
			case *workloadapi.Address_Workload:
				if addr.Workload.Node == nodeName {
					n := types.NamespacedName{Name: wl.ResourceName()}
					if currentSubs.Contains(n) {
						continue
					}
					shouldSubscribe.Insert(n)
				}
			case *workloadapi.Address_Service:
				// Services are not constrained to a particular node
				continue
			}
		}
	}

	return shouldSubscribe
}

func workloadNameAndType(pod *v1.Pod) (string, workloadapi.WorkloadType) {
	objMeta, typeMeta := kubeutil.GetDeployMetaFromPod(pod)
	switch typeMeta.Kind {
	case "Deployment":
		return objMeta.Name, workloadapi.WorkloadType_DEPLOYMENT
	case "Job":
		return objMeta.Name, workloadapi.WorkloadType_JOB
	case "CronJob":
		return objMeta.Name, workloadapi.WorkloadType_CRONJOB
	default:
		return pod.Name, workloadapi.WorkloadType_POD
	}
}
