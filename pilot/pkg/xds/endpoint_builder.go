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
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	"google.golang.org/protobuf/types/known/structpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	networkingapi "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	networking "istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/loadbalancer"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/util/hash"
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
	nodeType               model.NodeType
	failoverPriorityLabels []byte

	// These fields are provided for convenience only
	subsetName string
	hostname   host.Name
	port       int
	push       *model.PushContext
	proxy      *model.Proxy
	dir        model.TrafficDirection

	mtlsChecker *mtlsChecker
}

func NewEndpointBuilder(clusterName string, proxy *model.Proxy, push *model.PushContext) EndpointBuilder {
	dir, subsetName, hostname, port := model.ParseSubsetKey(clusterName)
	if dir == model.TrafficDirectionInboundVIP {
		subsetName = strings.TrimPrefix(subsetName, "http/")
		subsetName = strings.TrimPrefix(subsetName, "tcp/")
	}
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
		nodeType:        proxy.Type,

		mtlsChecker: newMtlsChecker(push, port, dr.GetRule(), subsetName),
		push:        push,
		proxy:       proxy,
		subsetName:  subsetName,
		hostname:    hostname,
		port:        port,
		dir:         dir,
	}

	b.populateFailoverPriorityLabels()

	return b
}

func (b *EndpointBuilder) DestinationRule() *networkingapi.DestinationRule {
	if dr := b.destinationRule.GetRule(); dr != nil {
		return dr.Spec.(*networkingapi.DestinationRule)
	}
	return nil
}

func (b *EndpointBuilder) Type() string {
	return model.EDSType
}

// Key provides the eds cache key and should include any information that could change the way endpoints are generated.
func (b *EndpointBuilder) Key() any {
	// nolint: gosec
	// Not security sensitive code
	h := hash.New()
	h.Write([]byte(b.clusterName))
	h.Write(Separator)
	h.Write([]byte(b.network))
	h.Write(Separator)
	h.Write([]byte(b.clusterID))
	h.Write(Separator)
	h.Write([]byte(b.nodeType))
	h.Write(Separator)
	h.Write([]byte(strconv.FormatBool(b.clusterLocal)))
	h.Write(Separator)
	if features.EnableHBONE && b.proxy != nil {
		h.Write([]byte(strconv.FormatBool(b.proxy.IsProxylessGrpc())))
		h.Write(Separator)
	}
	h.Write([]byte(util.LocalityToString(b.locality)))
	h.Write(Separator)
	if len(b.failoverPriorityLabels) > 0 {
		h.Write(b.failoverPriorityLabels)
		h.Write(Separator)
	}
	if b.service.Attributes.NodeLocal {
		h.Write([]byte(b.proxy.GetNodeName()))
		h.Write(Separator)
	}

	if b.push != nil && b.push.AuthnPolicies != nil {
		h.Write([]byte(b.push.AuthnPolicies.GetVersion()))
	}
	h.Write(Separator)

	for _, dr := range b.destinationRule.GetFrom() {
		h.Write([]byte(dr.Name))
		h.Write(Slash)
		h.Write([]byte(dr.Namespace))
	}
	h.Write(Separator)

	if b.service != nil {
		h.Write([]byte(b.service.Hostname))
		h.Write(Slash)
		h.Write([]byte(b.service.Attributes.Namespace))
	}
	h.Write(Separator)

	if b.proxyView != nil {
		h.Write([]byte(b.proxyView.String()))
	}
	h.Write(Separator)

	return h.Sum64()
}

func (b *EndpointBuilder) Cacheable() bool {
	// If service is not defined, we cannot do any caching as we will not have a way to
	// invalidate the results.
	// Service being nil means the EDS will be empty anyways, so not much lost here.
	return b.service != nil
}

