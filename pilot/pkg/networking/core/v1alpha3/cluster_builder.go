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
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/util/gogo"
)

var (
	defaultDestinationRule = networking.DestinationRule{}
)

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
		push:        cb.push,
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

	// Apply traffic policy for the main default cluster.
	applyTrafficPolicy(opts)

	// Apply EdsConfig if needed. This should be called after traffic policy is applied because, traffic policy might change
	// discovery type.
	maybeApplyEdsConfig(c, cb.proxy.RequestedTypes.CDS)

	var clusterMetadata *core.Metadata
	if destRule != nil {
		clusterMetadata = util.BuildConfigInfoMetadata(destRule.ConfigMeta)
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
		if c.GetType() != cluster.Cluster_EDS {
			if len(subset.Labels) != 0 {
				lbEndpoints = cb.buildLocalityLbEndpoints(proxyNetworkView, service, port.Port, []labels.Instance{subset.Labels})
			} else {
				lbEndpoints = cb.buildLocalityLbEndpoints(proxyNetworkView, service, port.Port, nil)
			}
		}

		subsetCluster := cb.buildDefaultCluster(subsetClusterName, c.GetType(), lbEndpoints,
			model.TrafficDirectionOutbound, nil, service.MeshExternal)

		if subsetCluster == nil {
			continue
		}
		if len(cb.push.Mesh.OutboundClusterStatName) != 0 {
			subsetCluster.AltStatName = util.BuildStatPrefix(cb.push.Mesh.OutboundClusterStatName, string(service.Hostname), subset.Name, port, service.Attributes)
		}
		setUpstreamProtocol(cb.proxy, subsetCluster, port, model.TrafficDirectionOutbound)

		// Apply traffic policy for subset cluster with the destination rule traffice policy.
		opts.cluster = subsetCluster
		opts.policy = destinationRule.TrafficPolicy
		opts.istioMtlsSni = defaultSni
		applyTrafficPolicy(opts)

		// If subset has a traffic policy, apply it so that it overrides the destination rule traffic policy.
		if subset.TrafficPolicy != nil {
			opts.policy = subset.TrafficPolicy
			applyTrafficPolicy(opts)
		}

		maybeApplyEdsConfig(subsetCluster, cb.proxy.RequestedTypes.CDS)

		subsetCluster.Metadata = util.AddSubsetToMetadata(clusterMetadata, subset.Name)
		subsetClusters = append(subsetClusters, subsetCluster)
	}
	return subsetClusters
}

// buildDefaultCluster builds the default cluster and also applies default traffic policy.
func (cb *ClusterBuilder) buildDefaultCluster(name string, discoveryType cluster.Cluster_DiscoveryType,
	localityLbEndpoints []*endpoint.LocalityLbEndpoints, direction model.TrafficDirection,
	port *model.Port, meshExternal bool) *cluster.Cluster {
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
			cb.push.AddMetric(model.DNSNoEndpointClusters, c.Name, cb.proxy,
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
		push:            cb.push,
		cluster:         c,
		policy:          cb.defaultTrafficPolicy(discoveryType),
		port:            port,
		serviceAccounts: nil,
		istioMtlsSni:    "",
		clusterMode:     DefaultClusterMode,
		direction:       direction,
		proxy:           cb.proxy,
		meshExternal:    meshExternal,
	}
	applyTrafficPolicy(opts)

	return c
}

func (cb *ClusterBuilder) buildLocalityLbEndpoints(proxyNetworkView map[string]bool, service *model.Service,
	port int, labels labels.Collection) []*endpoint.LocalityLbEndpoints {
	if service.Resolution != model.DNSLB {
		return nil
	}

	instances, err := cb.push.InstancesByPort(service, port, labels)
	if err != nil {
		log.Errorf("failed to retrieve instances for %s: %v", service.Hostname, err)
		return nil
	}

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
		ep.Metadata = util.BuildLbEndpointMetadata(instance.Endpoint.UID, instance.Endpoint.Network, instance.Endpoint.TLSMode, cb.push)
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
	applyConnectionPool(cb.push, cluster, passthroughSettings)
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
func castDestinationRuleOrDefault(config *model.Config) *networking.DestinationRule {
	if config != nil {
		return config.Spec.(*networking.DestinationRule)
	}

	return &defaultDestinationRule
}

// maybeApplyEdsConfig applies EdsClusterConfig on the passed in cluster if it is an EDS type of cluster.
func maybeApplyEdsConfig(c *cluster.Cluster, cdsVersion string) {
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
			InitialFetchTimeout: features.InitialFetchTimeout,
		},
	}

	if cdsVersion == v3.ClusterType {
		// For v3 clusters, send v3 eds config.
		c.EdsClusterConfig.EdsConfig.ResourceApiVersion = core.ApiVersion_V3
	}
}
