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
	"path"
	"strings"
	"time"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	v2_cluster "github.com/envoyproxy/go-control-plane/envoy/api/v2/cluster"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/gogo/protobuf/types"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/log"
)

const (
	// DefaultLbType set to round robin
	DefaultLbType = networking.LoadBalancerSettings_ROUND_ROBIN
	// ManagementClusterHostname indicates the hostname used for building inbound clusters for management ports
	ManagementClusterHostname = "mgmtCluster"

	// CDSv2 validation requires ConnectTimeout to be > 0s. This is applied if no explicit policy is set.
	defaultClusterConnectTimeout = 5 * time.Second
)

// TODO: Need to do inheritance of DestRules based on domain suffix match

// BuildClusters returns the list of clusters for the given proxy. This is the CDS output
// For outbound: Cluster for each service/subset hostname or cidr with SNI set to service hostname
// Cluster type based on resolution
// For inbound (sidecar only): Cluster for each inbound endpoint port and for each service port
func (configgen *ConfigGeneratorImpl) BuildClusters(env *model.Environment, proxy *model.Proxy, push *model.PushContext) ([]*v2.Cluster, error) {
	clusters := make([]*v2.Cluster, 0)

	services := push.Services

	clusters = append(clusters, configgen.buildOutboundClusters(env, proxy, push, services)...)
	for _, c := range clusters {
		// Envoy requires a non-zero connect timeout
		if c.ConnectTimeout == 0 {
			c.ConnectTimeout = defaultClusterConnectTimeout
		}
	}
	if proxy.Type == model.Sidecar {
		instances, err := env.GetProxyServiceInstances(proxy)
		if err != nil {
			log.Errorf("failed to get service proxy service instances: %v", err)
			return nil, err
		}

		serviceAccounts, err := getServiceAccounts(env, proxy)
		if err != nil {
			return nil, err
		}

		managementPorts := env.ManagementPorts(proxy.IPAddress)
		clusters = append(clusters, configgen.buildInboundClusters(env, proxy, push, instances, managementPorts, serviceAccounts)...)
	}

	// Add a blackhole cluster for catching traffic to unresolved routes
	// DO NOT CALL PLUGINS for this cluster.
	clusters = append(clusters, buildBlackHoleCluster())

	return normalizeClusters(clusters), nil
}

// resolves cluster name conflicts. there can be duplicate cluster names if there are conflicting service definitions.
// for any clusters that share the same name the first cluster is kept and the others are discarded.
func normalizeClusters(clusters []*v2.Cluster) []*v2.Cluster {
	have := make(map[string]bool)
	out := make([]*v2.Cluster, 0, len(clusters))
	for _, cluster := range clusters {
		if !have[cluster.Name] {
			out = append(out, cluster)
		} else {
			log.Warnf("duplicate cluster name: %q", cluster.Name)
		}
		have[cluster.Name] = true
	}
	return out
}

func (configgen *ConfigGeneratorImpl) buildOutboundClusters(env *model.Environment, proxy *model.Proxy, push *model.PushContext,
	services []*model.Service) []*v2.Cluster {
	clusters := make([]*v2.Cluster, 0)
	serviceAccounts, err := getServiceAccounts(env, proxy)
	if err != nil {
		return clusters
	}

	for _, service := range services {
		config := push.DestinationRule(service.Hostname)
		for _, port := range service.Ports {
			if port.Protocol == model.ProtocolUDP {
				continue
			}
			hosts := buildClusterHosts(env, service, port.Port)

			// create default cluster
			clusterName := model.BuildSubsetKey(model.TrafficDirectionOutbound, "", service.Hostname, port.Port)
			upstreamServiceAccounts := env.ServiceAccounts.GetIstioServiceAccounts(service.Hostname, []int{port.Port})
			defaultCluster := buildDefaultCluster(env, clusterName, convertResolution(service.Resolution), hosts, serviceAccounts)

			updateEds(defaultCluster)
			setUpstreamProtocol(defaultCluster, port)
			clusters = append(clusters, defaultCluster)

			if config != nil {
				destinationRule := config.Spec.(*networking.DestinationRule)
				convertIstioMutual(destinationRule, service, upstreamServiceAccounts)
				applyTrafficPolicy(defaultCluster, destinationRule.TrafficPolicy, port, env.Mesh, serviceAccounts)

				for _, subset := range destinationRule.Subsets {
					subsetClusterName := model.BuildSubsetKey(model.TrafficDirectionOutbound, subset.Name, service.Hostname, port.Port)
					subsetCluster := buildDefaultCluster(env, subsetClusterName, convertResolution(service.Resolution), hosts, serviceAccounts)
					updateEds(subsetCluster)
					setUpstreamProtocol(subsetCluster, port)
					applyTrafficPolicy(subsetCluster, destinationRule.TrafficPolicy, port, env.Mesh, serviceAccounts)
					applyTrafficPolicy(subsetCluster, subset.TrafficPolicy, port, env.Mesh, serviceAccounts)
					// call plugins
					for _, p := range configgen.Plugins {
						p.OnOutboundCluster(env, proxy, push, service, port, subsetCluster)
					}
					clusters = append(clusters, subsetCluster)
				}
			}

			// call plugins for the default cluster
			for _, p := range configgen.Plugins {
				p.OnOutboundCluster(env, proxy, push, service, port, defaultCluster)
			}
		}
	}

	return clusters
}

