// Copyright Istio Authors. All Rights Reserved.
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

package v1alpha3

import (
	"fmt"
	"math"
	"sort"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	auth "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	http "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	any "google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/telemetry"
	"istio.io/istio/pilot/pkg/networking/util"
	authn_model "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	istio_cluster "istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/util/sets"
	"istio.io/pkg/log"
)

var istioMtlsTransportSocketMatch = &structpb.Struct{
	Fields: map[string]*structpb.Value{
		model.TLSModeLabelShortname: {Kind: &structpb.Value_StringValue{StringValue: model.IstioMutualTLSModeLabel}},
	},
}

// h2UpgradeMap specifies the truth table when upgrade takes place.
var h2UpgradeMap = map[upgradeTuple]bool{
	{meshconfig.MeshConfig_DO_NOT_UPGRADE, networking.ConnectionPoolSettings_HTTPSettings_UPGRADE}:        true,
	{meshconfig.MeshConfig_DO_NOT_UPGRADE, networking.ConnectionPoolSettings_HTTPSettings_DO_NOT_UPGRADE}: false,
	{meshconfig.MeshConfig_DO_NOT_UPGRADE, networking.ConnectionPoolSettings_HTTPSettings_DEFAULT}:        false,
	{meshconfig.MeshConfig_UPGRADE, networking.ConnectionPoolSettings_HTTPSettings_UPGRADE}:               true,
	{meshconfig.MeshConfig_UPGRADE, networking.ConnectionPoolSettings_HTTPSettings_DO_NOT_UPGRADE}:        false,
	{meshconfig.MeshConfig_UPGRADE, networking.ConnectionPoolSettings_HTTPSettings_DEFAULT}:               true,
}

// passthroughHttpProtocolOptions are http protocol options used for pass through clusters.
// nolint
// revive:disable-next-line
var passthroughHttpProtocolOptions = util.MessageToAny(&http.HttpProtocolOptions{
	UpstreamProtocolOptions: &http.HttpProtocolOptions_UseDownstreamProtocolConfig{
		UseDownstreamProtocolConfig: &http.HttpProtocolOptions_UseDownstreamHttpConfig{
			HttpProtocolOptions:  &core.Http1ProtocolOptions{},
			Http2ProtocolOptions: http2ProtocolOptions(),
		},
	},
})

// MutableCluster wraps Cluster object along with options.
type MutableCluster struct {
	cluster *cluster.Cluster
	// httpProtocolOptions stores the HttpProtocolOptions which will be marshaled when build is called.
	httpProtocolOptions *http.HttpProtocolOptions
}

// metadataCerts hosts client certificate related metadata specified in proxy metadata.
type metadataCerts struct {
	// tlsClientCertChain is the absolute path to client cert-chain file
	tlsClientCertChain string
	// tlsClientKey is the absolute path to client private key file
	tlsClientKey string
	// tlsClientRootCert is the absolute path to client root cert file
	tlsClientRootCert string
}

// ClusterBuilder interface provides an abstraction for building Envoy Clusters.
type ClusterBuilder struct {
	// Proxy related information used to build clusters.
	serviceInstances  []*model.ServiceInstance // Service instances of Proxy.
	metadataCerts     *metadataCerts           // Client certificates specified in metadata.
	clusterID         string                   // Cluster in which proxy is running.
	proxyID           string                   // Identifier that uniquely identifies a proxy.
	proxyVersion      string                   // Version of Proxy.
	proxyType         model.NodeType           // Indicates whether the proxy is sidecar or gateway.
	sidecarScope      *model.SidecarScope      // Computed sidecar for the proxy.
	passThroughBindIP string                   // Passthrough IP to be used while building clusters.
	supportsIPv4      bool                     // Whether Proxy IPs has IPv4 address.
	supportsIPv6      bool                     // Whether Proxy IPs has IPv6 address.
	locality          *core.Locality           // Locality information of proxy.
	proxyLabels       map[string]string        // Proxy labels.
	proxyView         model.ProxyView          // Proxy view of endpoints.
	proxyIPAddresses  []string                 // IP addresses on which proxy is listening on.
	configNamespace   string                   // Proxy config namespace.
	// PushRequest to look for updates.
	req   *model.PushRequest
	cache model.XdsCache
}

// NewClusterBuilder builds an instance of ClusterBuilder.
func NewClusterBuilder(proxy *model.Proxy, req *model.PushRequest, cache model.XdsCache) *ClusterBuilder {
	cb := &ClusterBuilder{
		serviceInstances:  proxy.ServiceInstances,
		proxyID:           proxy.ID,
		proxyType:         proxy.Type,
		proxyVersion:      proxy.Metadata.IstioVersion,
		sidecarScope:      proxy.SidecarScope,
		passThroughBindIP: getPassthroughBindIP(proxy),
		supportsIPv4:      proxy.SupportsIPv4(),
		supportsIPv6:      proxy.SupportsIPv6(),
		locality:          proxy.Locality,
		proxyLabels:       proxy.Metadata.Labels,
		proxyView:         proxy.GetView(),
		proxyIPAddresses:  proxy.IPAddresses,
		configNamespace:   proxy.ConfigNamespace,
		req:               req,
		cache:             cache,
	}
	if proxy.Metadata != nil {
		if proxy.Metadata.TLSClientCertChain != "" {
			cb.metadataCerts = &metadataCerts{
				tlsClientCertChain: proxy.Metadata.TLSClientCertChain,
				tlsClientKey:       proxy.Metadata.TLSClientKey,
				tlsClientRootCert:  proxy.Metadata.TLSClientRootCert,
			}
		}
		cb.clusterID = string(proxy.Metadata.ClusterID)
	}
	return cb
}

func (m *metadataCerts) String() string {
	return m.tlsClientCertChain + "~" + m.tlsClientKey + "~" + m.tlsClientRootCert
}

// NewMutableCluster initializes MutableCluster with the cluster passed.
func NewMutableCluster(cluster *cluster.Cluster) *MutableCluster {
	return &MutableCluster{
		cluster: cluster,
	}
}

// sidecarProxy returns true if the clusters are being built for sidecar proxy otherwise false.
func (cb *ClusterBuilder) sidecarProxy() bool {
	return cb.proxyType == model.SidecarProxy
}

