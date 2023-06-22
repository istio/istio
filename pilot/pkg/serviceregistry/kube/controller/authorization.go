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
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"

	"istio.io/api/security/v1beta1"
	apiv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi/security"
)

const (
	convertedPeerAuthenticationPrefix = "converted_peer_authentication_" // use '_' character since those are illegal in k8s names
	staticStrictPolicyName            = "istio_converted_static_strict"  // use '_' character since those are illegal in k8s names
)

func (c *Controller) Policies(requested sets.Set[model.ConfigKey]) []*security.Authorization {
	if !c.configCluster {
		return nil
	}

	var cfgs []config.Config
	authzPolicies := c.configController.List(gvk.AuthorizationPolicy, metav1.NamespaceAll)
	peerAuthenticationPolicies := c.configController.List(gvk.PeerAuthentication, metav1.NamespaceAll)

	cfgs = append(cfgs, authzPolicies...)
	cfgs = append(cfgs, peerAuthenticationPolicies...)
	l := len(cfgs)
	if len(requested) > 0 {
		l = len(requested)
	}
	res := make([]*security.Authorization, 0, l)
	for _, cfg := range cfgs {
		k := model.ConfigKey{
			Kind:      kind.MustFromGVK(cfg.GroupVersionKind),
			Name:      cfg.Name,
			Namespace: cfg.Namespace,
		}
		if k.Kind == kind.PeerAuthentication {
			// PeerAuthentications are synthetic so prepend our special prefix
			k.Name = fmt.Sprintf("%s%s", convertedPeerAuthenticationPrefix, k.Name)
		}
		if len(requested) > 0 && !requested.Contains(k) {
			continue
		}

		var pol *security.Authorization
		switch cfg.GroupVersionKind {
		case gvk.AuthorizationPolicy:
			pol = convertAuthorizationPolicy(c.meshWatcher.Mesh().GetRootNamespace(), cfg)
			if pol == nil {
				continue
			}
			res = append(res, pol)
		case gvk.PeerAuthentication:
			pol = convertPeerAuthentication(c.meshWatcher.Mesh().GetRootNamespace(), cfg)
			if pol == nil {
				continue
			}
			res = append(res, pol)
		default:
			log.Errorf("unknown config type %v", cfg.GroupVersionKind)
			continue
		}
	}

	// If there are any PeerAuthentications in our cache, send our static STRICT policy
	if len(peerAuthenticationPolicies) > 0 {
		res = append(res, &security.Authorization{
			Name:      staticStrictPolicyName,
			Namespace: c.meshWatcher.Mesh().GetRootNamespace(),
			Scope:     security.Scope_WORKLOAD_SELECTOR,
			Action:    security.Action_DENY,
			Groups: []*security.Group{
				{
					Rules: []*security.Rules{
						{
							Matches: []*security.Match{
								{
									NotPrincipals: []*security.StringMatch{
										{
											MatchType: &security.StringMatch_Presence{},
										},
									},
								},
							},
						},
					},
				},
			},
		})
	}

	return res
}

