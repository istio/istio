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

	"github.com/hashicorp/go-multierror"
	"golang.org/x/exp/maps"
	"google.golang.org/protobuf/proto"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/api/security/v1beta1"
	"istio.io/istio/pilot/pkg/model"
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
	// TODO: this is broken, should be set of IP addresses
	waypoints map[model.WaypointScope]sets.String

	handlePods func(pods []*v1.Pod)
}

func (a *AmbientIndex) ToSnapshot() *model.AmbientSnapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return model.NewAmbientSnapshot(maps.Values(a.byPod), maps.Clone(a.waypoints))
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

func (a *AmbientIndex) updateWaypoint(sa model.WaypointScope, ipStr string, isDelete bool, c *Controller) map[model.ConfigKey]struct{} {
	addr := netip.MustParseAddr(ipStr).AsSlice()
	updates := map[model.ConfigKey]struct{}{}
	if isDelete {
		for _, wl := range a.byPod {
			if !(wl.Namespace == sa.Namespace && (sa.ServiceAccount == "" || wl.ServiceAccount == sa.ServiceAccount)) {
				continue
			}
			if wl.Labels[constants.ManagedGatewayLabel] == constants.ManagedGatewayMeshController {
				continue
			}
			wl := wl.Clone()
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
				updates[model.ConfigKey{Kind: kind.Address, Name: wl.ResourceName()}] = struct{}{}
				a.byPod[wl.ResourceName()] = wl
				for vip := range wl.VirtualIps {
					a.insertWorkloadToService(vip, wl)
				}
			}
			c.updateEndpointsOnWaypointChange(wl.CanonicalName, wl.Namespace)
		}
	} else {
		for _, wl := range a.byPod {
			if !(wl.Namespace == sa.Namespace && (sa.ServiceAccount == "" || wl.ServiceAccount == sa.ServiceAccount)) {
				continue
			}
			if wl.Labels[constants.ManagedGatewayLabel] == constants.ManagedGatewayMeshController {
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
				wl := wl.Clone()
				wl.WaypointAddresses = append(wl.WaypointAddresses, addr)
				// If there was a change, also update the VIPs and record for a push
				updates[model.ConfigKey{Kind: kind.Address, Name: wl.ResourceName()}] = struct{}{}
				a.byPod[wl.ResourceName()] = wl
				for vip := range wl.VirtualIps {
					a.insertWorkloadToService(vip, wl)
				}
			}
			c.updateEndpointsOnWaypointChange(wl.CanonicalName, wl.Namespace)
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

func (c *Controller) AmbientSnapshot() *model.AmbientSnapshot {
	return c.ambientIndex.ToSnapshot()
}

func (c *Controller) Policies(requested sets.Set[model.ConfigKey]) []*workloadapi.Authorization {
	cfgs, err := c.configController.List(gvk.AuthorizationPolicy, metav1.NamespaceAll)
	if err != nil {
		log.Warnf("failed to list policies")
		return nil
	}
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
	global, err := c.configController.List(gvk.AuthorizationPolicy, c.meshWatcher.Mesh().GetRootNamespace())
	if err != nil {
		return nil
	}
	local, err := c.configController.List(gvk.AuthorizationPolicy, ns)
	if err != nil {
		return nil
	}
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
		// Update the pod, since it now has new VIP info
		c.ambientIndex.mu.Lock()
		c.ambientIndex.byPod[ip] = newWl
		c.ambientIndex.mu.Unlock()
		updates[model.ConfigKey{Kind: kind.Address, Name: newWl.ResourceName()}] = struct{}{}
	}

	if len(updates) > 0 {
		c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
			Full:           addressChangeFullPush,
			ConfigsUpdated: updates,
			Reason:         []model.TriggerReason{model.AmbientUpdate},
		})
	}
}