func (cb *ClusterBuilder) buildSubsetCluster(opts buildClusterOpts, destRule *config.Config, subset *networking.Subset, service *model.Service,
	proxyView model.ProxyView,
) *cluster.Cluster {
	opts.serviceMTLSMode = cb.req.Push.BestEffortInferServiceMTLSMode(subset.GetTrafficPolicy(), service, opts.port)
	var subsetClusterName string
	var defaultSni string
	if opts.clusterMode == DefaultClusterMode {
		subsetClusterName = model.BuildSubsetKey(model.TrafficDirectionOutbound, subset.Name, service.Hostname, opts.port.Port)
		defaultSni = model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, subset.Name, service.Hostname, opts.port.Port)
	} else {
		subsetClusterName = model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, subset.Name, service.Hostname, opts.port.Port)
	}
	// clusters with discovery type STATIC, STRICT_DNS rely on cluster.LoadAssignment field.
	// ServiceEntry's need to filter hosts based on subset.labels in order to perform weighted routing
	var lbEndpoints []*endpoint.LocalityLbEndpoints

	isPassthrough := subset.GetTrafficPolicy().GetLoadBalancer().GetSimple() == networking.LoadBalancerSettings_PASSTHROUGH
	clusterType := opts.mutable.cluster.GetType()
	if isPassthrough {
		clusterType = cluster.Cluster_ORIGINAL_DST
	}
	if !(isPassthrough || clusterType == cluster.Cluster_EDS) {
		if len(subset.Labels) != 0 {
			lbEndpoints = cb.buildLocalityLbEndpoints(proxyView, service, opts.port.Port, subset.Labels)
		} else {
			lbEndpoints = cb.buildLocalityLbEndpoints(proxyView, service, opts.port.Port, nil)
		}
		if len(lbEndpoints) == 0 {
			log.Debugf("locality endpoints missing for cluster %s", subsetClusterName)
		}
	}

	subsetCluster := cb.buildDefaultCluster(subsetClusterName, clusterType, lbEndpoints, model.TrafficDirectionOutbound, opts.port, service, nil)
	if subsetCluster == nil {
		return nil
	}

	if len(cb.req.Push.Mesh.OutboundClusterStatName) != 0 {
		subsetCluster.cluster.AltStatName = telemetry.BuildStatPrefix(cb.req.Push.Mesh.OutboundClusterStatName,
			string(service.Hostname), subset.Name, opts.port, &service.Attributes)
	}

	// Apply traffic policy for subset cluster with the destination rule traffic policy.
	opts.mutable = subsetCluster
	opts.istioMtlsSni = defaultSni

	// If subset has a traffic policy, apply it so that it overrides the destination rule traffic policy.
	opts.policy = MergeTrafficPolicy(opts.policy, subset.TrafficPolicy, opts.port)

	if destRule != nil {
		destinationRule := CastDestinationRule(destRule)
		opts.isDrWithSelector = destinationRule.GetWorkloadSelector() != nil
	}
	// Apply traffic policy for the subset cluster.
	cb.applyTrafficPolicy(opts)

	maybeApplyEdsConfig(subsetCluster.cluster)

	if cb.proxyType == model.Router || opts.direction == model.TrafficDirectionOutbound {
		cb.applyMetadataExchange(opts.mutable.cluster)
	}

	// Add the DestinationRule+subsets metadata. Metadata here is generated on a per-cluster
	// basis in buildDefaultCluster, so we can just insert without a copy.
	subsetCluster.cluster.Metadata = util.AddConfigInfoMetadata(subsetCluster.cluster.Metadata, destRule.Meta)
	util.AddSubsetToMetadata(subsetCluster.cluster.Metadata, subset.Name)
	return subsetCluster.build()
}

// applyDestinationRule applies the destination rule if it exists for the Service. It returns the subset clusters if any created as it
// applies the destination rule.
func (cb *ClusterBuilder) applyDestinationRule(mc *MutableCluster, clusterMode ClusterMode, service *model.Service,
	port *model.Port, proxyView model.ProxyView, destRule *config.Config, serviceAccounts []string,
) []*cluster.Cluster {
	destinationRule := CastDestinationRule(destRule)
	// merge applicable port level traffic policy settings
	trafficPolicy := MergeTrafficPolicy(nil, destinationRule.GetTrafficPolicy(), port)
	opts := buildClusterOpts{
		mesh:             cb.req.Push.Mesh,
		serviceInstances: cb.serviceInstances,
		mutable:          mc,
		policy:           trafficPolicy,
		port:             port,
		clusterMode:      clusterMode,
		direction:        model.TrafficDirectionOutbound,
	}

	if clusterMode == DefaultClusterMode {
		opts.serviceAccounts = serviceAccounts
		opts.istioMtlsSni = model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, "", service.Hostname, port.Port)
		opts.meshExternal = service.MeshExternal
		opts.serviceRegistry = service.Attributes.ServiceRegistry
		opts.serviceMTLSMode = cb.req.Push.BestEffortInferServiceMTLSMode(destinationRule.GetTrafficPolicy(), service, port)
	}

	if destRule != nil {
		opts.isDrWithSelector = destinationRule.GetWorkloadSelector() != nil
	}
	// Apply traffic policy for the main default cluster.
	cb.applyTrafficPolicy(opts)

	// Apply EdsConfig if needed. This should be called after traffic policy is applied because, traffic policy might change
	// discovery type.
	maybeApplyEdsConfig(mc.cluster)

	if cb.proxyType == model.Router || opts.direction == model.TrafficDirectionOutbound {
		cb.applyMetadataExchange(opts.mutable.cluster)
	}

	if destRule != nil {
		mc.cluster.Metadata = util.AddConfigInfoMetadata(mc.cluster.Metadata, destRule.Meta)
	}
	subsetClusters := make([]*cluster.Cluster, 0)
	for _, subset := range destinationRule.GetSubsets() {
		subsetCluster := cb.buildSubsetCluster(opts, destRule, subset, service, proxyView)
		if subsetCluster != nil {
			subsetClusters = append(subsetClusters, subsetCluster)
		}
	}
	return subsetClusters
}

func (cb *ClusterBuilder) applyMetadataExchange(c *cluster.Cluster) {
	if features.MetadataExchange {
		c.Filters = append(c.Filters, xdsfilters.TCPClusterMx)
	}
}

// MergeTrafficPolicy returns the merged TrafficPolicy for a destination-level and subset-level policy on a given port.
func MergeTrafficPolicy(original, subsetPolicy *networking.TrafficPolicy, port *model.Port) *networking.TrafficPolicy {
	if subsetPolicy == nil {
		return original
	}

	// Sanity check that top-level port level settings have already been merged for the given port
	if original != nil && len(original.PortLevelSettings) != 0 {
		original = MergeTrafficPolicy(nil, original, port)
	}

	mergedPolicy := &networking.TrafficPolicy{}
	if original != nil {
		mergedPolicy.ConnectionPool = original.ConnectionPool
		mergedPolicy.LoadBalancer = original.LoadBalancer
		mergedPolicy.OutlierDetection = original.OutlierDetection
		mergedPolicy.Tls = original.Tls
	}

	// Override with subset values.
	if subsetPolicy.ConnectionPool != nil {
		mergedPolicy.ConnectionPool = subsetPolicy.ConnectionPool
	}
	if subsetPolicy.OutlierDetection != nil {
		mergedPolicy.OutlierDetection = subsetPolicy.OutlierDetection
	}
	if subsetPolicy.LoadBalancer != nil {
		mergedPolicy.LoadBalancer = subsetPolicy.LoadBalancer
	}
	if subsetPolicy.Tls != nil {
		mergedPolicy.Tls = subsetPolicy.Tls
	}

	// Check if port level overrides exist, if yes override with them.
	if port != nil {
		for _, p := range subsetPolicy.PortLevelSettings {
			if p.Port != nil && uint32(port.Port) == p.Port.Number {
				// per the docs, port level policies do not inherit and instead to defaults if not provided
				mergedPolicy.ConnectionPool = p.ConnectionPool
				mergedPolicy.OutlierDetection = p.OutlierDetection
				mergedPolicy.LoadBalancer = p.LoadBalancer
				mergedPolicy.Tls = p.Tls
				break
			}
		}
	}
	return mergedPolicy
}

