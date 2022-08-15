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
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	networkingapi "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/loadbalancer"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/security/authn/factory"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/network"
)

var (
	Separator = []byte{'~'}
	Slash     = []byte{'/'}
)

type EndpointBuilder struct {
	// These fields define the primary key for an endpoint, and can be used as a cache key
	clusterName            string
	network                network.ID
	proxyView              model.ProxyView
	clusterID              cluster.ID
	locality               *core.Locality
	destinationRule        *model.ConsolidatedDestRule
	service                *model.Service
	clusterLocal           bool
	failoverPriorityLabels []byte

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

	var dr *model.ConsolidatedDestRule
	if svc != nil {
		dr = proxy.SidecarScope.DestinationRule(model.TrafficDirectionOutbound, proxy, svc.Hostname)
	}
	b := EndpointBuilder{
		clusterName:     clusterName,
		network:         proxy.Metadata.Network,
		proxyView:       proxy.GetView(),
		clusterID:       proxy.Metadata.ClusterID,
		locality:        proxy.Locality,
		service:         svc,
		clusterLocal:    push.IsClusterLocal(svc),
		destinationRule: dr,

		push:       push,
		proxy:      proxy,
		subsetName: subsetName,
		hostname:   hostname,
		port:       port,
	}

	b.populateFailoverPriorityLabels()

	// We need this for multi-network, or for clusters meant for use with AUTO_PASSTHROUGH.
	if features.EnableAutomTLSCheckPolicies ||
		b.push.NetworkManager().IsMultiNetworkEnabled() || model.IsDNSSrvSubsetKey(clusterName) {
		b.mtlsChecker = newMtlsChecker(push, port, dr.GetRule())
	}
	return b
}

func (b EndpointBuilder) DestinationRule() *networkingapi.DestinationRule {
	if dr := b.destinationRule.GetRule(); dr != nil {
		return dr.Spec.(*networkingapi.DestinationRule)
	}
	return nil
}

// Key provides the eds cache key and should include any information that could change the way endpoints are generated.
func (b EndpointBuilder) Key() string {
	hash := md5.New()
	hash.Write([]byte(b.clusterName))
	hash.Write(Separator)
	hash.Write([]byte(b.network))
	hash.Write(Separator)
	hash.Write([]byte(b.clusterID))
	hash.Write(Separator)
	hash.Write([]byte(strconv.FormatBool(b.clusterLocal)))
	hash.Write(Separator)
	hash.Write([]byte(util.LocalityToString(b.locality)))
	hash.Write(Separator)
	if len(b.failoverPriorityLabels) > 0 {
		hash.Write(b.failoverPriorityLabels)
		hash.Write(Separator)
	}

	if b.push != nil && b.push.AuthnPolicies != nil {
		hash.Write([]byte(b.push.AuthnPolicies.GetVersion()))
	}
	hash.Write(Separator)

	for _, dr := range b.destinationRule.GetFrom() {
		hash.Write([]byte(dr.Name))
		hash.Write(Slash)
		hash.Write([]byte(dr.Namespace))
	}
	hash.Write(Separator)

	if b.service != nil {
		hash.Write([]byte(b.service.Hostname))
		hash.Write(Slash)
		hash.Write([]byte(b.service.Attributes.Namespace))
	}
	hash.Write(Separator)

	if b.proxyView != nil {
		hash.Write([]byte(b.proxyView.String()))
	}
	hash.Write(Separator)

	sum := hash.Sum(nil)
	return hex.EncodeToString(sum)
}

func (b EndpointBuilder) Cacheable() bool {
	// If service is not defined, we cannot do any caching as we will not have a way to
	// invalidate the results.
	// Service being nil means the EDS will be empty anyways, so not much lost here.
	return b.service != nil
}

func (b EndpointBuilder) DependentConfigs() []model.ConfigHash {
	drs := b.destinationRule.GetFrom()
	configs := make([]model.ConfigHash, 0, len(drs)+1)
	if b.destinationRule != nil {
		for _, dr := range drs {
			configs = append(configs, model.ConfigKey{
				Kind: kind.DestinationRule,
				Name: dr.Name, Namespace: dr.Namespace,
			}.HashCode())
		}
	}
	if b.service != nil {
		configs = append(configs, model.ConfigKey{
			Kind: kind.ServiceEntry,
			Name: string(b.service.Hostname), Namespace: b.service.Attributes.Namespace,
		}.HashCode())
	}
	return configs
}

