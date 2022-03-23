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
	"crypto/md5"
	"encoding/hex"
	"sort"
	"strconv"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	"google.golang.org/protobuf/proto"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	networkingapi "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/security/authn/factory"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/network"
)

// Return the tunnel type for this endpoint builder. If the endpoint builder builds h2tunnel, the final endpoint
// collection includes only the endpoints which support H2 tunnel and the non-tunnel endpoints. The latter case is to
// support multi-cluster service.
// Revisit non-tunnel endpoint decision once the gateways supports tunnel.
// TODO(lambdai): Propose to istio api.
func GetTunnelBuilderType(_ string, proxy *model.Proxy, _ *model.PushContext) networking.TunnelType {
	if proxy == nil || proxy.Metadata == nil || proxy.Metadata.ProxyConfig == nil {
		return networking.NoTunnel
	}
	if outTunnel, ok := proxy.Metadata.ProxyConfig.ProxyMetadata["tunnel"]; ok {
		switch outTunnel {
		case networking.H2TunnelTypeName:
			return networking.H2Tunnel
		default:
			// passthrough
		}
	}
	return networking.NoTunnel
}

type EndpointBuilder struct {
	// These fields define the primary key for an endpoint, and can be used as a cache key
	clusterName     string
	network         network.ID
	networkView     map[network.ID]bool
	clusterID       cluster.ID
	locality        *core.Locality
	destinationRule *config.Config
	service         *model.Service
	clusterLocal    bool
	tunnelType      networking.TunnelType

	// These fields are provided for convenience only
	subsetName string
	hostname   host.Name
	port       int
	push       *model.PushContext
	proxy      *model.Proxy

	mtlsChecker *mtlsChecker
}

