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
	"sort"
	"strconv"
	"strings"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	"github.com/golang/protobuf/ptypes/wrappers"

	networkingapi "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/security/authn/factory"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/gvk"
)

type EndpointBuilder struct {
	// These fields define the primary key for an endpoint, and can be used as a cache key
	clusterName     string
	network         string
	networkView     map[string]bool
	clusterID       string
	locality        *core.Locality
	destinationRule *config.Config
	service         *model.Service

	// These fields are provided for convenience only
	subsetName string
	hostname   host.Name
	port       int
	push       *model.PushContext

	mtlsChecker *mtlsChecker
}

func NewEndpointBuilder(clusterName string, proxy *model.Proxy, push *model.PushContext) EndpointBuilder {
	_, subsetName, hostname, port := model.ParseSubsetKey(clusterName)
	svc := push.ServiceForHostname(proxy, hostname)
	dr := push.DestinationRule(proxy, svc)
	b := EndpointBuilder{
		clusterName:     clusterName,
		network:         proxy.Metadata.Network,
		networkView:     model.GetNetworkView(proxy),
		clusterID:       proxy.Metadata.ClusterID,
		locality:        proxy.Locality,
		service:         svc,
		destinationRule: dr,

		push:       push,
		subsetName: subsetName,
		hostname:   hostname,
		port:       port,
	}
	if b.MultiNetworkConfigured() || model.IsDNSSrvSubsetKey(clusterName) {
		// We only need this for multi-network, or for clusters meant for use with AUTO_PASSTHROUGH
		// As an optimization, we skip this logic entirely for everything else.
		b.mtlsChecker = newMtlsChecker(push, port, dr)
	}
	return b
}

func (b EndpointBuilder) DestinationRule() *networkingapi.DestinationRule {
	if b.destinationRule == nil {
		return nil
	}
	return b.destinationRule.Spec.(*networkingapi.DestinationRule)
}

// Key provides the eds cache key and should include any information that could change the way endpoints are generated.
func (b EndpointBuilder) Key() string {
	params := []string{b.clusterName, b.network, b.clusterID, util.LocalityToString(b.locality)}
	if b.push != nil && b.push.AuthnBetaPolicies != nil {
		params = append(params, b.push.AuthnBetaPolicies.AggregateVersion)
	}
	if b.destinationRule != nil {
		params = append(params, b.destinationRule.Name+"/"+b.destinationRule.Namespace)
	}
	if b.service != nil {
		params = append(params, string(b.service.Hostname)+"/"+b.service.Attributes.Namespace)
	}
	if b.networkView != nil {
		nv := make([]string, 0, len(b.networkView))
		for nw := range b.networkView {
			nv = append(nv, nw)
		}
		sort.Strings(nv)
		params = append(params, nv...)
	}
	return strings.Join(params, "~")
}

// MultiNetworkConfigured determines if we have gateways to use for building cross-network endpoints.
func (b *EndpointBuilder) MultiNetworkConfigured() bool {
	return b.push.NetworkGateways() != nil && len(b.push.NetworkGateways()) > 0
}

func (b EndpointBuilder) Cacheable() bool {
	// If service is not defined, we cannot do any caching as we will not have a way to
	// invalidate the results.
	// Service being nil means the EDS will be empty anyways, so not much lost here.
	return b.service != nil
}

func (b EndpointBuilder) DependentConfigs() []model.ConfigKey {
	configs := []model.ConfigKey{}
	if b.destinationRule != nil {
		configs = append(configs, model.ConfigKey{Kind: gvk.DestinationRule, Name: b.destinationRule.Name, Namespace: b.destinationRule.Namespace})
	}
	if b.service != nil {
		configs = append(configs, model.ConfigKey{Kind: gvk.ServiceEntry, Name: string(b.service.Hostname), Namespace: b.service.Attributes.Namespace})
	}
	return configs
}

var edsDependentTypes = []config.GroupVersionKind{gvk.PeerAuthentication}

func (b EndpointBuilder) DependentTypes() []config.GroupVersionKind {
	return edsDependentTypes
}

func (b *EndpointBuilder) canViewNetwork(network string) bool {
	if b.networkView == nil {
		return true
	}
	return b.networkView[network]
}