var edsDependentTypes = []kind.Kind{kind.PeerAuthentication}

func (b EndpointBuilder) DependentTypes() []kind.Kind {
	return edsDependentTypes
}

type LocalityEndpoints struct {
	istioEndpoints []*model.IstioEndpoint
	// The protobuf message which contains LbEndpoint slice.
	llbEndpoints endpoint.LocalityLbEndpoints
}

func (e *LocalityEndpoints) append(ep *model.IstioEndpoint, le *endpoint.LbEndpoint) {
	e.istioEndpoints = append(e.istioEndpoints, ep)
	e.llbEndpoints.LbEndpoints = append(e.llbEndpoints.LbEndpoints, le)
}

func (e *LocalityEndpoints) refreshWeight() {
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

func (e *LocalityEndpoints) AssertInvarianceInTest() {
	if len(e.llbEndpoints.LbEndpoints) != len(e.istioEndpoints) {
		panic(" len(e.llbEndpoints.LbEndpoints) != len(e.tunnelMetadata)")
	}
}

func (b *EndpointBuilder) populateFailoverPriorityLabels() {
	enableFailover, lb := getOutlierDetectionAndLoadBalancerSettings(b.DestinationRule(), b.port, b.subsetName)
	if enableFailover {
		lbSetting := loadbalancer.GetLocalityLbSetting(b.push.Mesh.GetLocalityLbSetting(), lb.GetLocalityLbSetting())
		if lbSetting != nil && lbSetting.Distribute == nil &&
			len(lbSetting.FailoverPriority) > 0 && (lbSetting.Enabled == nil || lbSetting.Enabled.Value) {
			b.failoverPriorityLabels = util.GetFailoverPriorityLabels(b.proxy.Metadata.Labels, lbSetting.FailoverPriority)
		}
	}
}

// build LocalityLbEndpoints for a cluster from existing EndpointShards.
func (b *EndpointBuilder) buildLocalityLbEndpointsFromShards(
	shards *model.EndpointShards,
	svcPort *model.Port,
) []*LocalityEndpoints {
	localityEpMap := make(map[string]*LocalityEndpoints)
	// get the subset labels
	epLabels := getSubSetLabels(b.DestinationRule(), b.subsetName)

	// Determine whether or not the target service is considered local to the cluster
	// and should, therefore, not be accessed from outside the cluster.
	isClusterLocal := b.clusterLocal

	shards.Lock()
	// Extract shard keys so we can iterate in order. This ensures a stable EDS output.
	keys := shards.Keys()
	// The shards are updated independently, now need to filter and merge for this cluster
	for _, shardKey := range keys {
		endpoints := shards.Shards[shardKey]
		// If the downstream service is configured as cluster-local, only include endpoints that
		// reside in the same cluster.
		if isClusterLocal && (shardKey.Cluster != b.clusterID) {
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
			if !epLabels.SubsetOf(ep.Labels) {
				continue
			}

			locLbEps, found := localityEpMap[ep.Locality.Label]
			if !found {
				locLbEps = &LocalityEndpoints{
					llbEndpoints: endpoint.LocalityLbEndpoints{
						Locality:    util.ConvertLocality(ep.Locality.Label),
						LbEndpoints: make([]*endpoint.LbEndpoint, 0, len(endpoints)),
					},
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
			locLbEps.append(ep, ep.EnvoyEndpoint)
		}
	}
	shards.Unlock()

	locEps := make([]*LocalityEndpoints, 0, len(localityEpMap))
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

// Create the CLusterLoadAssignment. At this moment the options must have been applied to the locality lb endpoints.
func (b *EndpointBuilder) createClusterLoadAssignment(llbOpts []*LocalityEndpoints) *endpoint.ClusterLoadAssignment {
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
			NewPolicyApplier(c.push, ep.Namespace, ep.Labels).
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
