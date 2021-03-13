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

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	auth "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	http "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/ptypes/any"
	"github.com/golang/protobuf/ptypes/duration"
	structpb "github.com/golang/protobuf/ptypes/struct"
	"github.com/golang/protobuf/ptypes/wrappers"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	authn_model "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/util/sets"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/util/gogo"
	"istio.io/pkg/log"
)

var defaultDestinationRule = networking.DestinationRule{}

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

// MutableCluster wraps Cluster object along with options.
type MutableCluster struct {
	cluster *cluster.Cluster
	// httpProtocolOptions stores the HttpProtocolOptions which will marshaled when build is called.
	httpProtocolOptions *http.HttpProtocolOptions
}

// ClusterBuilder interface provides an abstraction for building Envoy Clusters.
type ClusterBuilder struct {
	proxy *model.Proxy
	push  *model.PushContext
}

// NewClusterBuilder builds an instance of ClusterBuilder.
func NewClusterBuilder(proxy *model.Proxy, push *model.PushContext) *ClusterBuilder {
	return &ClusterBuilder{
		proxy: proxy,
		push:  push,
	}
}

// NewMutalbeCluster initializes MutableCluster with the cluser passed.
func NewMutableCluster(cluster *cluster.Cluster) *MutableCluster {
	return &MutableCluster{
		cluster: cluster,
	}
}

