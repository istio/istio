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

package v1alpha3

import (
	"math"
	"strconv"
	"strings"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	xdstype "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	structpb "github.com/golang/protobuf/ptypes/struct"
	"github.com/golang/protobuf/ptypes/wrappers"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/envoyfilter"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/loadbalancer"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/util/gogo"
)

const (
	// DefaultLbType set to round robin
	DefaultLbType = networking.LoadBalancerSettings_ROUND_ROBIN
)

// defaultTransportSocketMatch applies to endpoints that have no security.istio.io/tlsMode label
// or those whose label value does not match "istio"
var defaultTransportSocketMatch = &cluster.Cluster_TransportSocketMatch{
	Name:  "tlsMode-disabled",
	Match: &structpb.Struct{},
	TransportSocket: &core.TransportSocket{
		Name: util.EnvoyRawBufferSocketName,
	},
}

// getDefaultCircuitBreakerThresholds returns a copy of the default circuit breaker thresholds for the given traffic direction.
func getDefaultCircuitBreakerThresholds() *cluster.CircuitBreakers_Thresholds {
	return &cluster.CircuitBreakers_Thresholds{
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
}

// BuildClusters returns the list of clusters for the given proxy. This is the CDS output
// For outbound: Cluster for each service/subset hostname or cidr with SNI set to service hostname
// Cluster type based on resolution
// For inbound (sidecar only): Cluster for each inbound endpoint port and for each service port
func (configgen *ConfigGeneratorImpl) BuildClusters(proxy *model.Proxy, push *model.PushContext) []*cluster.Cluster {
	clusters := make([]*cluster.Cluster, 0)
	envoyFilterPatches := push.EnvoyFilters(proxy)
	cb := NewClusterBuilder(proxy, push)
	instances := proxy.ServiceInstances

	switch proxy.Type {
	case model.SidecarProxy:
		// Setup outbound clusters
		outboundPatcher := clusterPatcher{envoyFilterPatches, networking.EnvoyFilter_SIDECAR_OUTBOUND}
		clusters = append(clusters, configgen.buildOutboundClusters(cb, outboundPatcher)...)
		// Add a blackhole and passthrough cluster for catching traffic to unresolved routes
		clusters = outboundPatcher.conditionallyAppend(clusters, nil, cb.buildBlackHoleCluster(), cb.buildDefaultPassthroughCluster())
		clusters = append(clusters, outboundPatcher.insertedClusters()...)

		// Setup inbound clusters
		inboundPatcher := clusterPatcher{envoyFilterPatches, networking.EnvoyFilter_SIDECAR_INBOUND}
		clusters = append(clusters, configgen.buildInboundClusters(cb, instances, inboundPatcher)...)
		// Pass through clusters for inbound traffic. These cluster bind loopback-ish src address to access node local service.
		clusters = inboundPatcher.conditionallyAppend(clusters, nil, cb.buildInboundPassthroughClusters()...)
		clusters = append(clusters, inboundPatcher.insertedClusters()...)
	default: // Gateways
		patcher := clusterPatcher{envoyFilterPatches, networking.EnvoyFilter_GATEWAY}
		clusters = append(clusters, configgen.buildOutboundClusters(cb, patcher)...)
		// Gateways do not require the default passthrough cluster as they do not have original dst listeners.
		clusters = patcher.conditionallyAppend(clusters, nil, cb.buildBlackHoleCluster())
		if proxy.Type == model.Router && proxy.GetRouterMode() == model.SniDnatRouter {
			clusters = append(clusters, configgen.buildOutboundSniDnatClusters(proxy, push, patcher)...)
		}
		clusters = append(clusters, patcher.insertedClusters()...)
	}

	return cb.normalizeClusters(clusters)
}

func (configgen *ConfigGeneratorImpl) buildOutboundClusters(cb *ClusterBuilder, cp clusterPatcher) []*cluster.Cluster {
	clusters := make([]*cluster.Cluster, 0)
	networkView := model.GetNetworkView(cb.proxy)

	var services []*model.Service
	if features.FilterGatewayClusterConfig && cb.proxy.Type == model.Router {
		services = cb.push.GatewayServices(cb.proxy)
	} else {
		services = cb.push.Services(cb.proxy)
	}
	for _, service := range services {
		for _, port := range service.Ports {
			if port.Protocol == protocol.UDP {
				continue
			}
			lbEndpoints := cb.buildLocalityLbEndpoints(networkView, service, port.Port, nil)

			// create default cluster
			discoveryType := convertResolution(cb.proxy, service)
			clusterName := model.BuildSubsetKey(model.TrafficDirectionOutbound, "", service.Hostname, port.Port)
			defaultCluster := cb.buildDefaultCluster(clusterName, discoveryType, lbEndpoints, model.TrafficDirectionOutbound, port, service, nil)
			if defaultCluster == nil {
				continue
			}
			// If stat name is configured, build the alternate stats name.
			if len(cb.push.Mesh.OutboundClusterStatName) != 0 {
				defaultCluster.cluster.AltStatName = util.BuildStatPrefix(cb.push.Mesh.OutboundClusterStatName, string(service.Hostname), "", port, service.Attributes)
			}

			cb.setUpstreamProtocol(cb.proxy, defaultCluster, port, model.TrafficDirectionOutbound)

			subsetClusters := cb.applyDestinationRule(defaultCluster, DefaultClusterMode, service, port, networkView)

			clusters = cp.conditionallyAppend(clusters, nil, defaultCluster.build())
			clusters = cp.conditionallyAppend(clusters, nil, subsetClusters...)
		}
	}

	return clusters
}

var NilClusterPatcher = clusterPatcher{}

type clusterPatcher struct {
	efw  *model.EnvoyFilterWrapper
	pctx networking.EnvoyFilter_PatchContext
}

func (p clusterPatcher) conditionallyAppend(l []*cluster.Cluster, hosts []host.Name, clusters ...*cluster.Cluster) []*cluster.Cluster {
	for _, c := range clusters {
		if envoyfilter.ShouldKeepCluster(p.pctx, p.efw, c, hosts) {
			l = append(l, envoyfilter.ApplyClusterMerge(p.pctx, p.efw, c, hosts))
		}
	}
	return l
}

func (p clusterPatcher) insertedClusters() []*cluster.Cluster {
	return envoyfilter.InsertedClusters(p.pctx, p.efw)
}

// SniDnat clusters do not have any TLS setting, as they simply forward traffic to upstream
// All SniDnat clusters are internal services in the mesh.
func (configgen *ConfigGeneratorImpl) buildOutboundSniDnatClusters(proxy *model.Proxy, push *model.PushContext,
	cp clusterPatcher) []*cluster.Cluster {
	clusters := make([]*cluster.Cluster, 0)
	cb := NewClusterBuilder(proxy, push)

	networkView := model.GetNetworkView(proxy)

	for _, service := range push.Services(proxy) {
		if service.MeshExternal {
			continue
		}
		for _, port := range service.Ports {
			if port.Protocol == protocol.UDP {
				continue
			}
			lbEndpoints := cb.buildLocalityLbEndpoints(networkView, service, port.Port, nil)

			// create default cluster
			discoveryType := convertResolution(proxy, service)

			clusterName := model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, "", service.Hostname, port.Port)
			defaultCluster := cb.buildDefaultCluster(clusterName, discoveryType, lbEndpoints, model.TrafficDirectionOutbound, port, service, nil)
			if defaultCluster == nil {
				continue
			}
			subsetClusters := cb.applyDestinationRule(defaultCluster, SniDnatClusterMode, service, port, networkView)
			clusters = cp.conditionallyAppend(clusters, nil, defaultCluster.build())
			clusters = cp.conditionallyAppend(clusters, nil, subsetClusters...)
		}
	}

	return clusters
}

