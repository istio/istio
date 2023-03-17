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
	"bytes"
	"net/netip"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/exp/maps"
	"google.golang.org/protobuf/proto"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	"istio.io/api/security/v1beta1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/controllers"
	kubelabels "istio.io/istio/pkg/kube/labels"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
)

// AmbientIndex maintains an index of ambient WorkloadInfo objects by various keys.
// These are intentionally pre-computed based on events such that lookups are efficient.
type AmbientIndex struct {
	mu sync.RWMutex
	// byService indexes by Service (virtual) *IP address*. A given Service may have multiple IPs, thus
	// multiple entries in the map. A given IP can have many workloads associated.
	byService map[string][]*model.WorkloadInfo
	// byPod indexes by Pod IP address.
	byPod map[string]*model.WorkloadInfo

	// Map of ServiceAccount -> IP
	waypoints map[model.WaypointScope]sets.String

	// serviceVipIndex maintains an index of VIP -> Service
	serviceVipIndex *controllers.Index[*v1.Service, string]
}

// Lookup finds a given IP address.
func (a *AmbientIndex) Lookup(ip string) []*model.WorkloadInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()
	// First look at pod...
	if p, f := a.byPod[ip]; f {
		return []*model.WorkloadInfo{p}
	}
	// Fallback to service. Note: these IP ranges should be non-overlapping
	return a.byService[ip]
}

func (a *AmbientIndex) dropWorkloadFromService(svcAddress, workloadAddress string) {
	wls := a.byService[svcAddress]
	// TODO: this is inefficient, but basically we are trying to update a keyed element in a list
	// Probably we want a Map? But the list is nice for fast lookups
	filtered := make([]*model.WorkloadInfo, 0, len(wls))
	for _, inc := range wls {
		if inc.ResourceName() != workloadAddress {
			filtered = append(filtered, inc)
		}
	}
	a.byService[svcAddress] = filtered
}

func (a *AmbientIndex) insertWorkloadToService(svcAddress string, workload *model.WorkloadInfo) {
	// For simplicity, to insert we drop it then add it to the end.
	// TODO: optimize this
	a.dropWorkloadFromService(svcAddress, workload.ResourceName())
	a.byService[svcAddress] = append(a.byService[svcAddress], workload)
}

func (a *AmbientIndex) updateWaypoint(scope model.WaypointScope, ipStr string, isDelete bool, c *Controller) map[model.ConfigKey]struct{} {
	addr := netip.MustParseAddr(ipStr).AsSlice()
	updates := sets.New[model.ConfigKey]()
	if isDelete {
		for _, wl := range a.byPod {
			if wl.Labels[constants.ManagedGatewayLabel] == constants.ManagedGatewayMeshControllerLabel {
				continue
			}
			if wl.Namespace != scope.Namespace || (scope.ServiceAccount != "" && wl.ServiceAccount != scope.ServiceAccount) {
				continue
			}

			addrs := make([][]byte, 0, len(wl.WaypointAddresses))
			filtered := false
			for _, a := range wl.WaypointAddresses {
				if !bytes.Equal(a, addr) {
					addrs = append(addrs, a)
				} else {
					filtered = true
				}
			}
			wl.WaypointAddresses = addrs
			if filtered {
				// If there was a change, also update the VIPs and record for a push
				updates.Insert(model.ConfigKey{Kind: kind.Address, Name: wl.ResourceName()})
			}
			updates.Merge(c.updateEndpointsOnWaypointChange(wl))
		}
	} else {
		for _, wl := range a.byPod {
			if wl.Labels[constants.ManagedGatewayLabel] == constants.ManagedGatewayMeshControllerLabel {
				continue
			}
			if wl.Namespace != scope.Namespace || (scope.ServiceAccount != "" && wl.ServiceAccount != scope.ServiceAccount) {
				continue
			}

			found := false
			for _, a := range wl.WaypointAddresses {
				if bytes.Equal(a, addr) {
					found = true
					break
				}
			}
			if !found {
				wl.WaypointAddresses = append(wl.WaypointAddresses, addr)
				// If there was a change, also update the VIPs and record for a push
				updates.Insert(model.ConfigKey{Kind: kind.Address, Name: wl.ResourceName()})
			}
			updates.Merge(c.updateEndpointsOnWaypointChange(wl))
		}
	}
	return updates
}

