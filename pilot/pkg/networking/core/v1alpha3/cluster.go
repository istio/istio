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
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	apiv2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	v2Cluster "github.com/envoyproxy/go-control-plane/envoy/api/v2/cluster"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	envoy_type "github.com/envoyproxy/go-control-plane/envoy/type"
	"github.com/gogo/protobuf/types"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/log"
)

const (
	// DefaultLbType set to round robin
	DefaultLbType = networking.LoadBalancerSettings_ROUND_ROBIN

	// ManagementClusterHostname indicates the hostname used for building inbound clusters for management ports
	ManagementClusterHostname = "mgmtCluster"
)

var (
	defaultInboundCircuitBreakerThresholds  = v2Cluster.CircuitBreakers_Thresholds{}
	defaultOutboundCircuitBreakerThresholds = v2Cluster.CircuitBreakers_Thresholds{
		// DefaultMaxRetries specifies the default for the Envoy circuit breaker parameter max_retries. This
		// defines the maximum number of parallel retries a given Envoy will allow to the upstream cluster. Envoy defaults
		// this value to 3, however that has shown to be insufficient during periods of pod churn (e.g. rolling updates),
		// where multiple endpoints in a cluster are terminated. In these scenarios the circuit breaker can kick
		// in before Pilot is able to deliver an updated endpoint list to Envoy, leading to client-facing 503s.
		MaxRetries: &types.UInt32Value{Value: 1024},
	}
)

// GetDefaultCircuitBreakerThresholds returns a copy of the default circuit breaker thresholds for the given traffic direction.
func GetDefaultCircuitBreakerThresholds(direction model.TrafficDirection) *v2Cluster.CircuitBreakers_Thresholds {
	if direction == model.TrafficDirectionInbound {
		thresholds := defaultInboundCircuitBreakerThresholds
		return &thresholds
	}
	thresholds := defaultOutboundCircuitBreakerThresholds
	return &thresholds
}

// TODO: Need to do inheritance of DestRules based on domain suffix match

// BuildClusters returns the list of clusters for the given proxy. This is the CDS output
// For outbound: Cluster for each service/subset hostname or cidr with SNI set to service hostname
// Cluster type based on resolution
// For inbound (sidecar only): Cluster for each inbound endpoint port and for each service port
func (configgen *ConfigGeneratorImpl) BuildClusters(env *model.Environment, proxy *model.Proxy, push *model.PushContext) ([]*apiv2.Cluster, error) {
	clusters := make([]*apiv2.Cluster, 0)
	instances := proxy.ServiceInstances

	// compute the proxy's locality. See if we have a CDS cache for that locality.
	// If not, compute one.
	locality := proxy.Locality
	if locality == nil {
		// Get the locality from the proxy's service instances.
		// We expect all instances to have the same locality. So its enough to look at the first instance
		if len(instances) > 0 {
			locality = util.ConvertLocality(instances[0].Endpoint.Locality)
		}
	}

	// Used as key to lookup in the cache
	localityAsString := util.NoProxyLocality
	if locality != nil {
		localityAsString = fmt.Sprintf("%s/%s/%s", locality.Region, locality.Zone, locality.SubZone)
	}

	switch proxy.Type {
	case model.SidecarProxy:
		sidecarScope := proxy.SidecarScope
		recomputeOutboundClusters := true
		if configgen.CanUsePrecomputedCDS(proxy) {
			if sidecarScope != nil && sidecarScope.CDSOutboundClusters != nil {
				sidecarScope.CDSMutex.RLock()
				if sidecarScope.CDSOutboundClusters[localityAsString] != nil {
					clusters = append(clusters, sidecarScope.CDSOutboundClusters[localityAsString]...)
					recomputeOutboundClusters = false
				} else {
					// Take the default output. We will modify this for the locality and cache later
					clusters = append(clusters, sidecarScope.CDSOutboundClusters[util.NoProxyLocality]...)
				}
				sidecarScope.CDSMutex.RUnlock()

				// If there was a cache miss, then create a locality specific cds output
				// based on the noproxylocality output.
				if recomputeOutboundClusters && locality != nil {
					// NOTE: the lock should be taken before the cloning to avoid
					// multiple threads creating N copies of outputs for same locality
					// This does mean that during heavy contention, several threads may
					// be waiting on this lock to update the locality cache
					sidecarScope.CDSMutex.Lock()

					// Check if during the period we waited to acquire the lock
					// another thread built the set of clusters. If it did, then dont bother
					// to recompute
					if sidecarScope.CDSOutboundClusters[localityAsString] == nil {
						for i, cluster := range clusters {
							// update the locality settings only if the cluster has
							// outlier detection settings
							if cluster.LoadAssignment != nil && cluster.OutlierDetection != nil {
								clone := util.CloneCluster(cluster)
								ApplyLocalityLBSetting(locality, clone.LoadAssignment, env.Mesh.LocalityLbSetting)
								clusters[i] = &clone
							}
						}
						// cache the output
						sidecarScope.CDSOutboundClusters[localityAsString] = clusters
					}
					sidecarScope.CDSMutex.Unlock()
				}
			}
		}

		if recomputeOutboundClusters {
			clusters = append(clusters, configgen.buildOutboundClusters(env, proxy, push)...)
		}

		// Let ServiceDiscovery decide which IP and Port are used for management if
		// there are multiple IPs
		managementPorts := make([]*model.Port, 0)
		for _, ip := range proxy.IPAddresses {
			managementPorts = append(managementPorts, env.ManagementPorts(ip)...)
		}
		clusters = append(clusters, configgen.buildInboundClusters(env, proxy, push, instances, managementPorts)...)

	default: // Gateways
		recomputeOutboundClusters := true
		if configgen.CanUsePrecomputedCDS(proxy) {
			if configgen.PrecomputedOutboundClustersForGateways != nil {
				configgen.gatewayCDSMutex.RLock()
				if configgen.PrecomputedOutboundClustersForGateways[proxy.ConfigNamespace] != nil {
					if configgen.PrecomputedOutboundClustersForGateways[proxy.ConfigNamespace][localityAsString] != nil {
						clusters = append(clusters, configgen.PrecomputedOutboundClustersForGateways[proxy.ConfigNamespace][localityAsString]...)
						recomputeOutboundClusters = false
					} else {
						clusters = append(clusters, configgen.PrecomputedOutboundClustersForGateways[proxy.ConfigNamespace][localityAsString]...)
					}
				}
				configgen.gatewayCDSMutex.RUnlock()

				if recomputeOutboundClusters && locality != nil {
					// NOTE: the lock should be taken before the cloning to avoid
					// multiple threads creating N copies of outputs for same locality
					// This does mean that during heavy contention, several threads may
					// be waiting on this lock to update the locality cache
					configgen.gatewayCDSMutex.Lock()

					// Check if during the period we waited to acquire the lock
					// another thread built the set of clusters. If it did, then dont bother
					// to recompute
					if configgen.PrecomputedOutboundClustersForGateways[proxy.ConfigNamespace][localityAsString] == nil {
						for i, cluster := range clusters {
							// update the locality settings only if the cluster has
							// outlier detection settings
							if cluster.LoadAssignment != nil && cluster.OutlierDetection != nil {
								clone := util.CloneCluster(cluster)
								ApplyLocalityLBSetting(locality, clone.LoadAssignment, env.Mesh.LocalityLbSetting)
								clusters[i] = &clone
							}
						}
						// cache the output
						configgen.PrecomputedOutboundClustersForGateways[proxy.ConfigNamespace][localityAsString] = clusters
					}
					configgen.gatewayCDSMutex.Unlock()
				}
			}
		}

		if recomputeOutboundClusters {
			clusters = append(clusters, configgen.buildOutboundClusters(env, proxy, push)...)
		}
		if proxy.Type == model.Router && proxy.GetRouterMode() == model.SniDnatRouter {
			clusters = append(clusters, configgen.buildOutboundSniDnatClusters(env, proxy, push)...)
		}
	}

	// Add a blackhole and passthrough cluster for catching traffic to unresolved routes
	// DO NOT CALL PLUGINS for these two clusters.
	clusters = append(clusters, buildBlackHoleCluster())
	clusters = append(clusters, buildDefaultPassthroughCluster())

	return normalizeClusters(push, proxy, clusters), nil
}