// buildDefaultCluster builds the default cluster and also applies default traffic policy.
func (cb *ClusterBuilder) buildDefaultCluster(name string, discoveryType cluster.Cluster_DiscoveryType,
	localityLbEndpoints []*endpoint.LocalityLbEndpoints, direction model.TrafficDirection,
	port *model.Port, service *model.Service, allInstances []*model.ServiceInstance,
) *MutableCluster {
	if allInstances == nil {
		allInstances = cb.serviceInstances
	}
	c := &cluster.Cluster{
		Name:                 name,
		ClusterDiscoveryType: &cluster.Cluster_Type{Type: discoveryType},
	}
	ec := NewMutableCluster(c)
	switch discoveryType {
	case cluster.Cluster_STRICT_DNS, cluster.Cluster_LOGICAL_DNS:
		if cb.supportsIPv4 {
			c.DnsLookupFamily = cluster.Cluster_V4_ONLY
		} else {
			c.DnsLookupFamily = cluster.Cluster_V6_ONLY
		}
		dnsRate := cb.req.Push.Mesh.DnsRefreshRate
		c.DnsRefreshRate = dnsRate
		c.RespectDnsTtl = true
		fallthrough
	case cluster.Cluster_STATIC:
		if len(localityLbEndpoints) == 0 {
			cb.req.Push.AddMetric(model.DNSNoEndpointClusters, c.Name, cb.proxyID,
				fmt.Sprintf("%s cluster without endpoints %s found while pushing CDS", discoveryType.String(), c.Name))
			return nil
		}
		c.LoadAssignment = &endpoint.ClusterLoadAssignment{
			ClusterName: name,
			Endpoints:   localityLbEndpoints,
		}
	}

	// For inbound clusters, the default traffic policy is used. For outbound clusters, the default traffic policy
	// will be applied, which would be overridden by traffic policy specified in destination rule, if any.
	opts := buildClusterOpts{
		mesh:             cb.req.Push.Mesh,
		mutable:          ec,
		policy:           nil,
		port:             port,
		serviceAccounts:  nil,
		istioMtlsSni:     "",
		clusterMode:      DefaultClusterMode,
		direction:        direction,
		serviceInstances: cb.serviceInstances,
	}
	// decides whether the cluster corresponds to a service external to mesh or not.
	if direction == model.TrafficDirectionInbound {
		// Inbound cluster always corresponds to service in the mesh.
		opts.meshExternal = false
	} else if service != nil {
		// otherwise, read this information from service object.
		opts.meshExternal = service.MeshExternal
	}

	cb.setUpstreamProtocol(ec, port, direction)
	addTelemetryMetadata(opts, service, direction, allInstances)
	addNetworkingMetadata(opts, service, direction)
	return ec
}

// buildInboundClusterForPortOrUDS constructs a single inbound cluster. The cluster will be bound to
// `inbound|clusterPort||`, and send traffic to <bind>:<instance.Endpoint.EndpointPort>. A workload
// will have a single inbound cluster per port. In general this works properly, with the exception of
// the Service-oriented DestinationRule, and upstream protocol selection. Our documentation currently
// requires a single protocol per port, and the DestinationRule issue is slated to move to Sidecar.
// Note: clusterPort and instance.Endpoint.EndpointPort are identical for standard Services; however,
// Sidecar.Ingress allows these to be different.
func (cb *ClusterBuilder) buildInboundClusterForPortOrUDS(clusterPort int, bind string,
	proxy *model.Proxy, instance *model.ServiceInstance, allInstance []*model.ServiceInstance,
) *MutableCluster {
	clusterName := model.BuildInboundSubsetKey(clusterPort)
	localityLbEndpoints := buildInboundLocalityLbEndpoints(bind, instance.Endpoint.EndpointPort)
	clusterType := cluster.Cluster_ORIGINAL_DST
	if len(localityLbEndpoints) > 0 {
		clusterType = cluster.Cluster_STATIC
	}
	localCluster := cb.buildDefaultCluster(clusterName, clusterType, localityLbEndpoints,
		model.TrafficDirectionInbound, instance.ServicePort, instance.Service, allInstance)
	if clusterType == cluster.Cluster_ORIGINAL_DST {
		// Extend cleanupInterval beyond 5s default. This ensures that upstream connections will stay
		// open for up to 60s. With the default of 5s, we may tear things down too quickly for
		// infrequently accessed services.
		localCluster.cluster.CleanupInterval = &durationpb.Duration{Seconds: 60}
	}
	// If stat name is configured, build the alt statname.
	if len(cb.req.Push.Mesh.InboundClusterStatName) != 0 {
		localCluster.cluster.AltStatName = telemetry.BuildStatPrefix(cb.req.Push.Mesh.InboundClusterStatName,
			string(instance.Service.Hostname), "", instance.ServicePort, &instance.Service.Attributes)
	}

	opts := buildClusterOpts{
		mesh:             cb.req.Push.Mesh,
		mutable:          localCluster,
		policy:           nil,
		port:             instance.ServicePort,
		serviceAccounts:  nil,
		serviceInstances: cb.serviceInstances,
		istioMtlsSni:     "",
		clusterMode:      DefaultClusterMode,
		direction:        model.TrafficDirectionInbound,
	}
	// When users specify circuit breakers, they need to be set on the receiver end
	// (server side) as well as client side, so that the server has enough capacity
	// (not the defaults) to handle the increased traffic volume
	// TODO: This is not foolproof - if instance is part of multiple services listening on same port,
	// choice of inbound cluster is arbitrary. So the connection pool settings may not apply cleanly.
	cfg := proxy.SidecarScope.DestinationRule(model.TrafficDirectionInbound, proxy, instance.Service.Hostname)
	if cfg != nil {
		destinationRule := cfg.Spec.(*networking.DestinationRule)
		opts.isDrWithSelector = destinationRule.GetWorkloadSelector() != nil
		if destinationRule.TrafficPolicy != nil {
			opts.policy = MergeTrafficPolicy(opts.policy, destinationRule.TrafficPolicy, instance.ServicePort)
			util.AddConfigInfoMetadata(localCluster.cluster.Metadata, cfg.Meta)
		}
	}
	cb.applyTrafficPolicy(opts)

	if bind != LocalhostAddress && bind != LocalhostIPv6Address {
		// iptables will redirect our own traffic to localhost back to us if we do not use the "magic" upstream bind
		// config which will be skipped.
		localCluster.cluster.UpstreamBindConfig = &core.BindConfig{
			SourceAddress: &core.SocketAddress{
				Address: cb.passThroughBindIP,
				PortSpecifier: &core.SocketAddress_PortValue{
					PortValue: uint32(0),
				},
			},
		}
	}
	return localCluster
}