// All return all known workloads. Result is un-ordered
func (a *AmbientIndex) All() []*model.WorkloadInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()
	res := make([]*model.WorkloadInfo, 0, len(a.byPod))
	// byPod will not have any duplicates, so we can just iterate over that.
	for _, wl := range a.byPod {
		res = append(res, wl)
	}
	return res
}

func (c *Controller) WorkloadsForWaypoint(scope model.WaypointScope) []*model.WorkloadInfo {
	a := c.ambientIndex
	a.mu.RLock()
	defer a.mu.RUnlock()
	var res []*model.WorkloadInfo
	// TODO: try to precompute
	for _, w := range a.byPod {
		if a.matchesScope(scope, w) {
			res = append(res, w)
		}
	}
	return res
}

// Waypoint finds all waypoint IP addresses for a given scope
func (c *Controller) Waypoint(scope model.WaypointScope) sets.Set[netip.Addr] {
	a := c.ambientIndex
	a.mu.RLock()
	defer a.mu.RUnlock()
	res := sets.New[netip.Addr]()
	for ip := range a.waypoints[scope] {
		res.Insert(netip.MustParseAddr(ip))
	}
	if len(res) > 0 {
		// Explicit has precedence
		return res
	}
	// Now look for namespace-wide
	scope.ServiceAccount = ""
	for ip := range a.waypoints[scope] {
		res.Insert(netip.MustParseAddr(ip))
	}

	return res
}