// resolves cluster name conflicts. there can be duplicate cluster names if there are conflicting service definitions.
// for any clusters that share the same name the first cluster is kept and the others are discarded.
func normalizeClusters(push *model.PushContext, proxy *model.Proxy, clusters []*apiv2.Cluster) []*apiv2.Cluster {
	have := make(map[string]bool)
	out := make([]*apiv2.Cluster, 0, len(clusters))
	for _, cluster := range clusters {
		if !have[cluster.Name] {
			out = append(out, cluster)
		} else {
			push.Add(model.DuplicatedClusters, cluster.Name, proxy,
				fmt.Sprintf("Duplicate cluster %s found while pushing CDS", cluster.Name))
		}
		have[cluster.Name] = true
	}
	return out
}

func (configgen *ConfigGeneratorImpl) buildOutboundClusters(env *model.Environment, proxy *model.Proxy, push *model.PushContext) []*apiv2.Cluster {
	clusters := make([]*apiv2.Cluster, 0)

	inputParams := &plugin.InputParams{
		Env:  env,
		Push: push,
		Node: proxy,
	}
	networkView := model.GetNetworkView(proxy)

	for _, service := range push.Services(proxy) {
		config := push.DestinationRule(proxy, service)
		for _, port := range service.Ports {
			if port.Protocol == model.ProtocolUDP {
				continue
			}
			inputParams.Service = service
			inputParams.Port = port

			lbEndpoints := buildLocalityLbEndpoints(env, networkView, service, port.Port, nil)

			// create default cluster
			discoveryType := convertResolution(service.Resolution)
			clusterName := model.BuildSubsetKey(model.TrafficDirectionOutbound, "", service.Hostname, port.Port)
			serviceAccounts := env.ServiceAccounts.GetIstioServiceAccounts(service.Hostname, []int{port.Port})
			defaultCluster := buildDefaultCluster(env, clusterName, discoveryType, lbEndpoints, model.TrafficDirectionOutbound)

			updateEds(defaultCluster)
			setUpstreamProtocol(defaultCluster, port)
			clusters = append(clusters, defaultCluster)

			if config != nil {
				destinationRule := config.Spec.(*networking.DestinationRule)
				defaultSni := model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, "", service.Hostname, port.Port)
				applyTrafficPolicy(env, defaultCluster, destinationRule.TrafficPolicy, port, serviceAccounts,
					defaultSni, DefaultClusterMode, model.TrafficDirectionOutbound)

				for _, subset := range destinationRule.Subsets {
					inputParams.Subset = subset.Name
					subsetClusterName := model.BuildSubsetKey(model.TrafficDirectionOutbound, subset.Name, service.Hostname, port.Port)
					defaultSni := model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, subset.Name, service.Hostname, port.Port)

					// clusters with discovery type STATIC, STRICT_DNS or LOGICAL_DNS rely on cluster.hosts field
					// ServiceEntry's need to filter hosts based on subset.labels in order to perform weighted routing
					if discoveryType != apiv2.Cluster_EDS && len(subset.Labels) != 0 {
						lbEndpoints = buildLocalityLbEndpoints(env, networkView, service, port.Port, []model.Labels{subset.Labels})
					}
					subsetCluster := buildDefaultCluster(env, subsetClusterName, discoveryType, lbEndpoints, model.TrafficDirectionOutbound)
					updateEds(subsetCluster)
					setUpstreamProtocol(subsetCluster, port)
					applyTrafficPolicy(env, subsetCluster, destinationRule.TrafficPolicy, port, serviceAccounts, defaultSni,
						DefaultClusterMode, model.TrafficDirectionOutbound)
					applyTrafficPolicy(env, subsetCluster, subset.TrafficPolicy, port, serviceAccounts, defaultSni,
						DefaultClusterMode, model.TrafficDirectionOutbound)
					// call plugins
					for _, p := range configgen.Plugins {
						p.OnOutboundCluster(inputParams, subsetCluster)
					}
					clusters = append(clusters, subsetCluster)
				}
			}

			// call plugins for the default cluster
			for _, p := range configgen.Plugins {
				p.OnOutboundCluster(inputParams, defaultCluster)
			}
		}
	}

	return clusters
}