func updateEds(cluster *v2.Cluster) {
	if cluster.Type != v2.Cluster_EDS {
		return
	}
	cluster.EdsClusterConfig = &v2.Cluster_EdsClusterConfig{
		ServiceName: cluster.Name,
		EdsConfig: &core.ConfigSource{
			ConfigSourceSpecifier: &core.ConfigSource_Ads{
				Ads: &core.AggregatedConfigSource{},
			},
		},
	}
}

func buildClusterHosts(env *model.Environment, service *model.Service, port int) []*core.Address {
	if service.Resolution != model.DNSLB {
		return nil
	}

	instances, err := env.InstancesByPort(service.Hostname, port, nil)
	if err != nil {
		log.Errorf("failed to retrieve instances for %s: %v", service.Hostname, err)
		return nil
	}

	hosts := make([]*core.Address, 0)
	for _, instance := range instances {
		host := util.BuildAddress(instance.Endpoint.Address, uint32(instance.Endpoint.Port))
		hosts = append(hosts, &host)
	}

	return hosts
}

func (configgen *ConfigGeneratorImpl) buildInboundClusters(env *model.Environment, proxy *model.Proxy, push *model.PushContext,
	instances []*model.ServiceInstance,
	managementPorts []*model.Port, serviceAccounts []string) []*v2.Cluster {
	clusters := make([]*v2.Cluster, 0)
	for _, instance := range instances {
		accounts := []string{}
		for _, port := range instance.Service.Ports {
			if port.Port == instance.Endpoint.Port {
				accounts = append(accounts, env.ServiceAccounts.GetIstioServiceAccounts(instance.Service.Hostname, []int{port.Port})...)
				break
			}
		}

		// This cluster name is mainly for stats.
		clusterName := model.BuildSubsetKey(model.TrafficDirectionInbound, "", instance.Service.Hostname, instance.Endpoint.ServicePort.Port)
		address := util.BuildAddress("127.0.0.1", uint32(instance.Endpoint.Port))
		localCluster := buildDefaultCluster(env, clusterName, v2.Cluster_STATIC, []*core.Address{&address}, accounts)
		setUpstreamProtocol(localCluster, instance.Endpoint.ServicePort)
		// call plugins
		for _, p := range configgen.Plugins {
			p.OnInboundCluster(env, proxy, push, instance.Service, instance.Endpoint.ServicePort, localCluster)
		}

		// When users specify circuit breakers, they need to be set on the receiver end
		// (server side) as well as client side, so that the server has enough capacity
		// (not the defaults) to handle the increased traffic volume
		// TODO: This is not foolproof - if instance is part of multiple services listening on same port,
		// choice of inbound cluster is arbitrary. So the connection pool settings may not apply cleanly.
		config := push.DestinationRule(instance.Service.Hostname)
		if config != nil {
			destinationRule := config.Spec.(*networking.DestinationRule)
			if destinationRule.TrafficPolicy != nil {
				// only connection pool settings make sense on the inbound path.
				// upstream TLS settings/outlier detection/load balancer don't apply here.
				applyConnectionPool(localCluster, destinationRule.TrafficPolicy.ConnectionPool)
			}
		}
		clusters = append(clusters, localCluster)
	}

	// Add a passthrough cluster for traffic to management ports (health check ports)
	for _, port := range managementPorts {
		clusterName := model.BuildSubsetKey(model.TrafficDirectionInbound, "", ManagementClusterHostname, port.Port)
		address := util.BuildAddress("127.0.0.1", uint32(port.Port))
		mgmtCluster := buildDefaultCluster(env, clusterName, v2.Cluster_STATIC, []*core.Address{&address}, serviceAccounts)
		setUpstreamProtocol(mgmtCluster, port)
		clusters = append(clusters, mgmtCluster)
	}
	return clusters
}

