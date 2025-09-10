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
	"math"
	"net"
	"sort"
	"strconv"
	"strings"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"istio.io/api/label"
	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/loadbalancer"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/kind"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/hash"
	netutil "istio.io/istio/pkg/util/net"
)

var (
	Separator = []byte{'~'}
	Slash     = []byte{'/'}

	// same as the above "xds" package
	log = istiolog.RegisterScope("ads", "ads debugging")
)

// ConnectOriginate is the name for the resources associated with the origination of HTTP CONNECT.
// Duplicated from networking/core/waypoint.go to avoid import cycle
const connectOriginate = "connect_originate"

// ForwardInnerConnect is the name for resources associated with the forwarding of an inner CONNECT tunnel.
// Duplicated from networking/core/waypoint.go to avoid import cycle
const forwardInnerConnect = "forward_inner_connect"

type EndpointBuilder struct {
	// These fields define the primary key for an endpoint, and can be used as a cache key
	clusterName            string
	network                network.ID
	proxyView              model.ProxyView
	clusterID              cluster.ID
	locality               *corev3.Locality
	destinationRule        *model.ConsolidatedDestRule
	service                *model.Service
	clusterLocal           bool
	nodeType               model.NodeType
	failoverPriorityLabels []byte

	// These fields are provided for convenience only
	subsetName   string
	subsetLabels labels.Instance
	hostname     host.Name
	port         int
	push         *model.PushContext
	proxy        *model.Proxy
	dir          model.TrafficDirection

	mtlsChecker *mtlsChecker
}

func NewEndpointBuilder(clusterName string, proxy *model.Proxy, push *model.PushContext) EndpointBuilder {
	dir, subsetName, hostname, port := model.ParseSubsetKey(clusterName)

	svc := push.ServiceForHostname(proxy, hostname)
	var dr *model.ConsolidatedDestRule
	if svc != nil {
		dr = proxy.SidecarScope.DestinationRule(model.TrafficDirectionOutbound, proxy, svc.Hostname)
	}

	return *NewCDSEndpointBuilder(
		proxy, push, clusterName,
		dir, subsetName, hostname, port,
		svc, dr,
	)
}

// NewCDSEndpointBuilder allows setting some fields directly when we already
// have the Service and DestinationRule.
func NewCDSEndpointBuilder(
	proxy *model.Proxy, push *model.PushContext, clusterName string,
	dir model.TrafficDirection, subsetName string, hostname host.Name, port int,
	service *model.Service, dr *model.ConsolidatedDestRule,
) *EndpointBuilder {
	b := EndpointBuilder{
		clusterName:     clusterName,
		network:         proxy.Metadata.Network,
		proxyView:       proxy.GetView(),
		clusterID:       proxy.Metadata.ClusterID,
		locality:        proxy.Locality,
		destinationRule: dr,
		service:         service,
		clusterLocal:    push.IsClusterLocal(service),
		nodeType:        proxy.Type,

		subsetName: subsetName,
		hostname:   hostname,
		port:       port,
		push:       push,
		proxy:      proxy,
		dir:        dir,
	}
	b.populateSubsetInfo()
	b.populateFailoverPriorityLabels()
	return &b
}

func (b *EndpointBuilder) servicePort(port int) *model.Port {
	if !b.ServiceFound() {
		log.Debugf("can not find the service %s for cluster %s", b.hostname, b.clusterName)
		return nil
	}
	svcPort, f := b.service.Ports.GetByPort(port)
	if !f {
		log.Debugf("can not find the service port %d for cluster %s", b.port, b.clusterName)
		return nil
	}
	return svcPort
}

func (b *EndpointBuilder) WithSubset(subset string) *EndpointBuilder {
	if b == nil {
		return nil
	}
	subsetBuilder := *b
	subsetBuilder.subsetName = subset
	subsetBuilder.populateSubsetInfo()
	return &subsetBuilder
}

func (b *EndpointBuilder) populateSubsetInfo() {
	if b.dir == model.TrafficDirectionInboundVIP {
		b.subsetName = strings.TrimPrefix(b.subsetName, "http/")
		b.subsetName = strings.TrimPrefix(b.subsetName, "tcp/")
	}
	b.mtlsChecker = newMtlsChecker(b.push, b.port, b.destinationRule.GetRule(), b.subsetName)
	b.subsetLabels = getSubSetLabels(b.DestinationRule(), b.subsetName)
}