// SniDnat clusters do not have any TLS setting, as they simply forward traffic to upstream
func (configgen *ConfigGeneratorImpl) buildOutboundSniDnatClusters(env *model.Environment, proxy *model.Proxy, push *model.PushContext) []*apiv2.Cluster {
	clusters := make([]*apiv2.Cluster, 0)

	networkView := model.GetNetworkView(proxy)

	for _, service := range push.Services(proxy) {
		config := push.DestinationRule(proxy, service)
		for _, port := range service.Ports {
			if port.Protocol == model.ProtocolUDP {
				continue
			}
			lbEndpoints := buildLocalityLbEndpoints(env, networkView, service, port.Port, nil)

			// create default cluster
			discoveryType := convertResolution(service.Resolution)

			clusterName := model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, "", service.Hostname, port.Port)
			defaultCluster := buildDefaultCluster(env, clusterName, discoveryType, lbEndpoints, model.TrafficDirectionOutbound)
			defaultCluster.TlsContext = nil
			updateEds(defaultCluster)
			clusters = append(clusters, defaultCluster)

			if config != nil {
				destinationRule := config.Spec.(*networking.DestinationRule)
				applyTrafficPolicy(env, defaultCluster, destinationRule.TrafficPolicy, port, nil, "",
					SniDnatClusterMode, model.TrafficDirectionOutbound)

				for _, subset := range destinationRule.Subsets {
					subsetClusterName := model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, subset.Name, service.Hostname, port.Port)
					// clusters with discovery type STATIC, STRICT_DNS or LOGICAL_DNS rely on cluster.hosts field
					// ServiceEntry's need to filter hosts based on subset.labels in order to perform weighted routing
					if discoveryType != apiv2.Cluster_EDS && len(subset.Labels) != 0 {
						lbEndpoints = buildLocalityLbEndpoints(env, networkView, service, port.Port, []model.Labels{subset.Labels})
					}
					subsetCluster := buildDefaultCluster(env, subsetClusterName, discoveryType, lbEndpoints, model.TrafficDirectionOutbound)
					subsetCluster.TlsContext = nil
					updateEds(subsetCluster)
					applyTrafficPolicy(env, subsetCluster, destinationRule.TrafficPolicy, port, nil, "",
						SniDnatClusterMode, model.TrafficDirectionOutbound)
					applyTrafficPolicy(env, subsetCluster, subset.TrafficPolicy, port, nil, "",
						SniDnatClusterMode, model.TrafficDirectionOutbound)
					clusters = append(clusters, subsetCluster)
				}
			}
		}
	}

	return clusters
}

func updateEds(cluster *apiv2.Cluster) {
	if cluster.Type != apiv2.Cluster_EDS {
		return
	}
	cluster.EdsClusterConfig = &apiv2.Cluster_EdsClusterConfig{
		ServiceName: cluster.Name,
		EdsConfig: &core.ConfigSource{
			ConfigSourceSpecifier: &core.ConfigSource_Ads{
				Ads: &core.AggregatedConfigSource{},
			},
		},
	}
}

func buildLocalityLbEndpoints(env *model.Environment, proxyNetworkView map[string]bool, service *model.Service,
	port int, labels model.LabelsCollection) []endpoint.LocalityLbEndpoints {

	if service.Resolution != model.DNSLB {
		return nil
	}

	instances, err := env.InstancesByPort(service.Hostname, port, labels)
	if err != nil {
		log.Errorf("failed to retrieve instances for %s: %v", service.Hostname, err)
		return nil
	}

	lbEndpoints := make(map[string][]endpoint.LbEndpoint)
	for _, instance := range instances {
		// Only send endpoints from the networks in the network view requested by the proxy.
		// The default network view assigned to the Proxy is the UnnamedNetwork (""), which matches
		// the default network assigned to endpoints that don't have an explicit network
		if !proxyNetworkView[instance.Endpoint.Network] {
			// Endpoint's network doesn't match the set of networks that the proxy wants to see.
			continue
		}
		host := util.BuildAddress(instance.Endpoint.Address, uint32(instance.Endpoint.Port))
		ep := endpoint.LbEndpoint{
			HostIdentifier: &endpoint.LbEndpoint_Endpoint{
				Endpoint: &endpoint.Endpoint{
					Address: &host,
				},
			},
			LoadBalancingWeight: &types.UInt32Value{
				Value: 1,
			},
		}
		if instance.Endpoint.LbWeight > 0 {
			ep.LoadBalancingWeight.Value = instance.Endpoint.LbWeight
		}
		locality := instance.GetLocality()
		lbEndpoints[locality] = append(lbEndpoints[locality], ep)
	}

	LocalityLbEndpoints := make([]endpoint.LocalityLbEndpoints, len(lbEndpoints))

	for locality, eps := range lbEndpoints {
		LocalityLbEndpoints = append(LocalityLbEndpoints, endpoint.LocalityLbEndpoints{
			Locality:    util.ConvertLocality(locality),
			LbEndpoints: eps,
		})
	}

	return util.LocalityLbWeightNormalize(LocalityLbEndpoints)
}