// convertedSelectorPeerAuthentications returns a list of keys corresponding to one or both of:
// [static STRICT policy, port-level STRICT policy] based on the effective PeerAuthentication policy
func (c *Controller) convertedSelectorPeerAuthentications(ns string, lbls map[string]string) []string {
	var meshCfg, namespaceCfg, workloadCfg *config.Config

	rootNamespace := c.meshWatcher.Mesh().GetRootNamespace()

	matches := func(c config.Config) bool {
		sel := c.Spec.(*v1beta1.PeerAuthentication).Selector
		if sel == nil {
			return false
		}
		return labels.Instance(sel.MatchLabels).SubsetOf(lbls)
	}

	configs := c.configController.List(gvk.PeerAuthentication, rootNamespace)
	configs = append(configs, c.configController.List(gvk.PeerAuthentication, ns)...)

	for i := range configs {
		cfg := configs[i]
		spec, ok := cfg.Spec.(*v1beta1.PeerAuthentication)

		if !ok || spec == nil {
			continue
		}

		// We don't support DISABLE policies in Ambient mode
		if isMtlsModeDisable(spec.Mtls) {
			log.Debugf("Skipped disable PeerAuthentication %s/%s because they aren't supported in Ambient", cfg.Name, cfg.Namespace)
			continue
		}

		if spec.Selector == nil || len(spec.Selector.MatchLabels) == 0 {
			// Namespace-level or mesh-level policy
			if cfg.Namespace == rootNamespace {
				if meshCfg == nil || cfg.CreationTimestamp.Before(meshCfg.CreationTimestamp) {
					log.Debugf("Switch selected mesh policy to %s.%s (%v)", cfg.Name, cfg.Namespace, cfg.CreationTimestamp)
					meshCfg = &cfg
				}
			} else {
				if namespaceCfg == nil || cfg.CreationTimestamp.Before(namespaceCfg.CreationTimestamp) {
					log.Debugf("Switch selected namespace policy to %s.%s (%v)", cfg.Name, cfg.Namespace, cfg.CreationTimestamp)
					namespaceCfg = &cfg
				}
			}
		} else if cfg.Namespace != rootNamespace {
			// Workload-level policy, aka the one with selector and not in root namespace.
			if !matches(cfg) {
				continue
			}

			if workloadCfg == nil || cfg.CreationTimestamp.Before(workloadCfg.CreationTimestamp) {
				log.Debugf("Switch selected workload policy to %s.%s (%v)", cfg.Name, cfg.Namespace, cfg.CreationTimestamp)
				workloadCfg = &cfg
			}
		}
	}

	// Whether it comes from a mesh-wide, namespace-wide, or workload-specific policy
	// if the effective policy is STRICT, then reference our static STRICT policy
	var isEffectiveStrictPolicy bool
	// Only 1 per port workload policy can be effective at a time. In the case of a conflict
	// the oldest policy wins.
	var effectivePortLevelPolicyKey string

	// Process in mesh, namespace, workload order to resolve inheritance (UNSET)
	if meshCfg != nil {
		meshSpec, ok := meshCfg.Spec.(*v1beta1.PeerAuthentication)
		if ok && !isMtlsModeUnset(meshSpec.Mtls) {
			isEffectiveStrictPolicy = isMtlsModeStrict(meshSpec.Mtls)
		}
	}

	if namespaceCfg != nil {
		namespaceSpec, ok := namespaceCfg.Spec.(*v1beta1.PeerAuthentication)
		if ok && !isMtlsModeUnset(namespaceSpec.Mtls) {
			isEffectiveStrictPolicy = isMtlsModeStrict(namespaceSpec.Mtls)
		}
	}

	if workloadCfg == nil {
		return c.effectivePeerAuthenticationKeys(isEffectiveStrictPolicy, "")
	}

	workloadSpec, ok := workloadCfg.Spec.(*v1beta1.PeerAuthentication)
	if !ok {
		// no workload policy to calculate; go ahead and return the calculated keys
		return c.effectivePeerAuthenticationKeys(isEffectiveStrictPolicy, "")
	}

	// Regardless of if we have port-level overrides, if the workload policy is STRICT, then we need to reference our static STRICT policy
	if isMtlsModeStrict(workloadSpec.Mtls) {
		isEffectiveStrictPolicy = true
	}

	// Regardless of if we have port-level overrides, if the workload policy is PERMISSIVE, then we shouldn't send our static STRICT policy
	if isMtlsModePermissive(workloadSpec.Mtls) {
		isEffectiveStrictPolicy = false
	}

	if workloadSpec.PortLevelMtls != nil {
		switch workloadSpec.GetMtls().GetMode() {
		case v1beta1.PeerAuthentication_MutualTLS_STRICT:
			foundPermissive := false
			for _, portMtls := range workloadSpec.PortLevelMtls {
				if isMtlsModePermissive(portMtls) {
					foundPermissive = true
					break
				}
			}

			if foundPermissive {
				// If we found a non-strict policy, we need to reference this workload policy to see the port level exceptions
				effectivePortLevelPolicyKey = fmt.Sprintf("%s/%s%s", workloadCfg.Namespace, convertedPeerAuthenticationPrefix, workloadCfg.Name)
				isEffectiveStrictPolicy = false // don't send our static STRICT policy since the converted form of this policy will include the default STRICT mode
			}
		case v1beta1.PeerAuthentication_MutualTLS_PERMISSIVE:
			isEffectiveStrictPolicy = false // any ports that aren't specified will be PERMISSIVE so this workload isn't effectively under a STRICT policy
			foundStrict := false
			for _, portMtls := range workloadSpec.PortLevelMtls {
				if isMtlsModeStrict(portMtls) {
					foundStrict = true
					continue
				}
			}

			// There's a STRICT port mode, so we need to reference this policy in the workload
			if foundStrict {
				effectivePortLevelPolicyKey = fmt.Sprintf("%s/%s%s", workloadCfg.Namespace, convertedPeerAuthenticationPrefix, workloadCfg.Name)
			}
		default: // Unset
			if isEffectiveStrictPolicy {
				// Strict mesh or namespace policy
				foundPermissive := false
				for _, portMtls := range workloadSpec.PortLevelMtls {
					if isMtlsModePermissive(portMtls) {
						foundPermissive = true
						break
					}
				}

				if foundPermissive {
					// If we found a non-strict policy, we need to reference this workload policy to see the port level exceptions
					effectivePortLevelPolicyKey = fmt.Sprintf("%s/%s%s", workloadCfg.Namespace, convertedPeerAuthenticationPrefix, workloadCfg.Name)
				}
			} else {
				// Permissive mesh or namespace policy
				isEffectiveStrictPolicy = false // any ports that aren't specified will be PERMISSIVE so this workload isn't effectively under a STRICT policy
				foundStrict := false
				for _, portMtls := range workloadSpec.PortLevelMtls {
					if isMtlsModeStrict(portMtls) {
						foundStrict = true
						continue
					}
				}

				// There's a STRICT port mode, so we need to reference this policy in the workload
				if foundStrict {
					effectivePortLevelPolicyKey = fmt.Sprintf("%s/%s%s", workloadCfg.Namespace, convertedPeerAuthenticationPrefix, workloadCfg.Name)
				}
			}
		}
	}

	return c.effectivePeerAuthenticationKeys(isEffectiveStrictPolicy, effectivePortLevelPolicyKey)
}