func (a *AmbientIndex) matchesScope(scope model.WaypointScope, w *model.WorkloadInfo) bool {
	if len(scope.ServiceAccount) == 0 {
		// We are a namespace wide waypoint. SA scope take precedence.
		// Check if there is one for this workloads service account
		if _, f := a.waypoints[model.WaypointScope{Namespace: scope.Namespace, ServiceAccount: w.ServiceAccount}]; f {
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

func (c *Controller) Policies(requested sets.Set[model.ConfigKey]) []*workloadapi.Authorization {
	cfgs := c.configController.List(gvk.AuthorizationPolicy, metav1.NamespaceAll)
	l := len(cfgs)
	if len(requested) > 0 {
		l = len(requested)
	}
	res := make([]*workloadapi.Authorization, 0, l)
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

func isNil(v interface{}) bool {
	return v == nil || (reflect.ValueOf(v).Kind() == reflect.Ptr && reflect.ValueOf(v).IsNil())
}

func (c *Controller) selectorAuthorizationPolicies(ns string, lbls map[string]string) []string {
	// Since this is an interface a normal nil check doesn't work (...)
	if isNil(c.configController) {
		return nil
	}
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

	pods := map[string]*v1.Pod{}
	for _, p := range c.getPodsInPolicy(obj.Namespace, sel) {
		pods[p.Status.PodIP] = p
	}
	if oldSel != nil {
		for _, p := range c.getPodsInPolicy(obj.Namespace, oldSel) {
			pods[p.Status.PodIP] = p
		}
	}

	updates := map[model.ConfigKey]struct{}{}
	for ip, pod := range pods {
		newWl := c.extractWorkload(pod)
		if newWl != nil {
			// Update the pod, since it now has new VIP info
			c.ambientIndex.mu.Lock()
			c.ambientIndex.byPod[ip] = newWl
			c.ambientIndex.mu.Unlock()
			updates[model.ConfigKey{Kind: kind.Address, Name: newWl.ResourceName()}] = struct{}{}
		}
	}

	if len(updates) > 0 {
		c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
			ConfigsUpdated: updates,
			Reason:         []model.TriggerReason{model.AmbientUpdate},
		})
	}
}

func (c *Controller) getPodsInPolicy(ns string, sel map[string]string) []*v1.Pod {
	if ns == c.meshWatcher.Mesh().GetRootNamespace() {
		ns = metav1.NamespaceAll
	}
	allPods := c.podsClient.List(ns, klabels.Everything())
	var pods []*v1.Pod
	for _, pod := range allPods {
		if labels.Instance(sel).SubsetOf(pod.Labels) {
			pods = append(pods, pod)
		}
	}

	return pods
}

func convertAuthorizationPolicy(rootns string, obj config.Config) *workloadapi.Authorization {
	pol := obj.Spec.(*v1beta1.AuthorizationPolicy)

	scope := workloadapi.Scope_WORKLOAD_SELECTOR
	if pol.Selector == nil {
		scope = workloadapi.Scope_NAMESPACE
		// TODO: TDA
		if rootns == obj.Namespace {
			scope = workloadapi.Scope_GLOBAL // TODO: global workload?
		}
	}
	action := workloadapi.Action_ALLOW
	switch pol.Action {
	case v1beta1.AuthorizationPolicy_ALLOW:
	case v1beta1.AuthorizationPolicy_DENY:
		action = workloadapi.Action_DENY
	default:
		return nil
	}
	opol := &workloadapi.Authorization{
		Name:      obj.Name,
		Namespace: obj.Namespace,
		Scope:     scope,
		Action:    action,
		Groups:    nil,
	}

	for _, rule := range pol.Rules {
		rules := handleRule(action, rule)
		if rules != nil {
			rg := &workloadapi.Group{
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

func handleRule(action workloadapi.Action, rule *v1beta1.Rule) []*workloadapi.Rules {
	toMatches := []*workloadapi.Match{}
	for _, to := range rule.To {
		op := to.Operation
		if action == workloadapi.Action_ALLOW && anyNonEmpty(op.Hosts, op.NotHosts, op.Methods, op.NotMethods, op.Paths, op.NotPaths) {
			// L7 policies never match for ALLOW
			// For DENY they will always match, so it is more restrictive
			return nil
		}
		match := &workloadapi.Match{
			DestinationPorts:    stringToPort(op.Ports),
			NotDestinationPorts: stringToPort(op.NotPorts),
		}
		// if !emptyRuleMatch(match) {
		toMatches = append(toMatches, match)
		//}
	}
	fromMatches := []*workloadapi.Match{}
	for _, from := range rule.From {
		op := from.Source
		if action == workloadapi.Action_ALLOW && anyNonEmpty(op.RemoteIpBlocks, op.NotRemoteIpBlocks, op.RequestPrincipals, op.NotRequestPrincipals) {
			// L7 policies never match for ALLOW
			// For DENY they will always match, so it is more restrictive
			return nil
		}
		match := &workloadapi.Match{
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

	rules := []*workloadapi.Rules{}
	if len(toMatches) > 0 {
		rules = append(rules, &workloadapi.Rules{Matches: toMatches})
	}
	if len(fromMatches) > 0 {
		rules = append(rules, &workloadapi.Rules{Matches: fromMatches})
	}
	for _, when := range rule.When {
		l7 := l4WhenAttributes.Contains(when.Key)
		if action == workloadapi.Action_ALLOW && !l7 {
			// L7 policies never match for ALLOW
			// For DENY they will always match, so it is more restrictive
			return nil
		}
		positiveMatch := &workloadapi.Match{
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
		rules = append(rules, &workloadapi.Rules{Matches: []*workloadapi.Match{positiveMatch}})
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

func stringToMatch(rules []string) []*workloadapi.StringMatch {
	res := make([]*workloadapi.StringMatch, 0, len(rules))
	for _, v := range rules {
		var sm *workloadapi.StringMatch
		switch {
		case v == "*":
			sm = &workloadapi.StringMatch{MatchType: &workloadapi.StringMatch_Presence{}}
		case strings.HasPrefix(v, "*"):
			sm = &workloadapi.StringMatch{MatchType: &workloadapi.StringMatch_Suffix{
				Suffix: strings.TrimPrefix(v, "*"),
			}}
		case strings.HasSuffix(v, "*"):
			sm = &workloadapi.StringMatch{MatchType: &workloadapi.StringMatch_Prefix{
				Prefix: strings.TrimSuffix(v, "*"),
			}}
		default:
			sm = &workloadapi.StringMatch{MatchType: &workloadapi.StringMatch_Exact{
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

func stringToIP(rules []string) []*workloadapi.Address {
	res := make([]*workloadapi.Address, 0, len(rules))
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

		res = append(res, &workloadapi.Address{
			Address: ipAddr.AsSlice(),
			Length:  maxCidrPrefix,
		})
	}
	return res
}

func (c *Controller) extractWorkload(p *v1.Pod) *model.WorkloadInfo {
	if p == nil || !IsPodRunning(p) || p.Spec.HostNetwork {
		return nil
	}
	var waypoints sets.String
	if p.Labels[constants.ManagedGatewayLabel] == constants.ManagedGatewayMeshControllerLabel {
		// Waypoints do not have waypoints
		waypoints = nil
	} else {
		// First check for a waypoint for our SA explicit
		// TODO: this is not robust against temporary waypoint downtime. We also need the users intent (Gateway).
		waypoints = c.ambientIndex.waypoints[model.WaypointScope{Namespace: p.Namespace, ServiceAccount: p.Spec.ServiceAccountName}]
		if len(waypoints) == 0 {
			// if there are none, check namespace wide waypoints
			waypoints = c.ambientIndex.waypoints[model.WaypointScope{Namespace: p.Namespace}]
		}
	}

	policies := c.selectorAuthorizationPolicies(p.Namespace, p.Labels)
	wl := c.constructWorkload(p, sets.SortedList(waypoints), policies)
	if wl == nil {
		return nil
	}
	return &model.WorkloadInfo{
		Workload: wl,
		Labels:   p.Labels,
	}
}

// updateEndpointsOnWaypointChange ensures that endpoints are synced for Envoy clients. Envoy clients
// maintain information about waypoints for each destination in metadata. If the waypoint changes, we need
// to sync this metadata again (add/remove waypoint IP).
// This is only needed for waypoints, as a normal workload update will already trigger and EDS push.
func (c *Controller) updateEndpointsOnWaypointChange(wl *model.WorkloadInfo) sets.Set[model.ConfigKey] {
	updates := sets.New[model.ConfigKey]()
	// For each VIP, trigger and EDS update
	for vip := range wl.VirtualIps {
		for _, svc := range c.ambientIndex.serviceVipIndex.Lookup(vip) {
			updates.Insert(model.ConfigKey{
				Kind:      kind.ServiceEntry,
				Name:      string(kube.ServiceHostname(svc.Name, svc.Namespace, c.opts.DomainSuffix)),
				Namespace: svc.Namespace,
			})
		}
	}
	return updates
}

func (c *Controller) setupIndex() *AmbientIndex {
	idx := AmbientIndex{
		byService: map[string][]*model.WorkloadInfo{},
		byPod:     map[string]*model.WorkloadInfo{},
		waypoints: map[model.WaypointScope]sets.String{},
	}

	podHandler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			updates := idx.handlePod(nil, obj, false, c)
			if len(updates) > 0 {
				c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
					ConfigsUpdated: updates,
					Reason:         []model.TriggerReason{model.AmbientUpdate},
				})
			}
		},
		UpdateFunc: func(oldObj, newObj any) {
			updates := idx.handlePod(oldObj, newObj, false, c)
			if len(updates) > 0 {
				c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
					ConfigsUpdated: updates,
					Reason:         []model.TriggerReason{model.AmbientUpdate},
				})
			}
		},
		DeleteFunc: func(obj any) {
			updates := idx.handlePod(nil, obj, true, c)
			if len(updates) > 0 {
				c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
					ConfigsUpdated: updates,
					Reason:         []model.TriggerReason{model.AmbientUpdate},
				})
			}
		},
	}
	c.podsClient.AddEventHandler(podHandler)

	serviceHandler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			idx.mu.Lock()
			defer idx.mu.Unlock()
			updates := idx.handleService(obj, false, c)
			if len(updates) > 0 {
				c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
					ConfigsUpdated: updates,
					Reason:         []model.TriggerReason{model.AmbientUpdate},
				})
			}
		},
		UpdateFunc: func(oldObj, newObj any) {
			idx.mu.Lock()
			defer idx.mu.Unlock()
			updates := idx.handleService(oldObj, true, c)
			updates2 := idx.handleService(newObj, false, c)
			if updates == nil {
				updates = updates2
			} else {
				for k, v := range updates2 {
					updates[k] = v
				}
			}

			if len(updates) > 0 {
				c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
					ConfigsUpdated: updates,
					Reason:         []model.TriggerReason{model.AmbientUpdate},
				})
			}
		},
		DeleteFunc: func(obj any) {
			idx.mu.Lock()
			defer idx.mu.Unlock()
			updates := idx.handleService(obj, true, c)
			if len(updates) > 0 {
				c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
					ConfigsUpdated: updates,
					Reason:         []model.TriggerReason{model.AmbientUpdate},
				})
			}
		},
	}
	c.services.AddEventHandler(serviceHandler)
	idx.serviceVipIndex = controllers.CreateIndex[*v1.Service, string](c.client.KubeInformer().Core().V1().Services().Informer(), getVIPs)
	return &idx
}