func buildInboundLocalityLbEndpoints(bind string, port int) []endpoint.LocalityLbEndpoints {
	address := util.BuildAddress(bind, uint32(port))
	lbEndpoint := endpoint.LbEndpoint{
		HostIdentifier: &endpoint.LbEndpoint_Endpoint{
			Endpoint: &endpoint.Endpoint{
				Address: &address,
			},
		},
	}
	return []endpoint.LocalityLbEndpoints{
		{
			LbEndpoints: []endpoint.LbEndpoint{lbEndpoint},
		},
	}
}

func (configgen *ConfigGeneratorImpl) buildInboundClusters(env *model.Environment, proxy *model.Proxy,
	push *model.PushContext, instances []*model.ServiceInstance, managementPorts []*model.Port) []*apiv2.Cluster {

	clusters := make([]*apiv2.Cluster, 0)

	// The inbound clusters for a node depends on whether the node has a SidecarScope with inbound listeners
	// or not. If the node has a sidecarscope with ingress listeners, we only return clusters corresponding
	// to those listeners i.e. clusters made out of the defaultEndpoint field.
	// If the node has no sidecarScope and has interception mode set to NONE, then we should skip the inbound
	// clusters, because there would be no corresponding inbound listeners
	sidecarScope := proxy.SidecarScope
	noneMode := proxy.GetInterceptionMode() == model.InterceptionNone

	if sidecarScope == nil || !sidecarScope.HasCustomIngressListeners {
		// No user supplied sidecar scope or the user supplied one has no ingress listeners

		// We should not create inbound listeners in NONE mode based on the service instances
		// Doing so will prevent the workloads from starting as they would be listening on the same port
		// Users are required to provide the sidecar config to define the inbound listeners
		if noneMode {
			return nil
		}

		for _, instance := range instances {
			pluginParams := &plugin.InputParams{
				Env:             env,
				Node:            proxy,
				ServiceInstance: instance,
				Port:            instance.Endpoint.ServicePort,
				Push:            push,
				Bind:            LocalhostAddress,
			}
			localCluster := configgen.buildInboundClusterForPortOrUDS(pluginParams)
			clusters = append(clusters, localCluster)
		}

		// Add a passthrough cluster for traffic to management ports (health check ports)
		for _, port := range managementPorts {
			clusterName := model.BuildSubsetKey(model.TrafficDirectionInbound, port.Name,
				ManagementClusterHostname, port.Port)
			localityLbEndpoints := buildInboundLocalityLbEndpoints(LocalhostAddress, port.Port)
			mgmtCluster := buildDefaultCluster(env, clusterName, apiv2.Cluster_STATIC, localityLbEndpoints,
				model.TrafficDirectionInbound)
			setUpstreamProtocol(mgmtCluster, port)
			clusters = append(clusters, mgmtCluster)
		}
	} else {
		if instances == nil || len(instances) == 0 {
			return clusters
		}
		rule := sidecarScope.Config.Spec.(*networking.Sidecar)
		for _, ingressListener := range rule.Ingress {
			// LDS would have setup the inbound clusters
			// as inbound|portNumber|portName|Hostname
			listenPort := &model.Port{
				Port:     int(ingressListener.Port.Number),
				Protocol: model.ParseProtocol(ingressListener.Port.Protocol),
				Name:     ingressListener.Port.Name,
			}

			// When building an inbound cluster for the ingress listener, we take the defaultEndpoint specified
			// by the user and parse it into host:port or a unix domain socket
			// The default endpoint can be 127.0.0.1:port or :port or unix domain socket
			endpointAddress := LocalhostAddress
			port := 0
			var err error
			if strings.HasPrefix(ingressListener.DefaultEndpoint, model.UnixAddressPrefix) {
				// this is a UDS endpoint. assign it as is
				endpointAddress = ingressListener.DefaultEndpoint
			} else {
				// parse the ip, port. Validation guarantees presence of :
				parts := strings.Split(ingressListener.DefaultEndpoint, ":")
				if port, err = strconv.Atoi(parts[1]); err != nil {
					continue
				}
			}

			// Find the service instance that corresponds to this ingress listener by looking
			// for a service instance that either matches this ingress port or one that has
			// a port with same name as this ingress port
			instance := configgen.findServiceInstanceForIngressListener(instances, ingressListener)

			if instance == nil {
				// We didn't find a matching instance
				continue
			}

			// Update the values here so that the plugins use the right ports
			// uds values
			// TODO: all plugins need to be updated to account for the fact that
			// the port may be 0 but bind may have a UDS value
			instance.Endpoint.Address = endpointAddress
			instance.Endpoint.ServicePort = listenPort
			instance.Endpoint.Port = port

			pluginParams := &plugin.InputParams{
				Env:             env,
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

func (configgen *ConfigGeneratorImpl) findServiceInstanceForIngressListener(instances []*model.ServiceInstance,
	ingressListener *networking.IstioIngressListener) *model.ServiceInstance {
	var instance *model.ServiceInstance
	// Search by port
	for _, realInstance := range instances {
		if realInstance.Endpoint.Port == int(ingressListener.Port.Number) {
			instance = &model.ServiceInstance{
				Endpoint:       realInstance.Endpoint,
				Service:        realInstance.Service,
				Labels:         realInstance.Labels,
				ServiceAccount: realInstance.ServiceAccount,
			}
			return instance
		}
	}

	// If the port number does not match, the user might have specified a
	// UDS socket with port number 0. So search by name
	for _, realInstance := range instances {
		for _, iport := range realInstance.Service.Ports {
			if iport.Name == ingressListener.Port.Name {
				instance = &model.ServiceInstance{
					Endpoint:       realInstance.Endpoint,
					Service:        realInstance.Service,
					Labels:         realInstance.Labels,
					ServiceAccount: realInstance.ServiceAccount,
				}
				return instance
			}
		}
	}

	return instance
}

func (configgen *ConfigGeneratorImpl) buildInboundClusterForPortOrUDS(pluginParams *plugin.InputParams) *apiv2.Cluster {
	instance := pluginParams.ServiceInstance
	clusterName := model.BuildSubsetKey(model.TrafficDirectionInbound, instance.Endpoint.ServicePort.Name,
		instance.Service.Hostname, instance.Endpoint.ServicePort.Port)
	localityLbEndpoints := buildInboundLocalityLbEndpoints(pluginParams.Bind, instance.Endpoint.Port)
	localCluster := buildDefaultCluster(pluginParams.Env, clusterName, apiv2.Cluster_STATIC, localityLbEndpoints,
		model.TrafficDirectionInbound)
	setUpstreamProtocol(localCluster, instance.Endpoint.ServicePort)
	// call plugins
	for _, p := range configgen.Plugins {
		p.OnInboundCluster(pluginParams, localCluster)
	}

	// When users specify circuit breakers, they need to be set on the receiver end
	// (server side) as well as client side, so that the server has enough capacity
	// (not the defaults) to handle the increased traffic volume
	// TODO: This is not foolproof - if instance is part of multiple services listening on same port,
	// choice of inbound cluster is arbitrary. So the connection pool settings may not apply cleanly.
	config := pluginParams.Push.DestinationRule(pluginParams.Node, instance.Service)
	if config != nil {
		destinationRule := config.Spec.(*networking.DestinationRule)
		if destinationRule.TrafficPolicy != nil {
			// only connection pool settings make sense on the inbound path.
			// upstream TLS settings/outlier detection/load balancer don't apply here.
			applyConnectionPool(pluginParams.Env, localCluster, destinationRule.TrafficPolicy.ConnectionPool,
				model.TrafficDirectionInbound)
		}
	}
	return localCluster
}

func convertResolution(resolution model.Resolution) apiv2.Cluster_DiscoveryType {
	switch resolution {
	case model.ClientSideLB:
		return apiv2.Cluster_EDS
	case model.DNSLB:
		return apiv2.Cluster_STRICT_DNS
	case model.Passthrough:
		return apiv2.Cluster_ORIGINAL_DST
	default:
		return apiv2.Cluster_EDS
	}
}

// conditionallyConvertToIstioMtls fills key cert fields for all TLSSettings when the mode is `ISTIO_MUTUAL`.
func conditionallyConvertToIstioMtls(tls *networking.TLSSettings, serviceAccounts []string, sni string) *networking.TLSSettings {
	if tls == nil {
		return nil
	}
	if tls.Mode == networking.TLSSettings_ISTIO_MUTUAL {
		// Use client provided SNI if set. Otherwise, overwrite with the auto generated SNI
		// user specified SNIs in the istio mtls settings are useful when routing via gateways
		sniToUse := tls.Sni
		if len(sniToUse) == 0 {
			sniToUse = sni
		}
		return buildIstioMutualTLS(serviceAccounts, sniToUse)
	}
	return tls
}

// buildIstioMutualTLS returns a `TLSSettings` for ISTIO_MUTUAL mode.
func buildIstioMutualTLS(serviceAccounts []string, sni string) *networking.TLSSettings {
	return &networking.TLSSettings{
		Mode:              networking.TLSSettings_ISTIO_MUTUAL,
		CaCertificates:    path.Join(model.AuthCertsPath, model.RootCertFilename),
		ClientCertificate: path.Join(model.AuthCertsPath, model.CertChainFilename),
		PrivateKey:        path.Join(model.AuthCertsPath, model.KeyFilename),
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
				switch selector := p.Port.Port.(type) {
				case *networking.PortSelector_Name:
					if port.Name == selector.Name {
						foundPort = true
					}
				case *networking.PortSelector_Number:
					if uint32(port.Port) == selector.Number {
						foundPort = true
					}
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

// FIXME: There are too many variables here. Create a clusterOpts struct and stick the values in it, just like
// listenerOpts
func applyTrafficPolicy(env *model.Environment, cluster *apiv2.Cluster, policy *networking.TrafficPolicy,
	port *model.Port, serviceAccounts []string, defaultSni string, clusterMode ClusterMode, direction model.TrafficDirection) {
	connectionPool, outlierDetection, loadBalancer, tls := SelectTrafficPolicyComponents(policy, port)

	applyConnectionPool(env, cluster, connectionPool, direction)
	applyOutlierDetection(cluster, outlierDetection)
	applyLoadBalancer(cluster, loadBalancer)
	if clusterMode != SniDnatClusterMode {
		tls = conditionallyConvertToIstioMtls(tls, serviceAccounts, defaultSni)
		applyUpstreamTLSSettings(env, cluster, tls)
	}
}

// FIXME: there isn't a way to distinguish between unset values and zero values
func applyConnectionPool(env *model.Environment, cluster *apiv2.Cluster, settings *networking.ConnectionPoolSettings, direction model.TrafficDirection) {
	if settings == nil {
		return
	}

	threshold := GetDefaultCircuitBreakerThresholds(direction)
	if settings.Http != nil {
		if settings.Http.Http2MaxRequests > 0 {
			// Envoy only applies MaxRequests in HTTP/2 clusters
			threshold.MaxRequests = &types.UInt32Value{Value: uint32(settings.Http.Http2MaxRequests)}
		}
		if settings.Http.Http1MaxPendingRequests > 0 {
			// Envoy only applies MaxPendingRequests in HTTP/1.1 clusters
			threshold.MaxPendingRequests = &types.UInt32Value{Value: uint32(settings.Http.Http1MaxPendingRequests)}
		}

		if settings.Http.MaxRequestsPerConnection > 0 {
			cluster.MaxRequestsPerConnection = &types.UInt32Value{Value: uint32(settings.Http.MaxRequestsPerConnection)}
		}

		// FIXME: zero is a valid value if explicitly set, otherwise we want to use the default
		if settings.Http.MaxRetries > 0 {
			threshold.MaxRetries = &types.UInt32Value{Value: uint32(settings.Http.MaxRetries)}
		}
	}

	if settings.Tcp != nil {
		if settings.Tcp.ConnectTimeout != nil {
			cluster.ConnectTimeout = util.GogoDurationToDuration(settings.Tcp.ConnectTimeout)
		}

		if settings.Tcp.MaxConnections > 0 {
			threshold.MaxConnections = &types.UInt32Value{Value: uint32(settings.Tcp.MaxConnections)}
		}

		applyTCPKeepalive(env, cluster, settings)
	}

	cluster.CircuitBreakers = &v2Cluster.CircuitBreakers{
		Thresholds: []*v2Cluster.CircuitBreakers_Thresholds{threshold},
	}
}

func applyTCPKeepalive(env *model.Environment, cluster *apiv2.Cluster, settings *networking.ConnectionPoolSettings) {
	var keepaliveProbes uint32
	var keepaliveTime *types.Duration
	var keepaliveInterval *types.Duration
	isTCPKeepaliveSet := false

	// Apply mesh wide TCP keepalive.
	if env.Mesh.TcpKeepalive != nil {
		keepaliveProbes = env.Mesh.TcpKeepalive.Probes
		keepaliveTime = env.Mesh.TcpKeepalive.Time
		keepaliveInterval = env.Mesh.TcpKeepalive.Interval
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
		upstreamConnectionOptions.TcpKeepalive.KeepaliveProbes = &types.UInt32Value{Value: keepaliveProbes}
	}

	if keepaliveTime != nil {
		upstreamConnectionOptions.TcpKeepalive.KeepaliveTime = &types.UInt32Value{Value: uint32(keepaliveTime.Seconds)}
	}

	if keepaliveInterval != nil {
		upstreamConnectionOptions.TcpKeepalive.KeepaliveInterval = &types.UInt32Value{Value: uint32(keepaliveInterval.Seconds)}
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
		out.BaseEjectionTime = outlier.BaseEjectionTime
	}
	if outlier.ConsecutiveErrors > 0 {
		// Only listen to gateway errors, see https://github.com/istio/api/pull/617
		out.EnforcingConsecutiveGatewayFailure = &types.UInt32Value{Value: uint32(100)} // defaults to 0
		out.EnforcingConsecutive_5Xx = &types.UInt32Value{Value: uint32(0)}             // defaults to 100
		out.ConsecutiveGatewayFailure = &types.UInt32Value{Value: uint32(outlier.ConsecutiveErrors)}
	}
	if outlier.Interval != nil {
		out.Interval = outlier.Interval
	}
	if outlier.MaxEjectionPercent > 0 {
		out.MaxEjectionPercent = &types.UInt32Value{Value: uint32(outlier.MaxEjectionPercent)}
	}

	cluster.OutlierDetection = out

	if outlier.MinHealthPercent > 0 {
		if cluster.CommonLbConfig == nil {
			cluster.CommonLbConfig = &apiv2.Cluster_CommonLbConfig{}
		}
		cluster.CommonLbConfig.HealthyPanicThreshold = &envoy_type.Percent{Value: float64(outlier.MinHealthPercent)}
	}
}

func applyLoadBalancer(cluster *apiv2.Cluster, lb *networking.LoadBalancerSettings) {
	if lb == nil {
		return
	}

	// TODO: MAGLEV
	switch lb.GetSimple() {
	case networking.LoadBalancerSettings_LEAST_CONN:
		cluster.LbPolicy = apiv2.Cluster_LEAST_REQUEST
	case networking.LoadBalancerSettings_RANDOM:
		cluster.LbPolicy = apiv2.Cluster_RANDOM
	case networking.LoadBalancerSettings_ROUND_ROBIN:
		cluster.LbPolicy = apiv2.Cluster_ROUND_ROBIN
	case networking.LoadBalancerSettings_PASSTHROUGH:
		cluster.LbPolicy = apiv2.Cluster_ORIGINAL_DST_LB
		cluster.Type = apiv2.Cluster_ORIGINAL_DST
	}

	// DO not do if else here. since lb.GetSimple returns a enum value (not pointer).

	consistentHash := lb.GetConsistentHash()
	if consistentHash != nil {
		cluster.LbPolicy = apiv2.Cluster_RING_HASH
		cluster.LbConfig = &apiv2.Cluster_RingHashLbConfig_{
			RingHashLbConfig: &apiv2.Cluster_RingHashLbConfig{
				MinimumRingSize: &types.UInt64Value{Value: consistentHash.GetMinimumRingSize()},
			},
		}
	}
}

func ApplyLocalityLBSetting(
	locality *core.Locality,
	loadAssignment *apiv2.ClusterLoadAssignment,
	localityLB *meshconfig.LocalityLoadBalancerSetting,
) {
	if locality == nil || loadAssignment == nil {
		return
	}

	// one of Distribute or Failover settings can be applied.
	if localityLB.GetDistribute() != nil {
		applyLocalityWeight(locality, loadAssignment, localityLB.GetDistribute())
	} else if localityLB.GetFailover() != nil {
		applyLocalityFailover(locality, loadAssignment, localityLB.GetFailover())
	}
}

// set locality loadbalancing weight
func applyLocalityWeight(
	locality *core.Locality,
	loadAssignment *apiv2.ClusterLoadAssignment,
	distribute []*meshconfig.LocalityLoadBalancerSetting_Distribute) {
	if distribute == nil {
		return
	}

	// Support Locality weighted load balancing
	// (https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/load_balancing/locality_weight.html)
	// by providing weights in LocalityLbEndpoints via load_balancing_weight.
	// By setting weights across different localities, it can allow
	// Envoy to weight assignments across different zones and geographical locations.
	for _, localityWeightSetting := range distribute {
		if localityWeightSetting != nil &&
			util.LocalityMatch(locality, localityWeightSetting.From) {
			misMatched := map[int]struct{}{}
			for i := range loadAssignment.Endpoints {
				misMatched[i] = struct{}{}
			}
			for locality, weight := range localityWeightSetting.To {
				// index -> original weight
				destLocMap := map[int]uint32{}
				totalWeight := uint32(0)
				for i, ep := range loadAssignment.Endpoints {
					if _, exist := misMatched[i]; exist {
						if util.LocalityMatch(ep.Locality, locality) {
							delete(misMatched, i)
							destLocMap[i] = ep.LoadBalancingWeight.Value
							totalWeight += destLocMap[i]
						}
					}
				}
				// in case wildcard dest matching multi groups of endpoints
				// the load balancing weight for a locality is divided by the sum of the weights of all localities
				for index, originalWeight := range destLocMap {
					weight := float64(originalWeight*weight) / float64(totalWeight)
					loadAssignment.Endpoints[index].LoadBalancingWeight = &types.UInt32Value{
						Value: uint32(math.Ceil(weight)),
					}
				}
			}

			// remove groups of endpoints in a locality that miss matched
			for i := range misMatched {
				loadAssignment.Endpoints[i].LbEndpoints = nil
			}
			break
		}
	}
}

// set locality loadbalancing priority
func applyLocalityFailover(
	locality *core.Locality,
	loadAssignment *apiv2.ClusterLoadAssignment,
	failover []*meshconfig.LocalityLoadBalancerSetting_Failover) {
	// key is priority, value is the index of the LocalityLbEndpoints in ClusterLoadAssignment
	priorityMap := map[int][]int{}

	// 1. calculate the LocalityLbEndpoints.Priority compared with proxy locality
	for i, localityEndpoint := range loadAssignment.Endpoints {
		priority := util.LbPriority(locality, localityEndpoint.Locality)
		// region not match, apply failover settings
		if priority == 3 {
			for _, failoverSetting := range failover {
				if failoverSetting.From == locality.Region {
					if localityEndpoint.Locality.Region != failoverSetting.To {
						priority = 4
					}
					break
				}
			}
		}
		loadAssignment.Endpoints[i].Priority = uint32(priority)
		priorityMap[priority] = append(priorityMap[priority], i)
	}

	// since Priorities should range from 0 (highest) to N (lowest) without skipping.
	// 2. adjust the priorities in order
	// 2.1 sort all priorities in increasing order.
	priorities := []int{}
	for priority := range priorityMap {
		priorities = append(priorities, priority)
	}
	sort.Ints(priorities)
	// 2.2 adjust LocalityLbEndpoints priority
	// if the index and value of priorities array is not equal.
	for i, priority := range priorities {
		if i != priority {
			// the LocalityLbEndpoints index in ClusterLoadAssignment.Endpoints
			for index := range priorityMap[priority] {
				loadAssignment.Endpoints[index].Priority = uint32(i)
			}
		}
	}

}

func applyUpstreamTLSSettings(env *model.Environment, cluster *apiv2.Cluster, tls *networking.TLSSettings) {
	if tls == nil {
		return
	}

	var certValidationContext *auth.CertificateValidationContext
	var trustedCa *core.DataSource
	if len(tls.CaCertificates) != 0 {
		trustedCa = &core.DataSource{
			Specifier: &core.DataSource_Filename{
				Filename: tls.CaCertificates,
			},
		}
	}
	if trustedCa != nil || len(tls.SubjectAltNames) > 0 {
		certValidationContext = &auth.CertificateValidationContext{
			TrustedCa:            trustedCa,
			VerifySubjectAltName: tls.SubjectAltNames,
		}
	}

	switch tls.Mode {
	case networking.TLSSettings_DISABLE:
		// TODO: Need to make sure that authN does not override this setting
		// We remove the TlsContext because it can be written because of configmap.MTLS settings.
		cluster.TlsContext = nil
	case networking.TLSSettings_SIMPLE:
		cluster.TlsContext = &auth.UpstreamTlsContext{
			CommonTlsContext: &auth.CommonTlsContext{
				ValidationContextType: &auth.CommonTlsContext_ValidationContext{
					ValidationContext: certValidationContext,
				},
			},
			Sni: tls.Sni,
		}
		if cluster.Http2ProtocolOptions != nil {
			// This is HTTP/2 cluster, advertise it with ALPN.
			cluster.TlsContext.CommonTlsContext.AlpnProtocols = util.ALPNH2Only
		}
	case networking.TLSSettings_MUTUAL, networking.TLSSettings_ISTIO_MUTUAL:
		if tls.ClientCertificate == "" || tls.PrivateKey == "" {
			log.Errorf("failed to apply tls setting for %s: client certificate and private key must not be empty",
				cluster.Name)
			return
		}

		cluster.TlsContext = &auth.UpstreamTlsContext{
			CommonTlsContext: &auth.CommonTlsContext{},
			Sni:              tls.Sni,
		}

		// Fallback to file mount secret instead of SDS if meshConfig.sdsUdsPath isn't set or tls.mode is TLSSettings_MUTUAL.
		if env.Mesh.SdsUdsPath == "" || tls.Mode == networking.TLSSettings_MUTUAL {
			cluster.TlsContext.CommonTlsContext.ValidationContextType = &auth.CommonTlsContext_ValidationContext{
				ValidationContext: certValidationContext,
			}
			cluster.TlsContext.CommonTlsContext.TlsCertificates = []*auth.TlsCertificate{
				{
					CertificateChain: &core.DataSource{
						Specifier: &core.DataSource_Filename{
							Filename: tls.ClientCertificate,
						},
					},
					PrivateKey: &core.DataSource{
						Specifier: &core.DataSource_Filename{
							Filename: tls.PrivateKey,
						},
					},
				},
			}
		} else {
			cluster.TlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs = append(cluster.TlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs,
				model.ConstructSdsSecretConfig(model.SDSDefaultResourceName, env.Mesh.SdsUdsPath, env.Mesh.EnableSdsTokenMount, env.Mesh.SdsUseK8SSaJwt))

			cluster.TlsContext.CommonTlsContext.ValidationContextType = &auth.CommonTlsContext_CombinedValidationContext{
				CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
					DefaultValidationContext: &auth.CertificateValidationContext{VerifySubjectAltName: tls.SubjectAltNames},
					ValidationContextSdsSecretConfig: model.ConstructSdsSecretConfig(model.SDSRootResourceName, env.Mesh.SdsUdsPath,
						env.Mesh.EnableSdsTokenMount, env.Mesh.SdsUseK8SSaJwt),
				},
			}
		}

		// Set default SNI of cluster name for istio_mutual if sni is not set.
		if len(tls.Sni) == 0 && tls.Mode == networking.TLSSettings_ISTIO_MUTUAL {
			cluster.TlsContext.Sni = cluster.Name
		}
		if cluster.Http2ProtocolOptions != nil {
			// This is HTTP/2 in-mesh cluster, advertise it with ALPN.
			if tls.Mode == networking.TLSSettings_ISTIO_MUTUAL {
				cluster.TlsContext.CommonTlsContext.AlpnProtocols = util.ALPNInMeshH2
			} else {
				cluster.TlsContext.CommonTlsContext.AlpnProtocols = util.ALPNH2Only
			}
		} else if tls.Mode == networking.TLSSettings_ISTIO_MUTUAL {
			// This is in-mesh cluster, advertise it with ALPN.
			cluster.TlsContext.CommonTlsContext.AlpnProtocols = util.ALPNInMesh
		}
	}
}

func setUpstreamProtocol(cluster *apiv2.Cluster, port *model.Port) {
	if port.Protocol.IsHTTP2() {
		cluster.Http2ProtocolOptions = &core.Http2ProtocolOptions{
			// Envoy default value of 100 is too low for data path.
			MaxConcurrentStreams: &types.UInt32Value{
				Value: 1073741824,
			},
		}
	}
}

// generates a cluster that sends traffic to dummy localport 0
// This cluster is used to catch all traffic to unresolved destinations in virtual service
func buildBlackHoleCluster() *apiv2.Cluster {
	cluster := &apiv2.Cluster{
		Name:           util.BlackHoleCluster,
		Type:           apiv2.Cluster_STATIC,
		ConnectTimeout: 1 * time.Second,
		LbPolicy:       apiv2.Cluster_ROUND_ROBIN,
	}
	return cluster
}

// generates a cluster that sends traffic to the original destination.
// This cluster is used to catch all traffic to unknown listener ports
func buildDefaultPassthroughCluster() *apiv2.Cluster {
	cluster := &apiv2.Cluster{
		Name:           util.PassthroughCluster,
		Type:           apiv2.Cluster_ORIGINAL_DST,
		ConnectTimeout: 1 * time.Second,
		LbPolicy:       apiv2.Cluster_ORIGINAL_DST_LB,
	}
	return cluster
}

// TODO: supply LbEndpoints or even better, LocalityLbEndpoints here
// change all other callsites accordingly
func buildDefaultCluster(env *model.Environment, name string, discoveryType apiv2.Cluster_DiscoveryType,
	localityLbEndpoints []endpoint.LocalityLbEndpoints, direction model.TrafficDirection) *apiv2.Cluster {
	cluster := &apiv2.Cluster{
		Name: name,
		Type: discoveryType,
	}

	if discoveryType == apiv2.Cluster_STRICT_DNS || discoveryType == apiv2.Cluster_LOGICAL_DNS {
		cluster.DnsLookupFamily = apiv2.Cluster_V4_ONLY
	}

	if discoveryType == apiv2.Cluster_STATIC || discoveryType == apiv2.Cluster_STRICT_DNS ||
		discoveryType == apiv2.Cluster_LOGICAL_DNS {
		cluster.LoadAssignment = &apiv2.ClusterLoadAssignment{
			ClusterName: name,
			Endpoints:   localityLbEndpoints,
		}
	}

	defaultTrafficPolicy := buildDefaultTrafficPolicy(env, discoveryType)
	applyTrafficPolicy(env, cluster, defaultTrafficPolicy, nil, nil, "",
		DefaultClusterMode, direction)
	return cluster
}

func buildDefaultTrafficPolicy(env *model.Environment, discoveryType apiv2.Cluster_DiscoveryType) *networking.TrafficPolicy {
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
					Seconds: env.Mesh.ConnectTimeout.Seconds,
					Nanos:   env.Mesh.ConnectTimeout.Nanos,
				},
			},
		},
	}
}