func (c *Controller) getPodsInPolicy(ns string, sel map[string]string) []*v1.Pod {
	if ns == c.meshWatcher.Mesh().GetRootNamespace() {
		ns = metav1.NamespaceAll
	}
	allPods, err := c.podLister.Pods(ns).List(klabels.Everything())
	if err != nil {
		return nil
	}
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
	if p == nil {
		return nil
	}
	// First check for a waypoint for our SA explicit
	// TODO: this is not robust against temporary waypoint downtime. We also need the users intent (Gateway).
	waypoints := c.ambientIndex.waypoints[model.WaypointScope{Namespace: p.Namespace, ServiceAccount: p.Spec.ServiceAccountName}]
	if len(waypoints) == 0 {
		// if there are none, check namespace wide waypoints
		waypoints = c.ambientIndex.waypoints[model.WaypointScope{Namespace: p.Namespace}]
	}
	if p.Labels[constants.ManagedGatewayLabel] == constants.ManagedGatewayMeshController {
		// Waypoints do not have waypoints
		waypoints = nil
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

func (c *Controller) updateEndpointsOnWaypointChange(name, namespace string) {
	var errs *multierror.Error
	esLabelSelector := endpointSliceSelectorForService(name)
	switch endpointController := c.endpoints.(type) {
	case *endpointsController:
		endpoints, err := listerv1.NewEndpointsLister(c.endpoints.getInformer().GetIndexer()).Endpoints(namespace).List(esLabelSelector)
		if err != nil {
			log.Errorf("error listing endpoints associated with waypoint (%v): %v", name, err)
		}
		for _, ep := range endpoints {
			errs = multierror.Append(errs, c.endpoints.onEvent(nil, ep, model.EventAdd))
		}
	case *endpointSliceController:
		endpointSlices, err := endpointController.listSlices(namespace, esLabelSelector)
		if err != nil {
			log.Errorf("error listing endpoints associated with waypoint (%v): %v", name, err)
		}
		for _, ep := range endpointSlices {
			errs = multierror.Append(errs, c.endpoints.onEvent(nil, ep, model.EventAdd))
		}
	}
	if err := multierror.Flatten(errs.ErrorOrNil()); err != nil {
		log.Errorf("one or more errors while pushing endpoint updates for waypoint %q in namespace %s: %v", name, namespace, err)
	}
}

const addressChangeFullPush = true // TODO: // TODO(https://github.com/istio/istio/issues/42318)

func (c *Controller) setupIndex() *AmbientIndex {
	idx := AmbientIndex{
		byService: map[string][]*model.WorkloadInfo{},
		byPod:     map[string]*model.WorkloadInfo{},
		waypoints: map[model.WaypointScope]sets.String{},
	}
	// handlePod handles a Pod event. Returned is XDS events to trigger, if any.
	handlePod := func(oldObj, newObj any, isDelete bool) sets.Set[model.ConfigKey] {
		oldPod := controllers.Extract[*v1.Pod](oldObj)
		p := controllers.Extract[*v1.Pod](newObj)
		updates := sets.New[model.ConfigKey]()
		// This is a waypoint update
		if p.Labels[constants.ManagedGatewayLabel] == constants.ManagedGatewayMeshController {
			n := model.WaypointScope{Namespace: p.Namespace, ServiceAccount: p.Annotations[constants.WaypointServiceAccount]}
			ip := p.Status.PodIP
			if isDelete || !IsPodReady(p) {
				if idx.waypoints[n].Contains(ip) {
					idx.waypoints[n].Delete(ip)
					updates.Merge(idx.updateWaypoint(n, ip, true, c))
				}
			} else {
				if _, f := idx.waypoints[n]; !f {
					idx.waypoints[n] = sets.New[string]()
				}
				if !idx.waypoints[n].InsertContains(ip) {
					updates.Merge(idx.updateWaypoint(n, ip, false, c))
				}
			}
		}

		var wl *model.WorkloadInfo
		if !isDelete {
			wl = c.extractWorkload(p)
		}
		if wl == nil {
			// This is an explicit delete event, or there is no longer a Workload to create (pod NotReady, etc)
			oldWl := idx.byPod[p.Status.PodIP]
			delete(idx.byPod, p.Status.PodIP)
			if oldWl != nil {
				// If we already knew about this workload, we need to make sure we drop all VIP references as well
				for vip := range oldWl.VirtualIps {
					idx.dropWorkloadFromService(vip, p.Status.PodIP)
				}
				log.Debugf("%v: workload removed, pushing", p.Status.PodIP)
				// TODO: namespace for network?
				updates.Insert(model.ConfigKey{Kind: kind.Address, Name: p.Status.PodIP})
				return updates
			}
			// It was a 'delete' for a resource we didn't know yet, no need to send an event
			return updates
		}
		oldWl := c.extractWorkload(oldPod)
		if oldWl != nil && proto.Equal(wl.Workload, oldWl.Workload) {
			log.Debugf("%v: no change, skipping", wl.ResourceName())
			return updates
		}
		idx.byPod[p.Status.PodIP] = wl
		if oldWl != nil {
			// For updates, we will drop the VIPs and then add the new ones back. This could be optimized
			for vip := range oldWl.VirtualIps {
				idx.dropWorkloadFromService(vip, wl.ResourceName())
			}
		}
		// Update the VIP indexes as well, as needed
		for vip := range wl.VirtualIps {
			idx.insertWorkloadToService(vip, wl)
		}

		log.Debugf("%v: workload updated, pushing", wl.ResourceName())
		updates.Insert(model.ConfigKey{Kind: kind.Address, Name: p.Status.PodIP})
		return updates
	}
	idx.handlePods = func(pods []*v1.Pod) {
		idx.mu.Lock()
		defer idx.mu.Unlock()
		updates := sets.New[model.ConfigKey]()
		for _, p := range pods {
			updates = updates.Merge(handlePod(nil, p, false))
		}
		if len(updates) > 0 {
			c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
				Full:           addressChangeFullPush,
				ConfigsUpdated: updates,
				Reason:         []model.TriggerReason{model.AmbientUpdate},
			})
		}
	}

	podHandler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			idx.mu.Lock()
			defer idx.mu.Unlock()
			updates := handlePod(nil, obj, false)
			if len(updates) > 0 {
				c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
					Full:           addressChangeFullPush,
					ConfigsUpdated: updates,
					Reason:         []model.TriggerReason{model.AmbientUpdate},
				})
			}
		},
		UpdateFunc: func(oldObj, newObj any) {
			idx.mu.Lock()
			defer idx.mu.Unlock()
			updates := handlePod(oldObj, newObj, false)
			if len(updates) > 0 {
				c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
					Full:           addressChangeFullPush,
					ConfigsUpdated: updates,
					Reason:         []model.TriggerReason{model.AmbientUpdate},
				})
			}
		},
		DeleteFunc: func(obj any) {
			idx.mu.Lock()
			defer idx.mu.Unlock()
			updates := handlePod(nil, obj, true)
			if len(updates) > 0 {
				c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
					Full:           addressChangeFullPush,
					ConfigsUpdated: updates,
					Reason:         []model.TriggerReason{model.AmbientUpdate},
				})
			}
		},
	}
	c.podInformer.AddEventHandler(podHandler)

	handleService := func(obj any, isDelete bool) map[model.ConfigKey]struct{} {
		svc := controllers.Extract[*v1.Service](obj)
		vips := getVIPs(svc)
		pods := getPodsInService(c.podLister, svc)
		var wls []*model.WorkloadInfo
		for _, p := range pods {
			wl := idx.byPod[p.Status.PodIP]
			// Can be nil if it's not ready, hostNetwork, etc
			if wl != nil {
				wl = c.extractWorkload(p)
				// Update the pod, since it now has new VIP info
				idx.byPod[p.Status.PodIP] = wl
				wls = append(wls, wl)
			}
		}

		// We send an update for each *workload* IP address previously in the service; they may have changed
		updates := map[model.ConfigKey]struct{}{}
		for _, vip := range vips {
			for _, wl := range idx.byService[vip] {
				updates[model.ConfigKey{Kind: kind.Address, Name: wl.ResourceName()}] = struct{}{}
			}
		}
		// Update indexes
		if isDelete {
			for _, vip := range vips {
				delete(idx.byService, vip)
			}
		} else {
			for _, vip := range vips {
				idx.byService[vip] = wls
			}
		}
		// Fetch updates again, in case it changed from adding new workloads
		for _, vip := range vips {
			for _, wl := range idx.byService[vip] {
				updates[model.ConfigKey{Kind: kind.Address, Name: wl.ResourceName()}] = struct{}{}
			}
		}
		return updates
	}
	serviceHandler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			idx.mu.Lock()
			defer idx.mu.Unlock()
			updates := handleService(obj, false)
			if len(updates) > 0 {
				c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
					Full:           addressChangeFullPush,
					ConfigsUpdated: updates,
					Reason:         []model.TriggerReason{model.AmbientUpdate},
				})
			}
		},
		UpdateFunc: func(oldObj, newObj any) {
			idx.mu.Lock()
			defer idx.mu.Unlock()
			updates := handleService(oldObj, true)
			updates2 := handleService(newObj, false)
			if updates == nil {
				updates = updates2
			} else {
				for k, v := range updates2 {
					updates[k] = v
				}
			}

			if len(updates) > 0 {
				c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
					Full:           addressChangeFullPush,
					ConfigsUpdated: updates,
					Reason:         []model.TriggerReason{model.AmbientUpdate},
				})
			}
		},
		DeleteFunc: func(obj any) {
			idx.mu.Lock()
			defer idx.mu.Unlock()
			updates := handleService(obj, true)
			if len(updates) > 0 {
				c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
					Full:           addressChangeFullPush,
					ConfigsUpdated: updates,
					Reason:         []model.TriggerReason{model.AmbientUpdate},
				})
			}
		},
	}
	c.serviceInformer.AddEventHandler(serviceHandler)
	return &idx
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
	if pod == nil {
		return nil
	}
	if !IsPodRunning(pod) {
		return nil
	}
	if pod.Spec.HostNetwork {
		return nil
	}
	vips := map[string]*workloadapi.PortList{}
	if services, err := getPodServices(c.serviceLister, pod); err == nil && len(services) > 0 {
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