func (a *AmbientIndex) handlePod(oldObj, newObj any, isDelete bool, c *Controller) sets.Set[model.ConfigKey] {
	p := controllers.Extract[*v1.Pod](newObj)
	old := controllers.Extract[*v1.Pod](oldObj)
	if old != nil {
		// compare only labels and pod phase, which are what we care about
		if maps.Equal(old.Labels, p.Labels) &&
			maps.Equal(old.Annotations, p.Annotations) &&
			old.Status.Phase == p.Status.Phase &&
			IsPodReady(old) == IsPodReady(p) {
			return nil
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	updates := sets.New[model.ConfigKey]()
	// This is a waypoint update
	if p.Labels[constants.ManagedGatewayLabel] == constants.ManagedGatewayMeshControllerLabel {
		scope := model.WaypointScope{Namespace: p.Namespace, ServiceAccount: p.Annotations[constants.WaypointServiceAccount]}
		ip := p.Status.PodIP
		if isDelete || !IsPodReady(p) {
			if a.waypoints[scope].Contains(ip) {
				sets.DeleteCleanupLast(a.waypoints, scope, ip)
				updates.Merge(a.updateWaypoint(scope, ip, true, c))
			}
		} else {
			if _, f := a.waypoints[scope]; !f {
				a.waypoints[scope] = sets.New[string]()
			}
			if !a.waypoints[scope].InsertContains(ip) {
				updates.Merge(a.updateWaypoint(scope, ip, false, c))
			}
		}
	}

	var wl *model.WorkloadInfo
	if !isDelete {
		wl = c.extractWorkload(p)
	}
	oldWl := a.byPod[p.Status.PodIP]
	if wl == nil {
		// This is an explicit delete event, or there is no longer a Workload to create (pod NotReady, etc)
		delete(a.byPod, p.Status.PodIP)
		if oldWl != nil {
			// If we already knew about this workload, we need to make sure we drop all VIP references as well
			for vip := range oldWl.VirtualIps {
				a.dropWorkloadFromService(vip, p.Status.PodIP)
			}
			log.Debugf("%v: workload removed, pushing", p.Status.PodIP)
			// TODO: namespace for network?
			updates.Insert(model.ConfigKey{Kind: kind.Address, Name: p.Status.PodIP})
			return updates
		}
		// It was a 'delete' for a resource we didn't know yet, no need to send an event
		return updates
	}
	if oldWl != nil && proto.Equal(wl.Workload, oldWl.Workload) {
		log.Debugf("%v: no change, skipping", wl.ResourceName())
		return updates
	}
	a.byPod[p.Status.PodIP] = wl
	if oldWl != nil {
		// For updates, we will drop the VIPs and then add the new ones back. This could be optimized
		for vip := range oldWl.VirtualIps {
			a.dropWorkloadFromService(vip, wl.ResourceName())
		}
	}
	// Update the VIP indexes as well, as needed
	for vip := range wl.VirtualIps {
		a.insertWorkloadToService(vip, wl)
	}

	log.Debugf("%v: workload updated, pushing", wl.ResourceName())
	updates.Insert(model.ConfigKey{Kind: kind.Address, Name: p.Status.PodIP})
	return updates
}

func (a *AmbientIndex) handlePods(pods []*v1.Pod, c *Controller) {
	updates := sets.New[model.ConfigKey]()
	for _, p := range pods {
		updates = updates.Merge(a.handlePod(nil, p, false, c))
	}
	if len(updates) > 0 {
		c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
			ConfigsUpdated: updates,
			Reason:         []model.TriggerReason{model.AmbientUpdate},
		})
	}
}