func buildInboundLocalityLbEndpoints(bind string, port uint32) []*endpoint.LocalityLbEndpoints {
	if bind == "" {
		return nil
	}
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

type ClusterInstances struct {
	PrimaryInstance *model.ServiceInstance
	AllInstances    []*model.ServiceInstance
}

func (configgen *ConfigGeneratorImpl) buildInboundClusters(cb *ClusterBuilder, instances []*model.ServiceInstance, cp clusterPatcher) []*cluster.Cluster {
	clusters := make([]*cluster.Cluster, 0)

	// The inbound clusters for a node depends on whether the node has a SidecarScope with inbound listeners
	// or not. If the node has a sidecarscope with ingress listeners, we only return clusters corresponding
	// to those listeners i.e. clusters made out of the defaultEndpoint field.
	// If the node has no sidecarScope and has interception mode set to NONE, then we should skip the inbound
	// clusters, because there would be no corresponding inbound listeners
	sidecarScope := cb.proxy.SidecarScope
	noneMode := cb.proxy.GetInterceptionMode() == model.InterceptionNone

	_, actualLocalHost := getActualWildcardAndLocalHost(cb.proxy)

	if !sidecarScope.HasCustomIngressListeners {
		// No user supplied sidecar scope or the user supplied one has no ingress listeners

		// We should not create inbound listeners in NONE mode based on the service instances
		// Doing so will prevent the workloads from starting as they would be listening on the same port
		// Users are required to provide the sidecar config to define the inbound listeners
		if noneMode {
			return nil
		}

		clustersToBuild := make(map[int][]*model.ServiceInstance)
		for _, instance := range instances {
			// Filter out service instances with the same port as we are going to mark them as duplicates any way
			// in normalizeClusters method.
			// However, we still need to capture all the instances on this port, as its required to populate telemetry metadata
			// The first instance will be used as the "primary" instance; this means if we have an conflicts between
			// Services the first one wins
			ep := int(instance.Endpoint.EndpointPort)
			clustersToBuild[ep] = append(clustersToBuild[ep], instance)
		}

		bind := actualLocalHost
		if features.EnableInboundPassthrough {
			bind = ""
		}
		// For each workload port, we will construct a cluster
		for _, instances := range clustersToBuild {
			instance := instances[0]
			localCluster := cb.buildInboundClusterForPortOrUDS(int(instance.Endpoint.EndpointPort), bind, instance, instances)
			// If inbound cluster match has service, we should see if it matches with any host name across all instances.
			var hosts []host.Name
			for _, si := range instances {
				hosts = append(hosts, si.Service.Hostname)
			}
			clusters = cp.conditionallyAppend(clusters, hosts, localCluster.build())
		}
		return clusters
	}

	for _, ingressListener := range sidecarScope.Sidecar.Ingress {
		// LDS would have setup the inbound clusters
		// as inbound|portNumber|portName|Hostname[or]SidecarScopeID
		listenPort := &model.Port{
			Port:     int(ingressListener.Port.Number),
			Protocol: protocol.Parse(ingressListener.Port.Protocol),
			Name:     ingressListener.Port.Name,
		}

		// Set up the endpoint. By default, we set this empty which will use ORIGINAL_DST passthrough.
		// This can be overridden by ingress.defaultEndpoint.
		// * 127.0.0.1: send to localhost
		// * 0.0.0.0: send to INSTANCE_IP
		// * unix:///...: send to configured unix domain socket
		endpointAddress := ""
		port := 0
		if strings.HasPrefix(ingressListener.DefaultEndpoint, model.UnixAddressPrefix) {
			// this is a UDS endpoint. assign it as is
			endpointAddress = ingressListener.DefaultEndpoint
		} else if len(ingressListener.DefaultEndpoint) > 0 {
			// parse the ip, port. Validation guarantees presence of :
			parts := strings.Split(ingressListener.DefaultEndpoint, ":")
			if len(parts) < 2 {
				continue
			}
			var err error
			if port, err = strconv.Atoi(parts[1]); err != nil {
				continue
			}
			if parts[0] == model.PodIPAddressPrefix {
				endpointAddress = cb.proxy.IPAddresses[0]
			} else if parts[0] == model.LocalhostAddressPrefix {
				endpointAddress = actualLocalHost
			}
		}

		// Find the service instance that corresponds to this ingress listener by looking
		// for a service instance that matches this ingress port as this will allow us
		// to generate the right cluster name that LDS expects inbound|portNumber|portName|Hostname
		instance := configgen.findOrCreateServiceInstance(instances, ingressListener, sidecarScope.Name, sidecarScope.Namespace)
		instance.Endpoint.Address = endpointAddress
		instance.ServicePort = listenPort
		instance.Endpoint.ServicePortName = listenPort.Name
		instance.Endpoint.EndpointPort = uint32(port)

		localCluster := cb.buildInboundClusterForPortOrUDS(int(ingressListener.Port.Number), endpointAddress, instance, nil)
		clusters = cp.conditionallyAppend(clusters, []host.Name{instance.Service.Hostname}, localCluster.build())
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
			EndpointPort: ingressListener.Port.Number,
		},
	}
}

