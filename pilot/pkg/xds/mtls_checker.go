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

package xds

import (
	"strconv"

	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"

	networkingapi "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/security/authn/factory"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/labels"
)

// TODO this logic is probably done elsewhere in XDS, possible code-reuse + perf improvements
type mtlsChecker struct {
	push            *model.PushContext
	svcPort         int
	destinationRule *networkingapi.DestinationRule

	// cache of host identifiers that have mTLS disabled
	mtlsDisabledHosts map[string]struct{}

	// cache of labels/port that have mTLS disabled by peerAuthn
	peerAuthDisabledMTLS map[string]bool
	// cache of labels that have mTLS modes set for subset policies
	subsetPolicyMode map[string]*networkingapi.ClientTLSSettings_TLSmode
	// the tlsMode of the root traffic policy if it's set
	rootPolicyMode *networkingapi.ClientTLSSettings_TLSmode
}

func newMtlsChecker(push *model.PushContext, svcPort int, dr *config.Config) *mtlsChecker {
	var drSpec *networkingapi.DestinationRule
	if dr != nil {
		drSpec = dr.Spec.(*networkingapi.DestinationRule)
	}
	return &mtlsChecker{
		push:                 push,
		svcPort:              svcPort,
		destinationRule:      drSpec,
		mtlsDisabledHosts:    map[string]struct{}{},
		peerAuthDisabledMTLS: map[string]bool{},
		subsetPolicyMode:     map[string]*networkingapi.ClientTLSSettings_TLSmode{},
		rootPolicyMode:       tlsModeForDefaultTrafficPolicy(dr, svcPort),
	}
}

// isMtlsDisabled returns true if the given lbEp has mTLS disabled due to any of:
// - disabled tlsMode
// - DestinationRule disabling mTLS on the entire host or the port
// - PeerAuthentication disabling mTLS at any applicable level (mesh, ns, workload, port)
func (c *mtlsChecker) isMtlsDisabled(lbEp *endpoint.LbEndpoint) bool {
	if c == nil {
		return false
	}
	_, ok := c.mtlsDisabledHosts[lbEpKey(lbEp)]
	return ok
}

// computeForEndpoint checks destination rule, peer authentication and tls mode labels to determine if mTLS was turned off.
func (c *mtlsChecker) computeForEndpoint(ep *model.IstioEndpoint) {
	if drMode := c.tlsModeForDestinationRule(ep); drMode != nil {
		switch *drMode {
		case networkingapi.ClientTLSSettings_DISABLE:
			c.mtlsDisabledHosts[lbEpKey(ep.EnvoyEndpoint)] = struct{}{}
			return
		case networkingapi.ClientTLSSettings_ISTIO_MUTUAL:
			// don't mark this EP disabled, even if PA or tlsMode meta mark disabled
			return
		}
	}

	// if endpoint has no sidecar or explicitly tls disabled by "security.istio.io/tlsMode" label.
	if ep.TLSMode != model.IstioMutualTLSModeLabel {
		c.mtlsDisabledHosts[lbEpKey(ep.EnvoyEndpoint)] = struct{}{}
		return
	}

	mtlsDisabledByPeerAuthentication := func(ep *model.IstioEndpoint) bool {
		// apply any matching peer authentications
		peerAuthnKey := ep.Labels.String() + ":" + strconv.Itoa(int(ep.EndpointPort))
		if value, ok := c.peerAuthDisabledMTLS[peerAuthnKey]; ok {
			// avoid recomputing since most EPs will have the same labels/port
			return value
		}
		c.peerAuthDisabledMTLS[peerAuthnKey] = factory.
			NewMtlsPolicy(c.push, ep.Namespace, ep.Labels).
			GetMutualTLSModeForPort(ep.EndpointPort) == model.MTLSDisable
		return c.peerAuthDisabledMTLS[peerAuthnKey]
	}

	//  mtls disabled by PeerAuthentication
	if mtlsDisabledByPeerAuthentication(ep) {
		c.mtlsDisabledHosts[lbEpKey(ep.EnvoyEndpoint)] = struct{}{}
	}
}

func (c *mtlsChecker) tlsModeForDestinationRule(ep *model.IstioEndpoint) *networkingapi.ClientTLSSettings_TLSmode {
	if c.destinationRule == nil || len(c.destinationRule.Subsets) == 0 {
		return c.rootPolicyMode
	}

	drSubsetKey := ep.Labels.String()
	if value, ok := c.subsetPolicyMode[drSubsetKey]; ok {
		// avoid recomputing since most EPs will have the same labels/port
		return value
	}

	subsetValue := c.rootPolicyMode
	for _, subset := range c.destinationRule.Subsets {
		if labels.Instance(subset.Labels).SubsetOf(ep.Labels) {
			mode := trafficPolicyTLSModeForPort(subset.TrafficPolicy, c.svcPort)
			if mode != nil {
				subsetValue = mode
			}
			break
		}
	}
	c.subsetPolicyMode[drSubsetKey] = subsetValue
	return subsetValue
}

// tlsModeForDefaultTrafficPolicy returns the tls mode of the port defined by a given destinationrule.
func tlsModeForDefaultTrafficPolicy(destinationRule *config.Config, port int) *networkingapi.ClientTLSSettings_TLSmode {
	if destinationRule == nil {
		return nil
	}
	dr, ok := destinationRule.Spec.(*networkingapi.DestinationRule)
	if !ok || dr == nil {
		return nil
	}
	return trafficPolicyTLSModeForPort(dr.GetTrafficPolicy(), port)
}

func trafficPolicyTLSModeForPort(tp *networkingapi.TrafficPolicy, port int) *networkingapi.ClientTLSSettings_TLSmode {
	if tp == nil {
		return nil
	}
	var mode *networkingapi.ClientTLSSettings_TLSmode
	if tp.Tls != nil {
		mode = &tp.Tls.Mode
	}
	// if there is a port-level setting matching this cluster
	for _, portSettings := range tp.GetPortLevelSettings() {
		if int(portSettings.GetPort().GetNumber()) == port && portSettings.Tls != nil {
			mode = &portSettings.Tls.Mode
			break
		}
	}
	return mode
}

func lbEpKey(b *endpoint.LbEndpoint) string {
	if addr := b.GetEndpoint().GetAddress().GetSocketAddress(); addr != nil {
		return addr.Address + ":" + strconv.Itoa(int(addr.GetPortValue()))
	}
	if addr := b.GetEndpoint().GetAddress().GetPipe(); addr != nil {
		return addr.GetPath() + ":" + strconv.Itoa(int(addr.GetMode()))
	}
	return ""
}