func (a *AmbientIndex) handleService(obj any, isDelete bool, c *Controller) map[model.ConfigKey]struct{} {
	svc := controllers.Extract[*v1.Service](obj)
	vips := getVIPs(svc)
	allPods := c.podsClient.List(svc.Namespace, klabels.Everything())
	pods := getPodsInService(allPods, svc)
	var wls []*model.WorkloadInfo
	for _, p := range pods {
		// Can be nil if it's not ready, hostNetwork, etc
		wl := c.extractWorkload(p)
		if wl != nil {
			// Update the pod, since it now has new VIP info
			a.byPod[p.Status.PodIP] = wl
			wls = append(wls, wl)
		}

	}

	// We send an update for each *workload* IP address previously in the service; they may have changed
	updates := map[model.ConfigKey]struct{}{}
	for _, vip := range vips {
		for _, wl := range a.byService[vip] {
			updates[model.ConfigKey{Kind: kind.Address, Name: wl.ResourceName()}] = struct{}{}
		}
	}
	// Update indexes
	if isDelete {
		for _, vip := range vips {
			delete(a.byService, vip)
		}
	} else {
		for _, vip := range vips {
			a.byService[vip] = wls
		}
	}
	// Fetch updates again, in case it changed from adding new workloads
	for _, vip := range vips {
		for _, wl := range a.byService[vip] {
			updates[model.ConfigKey{Kind: kind.Address, Name: wl.ResourceName()}] = struct{}{}
		}
	}
	return updates
}