func NewEndpointBuilder(clusterName string, proxy *model.Proxy, push *model.PushContext) EndpointBuilder {
	_, subsetName, hostname, port := model.ParseSubsetKey(clusterName)
	svc := push.ServiceForHostname(proxy, hostname)

	var dr *config.Config
	if svc != nil {
		dr = proxy.SidecarScope.DestinationRule(svc.Hostname)
	}
	b := EndpointBuilder{
		clusterName:     clusterName,
		network:         proxy.Metadata.Network,
		networkView:     proxy.GetNetworkView(),
		clusterID:       proxy.Metadata.ClusterID,
		locality:        proxy.Locality,
		service:         svc,
		clusterLocal:    push.IsClusterLocal(svc),
		destinationRule: dr,
		tunnelType:      GetTunnelBuilderType(clusterName, proxy, push),

		push:       push,
		proxy:      proxy,
		subsetName: subsetName,
		hostname:   hostname,
		port:       port,
	}

	// We need this for multi-network, or for clusters meant for use with AUTO_PASSTHROUGH.
	if features.EnableAutomTLSCheckPolicies ||
		b.push.NetworkManager().IsMultiNetworkEnabled() || model.IsDNSSrvSubsetKey(clusterName) {
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
	params := []string{
		b.clusterName,
		string(b.network),
		string(b.clusterID),
		strconv.FormatBool(b.clusterLocal),
		util.LocalityToString(b.locality),
		b.tunnelType.ToString(),
	}
	if b.push != nil && b.push.AuthnPolicies != nil {
		params = append(params, b.push.AuthnPolicies.GetVersion())
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
			nv = append(nv, string(nw))
		}
		sort.Strings(nv)
		params = append(params, nv...)
	}
	hash := md5.New()
	for _, param := range params {
		hash.Write([]byte(param))
	}
	sum := hash.Sum(nil)
	return hex.EncodeToString(sum)
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

func (b *EndpointBuilder) canViewNetwork(network network.ID) bool {
	if b.networkView == nil {
		return true
	}
	return b.networkView[network]
}

// TODO(lambdai): Receive port value(15009 by default), builder to cover wide cases.
type EndpointTunnelApplier interface {
	// Mutate LbEndpoint in place. Return non-nil on failure.
	ApplyTunnel(lep *endpoint.LbEndpoint, tunnelType networking.TunnelType) (*endpoint.LbEndpoint, error)
}

type EndpointNoTunnelApplier struct{}

// Note that this will not return error if another tunnel typs requested.
func (t *EndpointNoTunnelApplier) ApplyTunnel(lep *endpoint.LbEndpoint, _ networking.TunnelType) (*endpoint.LbEndpoint, error) {
	return lep, nil
}

type EndpointH2TunnelApplier struct{}

// TODO(lambdai): Set original port if the default cluster original port is not the same.
func (t *EndpointH2TunnelApplier) ApplyTunnel(lep *endpoint.LbEndpoint, tunnelType networking.TunnelType) (*endpoint.LbEndpoint, error) {
	switch tunnelType {
	case networking.H2Tunnel:
		if ep := lep.GetEndpoint(); ep != nil {
			if ep.Address.GetSocketAddress().GetPortValue() != 0 {
				newEp := proto.Clone(lep).(*endpoint.LbEndpoint)
				newEp.GetEndpoint().Address.GetSocketAddress().PortSpecifier = &core.SocketAddress_PortValue{
					PortValue: 15009,
				}
				return newEp, nil
			}
		}
		return lep, nil
	case networking.NoTunnel:
		return lep, nil
	default:
		panic("supported tunnel type")
	}
}

type LocLbEndpointsAndOptions struct {
	istioEndpoints []*model.IstioEndpoint
	// The protobuf message which contains LbEndpoint slice.
	llbEndpoints endpoint.LocalityLbEndpoints
	// The runtime information of the LbEndpoint slice. Each LbEndpoint has individual metadata at the same index.
	tunnelMetadata []EndpointTunnelApplier
}

// Return prefer H2 tunnel metadata.
func MakeTunnelApplier(_ *endpoint.LbEndpoint, tunnelOpt networking.TunnelAbility) EndpointTunnelApplier {
	if tunnelOpt.SupportH2Tunnel() {
		return &EndpointH2TunnelApplier{}
	}
	return &EndpointNoTunnelApplier{}
}

func (e *LocLbEndpointsAndOptions) append(ep *model.IstioEndpoint, le *endpoint.LbEndpoint, tunnelOpt networking.TunnelAbility) {
	e.istioEndpoints = append(e.istioEndpoints, ep)
	e.llbEndpoints.LbEndpoints = append(e.llbEndpoints.LbEndpoints, le)
	e.tunnelMetadata = append(e.tunnelMetadata, MakeTunnelApplier(le, tunnelOpt))
}

func (e *LocLbEndpointsAndOptions) refreshWeight() {
	var weight *wrappers.UInt32Value
	if len(e.llbEndpoints.LbEndpoints) == 0 {
		weight = nil
	} else {
		weight = &wrappers.UInt32Value{}
		for _, lbEp := range e.llbEndpoints.LbEndpoints {
			weight.Value += lbEp.GetLoadBalancingWeight().Value
		}
	}
	e.llbEndpoints.LoadBalancingWeight = weight
}

func (e *LocLbEndpointsAndOptions) AssertInvarianceInTest() {
	if len(e.llbEndpoints.LbEndpoints) != len(e.tunnelMetadata) {
		panic(" len(e.llbEndpoints.LbEndpoints) != len(e.tunnelMetadata)")
	}
}

// build LocalityLbEndpoints for a cluster from existing EndpointShards.
func (b *EndpointBuilder) buildLocalityLbEndpointsFromShards(
	shards *EndpointShards,
	svcPort *model.Port,
) []*LocLbEndpointsAndOptions {
	localityEpMap := make(map[string]*LocLbEndpointsAndOptions)
	// get the subset labels
	epLabels := getSubSetLabels(b.DestinationRule(), b.subsetName)

	// Determine whether or not the target service is considered local to the cluster
	// and should, therefore, not be accessed from outside the cluster.
	isClusterLocal := b.clusterLocal

	shards.mutex.Lock()
	// Extract shard keys so we can iterate in order. This ensures a stable EDS output. Since
	// len(shards) ~= number of remote clusters which isn't too large, doing this sort shouldn't be
	// too problematic. If it becomes an issue we can cache it in the EndpointShards struct.
	keys := make([]model.ShardKey, 0, len(shards.Shards))
	for k := range shards.Shards {
		keys = append(keys, k)
	}
	if len(keys) >= 2 {
		sort.Slice(keys, func(i, j int) bool {
			return keys[i] < keys[j]
		})
	}
	// The shards are updated independently, now need to filter and merge for this cluster
	for _, shardKey := range keys {
		endpoints := shards.Shards[shardKey]
		// If the downstream service is configured as cluster-local, only include endpoints that
		// reside in the same cluster.
		if isClusterLocal && (shardKey.Cluster() != b.clusterID) {
			continue
		}
		for _, ep := range endpoints {
			// TODO(nmittler): Consider merging discoverability policy with cluster-local
			if !ep.IsDiscoverableFromProxy(b.proxy) {
				continue
			}
			if svcPort.Name != ep.ServicePortName {
				continue
			}
			// Port labels
			if !epLabels.HasSubsetOf(ep.Labels) {
				continue
			}

			locLbEps, found := localityEpMap[ep.Locality.Label]
			if !found {
				locLbEps = &LocLbEndpointsAndOptions{
					llbEndpoints: endpoint.LocalityLbEndpoints{
						Locality:    util.ConvertLocality(ep.Locality.Label),
						LbEndpoints: make([]*endpoint.LbEndpoint, 0, len(endpoints)),
					},
					tunnelMetadata: make([]EndpointTunnelApplier, 0, len(endpoints)),
				}
				localityEpMap[ep.Locality.Label] = locLbEps
			}
			if ep.EnvoyEndpoint == nil {
				ep.EnvoyEndpoint = buildEnvoyLbEndpoint(ep)
			}
			// detect if mTLS is possible for this endpoint, used later during ep filtering
			// this must be done while converting IstioEndpoints because we still have workload labels
			if b.mtlsChecker != nil {
				b.mtlsChecker.computeForEndpoint(ep)
				if features.EnableAutomTLSCheckPolicies {
					tlsMode := ep.TLSMode
					if b.mtlsChecker.isMtlsDisabled(ep.EnvoyEndpoint) {
						tlsMode = ""
					}
					if nep, modified := util.MaybeApplyTLSModeLabel(ep.EnvoyEndpoint, tlsMode); modified {
						ep.EnvoyEndpoint = nep
					}
				}
			}
			locLbEps.append(ep, ep.EnvoyEndpoint, ep.TunnelAbility)
		}
	}
	shards.mutex.Unlock()

	locEps := make([]*LocLbEndpointsAndOptions, 0, len(localityEpMap))
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
		for _, ep := range locLbEps.llbEndpoints.LbEndpoints {
			weight += ep.LoadBalancingWeight.GetValue()
		}
		locLbEps.llbEndpoints.LoadBalancingWeight = &wrappers.UInt32Value{
			Value: weight,
		}
		locEps = append(locEps, locLbEps)
	}

	if len(locEps) == 0 {
		b.push.AddMetric(model.ProxyStatusClusterNoInstances, b.clusterName, "", "")
	}

	return locEps
}

