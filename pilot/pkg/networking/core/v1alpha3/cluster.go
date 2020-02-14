// Copyright 2017 Istio Authors
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
	"strconv"
	"strings"

	apiv2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	auth "github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	v2Cluster "github.com/envoyproxy/go-control-plane/envoy/api/v2/cluster"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	envoy_type "github.com/envoyproxy/go-control-plane/envoy/type"
	"github.com/gogo/protobuf/types"
	structpb "github.com/golang/protobuf/ptypes/struct"
	"github.com/golang/protobuf/ptypes/wrappers"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/envoyfilter"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/loadbalancer"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	authn_v1alpha1_applier "istio.io/istio/pilot/pkg/security/authn/v1alpha1"
	authn_model "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/util/gogo"
)

const (
	// DefaultLbType set to round robin
	DefaultLbType = networking.LoadBalancerSettings_ROUND_ROBIN

	// ManagementClusterHostname indicates the hostname used for building inbound clusters for management ports
	ManagementClusterHostname = "mgmtCluster"

	// StatName patterns
	serviceStatPattern         = "%SERVICE%"
	serviceFQDNStatPattern     = "%SERVICE_FQDN%"
	servicePortStatPattern     = "%SERVICE_PORT%"
	servicePortNameStatPattern = "%SERVICE_PORT_NAME%"
	subsetNameStatPattern      = "%SUBSET_NAME%"
)

var (
	// This disables circuit breaking by default by setting highest possible values.
	// See: https://www.envoyproxy.io/docs/envoy/v1.11.1/faq/disable_circuit_breaking
	defaultCircuitBreakerThresholds = v2Cluster.CircuitBreakers_Thresholds{
		// DefaultMaxRetries specifies the default for the Envoy circuit breaker parameter max_retries. This
		// defines the maximum number of parallel retries a given Envoy will allow to the upstream cluster. Envoy defaults
		// this value to 3, however that has shown to be insufficient during periods of pod churn (e.g. rolling updates),
		// where multiple endpoints in a cluster are terminated. In these scenarios the circuit breaker can kick
		// in before Pilot is able to deliver an updated endpoint list to Envoy, leading to client-facing 503s.
		MaxRetries:         &wrappers.UInt32Value{Value: math.MaxUint32},
		MaxRequests:        &wrappers.UInt32Value{Value: math.MaxUint32},
		MaxConnections:     &wrappers.UInt32Value{Value: math.MaxUint32},
		MaxPendingRequests: &wrappers.UInt32Value{Value: math.MaxUint32},
	}

	defaultDestinationRule = networking.DestinationRule{}

	// defaultTransportSocketMatch applies to endpoints that have no security.istio.io/tlsMode label
	// or those whose label value does not match "istio"
	defaultTransportSocketMatch = &apiv2.Cluster_TransportSocketMatch{
		Name:  "tlsMode-disabled",
		Match: &structpb.Struct{},
		TransportSocket: &core.TransportSocket{
			Name: util.EnvoyRawBufferSocketName,
		},
	}
)

// getDefaultCircuitBreakerThresholds returns a copy of the default circuit breaker thresholds for the given traffic direction.
func getDefaultCircuitBreakerThresholds() *v2Cluster.CircuitBreakers_Thresholds {
	thresholds := defaultCircuitBreakerThresholds
	return &thresholds
}

// BuildClusters returns the list of clusters for the given proxy. This is the CDS output
// For outbound: Cluster for each service/subset hostname or cidr with SNI set to service hostname
// Cluster type based on resolution
// For inbound (sidecar only): Cluster for each inbound endpoint port and for each service port
func (configgen *ConfigGeneratorImpl) BuildClusters(proxy *model.Proxy, push *model.PushContext) []*apiv2.Cluster {
	clusters := make([]*apiv2.Cluster, 0)
	instances := proxy.ServiceInstances

	outboundClusters := configgen.buildOutboundClusters(proxy, push)

	switch proxy.Type {
	case model.SidecarProxy:
		// Add a blackhole and passthrough cluster for catching traffic to unresolved routes
		// DO NOT CALL PLUGINS for these two clusters.
		outboundClusters = append(outboundClusters, buildBlackHoleCluster(push), buildDefaultPassthroughCluster(push, proxy))
		outboundClusters = envoyfilter.ApplyClusterPatches(networking.EnvoyFilter_SIDECAR_OUTBOUND, proxy, push, outboundClusters)
		// Let ServiceDiscovery decide which IP and Port are used for management if
		// there are multiple IPs
		managementPorts := make([]*model.Port, 0)
		for _, ip := range proxy.IPAddresses {
			managementPorts = append(managementPorts, push.ManagementPorts(ip)...)
		}
		inboundClusters := configgen.buildInboundClusters(proxy, push, instances, managementPorts)
		// Pass through clusters for inbound traffic. These cluster bind loopback-ish src address to access node local service.
		inboundClusters = append(inboundClusters, generateInboundPassthroughClusters(push, proxy)...)
		inboundClusters = envoyfilter.ApplyClusterPatches(networking.EnvoyFilter_SIDECAR_INBOUND, proxy, push, inboundClusters)
		clusters = append(clusters, outboundClusters...)
		clusters = append(clusters, inboundClusters...)

	default: // Gateways
		// Gateways do not require the default passthrough cluster as they do not have original dst listeners.
		outboundClusters = append(outboundClusters, buildBlackHoleCluster(push))
		if proxy.Type == model.Router && proxy.GetRouterMode() == model.SniDnatRouter {
			outboundClusters = append(outboundClusters, configgen.buildOutboundSniDnatClusters(proxy, push)...)
		}
		outboundClusters = envoyfilter.ApplyClusterPatches(networking.EnvoyFilter_GATEWAY, proxy, push, outboundClusters)
		clusters = outboundClusters
	}

	clusters = normalizeClusters(push, proxy, clusters)

	return clusters
}

// resolves cluster name conflicts. there can be duplicate cluster names if there are conflicting service definitions.
// for any clusters that share the same name the first cluster is kept and the others are discarded.
func normalizeClusters(metrics model.Metrics, proxy *model.Proxy, clusters []*apiv2.Cluster) []*apiv2.Cluster {
	have := make(map[string]bool)
	out := make([]*apiv2.Cluster, 0, len(clusters))
	for _, cluster := range clusters {
		if !have[cluster.Name] {
			out = append(out, cluster)
		} else {
			metrics.AddMetric(model.DuplicatedClusters, cluster.Name, proxy,
				fmt.Sprintf("Duplicate cluster %s found while pushing CDS", cluster.Name))
		}
		have[cluster.Name] = true
	}
	return out
}

// castDestinationRuleOrDefault returns the destination rule enclosed by the config, if not null.
// Otherwise, return default (empty) DR.
func castDestinationRuleOrDefault(config *model.Config) *networking.DestinationRule {
	if config != nil {
		return config.Spec.(*networking.DestinationRule)
	}

	return &defaultDestinationRule
}