func (b *EndpointBuilder) populateFailoverPriorityLabels() {
	enableFailover, lb := getOutlierDetectionAndLoadBalancerSettings(b.DestinationRule(), b.port, b.subsetName)
	if enableFailover {
		lbSetting, _ := loadbalancer.GetLocalityLbSetting(b.push.Mesh.GetLocalityLbSetting(), lb.GetLocalityLbSetting(), b.service)
		if lbSetting != nil && lbSetting.Distribute == nil &&
			len(lbSetting.FailoverPriority) > 0 && (lbSetting.Enabled == nil || lbSetting.Enabled.Value) {
			b.failoverPriorityLabels = util.GetFailoverPriorityLabels(b.proxy.Labels, lbSetting.FailoverPriority)
		}
	}
}

func (b *EndpointBuilder) DestinationRule() *v1alpha3.DestinationRule {
	if dr := b.destinationRule.GetRule(); dr != nil {
		dr, _ := dr.Spec.(*v1alpha3.DestinationRule)
		return dr
	}
	return nil
}

func (b *EndpointBuilder) Type() string {
	return model.EDSType
}

func (b *EndpointBuilder) ServiceFound() bool {
	return b.service != nil
}

func (b *EndpointBuilder) IsDNSCluster() bool {
	return b.service != nil && (b.service.Resolution == model.DNSLB || b.service.Resolution == model.DNSRoundRobinLB)
}

// Key provides the eds cache key and should include any information that could change the way endpoints are generated.
func (b *EndpointBuilder) Key() any {
	// nolint: gosec
	// Not security sensitive code
	h := hash.New()
	b.WriteHash(h)
	return h.Sum64()
}