func (c *Controller) effectivePeerAuthenticationKeys(isEffectiveStringPolicy bool, effectiveWorkloadPolicyKey string) []string {
	res := sets.New[string]()
	rootNamespace := c.meshWatcher.Mesh().GetRootNamespace()

	if isEffectiveStringPolicy {
		res.Insert(fmt.Sprintf("%s/%s", rootNamespace, staticStrictPolicyName))
	}

	if effectiveWorkloadPolicyKey != "" {
		res.Insert(effectiveWorkloadPolicyKey)
	}

	return sets.SortedList(res)
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

// We can't use the same optimizations as we do for AuthorizationPolicy because we dynamically send
// synthetic authorization policies to the proxy based on the effective mTLS mode. In other words, even
// if the selector of the PeerAuthentication doesn't change (indeed even if the PA has no selector),
// any number of workloads may be affected by its spec changing
func (c *Controller) PeerAuthenticationHandler(old config.Config, obj config.Config, ev model.Event) {
	getSelector := func(c config.Config) map[string]string {
		if c.Spec == nil {
			return nil
		}
		pol := c.Spec.(*v1beta1.PeerAuthentication)
		return pol.Selector.GetMatchLabels()
	}

	portMtlsEqual := func(m1, m2 map[uint32]*v1beta1.PeerAuthentication_MutualTLS) bool {
		diffDetected := false
		// Loop through all of the old PA ports
		for port, m := range m1 {
			newPortlevelMtls, ok := m2[port]
			if !ok {
				diffDetected = true // port not present in the new version of the resource; something changed
				break
			}

			if !proto.Equal(newPortlevelMtls, m) {
				diffDetected = true // port level mTLS settings changed
				break
			}
		}
		if !diffDetected {
			for port, m := range m2 {
				oldPortlevelMtls, ok := m1[port]
				if !ok {
					diffDetected = true // port not present in the old version of the resource; something changed
					break
				}

				if !proto.Equal(oldPortlevelMtls, m) {
					diffDetected = true // port level mTLS settings changed
					break
				}
			}
		}

		return !diffDetected
	}
	// Normal flow for PeerAuthentication (initRegistryEventHandlers) will trigger XDS push, so we don't need to push those. But we do need
	// to update any relevant workloads and push them.
	sel := getSelector(obj)
	oldSel := getSelector(old)

	oldPa, oldPaOk := old.Spec.(*v1beta1.PeerAuthentication)
	newPa := obj.Spec.(*v1beta1.PeerAuthentication)

	if oldPaOk && ev == model.EventUpdate {
		if sel == nil && oldSel == nil {
			// global or namespace level policy change
			if oldPa.GetMtls().GetMode() == newPa.GetMtls().GetMode() {
				// No change in mTLS mode, no workloads to push
				return
			}
		}

		mtlsUnchanged := oldPa.GetMtls().GetMode() == newPa.GetMtls().GetMode()

		portLevelMtlsUnchanged := portMtlsEqual(oldPa.GetPortLevelMtls(), newPa.GetPortLevelMtls())
		if maps.Equal(sel, oldSel) && mtlsUnchanged && portLevelMtlsUnchanged {
			// Update event, but nothing we care about changed. No workloads to push.
			return
		}
	}

	if (newPa.Mtls == nil || newPa.GetMtls().GetMode() == v1beta1.PeerAuthentication_MutualTLS_UNSET) && newPa.GetPortLevelMtls() == nil {
		// Nothing to do, no workloads to push
		return
	}

	pods := map[string]*v1.Pod{}
	for _, p := range c.getPodsInPolicy(obj.Namespace, sel, false) {
		pods[p.Status.PodIP] = p
	}
	if oldSel != nil {
		for _, p := range c.getPodsInPolicy(obj.Namespace, oldSel, false) {
			pods[p.Status.PodIP] = p
		}
	}

	updates := c.calculateUpdatedWorkloads(pods)

	if len(updates) > 0 {
		c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
			ConfigsUpdated: updates,
			Reason:         []model.TriggerReason{model.AmbientUpdate},
		})
	}
}