func (b *EndpointBuilder) DependentConfigs() []model.ConfigHash {
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
			b.failoverPriorityLabels = util.GetFailoverPriorityLabels(b.proxy.Labels, lbSetting.FailoverPriority)
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
	subsetLabels := getSubSetLabels(b.DestinationRule(), b.subsetName)

	// Determine whether or not the target service is considered local to the cluster
	// and should, therefore, not be accessed from outside the cluster.
	isClusterLocal := b.clusterLocal

	shards.Lock()
	// Extract shard keys so we can iterate in order. This ensures a stable EDS output.
	keys := shards.Keys()
	// The shards are updated independently, now need to filter and merge for this cluster
	for _, shardKey := range keys {
		if shardKey.Cluster != b.clusterID {
			// If the downstream service is configured as cluster-local, only include endpoints that
			// reside in the same cluster.
			if isClusterLocal || b.service.Attributes.NodeLocal {
				continue
			}
		}
		endpoints := shards.Shards[shardKey]
		for _, ep := range endpoints {
			// for ServiceInternalTrafficPolicy
			if b.service.Attributes.NodeLocal && ep.NodeName != b.proxy.GetNodeName() {
				continue
			}
			// TODO(nmittler): Consider merging discoverability policy with cluster-local
			if !ep.IsDiscoverableFromProxy(b.proxy) {
				continue
			}
			if svcPort.Name != ep.ServicePortName {
				continue
			}
			// Port labels
			if !subsetLabels.SubsetOf(ep.Labels) {
				continue
			}
			// If we don't know the address we must eventually use a gateway address
			if ep.Address == "" && ep.Network == b.network {
				continue
			}
			// Draining endpoints are only sent to 'persistent session' clusters.
			draining := ep.HealthStatus == model.Draining ||
				features.DrainingLabel != "" && ep.Labels[features.DrainingLabel] != ""
			if draining {
				persistentSession := b.service.Attributes.Labels[features.PersistentSessionLabel] != ""
				if !persistentSession {
					continue
				}
			}

			mtlsEnabled := b.mtlsChecker.checkMtlsEnabled(ep)
			// Determine if we need to build the endpoint. We try to cache it for performance reasons
			needToCompute := ep.EnvoyEndpoint == nil
			if features.EnableHBONE {
				// Currently the HBONE implementation leads to different endpoint generation depending on if the
				// client proxy supports HBONE or not. This breaks the cache.
				// For now, just disable caching if the global HBONE flag is enabled.
				needToCompute = true
			}
			if ep.EnvoyEndpoint != nil && mtlsEnabled != isMtlsEnabled(ep.EnvoyEndpoint) {
				// The mTLS settings may have changed, invalidating the cache endpoint. Rebuild it
				needToCompute = true
			}
			if needToCompute {
				eep := buildEnvoyLbEndpoint(b, ep, mtlsEnabled)
				if eep == nil {
					continue
				}
				ep.EnvoyEndpoint = eep
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
func buildEnvoyLbEndpoint(b *EndpointBuilder, e *model.IstioEndpoint, mtlsEnabled bool) *endpoint.LbEndpoint {
	addr := util.BuildAddress(e.Address, e.EndpointPort)
	healthStatus := e.HealthStatus
	if features.DrainingLabel != "" && e.Labels[features.DrainingLabel] != "" {
		healthStatus = model.Draining
	}

	ep := &endpoint.LbEndpoint{
		HealthStatus: core.HealthStatus(healthStatus),
		LoadBalancingWeight: &wrappers.UInt32Value{
			Value: e.GetLoadBalancingWeight(),
		},
		HostIdentifier: &endpoint.LbEndpoint_Endpoint{
			Endpoint: &endpoint.Endpoint{
				Address: addr,
			},
		},
		Metadata: &core.Metadata{},
	}

	// Istio telemetry depends on the metadata value being set for endpoints in the mesh.
	// Istio endpoint level tls transport socket configuration depends on this logic
	// Do not remove
	meta := e.Metadata()

	// detect if mTLS is possible for this endpoint, used later during ep filtering
	// this must be done while converting IstioEndpoints because we still have workload labels
	if !mtlsEnabled {
		meta.TLSMode = ""
	}
	util.AppendLbEndpointMetadata(meta, ep.Metadata)

	address, port := e.Address, e.EndpointPort
	tunnelAddress, tunnelPort := address, model.HBoneInboundListenPort

	supportsTunnel := false
	// Other side is a waypoint proxy.
	if al := e.Labels[constants.ManagedGatewayLabel]; al == constants.ManagedGatewayMeshControllerLabel {
		supportsTunnel = true
	}

	// Otherwise has ambient enabled. Note: this is a synthetic label, not existing in the real Pod.
	if b.push.SupportsTunnel(e.Network, e.Address) {
		supportsTunnel = true
	}
	// Otherwise supports tunnel
	// Currently we only support HTTP tunnel, so just check for that. If we support more, we will
	// need to pick the right one based on our support overlap.
	if e.SupportsTunnel(model.TunnelHTTP) {
		supportsTunnel = true
	}
	if b.proxy.IsProxylessGrpc() {
		// Proxyless client cannot handle tunneling, even if the server can
		supportsTunnel = false
	}

	if !b.proxy.EnableHBONE() {
		supportsTunnel = false
	}

	// Setup tunnel information, if needed
	if b.dir == model.TrafficDirectionInboundVIP {
		// This is only used in waypoint proxy
		inScope := waypointInScope(b.proxy, e)
		if !inScope {
			// A waypoint can *partially* select a Service in edge cases. In this case, some % of requests will
			// go through the waypoint, and the rest direct. Since these have already been load balanced across,
			// we want to make sure we only send to workloads behind our waypoint
			return nil
		}
		// For inbound, we only use EDS for the VIP cases. The VIP cluster will point to encap listener.
		if supportsTunnel {
			address := e.Address
			tunnelPort := 15008
			// We will connect to CONNECT origination internal listener, telling it to tunnel to ip:15008,
			// and add some detunnel metadata that had the original port.
			ep.Metadata.FilterMetadata[model.TunnelLabelShortName] = util.BuildTunnelMetadataStruct(address, address, int(e.EndpointPort), tunnelPort)
			ep = util.BuildInternalLbEndpoint(networking.ConnectOriginate, ep.Metadata)
			ep.LoadBalancingWeight = &wrappers.UInt32Value{
				Value: e.GetLoadBalancingWeight(),
			}
		}
	} else if supportsTunnel {
		// Support connecting to server side waypoint proxy, if the destination has one. This is for sidecars and ingress.
		if b.dir == model.TrafficDirectionOutbound && !b.proxy.IsWaypointProxy() && !b.proxy.IsAmbient() {
			workloads := findWaypoints(b.push, e)
			if len(workloads) > 0 {
				// TODO: load balance
				tunnelAddress = workloads[0].String()
			}
		}
		// Setup tunnel metadata so requests will go through the tunnel
		ep.HostIdentifier = &endpoint.LbEndpoint_Endpoint{Endpoint: &endpoint.Endpoint{
			Address: util.BuildInternalAddressWithIdentifier(networking.ConnectOriginate, net.JoinHostPort(address, strconv.Itoa(int(port)))),
		}}
		ep.Metadata.FilterMetadata[model.TunnelLabelShortName] = util.BuildTunnelMetadataStruct(tunnelAddress, address, int(port), tunnelPort)
		ep.Metadata.FilterMetadata[util.EnvoyTransportSocketMetadataKey] = &structpb.Struct{
			Fields: map[string]*structpb.Value{
				model.TunnelLabelShortName: {Kind: &structpb.Value_StringValue{StringValue: model.TunnelHTTP}},
			},
		}
	}

	return ep
}

// waypointInScope computes whether the endpoint is owned by the waypoint
func waypointInScope(waypoint *model.Proxy, e *model.IstioEndpoint) bool {
	scope := waypoint.WaypointScope()
	if scope.Namespace != e.Namespace {
		return false
	}
	ident, _ := spiffe.ParseIdentity(e.ServiceAccount)
	if scope.ServiceAccount != "" && (scope.ServiceAccount != ident.ServiceAccount) {
		return false
	}
	return true
}

func findWaypoints(push *model.PushContext, e *model.IstioEndpoint) []netip.Addr {
	ident, _ := spiffe.ParseIdentity(e.ServiceAccount)
	ips := push.WaypointsFor(model.WaypointScope{
		Namespace:      e.Namespace,
		ServiceAccount: ident.ServiceAccount,
	})
	return ips
}