// PodInformation returns all WorkloadInfo's in the cluster.
// This may be scoped to specific subsets by specifying a non-empty addresses field
func (c *Controller) PodInformation(addresses sets.Set[types.NamespacedName]) ([]*model.WorkloadInfo, []string) {
	if len(addresses) == 0 {
		// Full update
		return c.ambientIndex.All(), nil
	}
	var wls []*model.WorkloadInfo
	var removed []string
	for p := range addresses {
		wl := c.ambientIndex.Lookup(p.Name)
		if len(wl) == 0 {
			removed = append(removed, p.Name)
		} else {
			wls = append(wls, wl...)
		}
	}
	return wls, removed
}

func (c *Controller) constructWorkload(pod *v1.Pod, waypoints []string, policies []string) *workloadapi.Workload {
	vips := map[string]*workloadapi.PortList{}
	allServices := c.services.List(pod.Namespace, klabels.Everything())
	if services := getPodServices(allServices, pod); len(services) > 0 {
		for _, svc := range services {
			for _, vip := range getVIPs(svc) {
				if vips[vip] == nil {
					vips[vip] = &workloadapi.PortList{}
				}
				for _, port := range svc.Spec.Ports {
					if port.Protocol != v1.ProtocolTCP {
						continue
					}
					targetPort, err := FindPort(pod, &port)
					if err != nil {
						log.Debug(err)
						continue
					}
					vips[vip].Ports = append(vips[vip].Ports, &workloadapi.Port{
						ServicePort: uint32(port.Port),
						TargetPort:  uint32(targetPort),
					})
				}
			}
		}
	}

	wl := &workloadapi.Workload{
		Name:                  pod.Name,
		Namespace:             pod.Namespace,
		Address:               parseIP(pod.Status.PodIP),
		Network:               c.network.String(),
		ServiceAccount:        pod.Spec.ServiceAccountName,
		Node:                  pod.Spec.NodeName,
		VirtualIps:            vips,
		AuthorizationPolicies: policies,
		Status:                workloadapi.WorkloadStatus_HEALTHY,
		ClusterId:             c.Cluster().String(),
	}
	if !IsPodReady(pod) {
		wl.Status = workloadapi.WorkloadStatus_UNHEALTHY
	}
	if td := spiffe.GetTrustDomain(); td != "cluster.local" {
		wl.TrustDomain = td
	}

	wl.WorkloadName, wl.WorkloadType = workloadNameAndType(pod)
	wl.CanonicalName, wl.CanonicalRevision = kubelabels.CanonicalService(pod.Labels, wl.WorkloadName)
	// If we have a remote proxy, configure it
	if len(waypoints) > 0 {
		ips := make([][]byte, 0, len(waypoints))
		for _, r := range waypoints {
			ips = append(ips, netip.MustParseAddr(r).AsSlice())
		}
		wl.WaypointAddresses = ips
	}

	if pod.Annotations[constants.AmbientRedirection] == constants.AmbientRedirectionEnabled {
		// Configured for override
		wl.Protocol = workloadapi.Protocol_HTTP
	}
	// Otherwise supports tunnel directly
	if model.SupportsTunnel(pod.Labels, model.TunnelHTTP) {
		wl.Protocol = workloadapi.Protocol_HTTP
		wl.NativeHbone = true
	}
	return wl
}