func (cb *ClusterBuilder) buildLocalityLbEndpoints(proxyView model.ProxyView, service *model.Service,
	port int, labels labels.Instance,
) []*endpoint.LocalityLbEndpoints {
	if !(service.Resolution == model.DNSLB || service.Resolution == model.DNSRoundRobinLB) {
		return nil
	}

	instances := cb.req.Push.ServiceInstancesByPort(service, port, labels)

	// Determine whether or not the target service is considered local to the cluster
	// and should, therefore, not be accessed from outside the cluster.
	isClusterLocal := cb.req.Push.IsClusterLocal(service)

	lbEndpoints := make(map[string][]*endpoint.LbEndpoint)
	for _, instance := range instances {
		// Only send endpoints from the networks in the network view requested by the proxy.
		// The default network view assigned to the Proxy is nil, in that case match any network.
		if !proxyView.IsVisible(instance.Endpoint) {
			// Endpoint's network doesn't match the set of networks that the proxy wants to see.
			continue
		}
		// If the downstream service is configured as cluster-local, only include endpoints that
		// reside in the same cluster.
		if isClusterLocal && (cb.clusterID != string(instance.Endpoint.Locality.ClusterID)) {
			continue
		}
		// TODO(nmittler): Consider merging discoverability policy with cluster-local
		// TODO(ramaraochavali): Find a better way here so that we do not have build proxy.
		// Currently it works because we only determine discoverability only by cluster.
		if !instance.Endpoint.IsDiscoverableFromProxy(&model.Proxy{Metadata: &model.NodeMetadata{ClusterID: istio_cluster.ID(cb.clusterID)}}) {
			continue
		}
		addr := util.BuildAddress(instance.Endpoint.Address, instance.Endpoint.EndpointPort)
		ep := &endpoint.LbEndpoint{
			HostIdentifier: &endpoint.LbEndpoint_Endpoint{
				Endpoint: &endpoint.Endpoint{
					Address: addr,
				},
			},
			LoadBalancingWeight: &wrappers.UInt32Value{
				Value: instance.Endpoint.GetLoadBalancingWeight(),
			},
		}

		labels := instance.Endpoint.Labels
		ns := instance.Endpoint.Namespace
		if features.CanonicalServiceForMeshExternalServiceEntry && service.MeshExternal {
			ns = service.Attributes.Namespace
			svcLabels := service.Attributes.Labels
			if _, ok := svcLabels[model.IstioCanonicalServiceLabelName]; ok {
				labels = map[string]string{
					model.IstioCanonicalServiceLabelName:         svcLabels[model.IstioCanonicalServiceLabelName],
					model.IstioCanonicalServiceRevisionLabelName: svcLabels[model.IstioCanonicalServiceRevisionLabelName],
				}
				for k, v := range instance.Endpoint.Labels {
					labels[k] = v
				}
			}
		}

		ep.Metadata = util.BuildLbEndpointMetadata(instance.Endpoint.Network, instance.Endpoint.TLSMode, instance.Endpoint.WorkloadName,
			ns, instance.Endpoint.Locality.ClusterID, labels)

		locality := instance.Endpoint.Locality.Label
		lbEndpoints[locality] = append(lbEndpoints[locality], ep)
	}

	localityLbEndpoints := make([]*endpoint.LocalityLbEndpoints, 0, len(lbEndpoints))
	locs := make([]string, 0, len(lbEndpoints))
	for k := range lbEndpoints {
		locs = append(locs, k)
	}
	if len(locs) >= 2 {
		sort.Strings(locs)
	}
	for _, locality := range locs {
		eps := lbEndpoints[locality]
		var weight uint32
		var overflowStatus bool
		for _, ep := range eps {
			weight, overflowStatus = addUint32(weight, ep.LoadBalancingWeight.GetValue())
		}
		if overflowStatus {
			log.Warnf("Sum of localityLbEndpoints weight is overflow: service:%s, port: %d, locality:%s",
				service.Hostname, port, locality)
		}
		localityLbEndpoints = append(localityLbEndpoints, &endpoint.LocalityLbEndpoints{
			Locality:    util.ConvertLocality(locality),
			LbEndpoints: eps,
			LoadBalancingWeight: &wrappers.UInt32Value{
				Value: weight,
			},
		})
	}

	return localityLbEndpoints
}

// addUint32AvoidOverflow returns sum of two uint32 and status. If sum overflows,
// and returns MaxUint32 and status.
func addUint32(left, right uint32) (uint32, bool) {
	if math.MaxUint32-right < left {
		return math.MaxUint32, true
	}
	return left + right, false
}

// buildInboundPassthroughClusters builds passthrough clusters for inbound.
func (cb *ClusterBuilder) buildInboundPassthroughClusters() []*cluster.Cluster {
	// ipv4 and ipv6 feature detection. Envoy cannot ignore a config where the ip version is not supported
	clusters := make([]*cluster.Cluster, 0, 2)
	if cb.supportsIPv4 {
		inboundPassthroughClusterIpv4 := cb.buildDefaultPassthroughCluster()
		inboundPassthroughClusterIpv4.Name = util.InboundPassthroughClusterIpv4
		inboundPassthroughClusterIpv4.Filters = nil
		inboundPassthroughClusterIpv4.UpstreamBindConfig = &core.BindConfig{
			SourceAddress: &core.SocketAddress{
				Address: InboundPassthroughBindIpv4,
				PortSpecifier: &core.SocketAddress_PortValue{
					PortValue: uint32(0),
				},
			},
		}
		clusters = append(clusters, inboundPassthroughClusterIpv4)
	}
	if cb.supportsIPv6 {
		inboundPassthroughClusterIpv6 := cb.buildDefaultPassthroughCluster()
		inboundPassthroughClusterIpv6.Name = util.InboundPassthroughClusterIpv6
		inboundPassthroughClusterIpv6.Filters = nil
		inboundPassthroughClusterIpv6.UpstreamBindConfig = &core.BindConfig{
			SourceAddress: &core.SocketAddress{
				Address: InboundPassthroughBindIpv6,
				PortSpecifier: &core.SocketAddress_PortValue{
					PortValue: uint32(0),
				},
			},
		}
		clusters = append(clusters, inboundPassthroughClusterIpv6)
	}
	return clusters
}

// generates a cluster that sends traffic to dummy localport 0
// This cluster is used to catch all traffic to unresolved destinations in virtual service
func (cb *ClusterBuilder) buildBlackHoleCluster() *cluster.Cluster {
	c := &cluster.Cluster{
		Name:                 util.BlackHoleCluster,
		ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_STATIC},
		ConnectTimeout:       cb.req.Push.Mesh.ConnectTimeout,
		LbPolicy:             cluster.Cluster_ROUND_ROBIN,
	}
	return c
}

// generates a cluster that sends traffic to the original destination.
// This cluster is used to catch all traffic to unknown listener ports
func (cb *ClusterBuilder) buildDefaultPassthroughCluster() *cluster.Cluster {
	cluster := &cluster.Cluster{
		Name:                 util.PassthroughCluster,
		ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_ORIGINAL_DST},
		ConnectTimeout:       cb.req.Push.Mesh.ConnectTimeout,
		LbPolicy:             cluster.Cluster_CLUSTER_PROVIDED,
		TypedExtensionProtocolOptions: map[string]*any.Any{
			v3.HttpProtocolOptionsType: passthroughHttpProtocolOptions,
		},
	}
	cb.applyConnectionPool(cb.req.Push.Mesh, NewMutableCluster(cluster), &networking.ConnectionPoolSettings{})
	cb.applyMetadataExchange(cluster)
	return cluster
}

// applyH2Upgrade function will upgrade outbound cluster to http2 if specified by configuration.
func (cb *ClusterBuilder) applyH2Upgrade(opts buildClusterOpts, connectionPool *networking.ConnectionPoolSettings) {
	if cb.shouldH2Upgrade(opts.mutable.cluster.Name, opts.direction, opts.port, opts.mesh, connectionPool) {
		cb.setH2Options(opts.mutable)
	}
}