func convertResolution(proxy *model.Proxy, service *model.Service) cluster.Cluster_DiscoveryType {
	switch service.Resolution {
	case model.ClientSideLB:
		return cluster.Cluster_EDS
	case model.DNSLB:
		return cluster.Cluster_STRICT_DNS
	case model.Passthrough:
		// Gateways cannot use passthrough clusters. So fallback to EDS
		if proxy.Type == model.SidecarProxy {
			if service.Attributes.ServiceRegistry == string(serviceregistry.Kubernetes) && features.EnableEDSForHeadless {
				return cluster.Cluster_EDS
			}

			return cluster.Cluster_ORIGINAL_DST
		}
		return cluster.Cluster_EDS
	default:
		return cluster.Cluster_EDS
	}
}

type mtlsContextType int

const (
	userSupplied mtlsContextType = iota
	autoDetected
)

// buildAutoMtlsSettings fills key cert fields for all TLSSettings when the mode is `ISTIO_MUTUAL`.
// If the (input) TLS setting is nil (i.e not set), *and* the service mTLS mode is STRICT, it also
// creates and populates the config as if they are set as ISTIO_MUTUAL.
func buildAutoMtlsSettings(
	tls *networking.ClientTLSSettings,
	serviceAccounts []string,
	sni string,
	proxy *model.Proxy,
	autoMTLSEnabled bool,
	meshExternal bool,
	serviceMTLSMode model.MutualTLSMode) (*networking.ClientTLSSettings, mtlsContextType) {
	if tls != nil {
		if tls.Mode != networking.ClientTLSSettings_ISTIO_MUTUAL {
			return tls, userSupplied
		}

		// Update TLS settings for ISTIO_MUTUAL.
		// Use client provided SNI if set. Otherwise, overwrite with the auto generated SNI
		// user specified SNIs in the istio mtls settings are useful when routing via gateways.
		sniToUse := tls.Sni
		if len(sniToUse) == 0 {
			sniToUse = sni
		}
		subjectAltNamesToUse := tls.SubjectAltNames
		if len(subjectAltNamesToUse) == 0 {
			subjectAltNamesToUse = serviceAccounts
		}
		return buildIstioMutualTLS(subjectAltNamesToUse, sniToUse, proxy), userSupplied
	}

	if meshExternal || !autoMTLSEnabled || serviceMTLSMode == model.MTLSUnknown || serviceMTLSMode == model.MTLSDisable {
		return nil, userSupplied
	}

	// Build settings for auto MTLS.
	return buildIstioMutualTLS(serviceAccounts, sni, proxy), autoDetected
}