func parseIP(ip string) []byte {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return nil
	}
	return addr.AsSlice()
}

func getVIPs(svc *v1.Service) []string {
	res := []string{}
	if svc.Spec.ClusterIP != "" && svc.Spec.ClusterIP != "None" {
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
		for _, wl := range c.ambientIndex.Lookup(s.Name) {
			// We may have gotten an update for Pod, but are subscribe to a Service.
			// We need to force a subscription on the Pod as well
			for addr := range wl.VirtualIps {
				t := types.NamespacedName{Name: addr}
				if currentSubs.Contains(t) {
					shouldSubscribe.Insert(types.NamespacedName{Name: wl.ResourceName()})
					break
				}
			}
		}
	}

	// Next, as an optimization, we will send all node-local endpoints
	if nodeName := proxy.Metadata.NodeName; nodeName != "" {
		for _, wl := range c.ambientIndex.All() {
			if wl.Node == nodeName {
				n := types.NamespacedName{Name: wl.ResourceName()}
				if currentSubs.Contains(n) {
					continue
				}
				shouldSubscribe.Insert(n)
			}
		}
	}

	return shouldSubscribe
}

func workloadNameAndType(pod *v1.Pod) (string, workloadapi.WorkloadType) {
	if len(pod.GenerateName) == 0 {
		return pod.Name, workloadapi.WorkloadType_POD
	}

	// if the pod name was generated (or is scheduled for generation), we can begin an investigation into the controlling reference for the pod.
	var controllerRef metav1.OwnerReference
	controllerFound := false
	for _, ref := range pod.GetOwnerReferences() {
		if ref.Controller != nil && *ref.Controller {
			controllerRef = ref
			controllerFound = true
			break
		}
	}

	if !controllerFound {
		return pod.Name, workloadapi.WorkloadType_POD
	}

	// heuristic for deployment detection
	if controllerRef.Kind == "ReplicaSet" && strings.HasSuffix(controllerRef.Name, pod.Labels["pod-template-hash"]) {
		name := strings.TrimSuffix(controllerRef.Name, "-"+pod.Labels["pod-template-hash"])
		return name, workloadapi.WorkloadType_DEPLOYMENT
	}

	if controllerRef.Kind == "Job" {
		// figure out how to go from Job -> CronJob
		return controllerRef.Name, workloadapi.WorkloadType_JOB
	}

	if controllerRef.Kind == "CronJob" {
		// figure out how to go from Job -> CronJob
		return controllerRef.Name, workloadapi.WorkloadType_CRONJOB
	}

	return pod.Name, workloadapi.WorkloadType_POD
}