func (configgen *ConfigGeneratorImpl) buildOutboundClusters(proxy *model.Proxy, push *model.PushContext) []*apiv2.Cluster {
	clusters := make([]*apiv2.Cluster, 0)

	inputParams := &plugin.InputParams{
		Push: push,
		Node: proxy,
	}
	networkView := model.GetNetworkView(proxy)

	var services []*model.Service
	if features.FilterGatewayClusterConfig && proxy.Type == model.Router {
		services = push.GatewayServices(proxy)
	} else {
		services = push.Services(proxy)
	}
	for _, service := range services {
		destRule := push.DestinationRule(proxy, service)
		for _, port := range service.Ports {
			if port.Protocol == protocol.UDP {
				continue
			}
			inputParams.Service = service
			inputParams.Port = port

			lbEndpoints := buildLocalityLbEndpoints(push, networkView, service, port.Port, nil)

			// create default cluster
			discoveryType := convertResolution(proxy, service)
			clusterName := model.BuildSubsetKey(model.TrafficDirectionOutbound, "", service.Hostname, port.Port)
			serviceAccounts := push.ServiceAccounts[service.Hostname][port.Port]
			defaultCluster := buildDefaultCluster(push, clusterName, discoveryType, lbEndpoints, model.TrafficDirectionOutbound, proxy, port, service.MeshExternal)
			// If stat name is configured, build the alternate stats name.
			if len(push.Mesh.OutboundClusterStatName) != 0 {
				defaultCluster.AltStatName = altStatName(push.Mesh.OutboundClusterStatName, string(service.Hostname), "", port, service.Attributes)
			}

			setUpstreamProtocol(proxy, defaultCluster, port, model.TrafficDirectionOutbound)
			serviceMTLSMode := model.MTLSUnknown
			if !service.MeshExternal {
				// Only need the authentication MTLS mode when service is not external.
				policy, _ := push.AuthenticationPolicyForWorkload(service, port)
				serviceMTLSMode = authn_v1alpha1_applier.GetMutualTLSMode(policy)
			}
			clusters = append(clusters, defaultCluster)
			destinationRule := castDestinationRuleOrDefault(destRule)

			var clusterMetadata *core.Metadata
			if destRule != nil {
				clusterMetadata = util.BuildConfigInfoMetadata(destRule.ConfigMeta)
			}

			defaultSni := model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, "", service.Hostname, port.Port)
			opts := buildClusterOpts{
				push:            push,
				cluster:         defaultCluster,
				policy:          destinationRule.TrafficPolicy,
				port:            port,
				serviceAccounts: serviceAccounts,
				istioMtlsSni:    defaultSni,
				simpleTLSSni:    string(service.Hostname),
				clusterMode:     DefaultClusterMode,
				direction:       model.TrafficDirectionOutbound,
				proxy:           proxy,
				meshExternal:    service.MeshExternal,
				serviceMTLSMode: serviceMTLSMode,
			}

			applyTrafficPolicy(opts, proxy)
			defaultCluster.Metadata = clusterMetadata
			for _, subset := range destinationRule.Subsets {
				subsetClusterName := model.BuildSubsetKey(model.TrafficDirectionOutbound, subset.Name, service.Hostname, port.Port)
				defaultSni := model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, subset.Name, service.Hostname, port.Port)

				// clusters with discovery type STATIC, STRICT_DNS rely on cluster.hosts field
				// ServiceEntry's need to filter hosts based on subset.labels in order to perform weighted routing
				if discoveryType != apiv2.Cluster_EDS && len(subset.Labels) != 0 {
					lbEndpoints = buildLocalityLbEndpoints(push, networkView, service, port.Port, []labels.Instance{subset.Labels})
				}
				subsetCluster := buildDefaultCluster(push, subsetClusterName, discoveryType, lbEndpoints, model.TrafficDirectionOutbound, proxy, nil, service.MeshExternal)
				if len(push.Mesh.OutboundClusterStatName) != 0 {
					subsetCluster.AltStatName = altStatName(push.Mesh.OutboundClusterStatName, string(service.Hostname), subset.Name, port, service.Attributes)
				}
				setUpstreamProtocol(proxy, subsetCluster, port, model.TrafficDirectionOutbound)

				opts := buildClusterOpts{
					push:            push,
					cluster:         subsetCluster,
					policy:          destinationRule.TrafficPolicy,
					port:            port,
					serviceAccounts: serviceAccounts,
					istioMtlsSni:    defaultSni,
					simpleTLSSni:    string(service.Hostname),
					clusterMode:     DefaultClusterMode,
					direction:       model.TrafficDirectionOutbound,
					proxy:           proxy,
					meshExternal:    service.MeshExternal,
					serviceMTLSMode: serviceMTLSMode,
				}
				applyTrafficPolicy(opts, proxy)

				opts = buildClusterOpts{
					push:            push,
					cluster:         subsetCluster,
					policy:          subset.TrafficPolicy,
					port:            port,
					serviceAccounts: serviceAccounts,
					istioMtlsSni:    defaultSni,
					simpleTLSSni:    string(service.Hostname),
					clusterMode:     DefaultClusterMode,
					direction:       model.TrafficDirectionOutbound,
					proxy:           proxy,
					meshExternal:    service.MeshExternal,
					serviceMTLSMode: serviceMTLSMode,
				}
				applyTrafficPolicy(opts, proxy)

				updateEds(subsetCluster)

				subsetCluster.Metadata = clusterMetadata
				// call plugins
				for _, p := range configgen.Plugins {
					p.OnOutboundCluster(inputParams, subsetCluster)
				}
				clusters = append(clusters, subsetCluster)
			}

			updateEds(defaultCluster)

			// call plugins for the default cluster
			for _, p := range configgen.Plugins {
				p.OnOutboundCluster(inputParams, defaultCluster)
			}
		}
	}

	return clusters
}