func (c *Controller) calculateUpdatedWorkloads(pods map[string]*v1.Pod) map[model.ConfigKey]struct{} {
	updates := map[model.ConfigKey]struct{}{}
	for _, pod := range pods {
		newWl := c.extractWorkload(pod)
		if newWl != nil {
			// Update the pod, since it now has new VIP info
			networkAddrs := networkAddressFromWorkload(newWl)
			c.ambientIndex.mu.Lock()
			for _, networkAddr := range networkAddrs {
				c.ambientIndex.byPod[networkAddr] = newWl
			}
			c.ambientIndex.byUID[c.generatePodUID(pod)] = newWl
			c.ambientIndex.mu.Unlock()
			updates[model.ConfigKey{Kind: kind.Address, Name: newWl.ResourceName()}] = struct{}{}
		}
	}

	return updates
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
	for _, p := range c.getPodsInPolicy(obj.Namespace, sel, true) {
		pods[p.Status.PodIP] = p
	}
	if oldSel != nil {
		for _, p := range c.getPodsInPolicy(obj.Namespace, oldSel, true) {
			pods[p.Status.PodIP] = p
		}
	}

	updates := c.calculateUpdatedWorkloads(pods)

	workloadEntries := map[networkAddress]*apiv1alpha3.WorkloadEntry{}
	for _, w := range c.getWorkloadEntriesInPolicy(obj.Namespace, sel) {
		network := c.Network(w.Spec.Address, w.Spec.Labels).String()
		if w.Spec.Network != "" {
			network = w.Spec.Network
		}
		workloadEntries[networkAddress{
			ip:      w.Spec.Address,
			network: network,
		}] = w
	}
	if oldSel != nil {
		for _, w := range c.getWorkloadEntriesInPolicy(obj.Namespace, oldSel) {
			network := c.Network(w.Spec.Address, w.Spec.Labels).String()
			if w.Spec.Network != "" {
				network = w.Spec.Network
			}
			workloadEntries[networkAddress{
				ip:      w.Spec.Address,
				network: network,
			}] = w
		}
	}

	for _, w := range workloadEntries {
		newWl := c.extractWorkloadEntry(w)
		if newWl != nil {
			// Update the WorkloadEntry, since it now has new VIP info
			networkAddrs := networkAddressFromWorkload(newWl)
			c.ambientIndex.mu.Lock()
			for _, networkAddr := range networkAddrs {
				c.ambientIndex.byWorkloadEntry[networkAddr] = newWl
			}
			c.ambientIndex.byUID[c.generateWorkloadEntryUID(w.GetNamespace(), w.GetName())] = newWl
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

// meshWideSelectorEnabled indicates whether a mesh-wide policy can have a selector.
// Is only true for AuthorizationPolicy, since PeerAuthentication doesn't support mesh-wide selector policies.
func (c *Controller) getPodsInPolicy(ns string, sel map[string]string, meshWideSelectorEnabled bool) []*v1.Pod {
	if ns == c.meshWatcher.Mesh().GetRootNamespace() && (sel == nil || meshWideSelectorEnabled) {
		ns = metav1.NamespaceAll
	}
	return c.podsClient.List(ns, klabels.ValidatedSetSelector(sel))
}

// convertPeerAuthentication converts a PeerAuthentication to an L4 authorization policy (i.e. security.Authorization) iff
// 1. the PeerAuthentication has a workload selector
// 2. The PeerAuthentication is NOT in the root namespace
// 3. There is a portLevelMtls policy (technically implied by 1)
// 4. The top-level PeerAuthentication mode is STRICT, UNSET, or PERMISSIVE
// 5. If the top-level mode is PERMISSIVE, there is at least one portLevelMtls policy with mode STRICT
// STRICT policies that don't have portLevelMtls will be
// handled when the Workload xDS resource is pushed (a static STRICT-equivalent policy will always be pushed)
func convertPeerAuthentication(rootNamespace string, cfg config.Config) *security.Authorization {
	pa, ok := cfg.Spec.(*v1beta1.PeerAuthentication)

	if !ok {
		return nil
	}

	mode := pa.GetMtls().GetMode()

	// Violates case #4
	if mode == v1beta1.PeerAuthentication_MutualTLS_DISABLE {
		log.Debugf("skipping PeerAuthentication %s/%s for ambient with mTLS %s mode", cfg.Namespace, cfg.Name, mode)
		return nil
	}

	scope := security.Scope_WORKLOAD_SELECTOR
	// violates case #1, #2, or #3
	if cfg.Namespace == rootNamespace || pa.Selector == nil || len(pa.PortLevelMtls) == 0 {
		log.Debugf("skipping PeerAuthentication %s/%s for ambient  since it isn't a workload policy with port level mTLS", cfg.Namespace, cfg.Name)
		return nil
	}

	action := security.Action_DENY
	var groups []*security.Group

	if mode == v1beta1.PeerAuthentication_MutualTLS_STRICT {
		groups = append(groups, &security.Group{
			Rules: []*security.Rules{
				{
					Matches: []*security.Match{
						{
							NotPrincipals: []*security.StringMatch{
								{
									MatchType: &security.StringMatch_Presence{},
								},
							},
						},
					},
				},
			},
		})
	}

	// If we have a strict policy and all of the ports are strict, it's effectively a strict policy
	// so we can exit early and have the WorkloadRbac xDS server push its static strict policy.
	// Note that this doesn't actually attach the policy to any workload; it just makes it available
	// to ztunnel in case a workload needs it.
	foundNonStrictPortmTLS := false
	for port, mtls := range pa.PortLevelMtls {
		switch portMtlsMode := mtls.GetMode(); {
		case portMtlsMode == v1beta1.PeerAuthentication_MutualTLS_STRICT:
			groups = append(groups, &security.Group{
				Rules: []*security.Rules{
					{
						Matches: []*security.Match{
							{
								NotPrincipals: []*security.StringMatch{
									{
										MatchType: &security.StringMatch_Presence{},
									},
								},
								DestinationPorts: []uint32{port},
							},
						},
					},
				},
			})
		case portMtlsMode == v1beta1.PeerAuthentication_MutualTLS_PERMISSIVE:
			// Check top-level mode
			if mode == v1beta1.PeerAuthentication_MutualTLS_PERMISSIVE {
				// we don't care; log and continue
				log.Debugf("skipping port %s for PeerAuthentication %s/%s for ambient since the parent mTLS mode is also PERMISSIVE", port, cfg.Namespace, cfg.Name)
				continue
			}
			foundNonStrictPortmTLS = true

			// If the top level policy is STRICT, we need to add a rule for the port that exempts it from the deny policy
			groups = append(groups, &security.Group{
				Rules: []*security.Rules{
					{
						Matches: []*security.Match{
							{
								NotDestinationPorts: []uint32{port}, // if the incoming connection does not match this port, deny (notice there's no principals requirement)
							},
						},
					},
				},
			})
		default:
			log.Debugf("skipping port %s for PeerAuthentication %s/%s for ambient since it is UNSET or DISABLED", port, cfg.Namespace, cfg.Name)
			continue
		}
	}

	// If the top level TLS mode is STRICT and all of the port level mTLS modes are STRICT, this is just a strict policy and we'll exit early
	if mode == v1beta1.PeerAuthentication_MutualTLS_STRICT && !foundNonStrictPortmTLS {
		return nil
	}

	if len(groups) == 0 {
		// we never added any rules; return
		return nil
	}

	opol := &security.Authorization{
		Name:      convertedPeerAuthenticationPrefix + cfg.Name,
		Namespace: cfg.Namespace,
		Scope:     scope,
		Action:    action,
		Groups:    groups,
	}

	return opol
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
		toMatches = append(toMatches, match)
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
		fromMatches = append(fromMatches, match)
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

func isMtlsModeUnset(mtls *v1beta1.PeerAuthentication_MutualTLS) bool {
	return mtls == nil || mtls.Mode == v1beta1.PeerAuthentication_MutualTLS_UNSET
}

func isMtlsModeStrict(mtls *v1beta1.PeerAuthentication_MutualTLS) bool {
	return mtls != nil && mtls.Mode == v1beta1.PeerAuthentication_MutualTLS_STRICT
}

func isMtlsModeDisable(mtls *v1beta1.PeerAuthentication_MutualTLS) bool {
	return mtls != nil && mtls.Mode == v1beta1.PeerAuthentication_MutualTLS_DISABLE
}

func isMtlsModePermissive(mtls *v1beta1.PeerAuthentication_MutualTLS) bool {
	return mtls != nil && mtls.Mode == v1beta1.PeerAuthentication_MutualTLS_PERMISSIVE
}