func (b *EndpointBuilder) WriteHash(h hash.Hash) {
	if b == nil {
		return
	}
	h.WriteString(b.clusterName)
	h.Write(Separator)
	h.WriteString(string(b.network))
	h.Write(Separator)
	h.WriteString(string(b.clusterID))
	h.Write(Separator)
	h.WriteString(string(b.nodeType))
	h.Write(Separator)
	h.WriteString(strconv.FormatBool(b.clusterLocal))
	h.Write(Separator)
	if b.proxy != nil {
		h.WriteString(strconv.FormatBool(b.proxy.IsProxylessGrpc()))
		h.Write(Separator)
		h.WriteString(strconv.FormatBool(bool(b.proxy.Metadata.DisableHBONESend)))
		h.Write(Separator)
	}
	h.WriteString(util.LocalityToString(b.locality))
	h.Write(Separator)
	if len(b.failoverPriorityLabels) > 0 {
		h.Write(b.failoverPriorityLabels)
		h.Write(Separator)
	}
	if b.service.Attributes.NodeLocal {
		h.WriteString(b.proxy.GetNodeName())
		h.Write(Separator)
	}

	if b.push != nil && b.push.AuthnPolicies != nil {
		h.WriteString(b.push.AuthnPolicies.GetVersion())
	}
	h.Write(Separator)

	for _, dr := range b.destinationRule.GetFrom() {
		h.WriteString(dr.Name)
		h.Write(Slash)
		h.WriteString(dr.Namespace)
	}
	h.Write(Separator)

	if b.service != nil {
		h.WriteString(string(b.service.Hostname))
		h.Write(Slash)
		h.WriteString(b.service.Attributes.Namespace)
	}
	h.Write(Separator)

	if b.proxyView != nil {
		h.WriteString(b.proxyView.String())
	}
	h.Write(Separator)
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

	// For now, this matches clusterCache's DependentConfigs. If adding anything here, we may need to add them there.

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
	var weight *wrapperspb.UInt32Value
	if len(e.llbEndpoints.LbEndpoints) == 0 {
		weight = nil
	} else {
		weight = &wrapperspb.UInt32Value{}
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

// FromServiceEndpoints builds LocalityLbEndpoints from the PushContext's snapshotted ServiceIndex.
// Used for CDS (ClusterLoadAssignment constructed elsewhere).
func (b *EndpointBuilder) FromServiceEndpoints() []*endpoint.LocalityLbEndpoints {
	if b == nil {
		return nil
	}
	svcEps := b.push.ServiceEndpointsByPort(b.service, b.port, b.subsetLabels)
	// don't use the pre-computed endpoints for CDS to preserve previous behavior
	// CDS is always toServiceWaypoint=false. We do not yet support calling waypoints for CDS (DNS type)
	return ExtractEnvoyEndpoints(b.generate(svcEps, false))
}

// BuildClusterLoadAssignment converts the shards for this EndpointBuilder's Service
// into a ClusterLoadAssignment. Used for EDS.
func (b *EndpointBuilder) BuildClusterLoadAssignment(endpointIndex *model.EndpointIndex) *endpoint.ClusterLoadAssignment {
	svcPort := b.servicePort(b.port)
	if svcPort == nil {
		return buildEmptyClusterLoadAssignment(b.clusterName)
	}

	if features.EnableIngressWaypointRouting {
		if waypointEps, f := b.findServiceWaypoint(endpointIndex); f {
			// endpoints are from waypoint service but the envoy endpoint is different envoy cluster
			locLbEps := b.generate(waypointEps, true)
			return b.createClusterLoadAssignment(locLbEps)
		}
	}

	// If we're an east west gateway, then we also want to send to waypoints
	// TODO: depending on the final design, there will be times we DON'T want
	// the e/w gateway to send to waypoints. That conditional logic will either live
	// here or in the listener logic.
	if features.EnableAmbientMultiNetwork && isEastWestGateway(b.proxy) {
		if waypointEps, f := b.findServiceWaypoint(endpointIndex); f {
			// endpoints are from waypoint service but the envoy endpoint is different envoy cluster
			locLbEps := b.generate(waypointEps, true)
			return b.createClusterLoadAssignment(locLbEps)
		}
	}

	svcEps := b.snapshotShards(endpointIndex)
	svcEps = slices.FilterInPlace(svcEps, func(ep *model.IstioEndpoint) bool {
		// filter out endpoints that don't match the service port
		if svcPort.Name != ep.ServicePortName {
			return false
		}
		// filter out endpoint that has invalid ip address, mostly domain name. Because this is generated from ServiceEntry.
		// There are other two cases that should not be filtered out:
		// 1. ep.Addresses[0] can be empty since https://github.com/istio/istio/pull/45150, in this case we will replace it with gateway ip.
		// 2. ep.Addresses[0] can be uds when EndpointPort = 0
		if ep.Addresses[0] != "" && ep.EndpointPort != 0 && !netutil.IsValidIPAddress(ep.Addresses[0]) {
			return false
		}
		// filter out endpoints that don't match the subset
		if !b.subsetLabels.SubsetOf(ep.Labels) {
			return false
		}
		return true
	})

	localityLbEndpoints := b.generate(svcEps, false)
	if len(localityLbEndpoints) == 0 {
		return buildEmptyClusterLoadAssignment(b.clusterName)
	}

	l := b.createClusterLoadAssignment(localityLbEndpoints)

	// If locality aware routing is enabled, prioritize endpoints or set their lb weight.
	// Failover should only be enabled when there is an outlier detection, otherwise Envoy
	// will never detect the hosts are unhealthy and redirect traffic.
	enableFailover, lb := getOutlierDetectionAndLoadBalancerSettings(b.DestinationRule(), b.port, b.subsetName)
	lbSetting, forceFailover := loadbalancer.GetLocalityLbSetting(b.push.Mesh.GetLocalityLbSetting(), lb.GetLocalityLbSetting(), b.service)
	enableFailover = enableFailover || forceFailover
	if lbSetting != nil {
		// Make a shallow copy of the cla as we are mutating the endpoints with priorities/weights relative to the calling proxy
		l = util.CloneClusterLoadAssignment(l)
		wrappedLocalityLbEndpoints := make([]*loadbalancer.WrappedLocalityLbEndpoints, len(localityLbEndpoints))
		for i := range localityLbEndpoints {
			wrappedLocalityLbEndpoints[i] = &loadbalancer.WrappedLocalityLbEndpoints{
				IstioEndpoints:      localityLbEndpoints[i].istioEndpoints,
				LocalityLbEndpoints: l.Endpoints[i],
			}
		}
		loadbalancer.ApplyLocalityLoadBalancer(l, wrappedLocalityLbEndpoints, b.locality, b.proxy.Labels, lbSetting, enableFailover)
	}
	return l
}

// generate endpoints with applies weights, multi-network mapping and other filtering
func (b *EndpointBuilder) generate(eps []*model.IstioEndpoint, toServiceWaypoint bool) []*LocalityEndpoints {
	// shouldn't happen here
	if !b.ServiceFound() {
		return nil
	}

	eps = slices.Filter(eps, func(ep *model.IstioEndpoint) bool {
		return b.filterIstioEndpoint(ep)
	})

	localityEpMap := make(map[string]*LocalityEndpoints)
	for _, ep := range eps {
		mtlsEnabled := b.mtlsChecker.checkMtlsEnabled(ep, b.proxy.IsWaypointProxy())
		eep := buildEnvoyLbEndpoint(b, ep, mtlsEnabled, toServiceWaypoint)
		if eep == nil {
			continue
		}
		locLbEps, found := localityEpMap[ep.Locality.Label]
		if !found {
			locLbEps = &LocalityEndpoints{
				llbEndpoints: endpoint.LocalityLbEndpoints{
					Locality:    util.ConvertLocality(ep.Locality.Label),
					LbEndpoints: make([]*endpoint.LbEndpoint, 0, len(eps)),
				},
			}
			localityEpMap[ep.Locality.Label] = locLbEps
		}
		locLbEps.append(ep, eep)
	}

	locEps := make([]*LocalityEndpoints, 0, len(localityEpMap))
	locs := make([]string, 0, len(localityEpMap))
	for k := range localityEpMap {
		locs = append(locs, k)
	}
	if len(locs) >= 2 {
		sort.Strings(locs)
	}
	for _, locality := range locs {
		locLbEps := localityEpMap[locality]
		var weight uint32
		var overflowStatus bool
		for _, ep := range locLbEps.llbEndpoints.LbEndpoints {
			weight, overflowStatus = addUint32(weight, ep.LoadBalancingWeight.GetValue())
		}
		locLbEps.llbEndpoints.LoadBalancingWeight = &wrapperspb.UInt32Value{
			Value: weight,
		}
		if overflowStatus {
			log.Warnf("Sum of localityLbEndpoints weight is overflow: service:%s, port: %d, locality:%s",
				b.service.Hostname, b.port, locality)
		}
		locEps = append(locEps, locLbEps)
	}

	if len(locEps) == 0 {
		b.push.AddMetric(model.ProxyStatusClusterNoInstances, b.clusterName, "", "")
	}

	// Apply the Split Horizon EDS filter, if applicable.
	locEps = b.EndpointsByNetworkFilter(locEps)

	if model.IsDNSSrvSubsetKey(b.clusterName) {
		// For the SNI-DNAT clusters, we are using AUTO_PASSTHROUGH gateway. AUTO_PASSTHROUGH is intended
		// to passthrough mTLS requests. However, at the gateway we do not actually have any way to tell if the
		// request is a valid mTLS request or not, since its passthrough TLS.
		// To ensure we allow traffic only to mTLS endpoints, we filter out non-mTLS endpoints for these cluster types.
		locEps = b.EndpointsWithMTLSFilter(locEps)
	}

	return locEps
}

// addUint32AvoidOverflow returns sum of two uint32 and status. If sum overflows,
// and returns MaxUint32 and status.
func addUint32(left, right uint32) (uint32, bool) {
	if math.MaxUint32-right < left {
		return math.MaxUint32, true
	}
	return left + right, false
}

func (b *EndpointBuilder) filterIstioEndpoint(ep *model.IstioEndpoint) bool {
	// for ServiceInternalTrafficPolicy
	if b.service.Attributes.NodeLocal && ep.NodeName != b.proxy.GetNodeName() {
		return false
	}
	// Only send endpoints from the networks in the network view requested by the proxy.
	// The default network view assigned to the Proxy is nil, in that case match any network.
	if !b.proxyView.IsVisible(ep) {
		// Endpoint's network doesn't match the set of networks that the proxy wants to see.
		return false
	}
	// If the downstream service is configured as cluster-local, only include endpoints that
	// reside in the same cluster.
	if b.clusterLocal && (b.clusterID != ep.Locality.ClusterID) {
		return false
	}
	// TODO(nmittler): Consider merging discoverability policy with cluster-local
	if !ep.IsDiscoverableFromProxy(b.proxy) {
		return false
	}
	// If we don't know the address we must eventually use a gateway address
	if len(ep.Addresses) == 0 && (!b.gateways().IsMultiNetworkEnabled() || b.proxy.InNetwork(ep.Network)) {
		return false
	}
	// Filter out unhealthy endpoints, unless the service needs them.
	// This is used to let envoy know about the amount of health endpoints in a cluster.
	// This is used to let envoy know about the amount of health endpoints in a cluster.
	if !b.service.SupportsUnhealthyEndpoints() && ep.HealthStatus == model.UnHealthy {
		return false
	}
	// Filter out terminating endpoints -- we never need these. Even in "send unhealthy mode", there is no need
	// to consider terminating endpoints in the calculation.
	// For example, if I change a service with 1 pod, I will temporarily have 1 new pod and 1 terminating pod.
	// We want this to be 100% healthy, not 50% healthy.
	if ep.HealthStatus == model.Terminating {
		return false
	}
	// Draining endpoints are only sent to 'persistent session' clusters.
	draining := ep.HealthStatus == model.Draining ||
		features.DrainingLabel != "" && ep.Labels[features.DrainingLabel] != ""
	if draining {
		persistentSession := b.service.Attributes.Labels[features.PersistentSessionLabel] != ""
		if !persistentSession {
			return false
		}
	}
	return true
}

// snapshotShards into a local slice to avoid lock contention
func (b *EndpointBuilder) snapshotShards(endpointIndex *model.EndpointIndex) []*model.IstioEndpoint {
	shards := b.findShards(endpointIndex)
	if shards == nil {
		return nil
	}

	// Determine whether or not the target service is considered local to the cluster
	// and should, therefore, not be accessed from outside the cluster.
	isClusterLocal := b.clusterLocal
	var eps []*model.IstioEndpoint
	shards.RLock()
	defer shards.RUnlock()
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
		eps = append(eps, shards.Shards[shardKey]...)
	}
	return eps
}

// findShards returns the endpoints for a cluster
func (b *EndpointBuilder) findShards(endpointIndex *model.EndpointIndex) *model.EndpointShards {
	if b.service == nil {
		log.Debugf("can not find the service for cluster %s", b.clusterName)
		return nil
	}

	// Service resolution type might have changed and Cluster may be still in the EDS cluster list of "Connection.Clusters".
	// This can happen if a ServiceEntry's resolution is changed from STATIC to DNS which changes the Envoy cluster type from
	// EDS to STRICT_DNS or LOGICAL_DNS. When pushEds is called before Envoy sends the updated cluster list via Endpoint request which in turn
	// will update "Connection.Clusters", we might accidentally send EDS updates for STRICT_DNS cluster. This check guards
	// against such behavior and returns nil. When the updated cluster warms up in Envoy, it would update with new endpoints
	// automatically.
	// Gateways use EDS for Passthrough cluster. So we should allow Passthrough here.
	if b.IsDNSCluster() {
		log.Infof("cluster %s in eds cluster, but its resolution now is updated to %v, skipping it.", b.clusterName, b.service.Resolution)
		return nil
	}

	epShards, f := endpointIndex.ShardsForService(string(b.hostname), b.service.Attributes.Namespace)
	if !f {
		// Shouldn't happen here
		log.Debugf("can not find the endpointShards for cluster %s", b.clusterName)
		return nil
	}
	return epShards
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

// cluster with no endpoints
func buildEmptyClusterLoadAssignment(clusterName string) *endpoint.ClusterLoadAssignment {
	return &endpoint.ClusterLoadAssignment{
		ClusterName: clusterName,
	}
}

func (b *EndpointBuilder) gateways() *model.NetworkGateways {
	if b.IsDNSCluster() {
		return b.push.NetworkManager().Unresolved
	}
	return b.push.NetworkManager().NetworkGateways
}

func ExtractEnvoyEndpoints(locEps []*LocalityEndpoints) []*endpoint.LocalityLbEndpoints {
	var locLbEps []*endpoint.LocalityLbEndpoints
	for _, eps := range locEps {
		locLbEps = append(locLbEps, &eps.llbEndpoints)
	}
	return locLbEps
}

// buildEnvoyLbEndpoint packs the endpoint based on istio info.
func buildEnvoyLbEndpoint(b *EndpointBuilder, e *model.IstioEndpoint, mtlsEnabled bool, toServiceWaypoint bool) *endpoint.LbEndpoint {
	healthStatus := e.HealthStatus
	if features.DrainingLabel != "" && e.Labels[features.DrainingLabel] != "" {
		healthStatus = model.Draining
	}

	ep := &endpoint.LbEndpoint{
		HealthStatus: corev3.HealthStatus(healthStatus),
		LoadBalancingWeight: &wrapperspb.UInt32Value{
			Value: e.GetLoadBalancingWeight(),
		},
		Metadata: &corev3.Metadata{},
	}

	// Istio telemetry depends on the metadata value being set for endpoints in the mesh.
	// Istio endpoint level tls transport socket configuration depends on this logic
	// Do not remove
	var meta *model.EndpointMetadata
	if features.CanonicalServiceForMeshExternalServiceEntry && b.service.MeshExternal {
		svcLabels := b.service.Attributes.Labels
		if _, ok := svcLabels[model.IstioCanonicalServiceLabelName]; ok {
			meta = e.MetadataClone()
			if meta.Labels == nil {
				meta.Labels = make(map[string]string)
			}
			meta.Labels[model.IstioCanonicalServiceLabelName] = svcLabels[model.IstioCanonicalServiceLabelName]
			meta.Labels[model.IstioCanonicalServiceRevisionLabelName] = svcLabels[model.IstioCanonicalServiceRevisionLabelName]
		} else {
			meta = e.Metadata()
		}
		meta.Namespace = b.service.Attributes.Namespace
	} else {
		meta = e.Metadata()
	}

	// detect if mTLS is possible for this endpoint, used later during ep filtering
	// this must be done while converting IstioEndpoints because we still have workload labels
	if !mtlsEnabled {
		meta.TLSMode = ""
	}
	util.AppendLbEndpointMetadata(meta, ep.Metadata)

	tunnel := supportTunnel(b, e)
	// Only send HBONE if its necessary. If they support legacy mTLS and do not explicitly PreferHBONE, we will use legacy mTLS.
	// However, waypoints (TrafficDirectionInboundVIP) do not support legacy mTLS, so do not allow opting out of that case.
	supportsMtls := e.TLSMode == model.IstioMutualTLSModeLabel
	if supportsMtls && !features.PreferHBONESend && b.dir != model.TrafficDirectionInboundVIP {
		tunnel = false
	}
	if b.proxy.Metadata.DisableHBONESend {
		tunnel = false
	}
	// Waypoints always use HBONE
	if toServiceWaypoint {
		tunnel = true
	}

	if tunnel {
		// Currently, Envoy cannot support tunneling to multiple IP families.
		// TODO(https://github.com/envoyproxy/envoy/issues/36318)
		address, port := e.Addresses[0], int(e.EndpointPort)

		waypoint := ""
		if toServiceWaypoint {
			// Address of this specific waypoint endpoint
			// Currently our setup cannot support dual stack, so we need to pick the first address.
			waypoint = net.JoinHostPort(e.Addresses[0], strconv.Itoa(port))
			// Get the VIP of the service we are targeting (not the waypoint service)
			serviceVIPs := b.service.ClusterVIPs.GetAddressesFor(e.Locality.ClusterID)
			if len(serviceVIPs) == 0 {
				serviceVIPs = []string{b.service.DefaultAddress}
			}
			// // If there are multiple VIPs, we just use one. Hostname would be ideal here but isn't supported.
			address = serviceVIPs[0]
			port = b.port
		}
		// We intentionally do not take into account waypoints here.
		// 1. Workload waypoints: sidecar/ingress do not support sending traffic directly to workloads, only to services,
		//    so these are not applicable.
		// 2. Service waypoints: in ztunnel, we would defer handling service traffic if the service has a waypoint, and instead
		//    send to the waypoint. However, with sidecars this is problematic. We don't know which service is the intended destination
		//    until *after* we apply policies. If we then sent to a service waypoint, we apply service policies twice.
		//    This can be problematic: double mirroring, fault injection, request manipulation, ....
		//    Instead, we consider this to workload traffic. This gives the same behavior as if we were an application doing internal load balancing
		//    with ztunnel.
		//    Note: there is a pretty valid case for wanting to send to the service from ingress. This gives a two tier delegation.
		//    However, it's not safe to do that by default; perhaps a future API could opt into this.
		// Support connecting to server side waypoint proxy, if the destination has one. This is for sidecars and ingress.
		// Setup tunnel metadata so requests will go through the tunnel
		target := ptr.NonEmptyOrDefault(waypoint, net.JoinHostPort(address, strconv.Itoa(port)))
		innerAddressName := connectOriginate
		if isEastWestGateway(b.proxy) {
			innerAddressName = forwardInnerConnect
		}
		ep.HostIdentifier = &endpoint.LbEndpoint_Endpoint{Endpoint: &endpoint.Endpoint{
			Address: util.BuildInternalAddressWithIdentifier(innerAddressName, target),
		}}
		ep.Metadata.FilterMetadata[util.OriginalDstMetadataKey] = util.BuildTunnelMetadataStruct(address, port, waypoint)
		if b.dir != model.TrafficDirectionInboundVIP {
			// Add TLS metadata matcher to indicate we can use HBONE for this endpoint.
			// We skip this for service waypoint, which doesn't need to dynamically match mTLS vs HBONE.
			ep.Metadata.FilterMetadata[util.EnvoyTransportSocketMetadataKey] = &structpb.Struct{
				Fields: map[string]*structpb.Value{
					model.TunnelLabelShortName: {Kind: &structpb.Value_StringValue{StringValue: model.TunnelHTTP}},
				},
			}
		}
	} else {
		addr := util.BuildAddress(e.Addresses[0], e.EndpointPort)

		// This change can support multiple addresses for an endpoint, then there are some use cases for it, such as
		// 1. An endpoint can have both ipv4 or ipv6
		// 2. An endpoint can be represented a serviceentry/workload instance with multiple IP addresses
		// When the additional_addresses field is populated for EDS in Envoy configuration, there would be a Happy Eyeballs
		// algorithm to instantiate for the Endpoint, first attempt connecting to the IP address in the address field.
		// Thereafter it will interleave IP addresses in the `additional_addresses` field based on IP version, as described in rfc8305,
		// and attempt connections with them with a delay of 300ms each. The first connection to succeed will be used.
		// Note: it uses Hash Based Load Balancing Policies for multiple addresses support Endpoint, and only the first address of the
		// endpoint will be used as the hash key for the ring or maglev list, however, the upstream address that load balancer ends up
		// connecting to will depend on the one that ends up "winning" using the Happy Eyeballs algorithm.
		// Please refer to https://docs.google.com/document/d/1AjmTcMWwb7nia4rAgqE-iqIbSbfiXCI4h1vk-FONFdM/ for more details.
		var additionalAddrs []*endpoint.Endpoint_AdditionalAddress
		if features.EnableDualStack {
			for _, itemAddr := range e.Addresses[1:] {
				coreAddr := util.BuildAddress(itemAddr, e.EndpointPort)
				additionalAddr := &endpoint.Endpoint_AdditionalAddress{
					Address: coreAddr,
				}
				additionalAddrs = append(additionalAddrs, additionalAddr)
			}
		}
		ep.HostIdentifier = &endpoint.LbEndpoint_Endpoint{
			Endpoint: &endpoint.Endpoint{
				Address:             addr,
				AdditionalAddresses: additionalAddrs,
			},
		}
	}

	return ep
}

func supportTunnel(b *EndpointBuilder, e *model.IstioEndpoint) bool {
	if b.proxy.IsProxylessGrpc() {
		// Proxyless client cannot handle tunneling, even if the server can
		return false
	}

	// Other side is a waypoint proxy.
	if al := e.Labels[label.GatewayManaged.Name]; al == constants.ManagedGatewayMeshControllerLabel {
		return true
	}

	// Otherwise supports tunnel
	// Currently we only support HTTP tunnel, so just check for that. If we support more, we will
	// need to pick the right one based on our support overlap.
	if e.SupportsTunnel(model.TunnelHTTP) {
		return true
	}

	// Otherwise has ambient enabled. Note: this is a synthetic label, not existing in the real Pod.
	// Check all addresses and return true if there is any IP address that supports tunneling when current endpoint has multiple addresses
	for _, addr := range e.Addresses {
		if b.push.SupportsTunnel(e.Network, addr) {
			return true
		}
	}

	return false
}

func getOutlierDetectionAndLoadBalancerSettings(
	destinationRule *v1alpha3.DestinationRule,
	portNumber int,
	subsetName string,
) (bool, *v1alpha3.LoadBalancerSettings) {
	if destinationRule == nil {
		return false, nil
	}
	outlierDetectionEnabled := false
	var lbSettings *v1alpha3.LoadBalancerSettings

	port := &model.Port{Port: portNumber}
	policy := getSubsetTrafficPolicy(destinationRule, port, subsetName)
	if policy != nil {
		lbSettings = policy.LoadBalancer
		if policy.OutlierDetection != nil {
			outlierDetectionEnabled = true
		}
	}

	return outlierDetectionEnabled, lbSettings
}

func getSubsetTrafficPolicy(destinationRule *v1alpha3.DestinationRule, port *model.Port, subsetName string) *v1alpha3.TrafficPolicy {
	var subSetTrafficPolicy *v1alpha3.TrafficPolicy
	for _, subset := range destinationRule.Subsets {
		if subset.Name == subsetName {
			subSetTrafficPolicy = subset.TrafficPolicy
			break
		}
	}
	return util.MergeSubsetTrafficPolicy(destinationRule.TrafficPolicy, subSetTrafficPolicy, port)
}

// getSubSetLabels returns the labels associated with a subset of a given service.
func getSubSetLabels(dr *v1alpha3.DestinationRule, subsetName string) labels.Instance {
	// empty subset
	if subsetName == "" {
		return nil
	}

	if dr == nil {
		return nil
	}

	for _, subset := range dr.Subsets {
		if subset.Name == subsetName {
			if len(subset.Labels) == 0 {
				return nil
			}
			return subset.Labels
		}
	}

	return nil
}

// For services that have a waypoint, we want to send to the waypoints rather than the service endpoints.
// Lookup the service, find its waypoint, then find the waypoint's endpoints.
func (b *EndpointBuilder) findServiceWaypoint(endpointIndex *model.EndpointIndex) ([]*model.IstioEndpoint, bool) {
	// Currently we only support routers (gateways)
	if b.nodeType != model.Router && !isEastWestGateway(b.proxy) {
		// Currently only ingress and e/w gateway will call waypoints
		return nil, false
	}
	if !b.service.HasAddressOrAssigned(b.proxy.Metadata.ClusterID) {
		// No VIP, so skip this. Currently, waypoints can only accept VIP traffic
		return nil, false
	}

	svcs := b.push.ServicesWithWaypoint(b.service.Attributes.Namespace + "/" + string(b.hostname))
	if len(svcs) == 0 {
		// Service isn't captured by a waypoint
		return nil, false
	}
	if len(svcs) > 1 {
		log.Warnf("unexpected multiple waypoint services for %v", b.clusterName)
	}
	svc := svcs[0]
	// They need to explicitly opt-in on the service to send from ingress -> waypoint
	if !svc.IngressUseWaypoint && !isEastWestGateway(b.proxy) {
		return nil, false
	}
	waypointClusterName := model.BuildSubsetKey(
		model.TrafficDirectionOutbound,
		"",
		host.Name(svc.WaypointHostname),
		int(svc.Service.GetWaypoint().GetHboneMtlsPort()),
	)
	endpointBuilder := NewEndpointBuilder(waypointClusterName, b.proxy, b.push)
	waypointEndpoints, _ := endpointBuilder.snapshotEndpointsForPort(endpointIndex)
	return waypointEndpoints, true
}

func (b *EndpointBuilder) snapshotEndpointsForPort(endpointIndex *model.EndpointIndex) ([]*model.IstioEndpoint, bool) {
	svcPort := b.servicePort(b.port)
	if svcPort == nil {
		return nil, false
	}
	svcEps := b.snapshotShards(endpointIndex)
	svcEps = slices.FilterInPlace(svcEps, func(ep *model.IstioEndpoint) bool {
		// filter out endpoints that don't match the service port
		if svcPort.Name != ep.ServicePortName {
			return false
		}
		// filter out endpoints that don't match the subset
		if !b.subsetLabels.SubsetOf(ep.Labels) {
			return false
		}
		return true
	})
	return svcEps, true
}

// Duplicated from networking/core/waypoint to avoid circular dependency
func isEastWestGateway(node *model.Proxy) bool {
	if node == nil || node.Type != model.Waypoint {
		return false
	}
	controller, isManagedGateway := node.Labels[label.GatewayManaged.Name]

	return isManagedGateway && controller == constants.ManagedGatewayEastWestControllerLabel
}

// isWaypointProxy checks if the proxy is an actual waypoint and not a say E/W gateway.
// We need it because as you can see by looking at isEastWestGateway above, E/W gateway is also
// considered a waypoint proxy, so to tell if it's actually a waypoint just checking node type
// isn't enough
func isWaypointProxy(node *model.Proxy) bool {
	if node == nil || node.Type != model.Waypoint {
		return false
	}
	controller, isManagedGateway := node.Labels[label.GatewayManaged.Name]

	return isManagedGateway && controller == constants.ManagedGatewayMeshControllerLabel
}