// build LocalityLbEndpoints for a cluster from existing EndpointShards.
func (b *EndpointBuilder) buildLocalityLbEndpointsFromShards(
	shards *EndpointShards,
	svcPort *model.Port,
) []*endpoint.LocalityLbEndpoints {
	localityEpMap := make(map[string]*endpoint.LocalityLbEndpoints)
	// get the subset labels
	epLabels := getSubSetLabels(b.DestinationRule(), b.subsetName)

	// Determine whether or not the target service is considered local to the cluster
	// and should, therefore, not be accessed from outside the cluster.
	isClusterLocal := b.push.IsClusterLocal(b.service)

	shards.mutex.Lock()
	// Extract shard keys so we can iterate in order. This ensures a stable EDS output. Since
	// len(shards) ~= number of remote clusters which isn't too large, doing this sort shouldn't be
	// too problematic. If it becomes an issue we can cache it in the EndpointShards struct.
	keys := make([]string, 0, len(shards.Shards))
	for k := range shards.Shards {
		keys = append(keys, k)
	}
	if len(keys) >= 2 {
		sort.Strings(keys)
	}
	// The shards are updated independently, now need to filter and merge
	// for this cluster
	for _, clusterID := range keys {
		endpoints := shards.Shards[clusterID]
		// If the downstream service is configured as cluster-local, only include endpoints that
		// reside in the same cluster.
		if isClusterLocal && (clusterID != b.clusterID) {
			continue
		}

		for _, ep := range endpoints {
			if svcPort.Name != ep.ServicePortName {
				continue
			}
			// Port labels
			if !epLabels.HasSubsetOf(ep.Labels) {
				continue
			}

			locLbEps, found := localityEpMap[ep.Locality.Label]
			if !found {
				locLbEps = &endpoint.LocalityLbEndpoints{
					Locality:    util.ConvertLocality(ep.Locality.Label),
					LbEndpoints: make([]*endpoint.LbEndpoint, 0, len(endpoints)),
				}
				localityEpMap[ep.Locality.Label] = locLbEps
			}
			if ep.EnvoyEndpoint == nil {
				ep.EnvoyEndpoint = buildEnvoyLbEndpoint(ep)
			}
			locLbEps.LbEndpoints = append(locLbEps.LbEndpoints, ep.EnvoyEndpoint)

			// detect if mTLS is possible for this endpoint, used later during ep filtering
			// this must be done while converting IstioEndpoints because we still have workload labels
			if b.mtlsChecker != nil {
				b.mtlsChecker.computeForEndpoint(ep)
			}
		}
	}
	shards.mutex.Unlock()

	locEps := make([]*endpoint.LocalityLbEndpoints, 0, len(localityEpMap))
	locs := make([]string, 0, len(localityEpMap))
	for k := range localityEpMap {
		locs = append(locs, k)
	}
	if len(locs) >= 2 {
		sort.Strings(locs)
	}
	for _, k := range locs {
		locLbEps := localityEpMap[k]
		var weight uint32
		for _, ep := range locLbEps.LbEndpoints {
			weight += ep.LoadBalancingWeight.GetValue()
		}
		locLbEps.LoadBalancingWeight = &wrappers.UInt32Value{
			Value: weight,
		}
		locEps = append(locEps, locLbEps)
	}

	if len(locEps) == 0 {
		b.push.AddMetric(model.ProxyStatusClusterNoInstances, b.clusterName, "", "")
	}

	return locEps
}

// buildEnvoyLbEndpoint packs the endpoint based on istio info.
func buildEnvoyLbEndpoint(e *model.IstioEndpoint) *endpoint.LbEndpoint {
	addr := util.BuildAddress(e.Address, e.EndpointPort)

	epWeight := e.LbWeight
	if epWeight == 0 {
		epWeight = 1
	}
	ep := &endpoint.LbEndpoint{
		LoadBalancingWeight: &wrappers.UInt32Value{
			Value: epWeight,
		},
		HostIdentifier: &endpoint.LbEndpoint_Endpoint{
			Endpoint: &endpoint.Endpoint{
				Address: addr,
			},
		},
	}

	// Istio telemetry depends on the metadata value being set for endpoints in the mesh.
	// Istio endpoint level tls transport socket configuration depends on this logic
	// Do not removepilot/pkg/xds/fake.go
	ep.Metadata = util.BuildLbEndpointMetadata(e.Network, e.TLSMode, e.WorkloadName, e.Namespace, e.Labels)

	return ep
}

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
		rootPolicyMode:       mtlsModeForDefaultTrafficPolicy(dr, svcPort),
	}
}

// mTLSDisabled returns true if the given lbEp has mTLS disabled due to any of:
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

// computeForEndpoint checks destination rule, peer authentication and metadata to determine if mTLS was turned off.
// This must be done during conversion from IstioEndpoint since we still have workload metadata.
func (c *mtlsChecker) computeForEndpoint(ep *model.IstioEndpoint) {
	if drMode := c.mtlsModeForDestinationRule(ep); drMode != nil {
		switch *drMode {
		case networkingapi.ClientTLSSettings_DISABLE:
			c.mtlsDisabledHosts[lbEpKey(ep.EnvoyEndpoint)] = struct{}{}
			return
		case networkingapi.ClientTLSSettings_ISTIO_MUTUAL:
			// don't mark this EP disabled, even if PA or tlsMode meta mark disabled
			return
		}
	}

	if envoytransportSocketMetadata(ep.EnvoyEndpoint, model.TLSModeLabelShortname) != model.IstioMutualTLSModeLabel ||
		c.mtlsDisabledByPeerAuthentication(ep) {
		c.mtlsDisabledHosts[lbEpKey(ep.EnvoyEndpoint)] = struct{}{}
	}
}

func (c *mtlsChecker) mtlsDisabledByPeerAuthentication(ep *model.IstioEndpoint) bool {
	// apply any matching peer authentications
	peerAuthnKey := ep.Labels.String() + ":" + strconv.Itoa(int(ep.EndpointPort))
	if value, ok := c.peerAuthDisabledMTLS[peerAuthnKey]; ok {
		// avoid recomputing since most EPs will have the same labels/port
		return value
	}
	c.peerAuthDisabledMTLS[peerAuthnKey] = factory.
		NewPolicyApplier(c.push, ep.Namespace, labels.Collection{ep.Labels}).
		GetMutualTLSModeForPort(ep.EndpointPort) == model.MTLSDisable
	return c.peerAuthDisabledMTLS[peerAuthnKey]
}

func (c *mtlsChecker) mtlsModeForDestinationRule(ep *model.IstioEndpoint) *networkingapi.ClientTLSSettings_TLSmode {
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

// mtlsModeForDefaultTrafficPolicy returns true if the default traffic policy on a given dr disables mTLS
func mtlsModeForDefaultTrafficPolicy(destinationRule *config.Config, port int) *networkingapi.ClientTLSSettings_TLSmode {
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
		if int(portSettings.Port.Number) == port && portSettings.Tls != nil {
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