// TODO(lambdai): Handle ApplyTunnel error return value by filter out the failed endpoint.
func (b *EndpointBuilder) ApplyTunnelSetting(llbOpts []*LocLbEndpointsAndOptions, tunnelType networking.TunnelType) []*LocLbEndpointsAndOptions {
	for _, llb := range llbOpts {
		for i, ep := range llb.llbEndpoints.LbEndpoints {
			newEp, err := llb.tunnelMetadata[i].ApplyTunnel(ep, tunnelType)
			if err != nil {
				panic("not implemented yet on failing to apply tunnel")
			} else {
				llb.llbEndpoints.LbEndpoints[i] = newEp
			}
		}
	}
	return llbOpts
}

// Create the CLusterLoadAssignment. At this moment the options must have been applied to the locality lb endpoints.
func (b *EndpointBuilder) createClusterLoadAssignment(llbOpts []*LocLbEndpointsAndOptions) *endpoint.ClusterLoadAssignment {
	llbEndpoints := make([]*endpoint.LocalityLbEndpoints, 0, len(llbOpts))
	for _, l := range llbOpts {
		llbEndpoints = append(llbEndpoints, &l.llbEndpoints)
	}
	return &endpoint.ClusterLoadAssignment{
		ClusterName: b.clusterName,
		Endpoints:   llbEndpoints,
	}
}

// buildEnvoyLbEndpoint packs the endpoint based on istio info.
func buildEnvoyLbEndpoint(e *model.IstioEndpoint) *endpoint.LbEndpoint {
	addr := util.BuildAddress(e.Address, e.EndpointPort)
	healthStatus := core.HealthStatus_HEALTHY
	if e.HealthStatus == model.UnHealthy {
		healthStatus = core.HealthStatus_UNHEALTHY
	}

	ep := &endpoint.LbEndpoint{
		HealthStatus: healthStatus,
		LoadBalancingWeight: &wrappers.UInt32Value{
			Value: e.GetLoadBalancingWeight(),
		},
		HostIdentifier: &endpoint.LbEndpoint_Endpoint{
			Endpoint: &endpoint.Endpoint{
				Address: addr,
			},
		},
	}

	// Istio telemetry depends on the metadata value being set for endpoints in the mesh.
	// Istio endpoint level tls transport socket configuration depends on this logic
	// Do not remove pilot/pkg/xds/fake.go
	ep.Metadata = util.BuildLbEndpointMetadata(e.Network, e.TLSMode, e.WorkloadName, e.Namespace, e.Locality.ClusterID, e.Labels)

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

// computeForEndpoint checks destination rule, peer authentication and tls mode labels to determine if mTLS was turned off.
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
			NewPolicyApplier(c.push, ep.Namespace, labels.Collection{ep.Labels}).
			GetMutualTLSModeForPort(ep.EndpointPort) == model.MTLSDisable
		return c.peerAuthDisabledMTLS[peerAuthnKey]
	}

	//  mtls disabled by PeerAuthentication
	if mtlsDisabledByPeerAuthentication(ep) {
		c.mtlsDisabledHosts[lbEpKey(ep.EnvoyEndpoint)] = struct{}{}
	}
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