func getServiceAccounts(env *model.Environment, proxy *model.Proxy) ([]string, error) {
	instances, err := env.GetProxyServiceInstances(proxy)
	if err != nil {
		log.Errorf("failed to get service proxy service instances: %v", err)
		return nil, err
	}
	serviceAccounts := []string{}
	for _, si := range instances {
		for _, port := range si.Service.Ports {
			if port.Port == si.Endpoint.Port {
				serviceAccounts = append(serviceAccounts, env.ServiceAccounts.GetIstioServiceAccounts(si.Service.Hostname, []int{port.Port})...)
				break
			}
		}
	}

	return serviceAccounts, nil
}

func convertResolution(resolution model.Resolution) v2.Cluster_DiscoveryType {
	switch resolution {
	case model.ClientSideLB:
		return v2.Cluster_EDS
	case model.DNSLB:
		return v2.Cluster_STRICT_DNS
	case model.Passthrough:
		return v2.Cluster_ORIGINAL_DST
	default:
		return v2.Cluster_EDS
	}
}

// convertIstioMutual fills key cert fields for all TLSSettings when the mode is `ISTIO_MUTUAL`.
func convertIstioMutual(destinationRule *networking.DestinationRule, service *model.Service, upstreamServiceAccount []string) {
	converter := func(tls *networking.TLSSettings) {
		if tls == nil {
			return
		}
		if tls.Mode == networking.TLSSettings_ISTIO_MUTUAL {
			// Allow user specified SNIs in the istio mtls settings - which is useful
			// for routing via gateways. If there is no SNI, set the SNI as the service Hostname
			sni := tls.Sni
			if len(sni) == 0 {
				sni = string(service.Hostname)
			}
			*tls = *buildIstioMutualTLS(upstreamServiceAccount, sni)
		}
	}

	if destinationRule.TrafficPolicy != nil {
		converter(destinationRule.TrafficPolicy.Tls)
		for _, portTLS := range destinationRule.TrafficPolicy.PortLevelSettings {
			converter(portTLS.Tls)
		}
	}
	for _, subset := range destinationRule.Subsets {
		if subset.TrafficPolicy != nil {
			converter(subset.TrafficPolicy.Tls)
			for _, portTLS := range subset.TrafficPolicy.PortLevelSettings {
				converter(portTLS.Tls)
			}
		}
	}
}

// buildIstioMutualTLS returns a `TLSSettings` for ISTIO_MUTUAL mode.
func buildIstioMutualTLS(upstreamServiceAccount []string, sni string) *networking.TLSSettings {
	return &networking.TLSSettings{
		Mode:              networking.TLSSettings_ISTIO_MUTUAL,
		CaCertificates:    path.Join(model.AuthCertsPath, model.RootCertFilename),
		ClientCertificate: path.Join(model.AuthCertsPath, model.CertChainFilename),
		PrivateKey:        path.Join(model.AuthCertsPath, model.KeyFilename),
		SubjectAltNames:   upstreamServiceAccount,
		Sni:               sni,
	}
}

// SelectTrafficPolicyComponents returns the components of TrafficPolicy that should be used for given port.
func SelectTrafficPolicyComponents(policy *networking.TrafficPolicy, port *model.Port) (
	*networking.ConnectionPoolSettings, *networking.OutlierDetection, *networking.LoadBalancerSettings, *networking.TLSSettings) {
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

func applyTrafficPolicy(cluster *v2.Cluster, policy *networking.TrafficPolicy, port *model.Port, meshConfig *meshconfig.MeshConfig, serviceAccounts []string) {
	if policy == nil {
		return
	}
	connectionPool, outlierDetection, loadBalancer, tls := SelectTrafficPolicyComponents(policy, port)

	applyConnectionPool(cluster, connectionPool)
	applyOutlierDetection(cluster, outlierDetection)
	applyLoadBalancer(cluster, loadBalancer)
	applyUpstreamTLSSettings(cluster, tls, meshConfig, serviceAccounts)
}

// FIXME: there isn't a way to distinguish between unset values and zero values
func applyConnectionPool(cluster *v2.Cluster, settings *networking.ConnectionPoolSettings) {
	if settings == nil {
		return
	}

	threshold := &v2_cluster.CircuitBreakers_Thresholds{}

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

		// FIXME: zero is a valid value if explicitly set, otherwise we want to use the default value of 3
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
	}

	cluster.CircuitBreakers = &v2_cluster.CircuitBreakers{
		Thresholds: []*v2_cluster.CircuitBreakers_Thresholds{threshold},
	}
}

