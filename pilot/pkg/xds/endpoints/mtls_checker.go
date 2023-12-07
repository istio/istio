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
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"

	networkingapi "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/security/authn"
	"istio.io/istio/pkg/config"
)

// TODO this logic is probably done elsewhere in XDS, possible code-reuse + perf improvements
type mtlsChecker struct {
	push            *model.PushContext
	svcPort         int
	destinationRule *networkingapi.ClientTLSSettings_TLSmode
}

func newMtlsChecker(push *model.PushContext, svcPort int, dr *config.Config, subset string) *mtlsChecker {
	return &mtlsChecker{
		push:            push,
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

	return authn.
		NewMtlsPolicy(c.push, ep.Namespace, ep.Labels, isWaypoint).
		GetMutualTLSModeForPort(ep.EndpointPort) != model.MTLSDisable
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
		return trafficPolicyTLSModeForPort(ss.GetTrafficPolicy(), port)
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