// applyDestinationRule applies the destination rule if it exists for the Service. It returns the subset clusters if any created as it
// applies the destination rule.
func (cb *ClusterBuilder) applyDestinationRule(mc *MutableCluster, clusterMode ClusterMode, service *model.Service, port *model.Port,
	proxyNetworkView map[string]bool) []*cluster.Cluster {
	destRule := cb.push.DestinationRule(cb.proxy, service)
	destinationRule := castDestinationRuleOrDefault(destRule)

	opts := buildClusterOpts{
		mesh:        cb.push.Mesh,
		mutable:     mc,
		policy:      destinationRule.TrafficPolicy,
		port:        port,
		clusterMode: clusterMode,
		direction:   model.TrafficDirectionOutbound,
		proxy:       cb.proxy,
	}

	if clusterMode == DefaultClusterMode {
		opts.serviceAccounts = cb.push.ServiceAccounts[service.Hostname][port.Port]
		opts.istioMtlsSni = model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, "", service.Hostname, port.Port)
		opts.simpleTLSSni = string(service.Hostname)
		opts.meshExternal = service.MeshExternal
		opts.serviceMTLSMode = cb.push.BestEffortInferServiceMTLSMode(destinationRule.GetTrafficPolicy(), service, port)
	}

	// merge with applicable port level traffic policy settings
	opts.policy = MergeTrafficPolicy(nil, opts.policy, opts.port)
	// Apply traffic policy for the main default cluster.
	cb.applyTrafficPolicy(opts)

	// Apply EdsConfig if needed. This should be called after traffic policy is applied because, traffic policy might change
	// discovery type.
	maybeApplyEdsConfig(mc.cluster)

	var clusterMetadata *core.Metadata
	if destRule != nil {
		clusterMetadata = util.AddConfigInfoMetadata(mc.cluster.Metadata, destRule.Meta)
		mc.cluster.Metadata = clusterMetadata
	}
	subsetClusters := make([]*cluster.Cluster, 0)
	for _, subset := range destinationRule.Subsets {
		opts.serviceMTLSMode = cb.push.BestEffortInferServiceMTLSMode(subset.GetTrafficPolicy(), service, port)
		var subsetClusterName string
		var defaultSni string
		if clusterMode == DefaultClusterMode {
			subsetClusterName = model.BuildSubsetKey(model.TrafficDirectionOutbound, subset.Name, service.Hostname, port.Port)
			defaultSni = model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, subset.Name, service.Hostname, port.Port)
		} else {
			subsetClusterName = model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, subset.Name, service.Hostname, port.Port)
		}
		// clusters with discovery type STATIC, STRICT_DNS rely on cluster.LoadAssignment field.
		// ServiceEntry's need to filter hosts based on subset.labels in order to perform weighted routing
		var lbEndpoints []*endpoint.LocalityLbEndpoints

		isPassthrough := subset.GetTrafficPolicy().GetLoadBalancer().GetSimple() == networking.LoadBalancerSettings_PASSTHROUGH

		if !(isPassthrough || mc.cluster.GetType() == cluster.Cluster_EDS) {
			if len(subset.Labels) != 0 {
				lbEndpoints = cb.buildLocalityLbEndpoints(proxyNetworkView, service, port.Port, []labels.Instance{subset.Labels})
			} else {
				lbEndpoints = cb.buildLocalityLbEndpoints(proxyNetworkView, service, port.Port, nil)
			}
		}
		clusterType := mc.cluster.GetType()

		if isPassthrough {
			clusterType = cluster.Cluster_ORIGINAL_DST
		}

		subsetCluster := cb.buildDefaultCluster(subsetClusterName, clusterType, lbEndpoints, model.TrafficDirectionOutbound, port, service, nil)

		if subsetCluster == nil {
			continue
		}
		if len(cb.push.Mesh.OutboundClusterStatName) != 0 {
			subsetCluster.cluster.AltStatName = util.BuildStatPrefix(cb.push.Mesh.OutboundClusterStatName,
				string(service.Hostname), subset.Name, port, service.Attributes)
		}
		cb.setUpstreamProtocol(cb.proxy, subsetCluster, port, model.TrafficDirectionOutbound)

		// Apply traffic policy for subset cluster with the destination rule traffic policy.
		opts.mutable = subsetCluster
		opts.istioMtlsSni = defaultSni

		// If subset has a traffic policy, apply it so that it overrides the destination rule traffic policy.
		opts.policy = MergeTrafficPolicy(destinationRule.TrafficPolicy, subset.TrafficPolicy, opts.port)
		// Apply traffic policy for the subset cluster.
		cb.applyTrafficPolicy(opts)

		maybeApplyEdsConfig(subsetCluster.cluster)

		// Add the DestinationRule+subsets metadata. Metadata here is generated on a per-cluster
		// basis in buildDefaultCluster, so we can just insert without a copy.
		subsetCluster.cluster.Metadata = util.AddConfigInfoMetadata(subsetCluster.cluster.Metadata, destRule.Meta)
		util.AddSubsetToMetadata(subsetCluster.cluster.Metadata, subset.Name)
		subsetClusters = append(subsetClusters, subsetCluster.build())
	}
	return subsetClusters
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
	if port != nil && len(subsetPolicy.PortLevelSettings) > 0 {
		for _, p := range subsetPolicy.PortLevelSettings {
			if p.Port != nil && uint32(port.Port) == p.Port.Number {
				// per the docs, port level policies do not inherit and intead to defaults if not provided
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
	port *model.Port, service *model.Service, allInstances []*model.ServiceInstance) *MutableCluster {
	if allInstances == nil {
		allInstances = cb.proxy.ServiceInstances
	}
	c := &cluster.Cluster{
		Name:                 name,
		ClusterDiscoveryType: &cluster.Cluster_Type{Type: discoveryType},
	}
	ec := NewMutableCluster(c)
	switch discoveryType {
	case cluster.Cluster_STRICT_DNS:
		c.DnsLookupFamily = cluster.Cluster_V4_ONLY
		dnsRate := gogo.DurationToProtoDuration(cb.push.Mesh.DnsRefreshRate)
		c.DnsRefreshRate = dnsRate
		c.RespectDnsTtl = true
		fallthrough
	case cluster.Cluster_STATIC:
		if len(localityLbEndpoints) == 0 {
			cb.push.AddMetric(model.DNSNoEndpointClusters, c.Name, cb.proxy.ID,
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
		mesh:            cb.push.Mesh,
		mutable:         ec,
		policy:          cb.defaultTrafficPolicy(discoveryType),
		port:            port,
		serviceAccounts: nil,
		istioMtlsSni:    "",
		clusterMode:     DefaultClusterMode,
		direction:       direction,
		proxy:           cb.proxy,
	}
	// decides whether the cluster corresponds to a service external to mesh or not.
	if direction == model.TrafficDirectionInbound {
		// Inbound cluster always corresponds to service in the mesh.
		opts.meshExternal = false
	} else if service != nil {
		// otherwise, read this information from service object.
		opts.meshExternal = service.MeshExternal
	}
	cb.applyTrafficPolicy(opts)
	addTelemetryMetadata(opts, service, direction, allInstances)
	addNetworkingMetadata(opts, service, direction)
	return ec
}

// buildInboundClusterForPortOrUDS constructs a single inbound listener. The cluster will be bound to
// `inbound|clusterPort||`, and send traffic to <bind>:<instance.Endpoint.EndpointPort>. A workload
// will have a single inbound cluster per port. In general this works properly, with the exception of
// the Service-oriented DestinationRule, and upstream protocol selection. Our documentation currently
// requires a single protocol per port, and the DestinationRule issue is slated to move to Sidecar.
// Note: clusterPort and instance.Endpoint.EndpointPort are identical for standard Services; however,
// Sidecar.Ingress allows these to be different.
func (cb *ClusterBuilder) buildInboundClusterForPortOrUDS(clusterPort int, bind string,
	instance *model.ServiceInstance, allInstance []*model.ServiceInstance) *MutableCluster {
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
		localCluster.cluster.CleanupInterval = &duration.Duration{Seconds: 60}
	}
	// If stat name is configured, build the alt statname.
	if len(cb.push.Mesh.InboundClusterStatName) != 0 {
		localCluster.cluster.AltStatName = util.BuildStatPrefix(cb.push.Mesh.InboundClusterStatName,
			string(instance.Service.Hostname), "", instance.ServicePort, instance.Service.Attributes)
	}
	cb.setUpstreamProtocol(cb.proxy, localCluster, instance.ServicePort, model.TrafficDirectionInbound)

	// When users specify circuit breakers, they need to be set on the receiver end
	// (server side) as well as client side, so that the server has enough capacity
	// (not the defaults) to handle the increased traffic volume
	// TODO: This is not foolproof - if instance is part of multiple services listening on same port,
	// choice of inbound cluster is arbitrary. So the connection pool settings may not apply cleanly.
	cfg := cb.push.DestinationRule(cb.proxy, instance.Service)
	if cfg != nil {
		destinationRule := cfg.Spec.(*networking.DestinationRule)
		if destinationRule.TrafficPolicy != nil {
			connectionPool, _, _, _ := selectTrafficPolicyComponents(MergeTrafficPolicy(nil, destinationRule.TrafficPolicy, instance.ServicePort))
			// only connection pool settings make sense on the inbound path.
			// upstream TLS settings/outlier detection/load balancer don't apply here.
			cb.applyConnectionPool(cb.push.Mesh, localCluster, connectionPool)
			util.AddConfigInfoMetadata(localCluster.cluster.Metadata, cfg.Meta)
		}
	}
	if bind != LocalhostAddress && bind != LocalhostIPv6Address {
		// iptables will redirect our own traffic to localhost back to us if we do not use the "magic" upstream bind
		// config which will be skipped.
		localCluster.cluster.UpstreamBindConfig = &core.BindConfig{
			SourceAddress: &core.SocketAddress{
				Address: getPassthroughBindIP(cb.proxy),
				PortSpecifier: &core.SocketAddress_PortValue{
					PortValue: uint32(0),
				},
			},
		}
	}
	return localCluster
}

func (cb *ClusterBuilder) buildLocalityLbEndpoints(proxyNetworkView map[string]bool, service *model.Service,
	port int, labels labels.Collection) []*endpoint.LocalityLbEndpoints {
	if service.Resolution != model.DNSLB {
		return nil
	}

	instances := cb.push.ServiceInstancesByPort(service, port, labels)

	// Determine whether or not the target service is considered local to the cluster
	// and should, therefore, not be accessed from outside the cluster.
	isClusterLocal := cb.push.IsClusterLocal(service)

	lbEndpoints := make(map[string][]*endpoint.LbEndpoint)
	for _, instance := range instances {
		// Only send endpoints from the networks in the network view requested by the proxy.
		// The default network view assigned to the Proxy is nil, in that case match any network.
		if proxyNetworkView != nil && !proxyNetworkView[instance.Endpoint.Network] {
			// Endpoint's network doesn't match the set of networks that the proxy wants to see.
			continue
		}
		// If the downstream service is configured as cluster-local, only include endpoints that
		// reside in the same cluster.
		if isClusterLocal && (cb.proxy.Metadata.ClusterID != instance.Endpoint.Locality.ClusterID) {
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
				Value: 1,
			},
		}
		if instance.Endpoint.LbWeight > 0 {
			ep.LoadBalancingWeight.Value = instance.Endpoint.LbWeight
		}
		ep.Metadata = util.BuildLbEndpointMetadata(instance.Endpoint.Network, instance.Endpoint.TLSMode, instance.Endpoint.WorkloadName,
			instance.Endpoint.Namespace, instance.Endpoint.Locality.ClusterID, instance.Endpoint.Labels)
		locality := instance.Endpoint.Locality.Label
		lbEndpoints[locality] = append(lbEndpoints[locality], ep)
	}

	localityLbEndpoints := make([]*endpoint.LocalityLbEndpoints, 0, len(lbEndpoints))

	for locality, eps := range lbEndpoints {
		var weight uint32
		for _, ep := range eps {
			weight += ep.LoadBalancingWeight.GetValue()
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

// buildInboundPassthroughClusters builds passthrough clusters for inbound.
func (cb *ClusterBuilder) buildInboundPassthroughClusters() []*cluster.Cluster {
	// ipv4 and ipv6 feature detection. Envoy cannot ignore a config where the ip version is not supported
	clusters := make([]*cluster.Cluster, 0, 2)
	if cb.proxy.SupportsIPv4() {
		inboundPassthroughClusterIpv4 := cb.buildDefaultPassthroughCluster()
		inboundPassthroughClusterIpv4.Name = util.InboundPassthroughClusterIpv4
		inboundPassthroughClusterIpv4.UpstreamBindConfig = &core.BindConfig{
			SourceAddress: &core.SocketAddress{
				Address: util.InboundPassthroughBindIpv4,
				PortSpecifier: &core.SocketAddress_PortValue{
					PortValue: uint32(0),
				},
			},
		}
		clusters = append(clusters, inboundPassthroughClusterIpv4)
	}
	if cb.proxy.SupportsIPv6() {
		inboundPassthroughClusterIpv6 := cb.buildDefaultPassthroughCluster()
		inboundPassthroughClusterIpv6.Name = util.InboundPassthroughClusterIpv6
		inboundPassthroughClusterIpv6.UpstreamBindConfig = &core.BindConfig{
			SourceAddress: &core.SocketAddress{
				Address: util.InboundPassthroughBindIpv6,
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
		ConnectTimeout:       gogo.DurationToProtoDuration(cb.push.Mesh.ConnectTimeout),
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
		ConnectTimeout:       gogo.DurationToProtoDuration(cb.push.Mesh.ConnectTimeout),
		LbPolicy:             cluster.Cluster_CLUSTER_PROVIDED,
		ProtocolSelection:    cluster.Cluster_USE_DOWNSTREAM_PROTOCOL,
	}
	passthroughSettings := &networking.ConnectionPoolSettings{}
	cb.applyConnectionPool(cb.push.Mesh, NewMutableCluster(cluster), passthroughSettings)
	return cluster
}

// defaultTrafficPolicy builds a default traffic policy applying default connection timeouts.
func (cb *ClusterBuilder) defaultTrafficPolicy(discoveryType cluster.Cluster_DiscoveryType) *networking.TrafficPolicy {
	lbPolicy := DefaultLbType
	if discoveryType == cluster.Cluster_ORIGINAL_DST {
		lbPolicy = networking.LoadBalancerSettings_PASSTHROUGH
	}
	return &networking.TrafficPolicy{
		LoadBalancer: &networking.LoadBalancerSettings{
			LbPolicy: &networking.LoadBalancerSettings_Simple{
				Simple: lbPolicy,
			},
		},
		ConnectionPool: &networking.ConnectionPoolSettings{
			Tcp: &networking.ConnectionPoolSettings_TCPSettings{
				ConnectTimeout: &types.Duration{
					Seconds: cb.push.Mesh.ConnectTimeout.Seconds,
					Nanos:   cb.push.Mesh.ConnectTimeout.Nanos,
				},
			},
		},
	}
}

// applyH2Upgrade function will upgrade outbound cluster to http2 if specified by configuration.
func (cb *ClusterBuilder) applyH2Upgrade(opts buildClusterOpts, connectionPool *networking.ConnectionPoolSettings) {
	if cb.shouldH2Upgrade(opts.mutable.cluster.Name, opts.direction, opts.port, opts.mesh, connectionPool) {
		cb.setH2Options(opts.mutable)
	}
}

// shouldH2Upgrade function returns true if the cluster  should be upgraded to http2.
func (cb *ClusterBuilder) shouldH2Upgrade(clusterName string, direction model.TrafficDirection, port *model.Port, mesh *meshconfig.MeshConfig,
	connectionPool *networking.ConnectionPoolSettings) bool {
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

	// Do not upgrade non-http ports
	// This also ensures that we are only upgrading named ports so that
	// EnableProtocolSniffingForInbound does not interfere.
	// protocol sniffing uses Cluster_USE_DOWNSTREAM_PROTOCOL.
	// Therefore if the client upgrades connection to http2, the server will send h2 stream to the application,
	// even though the application only supports http 1.1.
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
	if opts.direction == model.TrafficDirectionOutbound && connectionPool != nil && connectionPool.Http != nil && connectionPool.Http.UseClientProtocol {
		// Use downstream protocol. If the incoming traffic use HTTP 1.1, the
		// upstream cluster will use HTTP 1.1, if incoming traffic use HTTP2,
		// the upstream cluster will use HTTP2.
		cb.setUseDownstreamProtocol(opts.mutable)
	}
	// Connection pool settings are applicable for both inbound and outbound clusters.
	cb.applyConnectionPool(opts.mesh, opts.mutable, connectionPool)
	if opts.direction != model.TrafficDirectionInbound {
		cb.applyH2Upgrade(opts, connectionPool)
		applyOutlierDetection(opts.mutable.cluster, outlierDetection)
		applyLoadBalancer(opts.mutable.cluster, loadBalancer, opts.port, opts.proxy, opts.mesh)
	}
	if opts.mutable.cluster.GetType() == cluster.Cluster_ORIGINAL_DST {
		opts.mutable.cluster.LbPolicy = cluster.Cluster_CLUSTER_PROVIDED
	}

	if opts.clusterMode != SniDnatClusterMode && opts.direction != model.TrafficDirectionInbound {
		autoMTLSEnabled := opts.mesh.GetEnableAutoMtls().Value
		var mtlsCtxType mtlsContextType
		tls, mtlsCtxType = buildAutoMtlsSettings(tls, opts.serviceAccounts, opts.istioMtlsSni, opts.proxy,
			autoMTLSEnabled, opts.meshExternal, opts.serviceMTLSMode)
		cb.applyUpstreamTLSSettings(&opts, tls, mtlsCtxType)
	}
}

// FIXME: there isn't a way to distinguish between unset values and zero values
func (cb *ClusterBuilder) applyConnectionPool(mesh *meshconfig.MeshConfig, mc *MutableCluster, settings *networking.ConnectionPoolSettings) {
	if settings == nil {
		return
	}

	threshold := getDefaultCircuitBreakerThresholds()
	var idleTimeout *types.Duration

	if settings.Http != nil {
		if settings.Http.Http2MaxRequests > 0 {
			// Envoy only applies MaxRequests in HTTP/2 clusters
			threshold.MaxRequests = &wrappers.UInt32Value{Value: uint32(settings.Http.Http2MaxRequests)}
		}
		if settings.Http.Http1MaxPendingRequests > 0 {
			// Envoy only applies MaxPendingRequests in HTTP/1.1 clusters
			threshold.MaxPendingRequests = &wrappers.UInt32Value{Value: uint32(settings.Http.Http1MaxPendingRequests)}
		}

		if settings.Http.MaxRequestsPerConnection > 0 {
			mc.cluster.MaxRequestsPerConnection = &wrappers.UInt32Value{Value: uint32(settings.Http.MaxRequestsPerConnection)}
		}

		// FIXME: zero is a valid value if explicitly set, otherwise we want to use the default
		if settings.Http.MaxRetries > 0 {
			threshold.MaxRetries = &wrappers.UInt32Value{Value: uint32(settings.Http.MaxRetries)}
		}

		idleTimeout = settings.Http.IdleTimeout
	}

	if settings.Tcp != nil {
		if settings.Tcp.ConnectTimeout != nil {
			mc.cluster.ConnectTimeout = gogo.DurationToProtoDuration(settings.Tcp.ConnectTimeout)
		}

		if settings.Tcp.MaxConnections > 0 {
			threshold.MaxConnections = &wrappers.UInt32Value{Value: uint32(settings.Tcp.MaxConnections)}
		}

		applyTCPKeepalive(mesh, mc.cluster, settings)
	}

	mc.cluster.CircuitBreakers = &cluster.CircuitBreakers{
		Thresholds: []*cluster.CircuitBreakers_Thresholds{threshold},
	}

	if idleTimeout != nil {
		idleTimeoutDuration := gogo.DurationToProtoDuration(idleTimeout)
		if mc.httpProtocolOptions == nil {
			mc.httpProtocolOptions = &http.HttpProtocolOptions{}
		}
		commonOptions := mc.httpProtocolOptions
		commonOptions.CommonHttpProtocolOptions = &core.HttpProtocolOptions{
			IdleTimeout: idleTimeoutDuration,
		}
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
			Name:       util.EnvoyTLSSocketName,
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
				defaultTransportSocketMatch,
			}
		}
	}
}

func (cb *ClusterBuilder) buildUpstreamClusterTLSContext(opts *buildClusterOpts, tls *networking.ClientTLSSettings) (*auth.UpstreamTlsContext, error) {
	c := opts.mutable
	proxy := opts.proxy

	// Hack to avoid egress sds cluster config generation for sidecar when
	// CredentialName is set in DestinationRule
	if tls.CredentialName != "" && proxy.Type == model.SidecarProxy {
		if tls.Mode == networking.ClientTLSSettings_SIMPLE || tls.Mode == networking.ClientTLSSettings_MUTUAL {
			return nil, nil
		}
	}

	var tlsContext *auth.UpstreamTlsContext

	switch tls.Mode {
	case networking.ClientTLSSettings_DISABLE:
		tlsContext = nil
	case networking.ClientTLSSettings_ISTIO_MUTUAL:

		tlsContext = &auth.UpstreamTlsContext{
			CommonTlsContext: &auth.CommonTlsContext{},
			Sni:              tls.Sni,
		}

		// For ISTIO_MUTUAL_TLS we serve certificates from two well known locations.
		// - Proxy Metadata: These are certs being mounted from within the pod. Rather
		//   than reading directly in Envoy, which does not support rotation, we will
		//   serve them over SDS by reading the files.
		// - Default SDS Locations served with resource names "default" and "ROOTCA".

		// buildIstioMutualTLS would have populated the TLSSettings with proxy metadata.
		// Use it if it exists otherwise fallback to default SDS locations.
		metadataSDS := model.SdsCertificateConfig{
			CertificatePath:   tls.ClientCertificate,
			PrivateKeyPath:    tls.PrivateKey,
			CaCertificatePath: tls.CaCertificates,
		}
		metadataCerts := metadataSDS.IsRootCertificate() && metadataSDS.IsKeyCertificate()

		tlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs = append(tlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs,
			authn_model.ConstructSdsSecretConfig(model.GetOrDefault(metadataSDS.GetResourceName(), authn_model.SDSDefaultResourceName), proxy))

		tlsContext.CommonTlsContext.ValidationContextType = &auth.CommonTlsContext_CombinedValidationContext{
			CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
				DefaultValidationContext: &auth.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch(tls.SubjectAltNames)},
				ValidationContextSdsSecretConfig: authn_model.ConstructSdsSecretConfig(model.GetOrDefault(metadataSDS.GetRootResourceName(),
					authn_model.SDSRootResourceName), proxy),
			},
		}
		// Set default SNI of cluster name for istio_mutual if sni is not set.
		if len(tls.Sni) == 0 {
			tlsContext.Sni = c.cluster.Name
		}

		// `istio-peer-exchange` alpn is only used when using mtls communication between peers.
		// We add `istio-peer-exchange` to the list of alpn strings.
		// The code has repeated snippets because We want to use predefined alpn strings for efficiency.
		switch {
		case metadataCerts:
			if cb.IsHttp2Cluster(c) {
				// This is HTTP/2 cluster, advertise it with ALPN.
				tlsContext.CommonTlsContext.AlpnProtocols = util.ALPNH2Only
			}
		default:
			if cb.IsHttp2Cluster(c) {
				// This is HTTP/2 in-mesh cluster, advertise it with ALPN.
				tlsContext.CommonTlsContext.AlpnProtocols = util.ALPNInMeshH2WithMxc
			} else {
				// This is in-mesh cluster, advertise it with ALPN.
				tlsContext.CommonTlsContext.AlpnProtocols = util.ALPNInMeshWithMxc
			}
		}
	case networking.ClientTLSSettings_SIMPLE:
		tlsContext = &auth.UpstreamTlsContext{
			CommonTlsContext: &auth.CommonTlsContext{},
			Sni:              tls.Sni,
		}

		if tls.CredentialName != "" {
			tlsContext = &auth.UpstreamTlsContext{
				CommonTlsContext: &auth.CommonTlsContext{},
				Sni:              tls.Sni,
			}
			// If  credential name is specified at Destination Rule config and originating node is egress gateway, create
			// SDS config for egress gateway to fetch key/cert at gateway agent.
			authn_model.ApplyCustomSDSToClientCommonTLSContext(tlsContext.CommonTlsContext, tls)
		} else {
			// If CredentialName is not set fallback to files specified in DR.
			res := model.SdsCertificateConfig{
				CaCertificatePath: model.GetOrDefault(proxy.Metadata.TLSClientRootCert, tls.CaCertificates),
			}

			// If tls.CaCertificate or CaCertificate in Metadata isn't configured don't set up SdsSecretConfig
			if !res.IsRootCertificate() {
				tlsContext.CommonTlsContext.ValidationContextType = &auth.CommonTlsContext_ValidationContext{}
			} else {
				tlsContext.CommonTlsContext.ValidationContextType = &auth.CommonTlsContext_CombinedValidationContext{
					CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
						DefaultValidationContext:         &auth.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch(tls.SubjectAltNames)},
						ValidationContextSdsSecretConfig: authn_model.ConstructSdsSecretConfig(res.GetRootResourceName(), proxy),
					},
				}
			}
		}

		if cb.IsHttp2Cluster(c) {
			// This is HTTP/2 cluster, advertise it with ALPN.
			tlsContext.CommonTlsContext.AlpnProtocols = util.ALPNH2Only
		}

	case networking.ClientTLSSettings_MUTUAL:
		tlsContext = &auth.UpstreamTlsContext{
			CommonTlsContext: &auth.CommonTlsContext{},
			Sni:              tls.Sni,
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

			var res model.SdsCertificateConfig
			if features.AllowMetadataCertsInMutualTLS {
				// These are certs being mounted from within the pod and specified in Metadata.
				// Rather than reading directly in Envoy, which does not support rotation, we will
				// serve them over SDS by reading the files. This is only enabled for temporary migration.
				res = model.SdsCertificateConfig{
					CertificatePath:   model.GetOrDefault(proxy.Metadata.TLSClientCertChain, tls.ClientCertificate),
					PrivateKeyPath:    model.GetOrDefault(proxy.Metadata.TLSClientKey, tls.PrivateKey),
					CaCertificatePath: model.GetOrDefault(proxy.Metadata.TLSClientRootCert, tls.CaCertificates),
				}
			} else {
				// These are certs being mounted from within the pod and specified in Destination Rules.
				// Rather than reading directly in Envoy, which does not support rotation, we will
				// serve them over SDS by reading the files.
				res = model.SdsCertificateConfig{
					CertificatePath:   tls.ClientCertificate,
					PrivateKeyPath:    tls.PrivateKey,
					CaCertificatePath: tls.CaCertificates,
				}
			}
			tlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs = append(tlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs,
				authn_model.ConstructSdsSecretConfig(res.GetResourceName(), proxy))

			// If tls.CaCertificate or CaCertificate in Metadata isn't configured don't set up RootSdsSecretConfig
			if !res.IsRootCertificate() {
				tlsContext.CommonTlsContext.ValidationContextType = &auth.CommonTlsContext_ValidationContext{}
			} else {
				tlsContext.CommonTlsContext.ValidationContextType = &auth.CommonTlsContext_CombinedValidationContext{
					CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
						DefaultValidationContext:         &auth.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch(tls.SubjectAltNames)},
						ValidationContextSdsSecretConfig: authn_model.ConstructSdsSecretConfig(res.GetRootResourceName(), proxy),
					},
				}
			}
		}

		if cb.IsHttp2Cluster(c) {
			// This is HTTP/2 cluster, advertise it with ALPN.
			tlsContext.CommonTlsContext.AlpnProtocols = util.ALPNH2Only
		}
	}
	return tlsContext, nil
}