// buildIstioMutualTLS returns a `TLSSettings` for ISTIO_MUTUAL mode.
func buildIstioMutualTLS(serviceAccounts []string, sni string, proxy *model.Proxy) *networking.ClientTLSSettings {
	return &networking.ClientTLSSettings{
		Mode:              networking.ClientTLSSettings_ISTIO_MUTUAL,
		CaCertificates:    proxy.Metadata.TLSClientRootCert,
		ClientCertificate: proxy.Metadata.TLSClientCertChain,
		PrivateKey:        proxy.Metadata.TLSClientKey,
		SubjectAltNames:   serviceAccounts,
		Sni:               sni,
	}
}

// SelectTrafficPolicyComponents returns the components of TrafficPolicy that should be used for given port.
func selectTrafficPolicyComponents(policy *networking.TrafficPolicy) (
	*networking.ConnectionPoolSettings, *networking.OutlierDetection, *networking.LoadBalancerSettings, *networking.ClientTLSSettings) {
	if policy == nil {
		return nil, nil, nil, nil
	}
	connectionPool := policy.ConnectionPool
	outlierDetection := policy.OutlierDetection
	loadBalancer := policy.LoadBalancer
	tls := policy.Tls

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
	mesh            *meshconfig.MeshConfig
	mutable         *MutableCluster
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

type upgradeTuple struct {
	meshdefault meshconfig.MeshConfig_H2UpgradePolicy
	override    networking.ConnectionPoolSettings_HTTPSettings_H2UpgradePolicy
}

func applyTCPKeepalive(mesh *meshconfig.MeshConfig, c *cluster.Cluster, settings *networking.ConnectionPoolSettings) {
	// Apply Keepalive config only if it is configured in mesh config or in destination rule.
	if mesh.TcpKeepalive != nil || settings.Tcp.TcpKeepalive != nil {

		// Start with empty tcp_keepalive, which would set SO_KEEPALIVE on the socket with OS default values.
		c.UpstreamConnectionOptions = &cluster.UpstreamConnectionOptions{
			TcpKeepalive: &core.TcpKeepalive{},
		}

		// Apply mesh wide TCP keepalive if available.
		if mesh.TcpKeepalive != nil {
			setKeepAliveSettings(c, mesh.TcpKeepalive)
		}

		// Apply/Override individual attributes with DestinationRule TCP keepalive if set.
		if settings.Tcp.TcpKeepalive != nil {
			setKeepAliveSettings(c, settings.Tcp.TcpKeepalive)
		}
	}
}

func setKeepAliveSettings(cluster *cluster.Cluster, keepalive *networking.ConnectionPoolSettings_TCPSettings_TcpKeepalive) {
	if keepalive.Probes > 0 {
		cluster.UpstreamConnectionOptions.TcpKeepalive.KeepaliveProbes = &wrappers.UInt32Value{Value: keepalive.Probes}
	}

	if keepalive.Time != nil {
		cluster.UpstreamConnectionOptions.TcpKeepalive.KeepaliveTime = &wrappers.UInt32Value{Value: uint32(keepalive.Time.Seconds)}
	}

	if keepalive.Interval != nil {
		cluster.UpstreamConnectionOptions.TcpKeepalive.KeepaliveInterval = &wrappers.UInt32Value{Value: uint32(keepalive.Interval.Seconds)}
	}
}

// FIXME: there isn't a way to distinguish between unset values and zero values
func applyOutlierDetection(c *cluster.Cluster, outlier *networking.OutlierDetection) {
	if outlier == nil {
		return
	}

	out := &cluster.OutlierDetection{}

	// SuccessRate based outlier detection should be disabled.
	out.EnforcingSuccessRate = &wrappers.UInt32Value{Value: 0}

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
	if outlier.BaseEjectionTime != nil {
		out.BaseEjectionTime = gogo.DurationToProtoDuration(outlier.BaseEjectionTime)
	}
	if outlier.MaxEjectionPercent > 0 {
		out.MaxEjectionPercent = &wrappers.UInt32Value{Value: uint32(outlier.MaxEjectionPercent)}
	}

	c.OutlierDetection = out

	// Disable panic threshold by default as its not typically applicable in k8s environments
	// with few pods per service.
	// To do so, set the healthy_panic_threshold field even if its value is 0 (defaults to 50).
	// FIXME: we can't distinguish between it being unset or being explicitly set to 0
	if outlier.MinHealthPercent >= 0 {
		if c.CommonLbConfig == nil {
			c.CommonLbConfig = &cluster.Cluster_CommonLbConfig{}
		}
		c.CommonLbConfig.HealthyPanicThreshold = &xdstype.Percent{Value: float64(outlier.MinHealthPercent)} // defaults to 50
	}
}

func applyLoadBalancer(c *cluster.Cluster, lb *networking.LoadBalancerSettings, port *model.Port, proxy *model.Proxy, meshConfig *meshconfig.MeshConfig) {
	localityLbSetting := loadbalancer.GetLocalityLbSetting(meshConfig.GetLocalityLbSetting(), lb.GetLocalityLbSetting())
	if localityLbSetting != nil && (localityLbSetting.Distribute != nil || localityLbSetting.Failover != nil) {
		if c.CommonLbConfig == nil {
			c.CommonLbConfig = &cluster.Cluster_CommonLbConfig{}
		}
		c.CommonLbConfig.LocalityConfigSpecifier = &cluster.Cluster_CommonLbConfig_LocalityWeightedLbConfig_{
			LocalityWeightedLbConfig: &cluster.Cluster_CommonLbConfig_LocalityWeightedLbConfig{},
		}
	}

	// Use locality lb settings from load balancer settings if present, else use mesh wide locality lb settings
	applyLocalityLBSetting(proxy.Locality, c, localityLbSetting)

	// The following order is important. If cluster type has been identified as Original DST since Resolution is PassThrough,
	// and port is named as redis-xxx we end up creating a cluster with type Original DST and LbPolicy as MAGLEV which would be
	// rejected by Envoy.

	if c.GetType() == cluster.Cluster_ORIGINAL_DST {
		return
	}

	// Redis protocol must be defaulted with MAGLEV to benefit from client side sharding.
	if features.EnableRedisFilter && port != nil && port.Protocol == protocol.Redis {
		c.LbPolicy = cluster.Cluster_MAGLEV
		return
	}

	if lb == nil {
		return
	}

	// DO not do if else here. since lb.GetSimple returns a enum value (not pointer).
	switch lb.GetSimple() {
	case networking.LoadBalancerSettings_LEAST_CONN:
		c.LbPolicy = cluster.Cluster_LEAST_REQUEST
	case networking.LoadBalancerSettings_RANDOM:
		c.LbPolicy = cluster.Cluster_RANDOM
	case networking.LoadBalancerSettings_ROUND_ROBIN:
		c.LbPolicy = cluster.Cluster_ROUND_ROBIN
	case networking.LoadBalancerSettings_PASSTHROUGH:
		c.LbPolicy = cluster.Cluster_CLUSTER_PROVIDED
		c.ClusterDiscoveryType = &cluster.Cluster_Type{Type: cluster.Cluster_ORIGINAL_DST}
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
		c.LbPolicy = cluster.Cluster_RING_HASH
		c.LbConfig = &cluster.Cluster_RingHashLbConfig_{
			RingHashLbConfig: &cluster.Cluster_RingHashLbConfig{
				MinimumRingSize: minRingSize,
			},
		}
	}
}

func applyLocalityLBSetting(locality *core.Locality, cluster *cluster.Cluster, localityLB *networking.LocalityLoadBalancerSetting) {
	if locality == nil || localityLB == nil {
		return
	}

	// Failover should only be applied with outlier detection, or traffic will never failover.
	enabledFailover := cluster.OutlierDetection != nil
	if cluster.LoadAssignment != nil {
		loadbalancer.ApplyLocalityLBSetting(locality, cluster.LoadAssignment, localityLB, enabledFailover)
	}
}

func addTelemetryMetadata(opts buildClusterOpts, service *model.Service, direction model.TrafficDirection, instances []*model.ServiceInstance) {
	if opts.mutable.cluster == nil {
		return
	}
	if direction == model.TrafficDirectionInbound && (opts.proxy == nil || opts.proxy.ServiceInstances == nil ||
		len(opts.proxy.ServiceInstances) == 0 || opts.port == nil) {
		// At inbound, port and local service instance has to be provided
		return
	}
	if direction == model.TrafficDirectionOutbound && service == nil {
		// At outbound, the service corresponding to the cluster has to be provided.
		return
	}

	im := getOrCreateIstioMetadata(opts.mutable.cluster)

	// Add services field into istio metadata
	im.Fields["services"] = &structpb.Value{
		Kind: &structpb.Value_ListValue{
			ListValue: &structpb.ListValue{
				Values: []*structpb.Value{},
			},
		},
	}

	svcMetaList := im.Fields["services"].GetListValue()

	// Add service related metadata. This will be consumed by telemetry v2 filter for metric labels.
	if direction == model.TrafficDirectionInbound {
		// For inbound cluster, add all services on the cluster port
		have := make(map[host.Name]bool)
		for _, svc := range instances {
			if svc.ServicePort.Port != opts.port.Port {
				// If the service port is different from the the port of the cluster that is being built,
				// skip adding telemetry metadata for the service to the cluster.
				continue
			}
			if _, ok := have[svc.Service.Hostname]; ok {
				// Skip adding metadata for instance with the same host name.
				// This could happen when a service has multiple IPs.
				continue
			}
			svcMetaList.Values = append(svcMetaList.Values, buildServiceMetadata(svc.Service))
			have[svc.Service.Hostname] = true
		}
	} else if direction == model.TrafficDirectionOutbound {
		// For outbound cluster, add telemetry metadata based on the service that the cluster is built for.
		svcMetaList.Values = append(svcMetaList.Values, buildServiceMetadata(service))
	}
}

// Insert the original port into the istio metadata. The port is used in BTS delivered from client sidecar to server sidecar.
// Server side car uses this port after de-multiplexed from tunnel.
func addNetworkingMetadata(opts buildClusterOpts, service *model.Service, direction model.TrafficDirection) {
	if opts.mutable == nil || direction == model.TrafficDirectionInbound {
		return
	}
	if service == nil {
		// At outbound, the service corresponding to the cluster has to be provided.
		return
	}

	if port, ok := service.Ports.GetByPort(opts.port.Port); ok {
		im := getOrCreateIstioMetadata(opts.mutable.cluster)

		// Add original_port field into istio metadata
		// Endpoint could override this port but the chance should be small.
		im.Fields["default_original_port"] = &structpb.Value{
			Kind: &structpb.Value_NumberValue{
				NumberValue: float64(port.Port),
			},
		}
	}
}

// Build a struct which contains service metadata and will be added into cluster label.
func buildServiceMetadata(svc *model.Service) *structpb.Value {
	return &structpb.Value{
		Kind: &structpb.Value_StructValue{
			StructValue: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					// service fqdn
					"host": {
						Kind: &structpb.Value_StringValue{
							StringValue: string(svc.Hostname),
						},
					},
					// short name of the service
					"name": {
						Kind: &structpb.Value_StringValue{
							StringValue: svc.Attributes.Name,
						},
					},
					// namespace of the service
					"namespace": {
						Kind: &structpb.Value_StringValue{
							StringValue: svc.Attributes.Namespace,
						},
					},
				},
			},
		},
	}
}

func getOrCreateIstioMetadata(cluster *cluster.Cluster) *structpb.Struct {
	if cluster.Metadata == nil {
		cluster.Metadata = &core.Metadata{
			FilterMetadata: map[string]*structpb.Struct{},
		}
	}
	// Create Istio metadata if does not exist yet
	if _, ok := cluster.Metadata.FilterMetadata[util.IstioMetadataKey]; !ok {
		cluster.Metadata.FilterMetadata[util.IstioMetadataKey] = &structpb.Struct{
			Fields: map[string]*structpb.Value{},
		}
	}
	return cluster.Metadata.FilterMetadata[util.IstioMetadataKey]
}