// FIXME: there isn't a way to distinguish between unset values and zero values
func applyOutlierDetection(cluster *v2.Cluster, outlier *networking.OutlierDetection) {
	if outlier == nil {
		return
	}

	out := &v2_cluster.OutlierDetection{}
	if outlier.BaseEjectionTime != nil {
		out.BaseEjectionTime = outlier.BaseEjectionTime
	}
	if outlier.ConsecutiveErrors > 0 {
		out.Consecutive_5Xx = &types.UInt32Value{Value: uint32(outlier.ConsecutiveErrors)}
	}
	if outlier.Interval != nil {
		out.Interval = outlier.Interval
	}
	if outlier.MaxEjectionPercent > 0 {
		out.MaxEjectionPercent = &types.UInt32Value{Value: uint32(outlier.MaxEjectionPercent)}
	}

	cluster.OutlierDetection = out
}

func applyLoadBalancer(cluster *v2.Cluster, lb *networking.LoadBalancerSettings) {
	if lb == nil {
		return
	}
	// TODO: MAGLEV
	switch lb.GetSimple() {
	case networking.LoadBalancerSettings_LEAST_CONN:
		cluster.LbPolicy = v2.Cluster_LEAST_REQUEST
	case networking.LoadBalancerSettings_RANDOM:
		cluster.LbPolicy = v2.Cluster_RANDOM
	case networking.LoadBalancerSettings_ROUND_ROBIN:
		cluster.LbPolicy = v2.Cluster_ROUND_ROBIN
	case networking.LoadBalancerSettings_PASSTHROUGH:
		cluster.LbPolicy = v2.Cluster_ORIGINAL_DST_LB
		cluster.Type = v2.Cluster_ORIGINAL_DST
	}

	// DO not do if else here. since lb.GetSimple returns a enum value (not pointer).

	consistentHash := lb.GetConsistentHash()
	if consistentHash != nil {
		cluster.LbPolicy = v2.Cluster_RING_HASH
		cluster.LbConfig = &v2.Cluster_RingHashLbConfig_{
			RingHashLbConfig: &v2.Cluster_RingHashLbConfig{
				MinimumRingSize: &types.UInt64Value{Value: uint64(consistentHash.GetMinimumRingSize())},
			},
		}
	}
}

func applyUpstreamTLSSettings(cluster *v2.Cluster, tls *networking.TLSSettings, meshConfig *meshconfig.MeshConfig, serviceAccounts []string) {
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
		if meshConfig.SdsUdsPath == "" || tls.Mode == networking.TLSSettings_MUTUAL {
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
			cluster.TlsContext.CommonTlsContext.ValidationContextType = model.ConstructValidationContext(model.CARootCertPath, tls.SubjectAltNames)
			cluster.TlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs = []*auth.SdsSecretConfig{}
			//refreshDuration, _ := ptypes.Duration(meshConfig.SdsRefreshDelay)
			refreshDuration, _ := types.DurationFromProto(meshConfig.SdsRefreshDelay)
			for _, sa := range serviceAccounts {
				// Skip service account which is added from annotation 'alpha.istio.io/canonical-serviceaccounts' to avoid fetching duplicated
				// cert from CA, the service account added in annotation is only used for building VerifySubjectAltName in TLS ValidationContext.
				if strings.HasPrefix(sa, "spiffe://accounts.google.com") {
					continue
				}

				cluster.TlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs =
					append(cluster.TlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs,
						model.ConstructSdsSecretConfig(sa, &refreshDuration, meshConfig.SdsUdsPath))
			}
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

func setUpstreamProtocol(cluster *v2.Cluster, port *model.Port) {
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
func buildBlackHoleCluster() *v2.Cluster {
	cluster := &v2.Cluster{
		Name:           util.BlackHoleCluster,
		Type:           v2.Cluster_STATIC,
		ConnectTimeout: defaultClusterConnectTimeout,
		LbPolicy:       v2.Cluster_ROUND_ROBIN,
	}
	return cluster
}

func buildDefaultCluster(env *model.Environment, name string, discoveryType v2.Cluster_DiscoveryType,
	hosts []*core.Address, serviceAccounts []string) *v2.Cluster {
	cluster := &v2.Cluster{
		Name:  name,
		Type:  discoveryType,
		Hosts: hosts,
	}

	if discoveryType == v2.Cluster_STRICT_DNS || discoveryType == v2.Cluster_LOGICAL_DNS {
		cluster.DnsLookupFamily = v2.Cluster_V4_ONLY
	}

	defaultTrafficPolicy := buildDefaultTrafficPolicy(env, discoveryType)
	applyTrafficPolicy(cluster, defaultTrafficPolicy, nil, env.Mesh, serviceAccounts)
	return cluster
}

func buildDefaultTrafficPolicy(env *model.Environment, discoveryType v2.Cluster_DiscoveryType) *networking.TrafficPolicy {
	lbPolicy := DefaultLbType
	if discoveryType == v2.Cluster_ORIGINAL_DST {
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