// shouldH2Upgrade function returns true if the cluster  should be upgraded to http2.
func (cb *ClusterBuilder) shouldH2Upgrade(clusterName string, direction model.TrafficDirection, port *model.Port, mesh *meshconfig.MeshConfig,
	connectionPool *networking.ConnectionPoolSettings,
) bool {
	if direction != model.TrafficDirectionOutbound {
		return false
	}

	// TODO (mjog)
	// Upgrade if tls.GetMode() == networking.TLSSettings_ISTIO_MUTUAL
	override := networking.ConnectionPoolSettings_HTTPSettings_DEFAULT
	if connectionPool != nil && connectionPool.Http != nil {
		override = connectionPool.Http.H2UpgradePolicy
	}
	// If user wants an upgrade at destination rule/port level that means he is sure that
	// it is a Http port - upgrade in such case. This is useful incase protocol sniffing is
	// enabled and user wants to upgrade/preserve http protocol from client.
	if override == networking.ConnectionPoolSettings_HTTPSettings_UPGRADE {
		log.Debugf("Upgrading cluster: %v (%v %v)", clusterName, mesh.H2UpgradePolicy, override)
		return true
	}

	// Do not upgrade non-http ports. This also ensures that we are only upgrading
	// named ports so that protocol sniffing does not interfere. Protocol sniffing
	// uses downstream protocol. Therefore if the client upgrades connection to http2,
	// the server will send h2 stream to the application,even though the application only
	// supports http 1.1.
	if port != nil && !port.Protocol.IsHTTP() {
		return false
	}

	if !h2UpgradeMap[upgradeTuple{mesh.H2UpgradePolicy, override}] {
		log.Debugf("Not upgrading cluster: %v (%v %v)", clusterName, mesh.H2UpgradePolicy, override)
		return false
	}

	log.Debugf("Upgrading cluster: %v (%v %v)", clusterName, mesh.H2UpgradePolicy, override)
	return true
}

// setH2Options make the cluster an h2 cluster by setting http2ProtocolOptions.
func (cb *ClusterBuilder) setH2Options(mc *MutableCluster) {
	if mc == nil {
		return
	}
	if mc.httpProtocolOptions == nil {
		mc.httpProtocolOptions = &http.HttpProtocolOptions{}
	}
	options := mc.httpProtocolOptions
	if options.UpstreamHttpProtocolOptions == nil {
		options.UpstreamProtocolOptions = &http.HttpProtocolOptions_ExplicitHttpConfig_{
			ExplicitHttpConfig: &http.HttpProtocolOptions_ExplicitHttpConfig{
				ProtocolConfig: &http.HttpProtocolOptions_ExplicitHttpConfig_Http2ProtocolOptions{
					Http2ProtocolOptions: http2ProtocolOptions(),
				},
			},
		}
	}
}

func (cb *ClusterBuilder) applyTrafficPolicy(opts buildClusterOpts) {
	connectionPool, outlierDetection, loadBalancer, tls := selectTrafficPolicyComponents(opts.policy)
	// Connection pool settings are applicable for both inbound and outbound clusters.
	if connectionPool == nil {
		connectionPool = &networking.ConnectionPoolSettings{}
	}
	cb.applyConnectionPool(opts.mesh, opts.mutable, connectionPool)
	if opts.direction != model.TrafficDirectionInbound {
		cb.applyH2Upgrade(opts, connectionPool)
		applyOutlierDetection(opts.mutable.cluster, outlierDetection)
		applyLoadBalancer(opts.mutable.cluster, loadBalancer, opts.port, cb.locality, cb.proxyLabels, opts.mesh)
		if opts.clusterMode != SniDnatClusterMode {
			autoMTLSEnabled := opts.mesh.GetEnableAutoMtls().Value
			tls, mtlsCtxType := cb.buildAutoMtlsSettings(tls, opts.serviceAccounts, opts.istioMtlsSni,
				autoMTLSEnabled, opts.meshExternal, opts.serviceMTLSMode)
			cb.applyUpstreamTLSSettings(&opts, tls, mtlsCtxType)
		}
	}

	if opts.mutable.cluster.GetType() == cluster.Cluster_ORIGINAL_DST {
		opts.mutable.cluster.LbPolicy = cluster.Cluster_CLUSTER_PROVIDED
	}
}

// buildAutoMtlsSettings fills key cert fields for all TLSSettings when the mode is `ISTIO_MUTUAL`.
// If the (input) TLS setting is nil (i.e not set), *and* the service mTLS mode is STRICT, it also
// creates and populates the config as if they are set as ISTIO_MUTUAL.
func (cb *ClusterBuilder) buildAutoMtlsSettings(
	tls *networking.ClientTLSSettings,
	serviceAccounts []string,
	sni string,
	autoMTLSEnabled bool,
	meshExternal bool,
	serviceMTLSMode model.MutualTLSMode,
) (*networking.ClientTLSSettings, mtlsContextType) {
	if tls != nil {
		if tls.Mode == networking.ClientTLSSettings_DISABLE || tls.Mode == networking.ClientTLSSettings_SIMPLE {
			return tls, userSupplied
		}
		// For backward compatibility, use metadata certs if provided.
		if cb.hasMetadataCerts() {
			// When building Mutual TLS settings, we should always use user supplied SubjectAltNames and SNI
			// in destination rule. The Service Accounts and auto computed SNI should only be used for
			// ISTIO_MUTUAL.
			return cb.buildMutualTLS(tls.SubjectAltNames, tls.Sni), userSupplied
		}
		if tls.Mode != networking.ClientTLSSettings_ISTIO_MUTUAL {
			return tls, userSupplied
		}
		// Update TLS settings for ISTIO_MUTUAL. Use client provided SNI if set. Otherwise,
		// overwrite with the auto generated SNI. User specified SNIs in the istio mtls settings
		// are useful when routing via gateways. Use Service Accounts if Subject Alt names
		// are not specified in TLS settings.
		sniToUse := tls.Sni
		if len(sniToUse) == 0 {
			sniToUse = sni
		}
		subjectAltNamesToUse := tls.SubjectAltNames
		if subjectAltNamesToUse == nil {
			subjectAltNamesToUse = serviceAccounts
		}
		return cb.buildIstioMutualTLS(subjectAltNamesToUse, sniToUse), userSupplied
	}

	if meshExternal || !autoMTLSEnabled || serviceMTLSMode == model.MTLSUnknown || serviceMTLSMode == model.MTLSDisable {
		return nil, userSupplied
	}

	// For backward compatibility, use metadata certs if provided.
	if cb.hasMetadataCerts() {
		return cb.buildMutualTLS(serviceAccounts, sni), autoDetected
	}

	// Build settings for auto MTLS.
	return cb.buildIstioMutualTLS(serviceAccounts, sni), autoDetected
}

func (cb *ClusterBuilder) hasMetadataCerts() bool {
	return cb.metadataCerts != nil
}

type mtlsContextType int

const (
	userSupplied mtlsContextType = iota
	autoDetected
)

// buildMutualTLS returns a `TLSSettings` for MUTUAL mode with proxy metadata certificates.
func (cb *ClusterBuilder) buildMutualTLS(serviceAccounts []string, sni string) *networking.ClientTLSSettings {
	return &networking.ClientTLSSettings{
		Mode:              networking.ClientTLSSettings_MUTUAL,
		CaCertificates:    cb.metadataCerts.tlsClientRootCert,
		ClientCertificate: cb.metadataCerts.tlsClientCertChain,
		PrivateKey:        cb.metadataCerts.tlsClientKey,
		SubjectAltNames:   serviceAccounts,
		Sni:               sni,
	}
}