func (cb *ClusterBuilder) setUseDownstreamProtocol(mc *MutableCluster) {
	if mc.httpProtocolOptions == nil {
		mc.httpProtocolOptions = &http.HttpProtocolOptions{}
	}
	options := mc.httpProtocolOptions
	options.UpstreamProtocolOptions = &http.HttpProtocolOptions_UseDownstreamProtocolConfig{
		UseDownstreamProtocolConfig: &http.HttpProtocolOptions_UseDownstreamHttpConfig{
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
func (cb *ClusterBuilder) IsHttp2Cluster(mc *MutableCluster) bool {
	options := mc.httpProtocolOptions
	return options != nil && (options.GetExplicitHttpConfig().GetHttp2ProtocolOptions() != nil ||
		options.GetUseDownstreamProtocolConfig().Http2ProtocolOptions != nil)
}

func (cb *ClusterBuilder) setUpstreamProtocol(node *model.Proxy, mc *MutableCluster, port *model.Port, direction model.TrafficDirection) {
	// Add use_downstream_protocol for sidecar proxy only if protocol sniffing is enabled.
	// Since protocol detection is disabled for gateway and use_downstream_protocol is used
	// under protocol detection for cluster to select upstream connection protocol when
	// the service port is unnamed. use_downstream_protocol should be disabled for gateway.
	if node.Type == model.SidecarProxy && ((util.IsProtocolSniffingEnabledForInboundPort(port) && direction == model.TrafficDirectionInbound) ||
		(util.IsProtocolSniffingEnabledForOutboundPort(port) && direction == model.TrafficDirectionOutbound)) {
		// Use downstream protocol. If the incoming traffic use HTTP 1.1, the
		// upstream cluster will use HTTP 1.1, if incoming traffic use HTTP2,
		// the upstream cluster will use HTTP2.
		cb.setUseDownstreamProtocol(mc)
	} else if port.Protocol.IsHTTP2() {
		cb.setH2Options(mc)
	}
}

// finalizeClusters does any final cluster field marshaling. This should be called
// at the end before adding the cluster to list of clusters.
func (cb *ClusterBuilder) normalizeClusters(clusters []*cluster.Cluster) []*cluster.Cluster {
	// resolve cluster name conflicts. there can be duplicate cluster names if there are conflicting service definitions.
	// for any clusters that share the same name the first cluster is kept and the others are discarded.
	have := sets.Set{}
	out := make([]*cluster.Cluster, 0, len(clusters))
	for _, c := range clusters {
		if !have.Contains(c.Name) {
			out = append(out, c)
		} else {
			cb.push.AddMetric(model.DuplicatedClusters, c.Name, cb.proxy.ID,
				fmt.Sprintf("Duplicate cluster %s found while pushing CDS", c.Name))
		}
		have.Insert(c.Name)
	}
	return out
}

// build does any final build operations needed, like marshaling etc.
func (mc *MutableCluster) build() *cluster.Cluster {
	if mc == nil {
		return nil
	}
	// Marshall Http Protocol options if they exist.
	if mc.httpProtocolOptions != nil {
		mc.cluster.TypedExtensionProtocolOptions = map[string]*any.Any{
			v3.HttpProtocolOptionsType: util.MessageToAny(mc.httpProtocolOptions),
		}
	}
	return mc.cluster
}

// castDestinationRuleOrDefault returns the destination rule enclosed by the config, if not null.
// Otherwise, return default (empty) DR.
func castDestinationRuleOrDefault(config *config.Config) *networking.DestinationRule {
	if config != nil {
		return config.Spec.(*networking.DestinationRule)
	}

	return &defaultDestinationRule
}

// maybeApplyEdsConfig applies EdsClusterConfig on the passed in cluster if it is an EDS type of cluster.
func maybeApplyEdsConfig(c *cluster.Cluster) {
	switch v := c.ClusterDiscoveryType.(type) {
	case *cluster.Cluster_Type:
		if v.Type != cluster.Cluster_EDS {
			return
		}
	}
	c.EdsClusterConfig = &cluster.Cluster_EdsClusterConfig{
		ServiceName: c.Name,
		EdsConfig: &core.ConfigSource{
			ConfigSourceSpecifier: &core.ConfigSource_Ads{
				Ads: &core.AggregatedConfigSource{},
			},
			ResourceApiVersion: core.ApiVersion_V3,
		},
	}
}