// SniDnat clusters do not have any TLS setting, as they simply forward traffic to upstream
// All SniDnat clusters are internal services in the mesh.
func (configgen *ConfigGeneratorImpl) buildOutboundSniDnatClusters(proxy *model.Proxy, push *model.PushContext) []*apiv2.Cluster {
	clusters := make([]*apiv2.Cluster, 0)

	networkView := model.GetNetworkView(proxy)

	for _, service := range push.Services(proxy) {
		if service.MeshExternal {
			continue
		}
		destRule := push.DestinationRule(proxy, service)
		for _, port := range service.Ports {
			if port.Protocol == protocol.UDP {
				continue
			}
			lbEndpoints := buildLocalityLbEndpoints(push, networkView, service, port.Port, nil)

			// create default cluster
			discoveryType := convertResolution(proxy, service)

			clusterName := model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, "", service.Hostname, port.Port)
			defaultCluster := buildDefaultCluster(push, clusterName, discoveryType, lbEndpoints, model.TrafficDirectionOutbound, proxy, nil, service.MeshExternal)
			clusters = append(clusters, defaultCluster)

			if destRule != nil {
				destinationRule := destRule.Spec.(*networking.DestinationRule)
				opts := buildClusterOpts{
					push:        push,
					cluster:     defaultCluster,
					policy:      destinationRule.TrafficPolicy,
					port:        port,
					clusterMode: SniDnatClusterMode,
					direction:   model.TrafficDirectionOutbound,
					proxy:       proxy,
				}
				applyTrafficPolicy(opts, proxy)
				defaultCluster.Metadata = util.BuildConfigInfoMetadata(destRule.ConfigMeta)
				for _, subset := range destinationRule.Subsets {
					subsetClusterName := model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, subset.Name, service.Hostname, port.Port)
					// clusters with discovery type STATIC, STRICT_DNS rely on cluster.hosts field
					// ServiceEntry's need to filter hosts based on subset.labels in order to perform weighted routing
					if discoveryType != apiv2.Cluster_EDS && len(subset.Labels) != 0 {
						lbEndpoints = buildLocalityLbEndpoints(push, networkView, service, port.Port, []labels.Instance{subset.Labels})
					}
					subsetCluster := buildDefaultCluster(push, subsetClusterName, discoveryType, lbEndpoints, model.TrafficDirectionOutbound, proxy, nil, service.MeshExternal)

					opts = buildClusterOpts{
						push:        push,
						cluster:     subsetCluster,
						policy:      destinationRule.TrafficPolicy,
						port:        port,
						clusterMode: SniDnatClusterMode,
						direction:   model.TrafficDirectionOutbound,
						proxy:       proxy,
					}
					applyTrafficPolicy(opts, proxy)

					opts = buildClusterOpts{
						push:        push,
						cluster:     subsetCluster,
						policy:      subset.TrafficPolicy,
						port:        port,
						clusterMode: SniDnatClusterMode,
						direction:   model.TrafficDirectionOutbound,
						proxy:       proxy,
					}
					applyTrafficPolicy(opts, proxy)

					updateEds(subsetCluster)

					subsetCluster.Metadata = util.BuildConfigInfoMetadata(destRule.ConfigMeta)
					clusters = append(clusters, subsetCluster)
				}
			}

			updateEds(defaultCluster)
		}
	}

	return clusters
}

func updateEds(cluster *apiv2.Cluster) {
	switch v := cluster.ClusterDiscoveryType.(type) {
	case *apiv2.Cluster_Type:
		if v.Type != apiv2.Cluster_EDS {
			return
		}
	}
	cluster.EdsClusterConfig = &apiv2.Cluster_EdsClusterConfig{
		ServiceName: cluster.Name,
		EdsConfig: &core.ConfigSource{
			ConfigSourceSpecifier: &core.ConfigSource_Ads{
				Ads: &core.AggregatedConfigSource{},
			},
			InitialFetchTimeout: features.InitialFetchTimeout,
		},
	}
}