// buildIstioMutualTLS returns a `TLSSettings` for ISTIO_MUTUAL mode.
func (cb *ClusterBuilder) buildIstioMutualTLS(san []string, sni string) *networking.ClientTLSSettings {
	return &networking.ClientTLSSettings{
		Mode:            networking.ClientTLSSettings_ISTIO_MUTUAL,
		SubjectAltNames: san,
		Sni:             sni,
	}
}

func (cb *ClusterBuilder) applyDefaultConnectionPool(cluster *cluster.Cluster) {
	cluster.ConnectTimeout = cb.req.Push.Mesh.ConnectTimeout
}

// FIXME: there isn't a way to distinguish between unset values and zero values
func (cb *ClusterBuilder) applyConnectionPool(mesh *meshconfig.MeshConfig, mc *MutableCluster, settings *networking.ConnectionPoolSettings) {
	if settings == nil {
		return
	}

	threshold := getDefaultCircuitBreakerThresholds()
	var idleTimeout *durationpb.Duration
	var maxRequestsPerConnection uint32

	if settings.Http != nil {
		if settings.Http.Http2MaxRequests > 0 {
			// Envoy only applies MaxRequests in HTTP/2 clusters
			threshold.MaxRequests = &wrappers.UInt32Value{Value: uint32(settings.Http.Http2MaxRequests)}
		}
		if settings.Http.Http1MaxPendingRequests > 0 {
			// Envoy only applies MaxPendingRequests in HTTP/1.1 clusters
			threshold.MaxPendingRequests = &wrappers.UInt32Value{Value: uint32(settings.Http.Http1MaxPendingRequests)}
		}

		// FIXME: zero is a valid value if explicitly set, otherwise we want to use the default
		if settings.Http.MaxRetries > 0 {
			threshold.MaxRetries = &wrappers.UInt32Value{Value: uint32(settings.Http.MaxRetries)}
		}

		idleTimeout = settings.Http.IdleTimeout
		maxRequestsPerConnection = uint32(settings.Http.MaxRequestsPerConnection)
	}

	cb.applyDefaultConnectionPool(mc.cluster)
	if settings.Tcp != nil {
		if settings.Tcp != nil && settings.Tcp.ConnectTimeout != nil {
			mc.cluster.ConnectTimeout = settings.Tcp.ConnectTimeout
		}

		if settings.Tcp != nil && settings.Tcp.MaxConnections > 0 {
			threshold.MaxConnections = &wrappers.UInt32Value{Value: uint32(settings.Tcp.MaxConnections)}
		}
	}
	applyTCPKeepalive(mesh, mc.cluster, settings.Tcp)

	mc.cluster.CircuitBreakers = &cluster.CircuitBreakers{
		Thresholds: []*cluster.CircuitBreakers_Thresholds{threshold},
	}

	if idleTimeout != nil || maxRequestsPerConnection > 0 {
		if mc.httpProtocolOptions == nil {
			mc.httpProtocolOptions = &http.HttpProtocolOptions{}
		}
		commonOptions := mc.httpProtocolOptions
		if commonOptions.CommonHttpProtocolOptions == nil {
			commonOptions.CommonHttpProtocolOptions = &core.HttpProtocolOptions{}
		}
		if idleTimeout != nil {
			idleTimeoutDuration := idleTimeout
			commonOptions.CommonHttpProtocolOptions.IdleTimeout = idleTimeoutDuration
		}
		if maxRequestsPerConnection > 0 {
			commonOptions.CommonHttpProtocolOptions.MaxRequestsPerConnection = &wrappers.UInt32Value{Value: maxRequestsPerConnection}
		}
	}

	if settings.Http != nil && settings.Http.UseClientProtocol {
		// Use downstream protocol. If the incoming traffic use HTTP 1.1, the
		// upstream cluster will use HTTP 1.1, if incoming traffic use HTTP2,
		// the upstream cluster will use HTTP2.
		cb.setUseDownstreamProtocol(mc)
	}
}

func (cb *ClusterBuilder) applyUpstreamTLSSettings(opts *buildClusterOpts, tls *networking.ClientTLSSettings, mtlsCtxType mtlsContextType) {
	if tls == nil {
		return
	}

	c := opts.mutable

	tlsContext, err := cb.buildUpstreamClusterTLSContext(opts, tls)
	if err != nil {
		log.Errorf("failed to build Upstream TLSContext: %s", err.Error())
		return
	}

	if tlsContext != nil {
		c.cluster.TransportSocket = &core.TransportSocket{
			Name:       wellknown.TransportSocketTls,
			ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: util.MessageToAny(tlsContext)},
		}
	}

	// For headless service, discover type will be `Cluster_ORIGINAL_DST`
	// Apply auto mtls to clusters excluding these kind of headless service
	if c.cluster.GetType() != cluster.Cluster_ORIGINAL_DST {
		// convert to transport socket matcher if the mode was auto detected
		if tls.Mode == networking.ClientTLSSettings_ISTIO_MUTUAL && mtlsCtxType == autoDetected {
			transportSocket := c.cluster.TransportSocket
			c.cluster.TransportSocket = nil
			c.cluster.TransportSocketMatches = []*cluster.Cluster_TransportSocketMatch{
				{
					Name:            "tlsMode-" + model.IstioMutualTLSModeLabel,
					Match:           istioMtlsTransportSocketMatch,
					TransportSocket: transportSocket,
				},
				defaultTransportSocketMatch(),
			}
		}
	}
}

