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

package endpoints

import (
	"strings"

	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"

	networkingapi "istio.io/api/networking/v1alpha3"
	securityapi "istio.io/api/security/v1beta1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/security/authn"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/util/sets"
)

// TODO this logic is probably done elsewhere in XDS, possible code-reuse + perf improvements
type mtlsChecker struct {
	push             *model.PushContext
	authnPolicies    model.PeerAuthnPolicies
	svcPort          int
	destinationRule  *networkingapi.ClientTLSSettings_TLSmode
	policies         map[string]authn.MtlsPolicy
	selectorKeysByNS map[string][]string
}

func newMtlsChecker(push *model.PushContext, authnPolicies model.PeerAuthnPolicies, svcPort int, dr *config.Config, subset string) *mtlsChecker {
	return &mtlsChecker{
		push:            push,
		authnPolicies:   authnPolicies,
		svcPort:         svcPort,
		destinationRule: tlsModeForDestinationRule(dr, subset, svcPort),
	}
}

// isMtlsEnabled returns true if the given lbEp has mTLS enabled.
func isMtlsEnabled(lbEp *endpoint.LbEndpoint) bool {
	return lbEp.Metadata.FilterMetadata[util.EnvoyTransportSocketMetadataKey].
		GetFields()[model.TLSModeLabelShortname].
		GetStringValue() == model.IstioMutualTLSModeLabel
}

// checkMtlsEnabled computes whether mTLS should be enabled or not. This is determined based
// on the DR, original endpoint TLSMode (based on injection of sidecar), and PeerAuthentication settings.
func (c *mtlsChecker) checkMtlsEnabled(ep *model.IstioEndpoint, isWaypoint bool) bool {
	if drMode := c.destinationRule; drMode != nil {
		return *drMode == networkingapi.ClientTLSSettings_ISTIO_MUTUAL
	}

	// if endpoint has no sidecar or explicitly tls disabled by "security.istio.io/tlsMode" label.
	if ep.TLSMode != model.IstioMutualTLSModeLabel {
		return false
	}

	// Selecting policies scans every PeerAuthentication in the namespace, so share the result between endpoints with the same labels.
	key := c.policyKey(ep)
	policy, f := c.policies[key]
	if !f {
		policy = authn.NewMtlsPolicy(c.push, c.authnPolicies, ep.Namespace, ep.Labels, isWaypoint)
		if c.policies == nil {
			c.policies = make(map[string]authn.MtlsPolicy)
		}
		c.policies[key] = policy
	}
	return policy.GetMutualTLSModeForPort(ep.EndpointPort) != model.MTLSDisable
}

// policyKey is built only from the labels some selector PeerAuthentication in the namespace looks at.
func (c *mtlsChecker) policyKey(ep *model.IstioEndpoint) string {
	keys, f := c.selectorKeysByNS[ep.Namespace]
	if !f {
		keys = c.selectorLabelKeys(ep.Namespace)
		if c.selectorKeysByNS == nil {
			c.selectorKeysByNS = make(map[string][]string)
		}
		c.selectorKeysByNS[ep.Namespace] = keys
	}
	if len(keys) == 0 {
		return ep.Namespace
	}
	var b strings.Builder
	b.WriteString(ep.Namespace)
	for _, k := range keys {
		v, ok := ep.Labels[k]
		b.WriteByte(0)
		if ok {
			b.WriteByte(1)
			b.WriteString(v)
		}
	}
	return b.String()
}

func (c *mtlsChecker) selectorLabelKeys(namespace string) []string {
	all := c.authnPolicies.GetPeerAuthentications()
	keys := sets.New[string]()
	for _, ns := range []string{namespace, c.authnPolicies.GetRootNamespace()} {
		for _, cfg := range all[ns] {
			for k := range cfg.Spec.(*securityapi.PeerAuthentication).GetSelector().GetMatchLabels() {
				keys.Insert(k)
			}
		}
	}
	return sets.SortedList(keys)
}

func tlsModeForDestinationRule(drc *config.Config, subset string, port int) *networkingapi.ClientTLSSettings_TLSmode {
	if drc == nil {
		return nil
	}
	dr, ok := drc.Spec.(*networkingapi.DestinationRule)
	if !ok || dr == nil {
		return nil
	}

	if subset == "" {
		return trafficPolicyTLSModeForPort(dr.GetTrafficPolicy(), port)
	}

	for _, ss := range dr.Subsets {
		if ss.Name != subset {
			continue
		}

		if mode := trafficPolicyTLSModeForPort(ss.GetTrafficPolicy(), port); mode != nil {
			return mode
		}

		// fallback to the destination rule traffic policy if the subset does not have a TLS mode for this port
		return trafficPolicyTLSModeForPort(dr.GetTrafficPolicy(), port)
	}
	return nil
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