func buildLocalityLbEndpoints(push *model.PushContext, proxyNetworkView map[string]bool, service *model.Service,
	port int, labels labels.Collection) []*endpoint.LocalityLbEndpoints {

	if service.Resolution != model.DNSLB {
		return nil
	}

	instances, err := push.InstancesByPort(service, port, labels)
	if err != nil {
		log.Errorf("failed to retrieve instances for %s: %v", service.Hostname, err)
		return nil
	}

	lbEndpoints := make(map[string][]*endpoint.LbEndpoint)
	for _, instance := range instances {
		// Only send endpoints from the networks in the network view requested by the proxy.
		// The default network view assigned to the Proxy is the UnnamedNetwork (""), which matches
		// the default network assigned to endpoints that don't have an explicit network
		if !proxyNetworkView[instance.Endpoint.Network] {
			// Endpoint's network doesn't match the set of networks that the proxy wants to see.
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
		ep.Metadata = util.BuildLbEndpointMetadata(instance.Endpoint.UID, instance.Endpoint.Network, instance.Endpoint.TLSMode, push)
		locality := instance.GetLocality()
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

func buildInboundLocalityLbEndpoints(bind string, port uint32) []*endpoint.LocalityLbEndpoints {
	address := util.BuildAddress(bind, port)
	lbEndpoint := &endpoint.LbEndpoint{
		HostIdentifier: &endpoint.LbEndpoint_Endpoint{
			Endpoint: &endpoint.Endpoint{
				Address: address,
			},
		},
	}
	return []*endpoint.LocalityLbEndpoints{
		{
			LbEndpoints: []*endpoint.LbEndpoint{lbEndpoint},
		},
	}
}

func generateInboundPassthroughClusters(push *model.PushContext, proxy *model.Proxy) []*apiv2.Cluster {
	// ipv4 and ipv6 feature detection. Envoy cannot ignore a config where the ip version is not supported
	ipv4, ipv6 := ipv4AndIpv6Support(proxy)
	clusters := make([]*apiv2.Cluster, 0, 2)
	if ipv4 {
		inboundPassthroughClusterIpv4 := buildDefaultPassthroughCluster(push, proxy)
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
	if ipv6 {
		inboundPassthroughClusterIpv6 := buildDefaultPassthroughCluster(push, proxy)
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

func (configgen *ConfigGeneratorImpl) buildInboundClusters(proxy *model.Proxy,
	push *model.PushContext, instances []*model.ServiceInstance, managementPorts []*model.Port) []*apiv2.Cluster {

	clusters := make([]*apiv2.Cluster, 0)

	// The inbound clusters for a node depends on whether the node has a SidecarScope with inbound listeners
	// or not. If the node has a sidecarscope with ingress listeners, we only return clusters corresponding
	// to those listeners i.e. clusters made out of the defaultEndpoint field.
	// If the node has no sidecarScope and has interception mode set to NONE, then we should skip the inbound
	// clusters, because there would be no corresponding inbound listeners
	sidecarScope := proxy.SidecarScope
	noneMode := proxy.GetInterceptionMode() == model.InterceptionNone

	_, actualLocalHost := getActualWildcardAndLocalHost(proxy)

	if !sidecarScope.HasCustomIngressListeners {
		// No user supplied sidecar scope or the user supplied one has no ingress listeners

		// We should not create inbound listeners in NONE mode based on the service instances
		// Doing so will prevent the workloads from starting as they would be listening on the same port
		// Users are required to provide the sidecar config to define the inbound listeners
		if noneMode {
			return nil
		}

		have := make(map[*model.Port]bool)
		for _, instance := range instances {
			// Filter out service instances with the same port as we are going to mark them as duplicates any way
			// in normalizeClusters method.
			if !have[instance.ServicePort] {
				pluginParams := &plugin.InputParams{
					Node:            proxy,
					ServiceInstance: instance,
					Port:            instance.ServicePort,
					Push:            push,
					Bind:            actualLocalHost,
				}
				localCluster := configgen.buildInboundClusterForPortOrUDS(pluginParams)
				clusters = append(clusters, localCluster)
				have[instance.ServicePort] = true
			}
		}

		// Add a passthrough cluster for traffic to management ports (health check ports)
		for _, port := range managementPorts {
			clusterName := model.BuildSubsetKey(model.TrafficDirectionInbound, port.Name,
				ManagementClusterHostname, port.Port)
			localityLbEndpoints := buildInboundLocalityLbEndpoints(actualLocalHost, uint32(port.Port))
			mgmtCluster := buildDefaultCluster(push, clusterName, apiv2.Cluster_STATIC, localityLbEndpoints,
				model.TrafficDirectionInbound, proxy, nil, false)
			setUpstreamProtocol(proxy, mgmtCluster, port, model.TrafficDirectionInbound)
			clusters = append(clusters, mgmtCluster)
		}
	} else {
		rule := sidecarScope.Config.Spec.(*networking.Sidecar)
		for _, ingressListener := range rule.Ingress {
			// LDS would have setup the inbound clusters
			// as inbound|portNumber|portName|Hostname[or]SidecarScopeID
			listenPort := &model.Port{
				Port:     int(ingressListener.Port.Number),
				Protocol: protocol.Parse(ingressListener.Port.Protocol),
				Name:     ingressListener.Port.Name,
			}

			// When building an inbound cluster for the ingress listener, we take the defaultEndpoint specified
			// by the user and parse it into host:port or a unix domain socket
			// The default endpoint can be 127.0.0.1:port or :port or unix domain socket
			endpointAddress := actualLocalHost
			endpointFamily := model.AddressFamilyTCP
			port := 0
			var err error
			if strings.HasPrefix(ingressListener.DefaultEndpoint, model.UnixAddressPrefix) {
				// this is a UDS endpoint. assign it as is
				endpointAddress = ingressListener.DefaultEndpoint
				endpointFamily = model.AddressFamilyUnix
			} else {
				// parse the ip, port. Validation guarantees presence of :
				parts := strings.Split(ingressListener.DefaultEndpoint, ":")
				if len(parts) < 2 {
					continue
				}
				if port, err = strconv.Atoi(parts[1]); err != nil {
					continue
				}
			}

			// Find the service instance that corresponds to this ingress listener by looking
			// for a service instance that matches this ingress port as this will allow us
			// to generate the right cluster name that LDS expects inbound|portNumber|portName|Hostname
			instance := configgen.findOrCreateServiceInstance(instances, ingressListener, sidecarScope.Config.Name, sidecarScope.Config.Namespace)
			instance.Endpoint.Family = endpointFamily
			instance.Endpoint.Address = endpointAddress
			instance.ServicePort = listenPort
			instance.Endpoint.ServicePortName = listenPort.Name
			instance.Endpoint.EndpointPort = uint32(port)

			pluginParams := &plugin.InputParams{
				Node:            proxy,
				ServiceInstance: instance,
				Port:            listenPort,
				Push:            push,
				Bind:            endpointAddress,
			}
			localCluster := configgen.buildInboundClusterForPortOrUDS(pluginParams)
			clusters = append(clusters, localCluster)
		}
	}

	return clusters
}

func (configgen *ConfigGeneratorImpl) findOrCreateServiceInstance(instances []*model.ServiceInstance,
	ingressListener *networking.IstioIngressListener, sidecar string, sidecarns string) *model.ServiceInstance {
	for _, realInstance := range instances {
		if realInstance.Endpoint.EndpointPort == ingressListener.Port.Number {
			// We need to create a copy of the instance, as it is modified later while building clusters/listeners.
			return realInstance.DeepCopy()
		}
	}
	// We didn't find a matching instance. Create a dummy one because we need the right
	// params to generate the right cluster name i.e. inbound|portNumber|portName|SidecarScopeID - which is uniformly generated by LDS/CDS.
	attrs := model.ServiceAttributes{
		Name: sidecar,
		// This will ensure that the right AuthN policies are selected
		Namespace: sidecarns,
	}
	return &model.ServiceInstance{
		Service: &model.Service{
			Hostname:   host.Name(sidecar + "." + sidecarns),
			Attributes: attrs,
		},
		Endpoint: &model.IstioEndpoint{
			Attributes: attrs,
		},
	}
}

func (configgen *ConfigGeneratorImpl) buildInboundClusterForPortOrUDS(pluginParams *plugin.InputParams) *apiv2.Cluster {
	instance := pluginParams.ServiceInstance
	clusterName := model.BuildSubsetKey(model.TrafficDirectionInbound, instance.ServicePort.Name,
		instance.Service.Hostname, instance.ServicePort.Port)
	localityLbEndpoints := buildInboundLocalityLbEndpoints(pluginParams.Bind, instance.Endpoint.EndpointPort)
	localCluster := buildDefaultCluster(pluginParams.Push, clusterName, apiv2.Cluster_STATIC, localityLbEndpoints,
		model.TrafficDirectionInbound, pluginParams.Node, nil, false)
	// If stat name is configured, build the alt statname.
	if len(pluginParams.Push.Mesh.InboundClusterStatName) != 0 {
		localCluster.AltStatName = altStatName(pluginParams.Push.Mesh.InboundClusterStatName,
			string(instance.Service.Hostname), "", instance.ServicePort, instance.Service.Attributes)
	}
	setUpstreamProtocol(pluginParams.Node, localCluster, instance.ServicePort, model.TrafficDirectionInbound)
	// call plugins
	for _, p := range configgen.Plugins {
		p.OnInboundCluster(pluginParams, localCluster)
	}

	// When users specify circuit breakers, they need to be set on the receiver end
	// (server side) as well as client side, so that the server has enough capacity
	// (not the defaults) to handle the increased traffic volume
	// TODO: This is not foolproof - if instance is part of multiple services listening on same port,
	// choice of inbound cluster is arbitrary. So the connection pool settings may not apply cleanly.
	cfg := pluginParams.Push.DestinationRule(pluginParams.Node, instance.Service)
	if cfg != nil {
		destinationRule := cfg.Spec.(*networking.DestinationRule)
		if destinationRule.TrafficPolicy != nil {
			// only connection pool settings make sense on the inbound path.
			// upstream TLS settings/outlier detection/load balancer don't apply here.
			applyConnectionPool(pluginParams.Push, localCluster, destinationRule.TrafficPolicy.ConnectionPool)
			localCluster.Metadata = util.BuildConfigInfoMetadata(cfg.ConfigMeta)
		}
	}
	return localCluster
}

func convertResolution(proxy *model.Proxy, service *model.Service) apiv2.Cluster_DiscoveryType {
	switch service.Resolution {
	case model.ClientSideLB:
		return apiv2.Cluster_EDS
	case model.DNSLB:
		return apiv2.Cluster_STRICT_DNS
	case model.Passthrough:
		// Gateways cannot use passthrough clusters. So fallback to EDS
		if proxy.Type == model.SidecarProxy {
			if service.Attributes.ServiceRegistry == string(serviceregistry.Kubernetes) && features.EnableEDSForHeadless.Get() {
				return apiv2.Cluster_EDS
			}

			return apiv2.Cluster_ORIGINAL_DST
		}
		return apiv2.Cluster_EDS
	default:
		return apiv2.Cluster_EDS
	}
}

type mtlsContextType int

const (
	userSupplied mtlsContextType = iota
	autoDetected
)

// conditionallyConvertToIstioMtls fills key cert fields for all TLSSettings when the mode is `ISTIO_MUTUAL`.
// If the (input) TLS setting is nil (i.e not set), *and* the service mTLS mode is STRICT, it also
// creates and populates the config as if they are set as ISTIO_MUTUAL.
func conditionallyConvertToIstioMtls(
	tls *networking.TLSSettings,
	serviceAccounts []string,
	sni string,
	proxy *model.Proxy,
	autoMTLSEnabled bool,
	meshExternal bool,
	serviceMTLSMode model.MutualTLSMode,
) (*networking.TLSSettings, mtlsContextType) {
	mtlsCtx := userSupplied
	if tls == nil {
		if meshExternal || !autoMTLSEnabled || serviceMTLSMode == model.MTLSUnknown || serviceMTLSMode == model.MTLSDisable {
			return nil, mtlsCtx
		}

		mtlsCtx = autoDetected
		// we will setup transport sockets later
		tls = &networking.TLSSettings{
			Mode: networking.TLSSettings_ISTIO_MUTUAL,
		}
	}
	if tls.Mode == networking.TLSSettings_ISTIO_MUTUAL {
		// Use client provided SNI if set. Otherwise, overwrite with the auto generated SNI
		// user specified SNIs in the istio mtls settings are useful when routing via gateways
		sniToUse := tls.Sni
		if len(sniToUse) == 0 {
			sniToUse = sni
		}
		subjectAltNamesToUse := tls.SubjectAltNames
		if len(subjectAltNamesToUse) == 0 {
			subjectAltNamesToUse = serviceAccounts
		}
		return buildIstioMutualTLS(subjectAltNamesToUse, sniToUse, proxy), mtlsCtx
	}
	return tls, mtlsCtx
}

// buildIstioMutualTLS returns a `TLSSettings` for ISTIO_MUTUAL mode.
func buildIstioMutualTLS(serviceAccounts []string, sni string, proxy *model.Proxy) *networking.TLSSettings {
	return &networking.TLSSettings{
		Mode:              networking.TLSSettings_ISTIO_MUTUAL,
		CaCertificates:    model.GetOrDefault(proxy.Metadata.TLSClientRootCert, constants.DefaultRootCert),
		ClientCertificate: model.GetOrDefault(proxy.Metadata.TLSClientCertChain, constants.DefaultCertChain),
		PrivateKey:        model.GetOrDefault(proxy.Metadata.TLSClientKey, constants.DefaultKey),
		SubjectAltNames:   serviceAccounts,
		Sni:               sni,
	}
}

// SelectTrafficPolicyComponents returns the components of TrafficPolicy that should be used for given port.
func SelectTrafficPolicyComponents(policy *networking.TrafficPolicy, port *model.Port) (
	*networking.ConnectionPoolSettings, *networking.OutlierDetection, *networking.LoadBalancerSettings, *networking.TLSSettings) {
	if policy == nil {
		return nil, nil, nil, nil
	}
	connectionPool := policy.ConnectionPool
	outlierDetection := policy.OutlierDetection
	loadBalancer := policy.LoadBalancer
	tls := policy.Tls

	if port != nil && len(policy.PortLevelSettings) > 0 {
		foundPort := false
		for _, p := range policy.PortLevelSettings {
			if p.Port != nil {
				if uint32(port.Port) == p.Port.Number {
					foundPort = true
				}
			}
			if foundPort {
				connectionPool = p.ConnectionPool
				outlierDetection = p.OutlierDetection
				loadBalancer = p.LoadBalancer
				tls = p.Tls
				break
			}
		}
	}
	return connectionPool, outlierDetection, loadBalancer, tls
}

// ClusterMode defines whether the cluster is being built for SNI-DNATing (sni passthrough) or not
type ClusterMode string

const (
	// SniDnatClusterMode indicates cluster is being built for SNI dnat mode
	SniDnatClusterMode ClusterMode = "sni-dnat"
	// DefaultClusterMode indicates usual cluster with mTLS et al
	DefaultClusterMode ClusterMode = "outbound"
)

type buildClusterOpts struct {
	push            *model.PushContext
	cluster         *apiv2.Cluster
	policy          *networking.TrafficPolicy
	port            *model.Port
	serviceAccounts []string
	// Used for traffic across multiple Istio clusters
	// the ingress gateway in a remote cluster will use this value to route
	// traffic to the appropriate service
	istioMtlsSni string
	// This is used when the sidecar is sending simple TLS traffic
	// to endpoints. This is different from the previous SNI
	// because usually in this case the traffic is going to a
	// non-sidecar workload that can only understand the service's
	// hostname in the SNI.
	simpleTLSSni    string
	clusterMode     ClusterMode
	direction       model.TrafficDirection
	proxy           *model.Proxy
	meshExternal    bool
	serviceMTLSMode model.MutualTLSMode
}

func applyTrafficPolicy(opts buildClusterOpts, proxy *model.Proxy) {
	connectionPool, outlierDetection, loadBalancer, tls := SelectTrafficPolicyComponents(opts.policy, opts.port)

	applyConnectionPool(opts.push, opts.cluster, connectionPool)
	applyOutlierDetection(opts.cluster, outlierDetection)
	applyLoadBalancer(opts.cluster, loadBalancer, opts.port, proxy, opts.push.Mesh)

	if opts.clusterMode != SniDnatClusterMode && opts.direction != model.TrafficDirectionInbound {
		autoMTLSEnabled := opts.push.Mesh.GetEnableAutoMtls().Value
		var mtlsCtxType mtlsContextType
		tls, mtlsCtxType = conditionallyConvertToIstioMtls(tls, opts.serviceAccounts, opts.istioMtlsSni, opts.proxy,
			autoMTLSEnabled, opts.meshExternal, opts.serviceMTLSMode)
		applyUpstreamTLSSettings(&opts, tls, mtlsCtxType, proxy)
	}
}

// FIXME: there isn't a way to distinguish between unset values and zero values
func applyConnectionPool(push *model.PushContext, cluster *apiv2.Cluster, settings *networking.ConnectionPoolSettings) {
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
			cluster.MaxRequestsPerConnection = &wrappers.UInt32Value{Value: uint32(settings.Http.MaxRequestsPerConnection)}
		}

		// FIXME: zero is a valid value if explicitly set, otherwise we want to use the default
		if settings.Http.MaxRetries > 0 {
			threshold.MaxRetries = &wrappers.UInt32Value{Value: uint32(settings.Http.MaxRetries)}
		}

		idleTimeout = settings.Http.IdleTimeout
	}

	if settings.Tcp != nil {
		if settings.Tcp.ConnectTimeout != nil {
			cluster.ConnectTimeout = gogo.DurationToProtoDuration(settings.Tcp.ConnectTimeout)
		}

		if settings.Tcp.MaxConnections > 0 {
			threshold.MaxConnections = &wrappers.UInt32Value{Value: uint32(settings.Tcp.MaxConnections)}
		}

		applyTCPKeepalive(push, cluster, settings)
	}

	cluster.CircuitBreakers = &v2Cluster.CircuitBreakers{
		Thresholds: []*v2Cluster.CircuitBreakers_Thresholds{threshold},
	}

	if idleTimeout != nil {
		idleTimeoutDuration := gogo.DurationToProtoDuration(idleTimeout)
		cluster.CommonHttpProtocolOptions = &core.HttpProtocolOptions{IdleTimeout: idleTimeoutDuration}
	}
}

func applyTCPKeepalive(push *model.PushContext, cluster *apiv2.Cluster, settings *networking.ConnectionPoolSettings) {
	var keepaliveProbes uint32
	var keepaliveTime *types.Duration
	var keepaliveInterval *types.Duration
	isTCPKeepaliveSet := false

	// Apply mesh wide TCP keepalive.
	if push.Mesh.TcpKeepalive != nil {
		keepaliveProbes = push.Mesh.TcpKeepalive.Probes
		keepaliveTime = push.Mesh.TcpKeepalive.Time
		keepaliveInterval = push.Mesh.TcpKeepalive.Interval
		isTCPKeepaliveSet = true
	}

	// Apply/Override with DestinationRule TCP keepalive if set.
	if settings.Tcp.TcpKeepalive != nil {
		keepaliveProbes = settings.Tcp.TcpKeepalive.Probes
		keepaliveTime = settings.Tcp.TcpKeepalive.Time
		keepaliveInterval = settings.Tcp.TcpKeepalive.Interval
		isTCPKeepaliveSet = true
	}

	if !isTCPKeepaliveSet {
		return
	}

	// If none of the proto fields are set, then an empty tcp_keepalive is set in Envoy.
	// That would set SO_KEEPALIVE on the socket with OS default values.
	upstreamConnectionOptions := &apiv2.UpstreamConnectionOptions{
		TcpKeepalive: &core.TcpKeepalive{},
	}

	// If any of the TCP keepalive options are not set, skip them from the config so that OS defaults are used.
	if keepaliveProbes > 0 {
		upstreamConnectionOptions.TcpKeepalive.KeepaliveProbes = &wrappers.UInt32Value{Value: keepaliveProbes}
	}

	if keepaliveTime != nil {
		upstreamConnectionOptions.TcpKeepalive.KeepaliveTime = &wrappers.UInt32Value{Value: uint32(keepaliveTime.Seconds)}
	}

	if keepaliveInterval != nil {
		upstreamConnectionOptions.TcpKeepalive.KeepaliveInterval = &wrappers.UInt32Value{Value: uint32(keepaliveInterval.Seconds)}
	}

	cluster.UpstreamConnectionOptions = upstreamConnectionOptions
}

// FIXME: there isn't a way to distinguish between unset values and zero values
func applyOutlierDetection(cluster *apiv2.Cluster, outlier *networking.OutlierDetection) {
	if outlier == nil {
		return
	}

	out := &v2Cluster.OutlierDetection{}
	if outlier.BaseEjectionTime != nil {
		out.BaseEjectionTime = gogo.DurationToProtoDuration(outlier.BaseEjectionTime)
	}
	if outlier.ConsecutiveErrors > 0 {
		// Only listen to gateway errors, see https://github.com/istio/api/pull/617
		out.EnforcingConsecutiveGatewayFailure = &wrappers.UInt32Value{Value: uint32(100)} // defaults to 0
		out.EnforcingConsecutive_5Xx = &wrappers.UInt32Value{Value: uint32(0)}             // defaults to 100
		out.ConsecutiveGatewayFailure = &wrappers.UInt32Value{Value: uint32(outlier.ConsecutiveErrors)}
	}

	if e := outlier.Consecutive_5XxErrors; e != nil {
		v := e.GetValue()

		out.Consecutive_5Xx = &wrappers.UInt32Value{Value: v}

		if v > 0 {
			v = 100
		}
		out.EnforcingConsecutive_5Xx = &wrappers.UInt32Value{Value: v}
	}
	if e := outlier.ConsecutiveGatewayErrors; e != nil {
		v := e.GetValue()

		out.ConsecutiveGatewayFailure = &wrappers.UInt32Value{Value: v}

		if v > 0 {
			v = 100
		}
		out.EnforcingConsecutiveGatewayFailure = &wrappers.UInt32Value{Value: v}
	}

	if outlier.Interval != nil {
		out.Interval = gogo.DurationToProtoDuration(outlier.Interval)
	}
	if outlier.MaxEjectionPercent > 0 {
		out.MaxEjectionPercent = &wrappers.UInt32Value{Value: uint32(outlier.MaxEjectionPercent)}
	}

	cluster.OutlierDetection = out

	// Disable panic threshold by default as its not typically applicable in k8s environments
	// with few pods per service.
	// To do so, set the healthy_panic_threshold field even if its value is 0 (defaults to 50).
	// FIXME: we can't distinguish between it being unset or being explicitly set to 0
	if outlier.MinHealthPercent >= 0 {
		if cluster.CommonLbConfig == nil {
			cluster.CommonLbConfig = &apiv2.Cluster_CommonLbConfig{}
		}
		cluster.CommonLbConfig.HealthyPanicThreshold = &envoy_type.Percent{Value: float64(outlier.MinHealthPercent)} // defaults to 50
	}
}

func applyLoadBalancer(cluster *apiv2.Cluster, lb *networking.LoadBalancerSettings, port *model.Port, proxy *model.Proxy, meshConfig *meshconfig.MeshConfig) {
	if cluster.OutlierDetection != nil {
		if cluster.CommonLbConfig == nil {
			cluster.CommonLbConfig = &apiv2.Cluster_CommonLbConfig{}
		}
		// Locality weighted load balancing
		cluster.CommonLbConfig.LocalityConfigSpecifier = &apiv2.Cluster_CommonLbConfig_LocalityWeightedLbConfig_{
			LocalityWeightedLbConfig: &apiv2.Cluster_CommonLbConfig_LocalityWeightedLbConfig{},
		}
	}

	// Use locality lb settings from load balancer settings if present, else use mesh wide locality lb settings
	lbSetting := loadbalancer.GetLocalityLbSetting(meshConfig.GetLocalityLbSetting(), lb.GetLocalityLbSetting())
	applyLocalityLBSetting(proxy.Locality, cluster, lbSetting)

	// The following order is important. If cluster type has been identified as Original DST since Resolution is PassThrough,
	// and port is named as redis-xxx we end up creating a cluster with type Original DST and LbPolicy as MAGLEV which would be
	// rejected by Envoy.

	// Original destination service discovery must be used with the original destination load balancer.
	switch t := cluster.GetClusterDiscoveryType().(type) {
	case *apiv2.Cluster_Type:
		if t.Type == apiv2.Cluster_ORIGINAL_DST {
			cluster.LbPolicy = lbPolicyClusterProvided(proxy)
			return
		}
	}

	// Redis protocol must be defaulted with MAGLEV to benefit from client side sharding.
	if features.EnableRedisFilter.Get() && port != nil && port.Protocol == protocol.Redis {
		cluster.LbPolicy = apiv2.Cluster_MAGLEV
		return
	}

	if lb == nil {
		return
	}

	// DO not do if else here. since lb.GetSimple returns a enum value (not pointer).
	switch lb.GetSimple() {
	case networking.LoadBalancerSettings_LEAST_CONN:
		cluster.LbPolicy = apiv2.Cluster_LEAST_REQUEST
	case networking.LoadBalancerSettings_RANDOM:
		cluster.LbPolicy = apiv2.Cluster_RANDOM
	case networking.LoadBalancerSettings_ROUND_ROBIN:
		cluster.LbPolicy = apiv2.Cluster_ROUND_ROBIN
	case networking.LoadBalancerSettings_PASSTHROUGH:
		cluster.LbPolicy = lbPolicyClusterProvided(proxy)
		cluster.ClusterDiscoveryType = &apiv2.Cluster_Type{Type: apiv2.Cluster_ORIGINAL_DST}
	}

	consistentHash := lb.GetConsistentHash()
	if consistentHash != nil {
		// TODO MinimumRingSize is an int, and zero could potentially be a valid value
		// unable to distinguish between set and unset case currently GregHanson
		// 1024 is the default value for envoy
		minRingSize := &wrappers.UInt64Value{Value: 1024}
		if consistentHash.MinimumRingSize != 0 {
			minRingSize = &wrappers.UInt64Value{Value: consistentHash.GetMinimumRingSize()}
		}
		cluster.LbPolicy = apiv2.Cluster_RING_HASH
		cluster.LbConfig = &apiv2.Cluster_RingHashLbConfig_{
			RingHashLbConfig: &apiv2.Cluster_RingHashLbConfig{
				MinimumRingSize: minRingSize,
			},
		}
	}
}

func applyLocalityLBSetting(
	locality *core.Locality,
	cluster *apiv2.Cluster,
	localityLB *networking.LocalityLoadBalancerSetting,
) {
	if locality == nil || localityLB == nil {
		return
	}

	// Failover should only be applied with outlier detection, or traffic will never failover.
	enabledFailover := cluster.OutlierDetection != nil
	if cluster.LoadAssignment != nil {
		loadbalancer.ApplyLocalityLBSetting(locality, cluster.LoadAssignment, localityLB, enabledFailover)
	}
}

func applyUpstreamTLSSettings(opts *buildClusterOpts, tls *networking.TLSSettings, mtlsCtxType mtlsContextType, node *model.Proxy) {
	if tls == nil {
		return
	}

	cluster := opts.cluster
	proxy := opts.proxy
	certValidationContext := &auth.CertificateValidationContext{}
	var trustedCa *core.DataSource
	if len(tls.CaCertificates) != 0 {
		trustedCa = &core.DataSource{
			Specifier: &core.DataSource_Filename{
				Filename: model.GetOrDefault(proxy.Metadata.TLSClientRootCert, tls.CaCertificates),
			},
		}
	}
	if trustedCa != nil || len(tls.SubjectAltNames) > 0 {
		certValidationContext = &auth.CertificateValidationContext{
			TrustedCa:            trustedCa,
			VerifySubjectAltName: tls.SubjectAltNames,
		}
	}

	tlsContext := &auth.UpstreamTlsContext{}
	switch tls.Mode {
	case networking.TLSSettings_DISABLE:
		tlsContext = nil
	case networking.TLSSettings_SIMPLE:
		tlsContext = &auth.UpstreamTlsContext{
			CommonTlsContext: &auth.CommonTlsContext{
				ValidationContextType: &auth.CommonTlsContext_ValidationContext{
					ValidationContext: certValidationContext,
				},
			},
			Sni: tls.Sni,
		}
		if cluster.Http2ProtocolOptions != nil {
			// This is HTTP/2 cluster, advertise it with ALPN.
			tlsContext.CommonTlsContext.AlpnProtocols = util.ALPNH2Only
		}
	case networking.TLSSettings_MUTUAL, networking.TLSSettings_ISTIO_MUTUAL:
		if tls.ClientCertificate == "" || tls.PrivateKey == "" {
			log.Errorf("failed to apply tls setting for %s: client certificate and private key must not be empty",
				cluster.Name)
			return
		}

		tlsContext = &auth.UpstreamTlsContext{
			CommonTlsContext: &auth.CommonTlsContext{},
			Sni:              tls.Sni,
		}

		// Fallback to file mount secret instead of SDS if meshConfig.sdsUdsPath isn't set or tls.mode is TLSSettings_MUTUAL.
		if !node.Metadata.SdsEnabled || opts.push.Mesh.SdsUdsPath == "" || tls.Mode == networking.TLSSettings_MUTUAL {
			tlsContext.CommonTlsContext.ValidationContextType = &auth.CommonTlsContext_ValidationContext{
				ValidationContext: certValidationContext,
			}
			tlsContext.CommonTlsContext.TlsCertificates = []*auth.TlsCertificate{
				{
					CertificateChain: &core.DataSource{
						Specifier: &core.DataSource_Filename{
							Filename: model.GetOrDefault(proxy.Metadata.TLSClientCertChain, tls.ClientCertificate),
						},
					},
					PrivateKey: &core.DataSource{
						Specifier: &core.DataSource_Filename{
							Filename: model.GetOrDefault(proxy.Metadata.TLSClientKey, tls.PrivateKey),
						},
					},
				},
			}
		} else {
			tlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs = append(tlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs,
				authn_model.ConstructSdsSecretConfig(authn_model.SDSDefaultResourceName, opts.push.Mesh.SdsUdsPath))

			tlsContext.CommonTlsContext.ValidationContextType = &auth.CommonTlsContext_CombinedValidationContext{
				CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
					DefaultValidationContext:         &auth.CertificateValidationContext{VerifySubjectAltName: tls.SubjectAltNames},
					ValidationContextSdsSecretConfig: authn_model.ConstructSdsSecretConfig(authn_model.SDSRootResourceName, opts.push.Mesh.SdsUdsPath),
				},
			}
		}

		// Set default SNI of cluster name for istio_mutual if sni is not set.
		if len(tls.Sni) == 0 && tls.Mode == networking.TLSSettings_ISTIO_MUTUAL {
			tlsContext.Sni = cluster.Name
		}
		if cluster.Http2ProtocolOptions != nil {
			// This is HTTP/2 in-mesh cluster, advertise it with ALPN.
			if tls.Mode == networking.TLSSettings_ISTIO_MUTUAL {
				tlsContext.CommonTlsContext.AlpnProtocols = util.ALPNInMeshH2
			} else {
				tlsContext.CommonTlsContext.AlpnProtocols = util.ALPNH2Only
			}
		} else if tls.Mode == networking.TLSSettings_ISTIO_MUTUAL {
			// This is in-mesh cluster, advertise it with ALPN.
			if util.IsTCPMetadataExchangeEnabled(node) {
				tlsContext.CommonTlsContext.AlpnProtocols = util.ALPNInMeshWithMxc
			} else {
				tlsContext.CommonTlsContext.AlpnProtocols = util.ALPNInMesh
			}
		}
	}

	if tlsContext != nil {
		cluster.TransportSocket = &core.TransportSocket{
			Name:       util.EnvoyTLSSocketName,
			ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: util.MessageToAny(tlsContext)},
		}
	}

	// For headless service, discover type will be `Cluster_ORIGINAL_DST`, and corresponding LbPolicy is `Cluster_CLUSTER_PROVIDED`
	// Apply auto mtls to clusters excluding these kind of headless service
	if cluster.LbPolicy != apiv2.Cluster_CLUSTER_PROVIDED {
		// convert to transport socket matcher if the mode was auto detected
		if tls.Mode == networking.TLSSettings_ISTIO_MUTUAL && mtlsCtxType == autoDetected && util.IsIstioVersionGE14(proxy) {
			transportSocket := cluster.TransportSocket
			cluster.TransportSocket = nil
			cluster.TransportSocketMatches = []*apiv2.Cluster_TransportSocketMatch{
				{
					Name: "tlsMode-" + model.IstioMutualTLSModeLabel,
					Match: &structpb.Struct{
						Fields: map[string]*structpb.Value{
							model.TLSModeLabelShortname: {Kind: &structpb.Value_StringValue{StringValue: model.IstioMutualTLSModeLabel}},
						},
					},
					TransportSocket: transportSocket,
				},
				defaultTransportSocketMatch,
			}
		}
	}
}

func setUpstreamProtocol(node *model.Proxy, cluster *apiv2.Cluster, port *model.Port, direction model.TrafficDirection) {
	if port.Protocol.IsHTTP2() {
		cluster.Http2ProtocolOptions = &core.Http2ProtocolOptions{
			// Envoy default value of 100 is too low for data path.
			MaxConcurrentStreams: &wrappers.UInt32Value{
				Value: 1073741824,
			},
		}
	}

	if (util.IsProtocolSniffingEnabledForInboundPort(node, port) && direction == model.TrafficDirectionInbound) ||
		(util.IsProtocolSniffingEnabledForOutboundPort(node, port) && direction == model.TrafficDirectionOutbound) {
		// setup http2 protocol options for upstream connection.
		cluster.Http2ProtocolOptions = &core.Http2ProtocolOptions{
			// Envoy default value of 100 is too low for data path.
			MaxConcurrentStreams: &wrappers.UInt32Value{
				Value: 1073741824,
			},
		}

		// Use downstream protocol. If the incoming traffic use HTTP 1.1, the
		// upstream cluster will use HTTP 1.1, if incoming traffic use HTTP2,
		// the upstream cluster will use HTTP2.
		cluster.ProtocolSelection = apiv2.Cluster_USE_DOWNSTREAM_PROTOCOL
	}
}

// generates a cluster that sends traffic to dummy localport 0
// This cluster is used to catch all traffic to unresolved destinations in virtual service
func buildBlackHoleCluster(push *model.PushContext) *apiv2.Cluster {
	cluster := &apiv2.Cluster{
		Name:                 util.BlackHoleCluster,
		ClusterDiscoveryType: &apiv2.Cluster_Type{Type: apiv2.Cluster_STATIC},
		ConnectTimeout:       gogo.DurationToProtoDuration(push.Mesh.ConnectTimeout),
		LbPolicy:             apiv2.Cluster_ROUND_ROBIN,
	}
	return cluster
}

// generates a cluster that sends traffic to the original destination.
// This cluster is used to catch all traffic to unknown listener ports
func buildDefaultPassthroughCluster(push *model.PushContext, proxy *model.Proxy) *apiv2.Cluster {
	cluster := &apiv2.Cluster{
		Name:                 util.PassthroughCluster,
		ClusterDiscoveryType: &apiv2.Cluster_Type{Type: apiv2.Cluster_ORIGINAL_DST},
		ConnectTimeout:       gogo.DurationToProtoDuration(push.Mesh.ConnectTimeout),
		LbPolicy:             lbPolicyClusterProvided(proxy),
	}
	passthroughSettings := &networking.ConnectionPoolSettings{}
	applyConnectionPool(push, cluster, passthroughSettings)
	return cluster
}

func buildDefaultCluster(push *model.PushContext, name string, discoveryType apiv2.Cluster_DiscoveryType,
	localityLbEndpoints []*endpoint.LocalityLbEndpoints, direction model.TrafficDirection, proxy *model.Proxy,
	port *model.Port, meshExternal bool) *apiv2.Cluster {
	cluster := &apiv2.Cluster{
		Name:                 name,
		ClusterDiscoveryType: &apiv2.Cluster_Type{Type: discoveryType},
	}

	if discoveryType == apiv2.Cluster_STRICT_DNS {
		cluster.DnsLookupFamily = apiv2.Cluster_V4_ONLY
		dnsRate := gogo.DurationToProtoDuration(push.Mesh.DnsRefreshRate)
		cluster.DnsRefreshRate = dnsRate
		if util.IsIstioVersionGE13(proxy) && features.RespectDNSTTL.Get() {
			cluster.RespectDnsTtl = true
		}
	}

	if discoveryType == apiv2.Cluster_STATIC || discoveryType == apiv2.Cluster_STRICT_DNS {
		cluster.LoadAssignment = &apiv2.ClusterLoadAssignment{
			ClusterName: name,
			Endpoints:   localityLbEndpoints,
		}
	}

	// TODO: Should this be done only for inbound as outbound will call applyTrafficPolicy anyway.
	defaultTrafficPolicy := buildDefaultTrafficPolicy(push, discoveryType)
	opts := buildClusterOpts{
		push:            push,
		cluster:         cluster,
		policy:          defaultTrafficPolicy,
		port:            port,
		serviceAccounts: nil,
		istioMtlsSni:    "",
		clusterMode:     DefaultClusterMode,
		direction:       direction,
		proxy:           proxy,
		meshExternal:    meshExternal,
	}
	applyTrafficPolicy(opts, proxy)
	return cluster
}

func buildDefaultTrafficPolicy(push *model.PushContext, discoveryType apiv2.Cluster_DiscoveryType) *networking.TrafficPolicy {
	lbPolicy := DefaultLbType
	if discoveryType == apiv2.Cluster_ORIGINAL_DST {
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
					Seconds: push.Mesh.ConnectTimeout.Seconds,
					Nanos:   push.Mesh.ConnectTimeout.Nanos,
				},
			},
		},
	}
}

func altStatName(statPattern string, host string, subset string, port *model.Port, attributes model.ServiceAttributes) string {
	name := strings.ReplaceAll(statPattern, serviceStatPattern, shortHostName(host, attributes))
	name = strings.ReplaceAll(name, serviceFQDNStatPattern, host)
	name = strings.ReplaceAll(name, subsetNameStatPattern, subset)
	name = strings.ReplaceAll(name, servicePortStatPattern, strconv.Itoa(port.Port))
	name = strings.ReplaceAll(name, servicePortNameStatPattern, port.Name)
	return name
}

// shotHostName removes the domain from kubernetes hosts. For other hosts like VMs, this method does not do any thing.
func shortHostName(host string, attributes model.ServiceAttributes) string {
	if attributes.ServiceRegistry == string(serviceregistry.Kubernetes) {
		return fmt.Sprintf("%s.%s", attributes.Name, attributes.Namespace)
	}
	return host
}

func lbPolicyClusterProvided(proxy *model.Proxy) apiv2.Cluster_LbPolicy {
	if util.IsIstioVersionGE13(proxy) {
		return apiv2.Cluster_CLUSTER_PROVIDED
	}
	return apiv2.Cluster_ORIGINAL_DST_LB
}