func (cb *ClusterBuilder) buildUpstreamClusterTLSContext(opts *buildClusterOpts, tls *networking.ClientTLSSettings) (*auth.UpstreamTlsContext, error) {
	// Hack to avoid egress sds cluster config generation for sidecar when
	// CredentialName is set in DestinationRule without a workloadSelector.
	// We do not want to support CredentialName setting in non workloadSelector based DestinationRules, because
	// that would result in the CredentialName being supplied to all the sidecars which the DestinationRule is scoped to,
	// resulting in delayed startup of sidecars who do not have access to the credentials.
	if tls.CredentialName != "" && cb.sidecarProxy() && !opts.isDrWithSelector {
		if tls.Mode == networking.ClientTLSSettings_SIMPLE || tls.Mode == networking.ClientTLSSettings_MUTUAL {
			return nil, nil
		}
	}

	c := opts.mutable
	var tlsContext *auth.UpstreamTlsContext

	switch tls.Mode {
	case networking.ClientTLSSettings_DISABLE:
		tlsContext = nil
	case networking.ClientTLSSettings_ISTIO_MUTUAL:
		tlsContext = &auth.UpstreamTlsContext{
			CommonTlsContext: defaultUpstreamCommonTLSContext(),
			Sni:              tls.Sni,
		}

		tlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs = append(tlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs,
			authn_model.ConstructSdsSecretConfig(authn_model.SDSDefaultResourceName))

		tlsContext.CommonTlsContext.ValidationContextType = &auth.CommonTlsContext_CombinedValidationContext{
			CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
				DefaultValidationContext:         &auth.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch(tls.SubjectAltNames)},
				ValidationContextSdsSecretConfig: authn_model.ConstructSdsSecretConfig(authn_model.SDSRootResourceName),
			},
		}
		// Set default SNI of cluster name for istio_mutual if sni is not set.
		if len(tlsContext.Sni) == 0 {
			tlsContext.Sni = c.cluster.Name
		}
		// `istio-peer-exchange` alpn is only used when using mtls communication between peers.
		// We add `istio-peer-exchange` to the list of alpn strings.
		// The code has repeated snippets because We want to use predefined alpn strings for efficiency.
		if cb.isHttp2Cluster(c) {
			// This is HTTP/2 in-mesh cluster, advertise it with ALPN.
			if features.MetadataExchange {
				tlsContext.CommonTlsContext.AlpnProtocols = util.ALPNInMeshH2WithMxc
			} else {
				tlsContext.CommonTlsContext.AlpnProtocols = util.ALPNInMeshH2
			}
		} else {
			// This is in-mesh cluster, advertise it with ALPN.
			if features.MetadataExchange {
				tlsContext.CommonTlsContext.AlpnProtocols = util.ALPNInMeshWithMxc
			} else {
				tlsContext.CommonTlsContext.AlpnProtocols = util.ALPNInMesh
			}
		}
	case networking.ClientTLSSettings_SIMPLE:
		tlsContext = &auth.UpstreamTlsContext{
			CommonTlsContext: defaultUpstreamCommonTLSContext(),
			Sni:              tls.Sni,
		}

		cb.setAutoSniAndAutoSanValidation(c, tls)

		// Use subject alt names specified in service entry if TLS settings does not have subject alt names.
		if opts.serviceRegistry == provider.External && len(tls.SubjectAltNames) == 0 {
			tls.SubjectAltNames = opts.serviceAccounts
		}
		if tls.CredentialName != "" {
			// If  credential name is specified at Destination Rule config and originating node is egress gateway, create
			// SDS config for egress gateway to fetch key/cert at gateway agent.
			authn_model.ApplyCustomSDSToClientCommonTLSContext(tlsContext.CommonTlsContext, tls)
		} else {
			// If CredentialName is not set fallback to files specified in DR.
			res := security.SdsCertificateConfig{
				CaCertificatePath: tls.CaCertificates,
			}
			// If tls.CaCertificate or CaCertificate in Metadata isn't configured don't set up SdsSecretConfig
			if !res.IsRootCertificate() {
				tlsContext.CommonTlsContext.ValidationContextType = &auth.CommonTlsContext_ValidationContext{}
			} else {
				tlsContext.CommonTlsContext.ValidationContextType = &auth.CommonTlsContext_CombinedValidationContext{
					CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
						DefaultValidationContext:         &auth.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch(tls.SubjectAltNames)},
						ValidationContextSdsSecretConfig: authn_model.ConstructSdsSecretConfig(res.GetRootResourceName()),
					},
				}
			}
		}

		if cb.isHttp2Cluster(c) {
			// This is HTTP/2 cluster, advertise it with ALPN.
			tlsContext.CommonTlsContext.AlpnProtocols = util.ALPNH2Only
		}

	case networking.ClientTLSSettings_MUTUAL:
		tlsContext = &auth.UpstreamTlsContext{
			CommonTlsContext: defaultUpstreamCommonTLSContext(),
			Sni:              tls.Sni,
		}

		cb.setAutoSniAndAutoSanValidation(c, tls)

		// Use subject alt names specified in service entry if TLS settings does not have subject alt names.
		if opts.serviceRegistry == provider.External && len(tls.SubjectAltNames) == 0 {
			tls.SubjectAltNames = opts.serviceAccounts
		}
		if tls.CredentialName != "" {
			// If  credential name is specified at Destination Rule config and originating node is egress gateway, create
			// SDS config for egress gateway to fetch key/cert at gateway agent.
			authn_model.ApplyCustomSDSToClientCommonTLSContext(tlsContext.CommonTlsContext, tls)
		} else {
			// If CredentialName is not set fallback to file based approach
			if tls.ClientCertificate == "" || tls.PrivateKey == "" {
				err := fmt.Errorf("failed to apply tls setting for %s: client certificate and private key must not be empty",
					c.cluster.Name)
				return nil, err
			}
			// These are certs being mounted from within the pod and specified in Destination Rules.
			// Rather than reading directly in Envoy, which does not support rotation, we will
			// serve them over SDS by reading the files.
			res := security.SdsCertificateConfig{
				CertificatePath:   tls.ClientCertificate,
				PrivateKeyPath:    tls.PrivateKey,
				CaCertificatePath: tls.CaCertificates,
			}
			tlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs = append(tlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs,
				authn_model.ConstructSdsSecretConfig(res.GetResourceName()))

			// If tls.CaCertificate or CaCertificate in Metadata isn't configured don't set up RootSdsSecretConfig
			if !res.IsRootCertificate() {
				tlsContext.CommonTlsContext.ValidationContextType = &auth.CommonTlsContext_ValidationContext{}
			} else {
				tlsContext.CommonTlsContext.ValidationContextType = &auth.CommonTlsContext_CombinedValidationContext{
					CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
						DefaultValidationContext:         &auth.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch(tls.SubjectAltNames)},
						ValidationContextSdsSecretConfig: authn_model.ConstructSdsSecretConfig(res.GetRootResourceName()),
					},
				}
			}
		}

		if cb.isHttp2Cluster(c) {
			// This is HTTP/2 cluster, advertise it with ALPN.
			tlsContext.CommonTlsContext.AlpnProtocols = util.ALPNH2Only
		}
	}
	return tlsContext, nil
}

// Set auto_sni if EnableAutoSni feature flag is enabled and if sni field is not explicitly set in DR.
// Set auto_san_validation if VerifyCertAtClient feature flag is enabled and if there is no explicit SubjectAltNames specified  in DR.
func (cb *ClusterBuilder) setAutoSniAndAutoSanValidation(mc *MutableCluster, tls *networking.ClientTLSSettings) {
	if mc == nil || !features.EnableAutoSni {
		return
	}

	setAutoSni := false
	setAutoSanValidation := false
	if len(tls.Sni) == 0 {
		setAutoSni = true
	}
	if features.VerifyCertAtClient && len(tls.SubjectAltNames) == 0 {
		setAutoSanValidation = true
	}

	if setAutoSni || setAutoSanValidation {
		if mc.httpProtocolOptions == nil {
			mc.httpProtocolOptions = &http.HttpProtocolOptions{}
		}
		if mc.httpProtocolOptions.UpstreamHttpProtocolOptions == nil {
			mc.httpProtocolOptions.UpstreamHttpProtocolOptions = &core.UpstreamHttpProtocolOptions{}
		}
		if setAutoSni {
			mc.httpProtocolOptions.UpstreamHttpProtocolOptions.AutoSni = true
		}
		if setAutoSanValidation {
			mc.httpProtocolOptions.UpstreamHttpProtocolOptions.AutoSanValidation = true
		}
	}
}

func (cb *ClusterBuilder) setUseDownstreamProtocol(mc *MutableCluster) {
	if mc.httpProtocolOptions == nil {
		mc.httpProtocolOptions = &http.HttpProtocolOptions{}
	}
	options := mc.httpProtocolOptions
	options.UpstreamProtocolOptions = &http.HttpProtocolOptions_UseDownstreamProtocolConfig{
		UseDownstreamProtocolConfig: &http.HttpProtocolOptions_UseDownstreamHttpConfig{
			HttpProtocolOptions:  &core.Http1ProtocolOptions{},
			Http2ProtocolOptions: http2ProtocolOptions(),
		},
	}
}

