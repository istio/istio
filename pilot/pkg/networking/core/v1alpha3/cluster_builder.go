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
	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/ptypes/wrappers"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/util/gogo"
)

var defaultDestinationRule = networking.DestinationRule{}

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

// applyDestinationRule applies the destination rule if it exists for the Service. It returns the subset clusters if any created as it
// applies the destination rule.
func (cb *ClusterBuilder) applyDestinationRule(c *cluster.Cluster, clusterMode ClusterMode, service *model.Service, port *model.Port,
	proxyNetworkView map[string]bool) []*cluster.Cluster {
	destRule := cb.push.DestinationRule(cb.proxy, service)
	destinationRule := castDestinationRuleOrDefault(destRule)

	opts := buildClusterOpts{
		mesh:        cb.push.Mesh,
		cluster:     c,
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
		opts.serviceMTLSMode = cb.push.BestEffortInferServiceMTLSMode(service, port)
	}

	// merge with applicable port level traffic policy settings
	opts.policy = MergeTrafficPolicy(nil, opts.policy, opts.port)
	// Apply traffic policy for the main default cluster.
	applyTrafficPolicy(opts)

	// Apply EdsConfig if needed. This should be called after traffic policy is applied because, traffic policy might change
	// discovery type.
	maybeApplyEdsConfig(c)

	var clusterMetadata *core.Metadata
	if destRule != nil {
		clusterMetadata = util.AddConfigInfoMetadata(c.Metadata, destRule.Meta)
		c.Metadata = clusterMetadata
	}
	subsetClusters := make([]*cluster.Cluster, 0)
	for _, subset := range destinationRule.Subsets {
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

		if !(isPassthrough || c.GetType() == cluster.Cluster_EDS) {
			if len(subset.Labels) != 0 {
				lbEndpoints = cb.buildLocalityLbEndpoints(proxyNetworkView, service, port.Port, []labels.Instance{subset.Labels})
			} else {
				lbEndpoints = cb.buildLocalityLbEndpoints(proxyNetworkView, service, port.Port, nil)
			}
		}
		clusterType := c.GetType()

		if isPassthrough {
			clusterType = cluster.Cluster_ORIGINAL_DST
		}

		subsetCluster := cb.buildDefaultCluster(subsetClusterName, clusterType, lbEndpoints, model.TrafficDirectionOutbound, port, service, nil)

		if subsetCluster == nil {
			continue
		}
		if len(cb.push.Mesh.OutboundClusterStatName) != 0 {
			subsetCluster.AltStatName = util.BuildStatPrefix(cb.push.Mesh.OutboundClusterStatName, string(service.Hostname), subset.Name, port, service.Attributes)
		}
		setUpstreamProtocol(cb.proxy, subsetCluster, port, model.TrafficDirectionOutbound)

		// Apply traffic policy for subset cluster with the destination rule traffic policy.
		opts.cluster = subsetCluster
		opts.istioMtlsSni = defaultSni

		// If subset has a traffic policy, apply it so that it overrides the destination rule traffic policy.
		opts.policy = MergeTrafficPolicy(destinationRule.TrafficPolicy, subset.TrafficPolicy, opts.port)
		// Apply traffic policy for the subset cluster.
		applyTrafficPolicy(opts)

		maybeApplyEdsConfig(subsetCluster)

		subsetCluster.Metadata = util.AddSubsetToMetadata(clusterMetadata, subset.Name)
		subsetClusters = append(subsetClusters, subsetCluster)
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
	port *model.Port, service *model.Service, allInstances []*model.ServiceInstance) *cluster.Cluster {
	if allInstances == nil {
		allInstances = cb.proxy.ServiceInstances
	}
	c := &cluster.Cluster{
		Name:                 name,
		ClusterDiscoveryType: &cluster.Cluster_Type{Type: discoveryType},
	}
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
		cluster:         c,
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
	applyTrafficPolicy(opts)
	addTelemetryMetadata(opts, service, direction, allInstances)
	addNetworkingMetadata(opts, service, direction)
	return c
}

// buildInboundClusterForPortOrUDS constructs a single inbound listener. The cluster will be bound to
// `inbound|clusterPort||`, and send traffic to <bind>:<instance.Endpoint.EndpointPort>. A workload
// will have a single inbound cluster per port. In general this works properly, with the exception of
// the Service-oriented DestinationRule, and upstream protocol selection. Our documentation currently
// requires a single protocol per port, and the DestinationRule issue is slated to move to Sidecar.
// Note: clusterPort and instance.Endpoint.EndpointPort are identical for standard Services; however,
// Sidecar.Ingress allows these to be different.
func (cb *ClusterBuilder) buildInboundClusterForPortOrUDS(proxy *model.Proxy, clusterPort int, bind string,
	instance *model.ServiceInstance, allInstance []*model.ServiceInstance) *cluster.Cluster {
	clusterName := util.BuildInboundSubsetKey(proxy, instance.ServicePort.Name,
		instance.Service.Hostname, instance.ServicePort.Port, clusterPort)
	localityLbEndpoints := buildInboundLocalityLbEndpoints(bind, instance.Endpoint.EndpointPort)
	localCluster := cb.buildDefaultCluster(clusterName, cluster.Cluster_STATIC, localityLbEndpoints,
		model.TrafficDirectionInbound, instance.ServicePort, instance.Service, allInstance)
	// If stat name is configured, build the alt statname.
	if len(cb.push.Mesh.InboundClusterStatName) != 0 {
		localCluster.AltStatName = util.BuildStatPrefix(cb.push.Mesh.InboundClusterStatName,
			string(instance.Service.Hostname), "", instance.ServicePort, instance.Service.Attributes)
	}
	setUpstreamProtocol(cb.proxy, localCluster, instance.ServicePort, model.TrafficDirectionInbound)

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
			applyConnectionPool(cb.push.Mesh, localCluster, connectionPool)
			util.AddConfigInfoMetadata(localCluster.Metadata, cfg.Meta)
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
			instance.Endpoint.Namespace, instance.Endpoint.Labels)
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
	applyConnectionPool(cb.push.Mesh, cluster, passthroughSettings)
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
			ResourceApiVersion:  core.ApiVersion_V3,
			InitialFetchTimeout: features.InitialFetchTimeout,
		},
	}
}