func http2ProtocolOptions() *core.Http2ProtocolOptions {
	return &core.Http2ProtocolOptions{
		// Envoy default value of 100 is too low for data path.
		MaxConcurrentStreams: &wrappers.UInt32Value{
			Value: 1073741824,
		},
	}
}

// nolint
// revive:disable-next-line
func (cb *ClusterBuilder) isHttp2Cluster(mc *MutableCluster) bool {
	options := mc.httpProtocolOptions
	return options != nil && options.GetExplicitHttpConfig().GetHttp2ProtocolOptions() != nil
}

func (cb *ClusterBuilder) setUpstreamProtocol(mc *MutableCluster, port *model.Port, direction model.TrafficDirection) {
	if port.Protocol.IsHTTP2() {
		cb.setH2Options(mc)
		return
	}

	// Add use_downstream_protocol for sidecar proxy only if protocol sniffing is enabled. Since
	// protocol detection is disabled for gateway and use_downstream_protocol is used under protocol
	// detection for cluster to select upstream connection protocol when the service port is unnamed.
	// use_downstream_protocol should be disabled for gateway; while it sort of makes sense there, even
	// without sniffing, a concern is that clients will do ALPN negotiation, and we always advertise
	// h2. Clients would then connect with h2, while the upstream may not support it. This is not a
	// concern for plaintext, but we do not have a way to distinguish https vs http here. If users of
	// gateway want this behavior, they can configure UseClientProtocol explicitly.
	if cb.sidecarProxy() && ((util.IsProtocolSniffingEnabledForInboundPort(port) && direction == model.TrafficDirectionInbound) ||
		(util.IsProtocolSniffingEnabledForOutboundPort(port) && direction == model.TrafficDirectionOutbound)) {
		// Use downstream protocol. If the incoming traffic use HTTP 1.1, the
		// upstream cluster will use HTTP 1.1, if incoming traffic use HTTP2,
		// the upstream cluster will use HTTP2.
		cb.setUseDownstreamProtocol(mc)
	}
}

// normalizeClusters normalizes clusters to avoid duplicate clusters. This should be called
// at the end before adding the cluster to list of clusters.
func (cb *ClusterBuilder) normalizeClusters(clusters []*discovery.Resource) []*discovery.Resource {
	// resolve cluster name conflicts. there can be duplicate cluster names if there are conflicting service definitions.
	// for any clusters that share the same name the first cluster is kept and the others are discarded.
	have := sets.Set{}
	out := make([]*discovery.Resource, 0, len(clusters))
	for _, c := range clusters {
		if !have.Contains(c.Name) {
			out = append(out, c)
		} else {
			cb.req.Push.AddMetric(model.DuplicatedClusters, c.Name, cb.proxyID,
				fmt.Sprintf("Duplicate cluster %s found while pushing CDS", c.Name))
		}
		have.Insert(c.Name)
	}
	return out
}

// getAllCachedSubsetClusters either fetches all cached clusters for a given key (there may be multiple due to subsets)
// and returns them along with allFound=True, or returns allFound=False indicating a cache miss. In either case,
// the cache tokens are returned to allow future writes to the cache.
// This code will only trigger a cache hit if all subset clusters are present. This simplifies the code a bit,
// as the non-subset and subset cluster generation are tightly coupled, in exchange for a likely trivial cache hit rate impact.
func (cb *ClusterBuilder) getAllCachedSubsetClusters(clusterKey clusterCache) ([]*discovery.Resource, bool) {
	if !features.EnableCDSCaching {
		return nil, false
	}
	destinationRule := CastDestinationRule(clusterKey.destinationRule)
	res := make([]*discovery.Resource, 0, 1+len(destinationRule.GetSubsets()))
	cachedCluster, f := cb.cache.Get(&clusterKey)
	allFound := f
	res = append(res, cachedCluster)
	dir, _, host, port := model.ParseSubsetKey(clusterKey.clusterName)
	for _, ss := range destinationRule.GetSubsets() {
		clusterKey.clusterName = model.BuildSubsetKey(dir, ss.Name, host, port)
		cachedCluster, f := cb.cache.Get(&clusterKey)
		if !f {
			allFound = false
		}
		res = append(res, cachedCluster)
	}
	return res, allFound
}

// build does any final build operations needed, like marshaling etc.
func (mc *MutableCluster) build() *cluster.Cluster {
	if mc == nil {
		return nil
	}
	// Marshall Http Protocol options if they exist.
	if mc.httpProtocolOptions != nil {
		// UpstreamProtocolOptions is required field in Envoy. If we have not set this option earlier
		// we need to set it to default http protocol options.
		if mc.httpProtocolOptions.UpstreamProtocolOptions == nil {
			mc.httpProtocolOptions.UpstreamProtocolOptions = &http.HttpProtocolOptions_ExplicitHttpConfig_{
				ExplicitHttpConfig: &http.HttpProtocolOptions_ExplicitHttpConfig{
					ProtocolConfig: &http.HttpProtocolOptions_ExplicitHttpConfig_HttpProtocolOptions{},
				},
			}
		}
		mc.cluster.TypedExtensionProtocolOptions = map[string]*any.Any{
			v3.HttpProtocolOptionsType: util.MessageToAny(mc.httpProtocolOptions),
		}
	}
	return mc.cluster
}

// CastDestinationRule returns the destination rule enclosed by the config, if not null.
// Otherwise, return nil.
func CastDestinationRule(config *config.Config) *networking.DestinationRule {
	if config != nil {
		return config.Spec.(*networking.DestinationRule)
	}

	return nil
}

// maybeApplyEdsConfig applies EdsClusterConfig on the passed in cluster if it is an EDS type of cluster.
func maybeApplyEdsConfig(c *cluster.Cluster) {
	if c.GetType() != cluster.Cluster_EDS {
		return
	}

	c.EdsClusterConfig = &cluster.Cluster_EdsClusterConfig{
		ServiceName: c.Name,
		EdsConfig: &core.ConfigSource{
			ConfigSourceSpecifier: &core.ConfigSource_Ads{
				Ads: &core.AggregatedConfigSource{},
			},
			InitialFetchTimeout: durationpb.New(0),
			ResourceApiVersion:  core.ApiVersion_V3,
		},
	}
}

func defaultUpstreamCommonTLSContext() *auth.CommonTlsContext {
	return &auth.CommonTlsContext{
		TlsParams: &auth.TlsParameters{
			// if not specified, envoy use TLSv1_2 as default for client.
			TlsMaximumProtocolVersion: auth.TlsParameters_TLSv1_3,
			TlsMinimumProtocolVersion: auth.TlsParameters_TLSv1_2,
		},
	}
}

// defaultTransportSocketMatch applies to endpoints that have no security.istio.io/tlsMode label
// or those whose label value does not match "istio"
func defaultTransportSocketMatch() *cluster.Cluster_TransportSocketMatch {
	return &cluster.Cluster_TransportSocketMatch{
		Name:            "tlsMode-disabled",
		Match:           &structpb.Struct{},
		TransportSocket: xdsfilters.RawBufferTransportSocket,
	}
}
