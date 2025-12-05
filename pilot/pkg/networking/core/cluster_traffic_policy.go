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

package core

import (
	"math"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	proxyprotocol "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/proxy_protocol/v3"
	http "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	xdstype "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/loadbalancer"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/wellknown"
)

// applyTrafficPolicy applies the trafficPolicy defined within destinationRule,
// which can be called for both outbound and inbound cluster, but only connection pool will be applied to inbound cluster.
func (cb *ClusterBuilder) applyTrafficPolicy(service *model.Service, opts buildClusterOpts) {
	connectionPool, outlierDetection, loadBalancer, tls, proxyProtocol, retryBudget := selectTrafficPolicyComponents(opts.policy)
	// Connection pool settings are applicable for both inbound and outbound clusters.
	if connectionPool == nil {
		connectionPool = &networking.ConnectionPoolSettings{}
	}
	// Apply h2 upgrade s.t. h2 connection pool settings can be applied to the cluster.
	if opts.direction != model.TrafficDirectionInbound {
		cb.applyH2Upgrade(opts.mutable, opts.port, opts.mesh, connectionPool)
	}
	cb.applyConnectionPool(opts.mesh, opts.mutable, connectionPool, retryBudget)
	if opts.direction != model.TrafficDirectionInbound {
		applyOutlierDetection(service, opts.mutable.cluster, outlierDetection)
		applyLoadBalancer(service, opts.mutable.cluster, loadBalancer, opts.port, cb.locality, cb.proxyLabels, opts.mesh)
		if opts.clusterMode != SniDnatClusterMode {
			autoMTLSEnabled := opts.mesh.GetEnableAutoMtls().Value
			tls, mtlsCtxType := cb.buildUpstreamTLSSettings(tls, opts.serviceAccounts, opts.istioMtlsSni,
				autoMTLSEnabled, opts.meshExternal, opts.serviceMTLSMode)
			cb.applyUpstreamTLSSettings(&opts, tls, mtlsCtxType)
			cb.applyUpstreamProxyProtocol(&opts, proxyProtocol)
		}
	}

	if opts.mutable.cluster.GetType() == cluster.Cluster_ORIGINAL_DST {
		opts.mutable.cluster.LbPolicy = cluster.Cluster_CLUSTER_PROVIDED
	}
}

// selectTrafficPolicyComponents returns the components of TrafficPolicy that should be used for given port.
func selectTrafficPolicyComponents(policy *networking.TrafficPolicy) (
	*networking.ConnectionPoolSettings,
	*networking.OutlierDetection,
	*networking.LoadBalancerSettings,
	*networking.ClientTLSSettings,
	*networking.TrafficPolicy_ProxyProtocol,
	*networking.TrafficPolicy_RetryBudget,
) {
	if policy == nil {
		return nil, nil, nil, nil, nil, nil
	}
	connectionPool := policy.ConnectionPool
	outlierDetection := policy.OutlierDetection
	loadBalancer := policy.LoadBalancer
	tls := policy.Tls
	proxyProtocol := policy.ProxyProtocol
	retryBudget := policy.RetryBudget

	return connectionPool, outlierDetection, loadBalancer, tls, proxyProtocol, retryBudget
}

// FIXME: there isn't a way to distinguish between unset values and zero values
func (cb *ClusterBuilder) applyConnectionPool(mesh *meshconfig.MeshConfig,
	mc *clusterWrapper, settings *networking.ConnectionPoolSettings,
	retryBudget *networking.TrafficPolicy_RetryBudget,
) {
	if settings == nil {
		return
	}

	threshold := getDefaultCircuitBreakerThresholds()
	applyRetryBudget(threshold, retryBudget)

	var idleTimeout *durationpb.Duration
	var maxRequestsPerConnection uint32
	var maxConcurrentStreams uint32
	var maxConnectionDuration *durationpb.Duration

	if settings.Http != nil {
		if settings.Http.Http2MaxRequests > 0 {
			threshold.MaxRequests = &wrapperspb.UInt32Value{Value: uint32(settings.Http.Http2MaxRequests)}
		}
		if settings.Http.Http1MaxPendingRequests > 0 {
			threshold.MaxPendingRequests = &wrapperspb.UInt32Value{Value: uint32(settings.Http.Http1MaxPendingRequests)}
		}

		// FIXME: zero is a valid value if explicitly set, otherwise we want to use the default
		if settings.Http.MaxRetries > 0 {
			threshold.MaxRetries = &wrapperspb.UInt32Value{Value: uint32(settings.Http.MaxRetries)}
		}

		idleTimeout = settings.Http.IdleTimeout
		maxRequestsPerConnection = uint32(settings.Http.MaxRequestsPerConnection)
		maxConcurrentStreams = uint32(settings.Http.MaxConcurrentStreams)
	}

	cb.applyDefaultConnectionPool(mc.cluster)
	if settings.Tcp != nil {
		if settings.Tcp.ConnectTimeout != nil {
			mc.cluster.ConnectTimeout = settings.Tcp.ConnectTimeout
		}

		if settings.Tcp.MaxConnections > 0 {
			threshold.MaxConnections = &wrapperspb.UInt32Value{Value: uint32(settings.Tcp.MaxConnections)}
		}
		if settings.Tcp.MaxConnectionDuration != nil {
			maxConnectionDuration = settings.Tcp.MaxConnectionDuration
		}
		if idleTimeout == nil {
			idleTimeout = settings.Tcp.IdleTimeout
		}
	}
	applyTCPKeepalive(mesh, mc.cluster, settings.Tcp)

	mc.cluster.CircuitBreakers = &cluster.CircuitBreakers{
		Thresholds: []*cluster.CircuitBreakers_Thresholds{threshold},
	}

	if maxConnectionDuration != nil || idleTimeout != nil || maxRequestsPerConnection > 0 || maxConcurrentStreams > 0 {
		if mc.httpProtocolOptions == nil {
			mc.httpProtocolOptions = &http.HttpProtocolOptions{}
		}
		options := mc.httpProtocolOptions
		if options.CommonHttpProtocolOptions == nil {
			options.CommonHttpProtocolOptions = &core.HttpProtocolOptions{}
		}
		if idleTimeout != nil {
			idleTimeoutDuration := idleTimeout
			options.CommonHttpProtocolOptions.IdleTimeout = idleTimeoutDuration
		}
		if maxRequestsPerConnection > 0 {
			options.CommonHttpProtocolOptions.MaxRequestsPerConnection = &wrapperspb.UInt32Value{Value: maxRequestsPerConnection}
		}
		if maxConnectionDuration != nil {
			options.CommonHttpProtocolOptions.MaxConnectionDuration = maxConnectionDuration
		}
		// Check if cluster is HTTP2
		http2ProtocolOptions := options.GetExplicitHttpConfig().GetHttp2ProtocolOptions()
		if http2ProtocolOptions != nil && maxConcurrentStreams > 0 {
			http2ProtocolOptions.MaxConcurrentStreams = &wrapperspb.UInt32Value{Value: maxConcurrentStreams}
		}
	}
	if settings.Http != nil && settings.Http.UseClientProtocol {
		// Use downstream protocol. If the incoming traffic use HTTP 1.1, the
		// upstream cluster will use HTTP 1.1, if incoming traffic use HTTP2,
		// the upstream cluster will use HTTP2.
		cb.setUseDownstreamProtocol(mc)
	}
}

func applyRetryBudget(
	thresholds *cluster.CircuitBreakers_Thresholds,
	retryBudget *networking.TrafficPolicy_RetryBudget,
) {
	if retryBudget == nil {
		return
	}

	percent := &xdstype.Percent{Value: 0.2} // default to 20%
	if retryBudget.Percent != nil {
		percent = &xdstype.Percent{Value: retryBudget.Percent.Value}
	}
	retryConcurrency := &wrapperspb.UInt32Value{Value: 3} // default to	3
	if retryBudget.MinRetryConcurrency > 0 {
		retryConcurrency = &wrapperspb.UInt32Value{Value: retryBudget.MinRetryConcurrency}
	}

	thresholds.RetryBudget = &cluster.CircuitBreakers_Thresholds_RetryBudget{
		BudgetPercent:       percent,
		MinRetryConcurrency: retryConcurrency,
	}
}

// applyH2Upgrade function will upgrade cluster to http2 if specified by configuration.
// applyH2Upgrade can only be called for outbound cluster
func (cb *ClusterBuilder) applyH2Upgrade(mc *clusterWrapper, port *model.Port,
	mesh *meshconfig.MeshConfig, connectionPool *networking.ConnectionPoolSettings,
) {
	if shouldH2Upgrade(mc.cluster.Name, port, mesh, connectionPool) {
		setH2Options(mc)
	}
}

// shouldH2Upgrade function returns true if the cluster should be upgraded to http2.
// shouldH2Upgrade can only be called for outbound cluster
func shouldH2Upgrade(clusterName string, port *model.Port, mesh *meshconfig.MeshConfig,
	connectionPool *networking.ConnectionPoolSettings,
) bool {
	// TODO (mjog)
	// Upgrade if tls.GetMode() == networking.TLSSettings_ISTIO_MUTUAL
	if connectionPool != nil && connectionPool.Http != nil {
		override := connectionPool.Http.H2UpgradePolicy
		// If useClientProtocol is set, do not upgrade
		if connectionPool.Http.UseClientProtocol {
			log.Debugf("Not upgrading cluster because useClientProtocol is set: %v (%v %v)",
				clusterName, mesh.H2UpgradePolicy, override)
			return false
		}
		// If user wants an upgrade at destination rule/port level that means he is sure that
		// it is a Http port - upgrade in such case. This is useful in case protocol sniffing is
		// enabled and user wants to upgrade/preserve http protocol from client.
		if override == networking.ConnectionPoolSettings_HTTPSettings_UPGRADE {
			log.Debugf("Upgrading cluster: %v (%v %v)", clusterName, mesh.H2UpgradePolicy, override)
			return true
		}
		if override == networking.ConnectionPoolSettings_HTTPSettings_DO_NOT_UPGRADE {
			log.Debugf("Not upgrading cluster: %v (%v %v)", clusterName, mesh.H2UpgradePolicy, override)
			return false
		}
	}

	// Do not upgrade non-http ports. This also ensures that we are only upgrading
	// named ports so that protocol sniffing does not interfere. Protocol sniffing
	// uses downstream protocol. Therefore if the client upgrades connection to http2,
	// the server will send h2 stream to the application,even though the application only
	// supports http 1.1.
	if port != nil && !port.Protocol.IsHTTP() {
		return false
	}

	return mesh.H2UpgradePolicy == meshconfig.MeshConfig_UPGRADE
}

func (cb *ClusterBuilder) applyDefaultConnectionPool(cluster *cluster.Cluster) {
	cluster.ConnectTimeout = protomarshal.Clone(cb.req.Push.Mesh.ConnectTimeout)
}

func applyLoadBalancer(
	svc *model.Service,
	c *cluster.Cluster,
	lb *networking.LoadBalancerSettings,
	port *model.Port,
	locality *core.Locality,
	proxyLabels map[string]string,
	meshConfig *meshconfig.MeshConfig,
) {
	// Disable panic threshold when SendUnhealthyEndpoints is enabled as enabling it "may" send traffic to unready
	// end points when load balancer is in panic mode.
	if svc.SupportsUnhealthyEndpoints() {
		c.CommonLbConfig.HealthyPanicThreshold = &xdstype.Percent{Value: 0}
	}
	// Use locality lb settings from load balancer settings if present, else use mesh wide locality lb settings
	localityLbSetting, forceFailover := loadbalancer.GetLocalityLbSetting(meshConfig.GetLocalityLbSetting(), lb.GetLocalityLbSetting(), svc)
	applyLocalityLoadBalancer(locality, proxyLabels, c, localityLbSetting, forceFailover)

	if c.GetType() == cluster.Cluster_ORIGINAL_DST {
		c.LbPolicy = cluster.Cluster_CLUSTER_PROVIDED
		return
	}

	// Redis protocol must be defaulted with MAGLEV to benefit from client side sharding.
	if features.EnableRedisFilter && port != nil && port.Protocol == protocol.Redis {
		c.LbPolicy = cluster.Cluster_MAGLEV
		return
	}

	// DO not do if else here. since lb.GetSimple returns a enum value (not pointer).
	switch lb.GetSimple() {
	// nolint: staticcheck
	case networking.LoadBalancerSettings_LEAST_CONN, networking.LoadBalancerSettings_LEAST_REQUEST:
		applyLeastRequestLoadBalancer(c, lb)
	case networking.LoadBalancerSettings_RANDOM:
		c.LbPolicy = cluster.Cluster_RANDOM
	case networking.LoadBalancerSettings_ROUND_ROBIN:
		applyRoundRobinLoadBalancer(c, lb)
	case networking.LoadBalancerSettings_PASSTHROUGH:
		c.LbPolicy = cluster.Cluster_CLUSTER_PROVIDED
		c.ClusterDiscoveryType = &cluster.Cluster_Type{Type: cluster.Cluster_ORIGINAL_DST}
		// Wipe out any LoadAssignment, if set. This can occur when we have a STATIC Service but PASSTHROUGH traffic policy
		c.LoadAssignment = nil
	default:
		applySimpleDefaultLoadBalancer(c, lb)
	}

	ApplyRingHashLoadBalancer(c, lb)
}

func applyLocalityLoadBalancer(
	locality *core.Locality,
	proxyLabels map[string]string,
	c *cluster.Cluster,
	localityLB *networking.LocalityLoadBalancerSetting,
	failover bool,
) {
	// Failover should only be applied with outlier detection, or traffic will never failover.
	enableFailover := failover || c.OutlierDetection != nil
	// set locality weighted lb config when locality lb is enabled, otherwise it will influence the result of LBPolicy like `least request`
	if features.EnableLocalityWeightedLbConfig ||
		(enableFailover && (localityLB.GetFailover() != nil || localityLB.GetFailoverPriority() != nil)) ||
		localityLB.GetDistribute() != nil {
		c.CommonLbConfig.LocalityConfigSpecifier = &cluster.Cluster_CommonLbConfig_LocalityWeightedLbConfig_{
			LocalityWeightedLbConfig: &cluster.Cluster_CommonLbConfig_LocalityWeightedLbConfig{},
		}
	}

	if c.LoadAssignment != nil {
		// TODO: enable failoverPriority for `STRICT_DNS` cluster type
		loadbalancer.ApplyLocalityLoadBalancer(c.LoadAssignment, nil, locality, proxyLabels, localityLB, enableFailover)
	}
}

// applySimpleDefaultLoadBalancer will set the DefaultLBPolicy and create an LbConfig if used in LoadBalancerSettings
func applySimpleDefaultLoadBalancer(c *cluster.Cluster, loadbalancer *networking.LoadBalancerSettings) {
	c.LbPolicy = defaultLBAlgorithm()
	switch c.LbPolicy {
	case cluster.Cluster_ROUND_ROBIN:
		applyRoundRobinLoadBalancer(c, loadbalancer)
	case cluster.Cluster_LEAST_REQUEST:
		applyLeastRequestLoadBalancer(c, loadbalancer)
	}
}

func defaultLBAlgorithm() cluster.Cluster_LbPolicy {
	return cluster.Cluster_LEAST_REQUEST
}

// applyRoundRobinLoadBalancer will set the LbPolicy and create an LbConfig for ROUND_ROBIN if used in LoadBalancerSettings
func applyRoundRobinLoadBalancer(c *cluster.Cluster, loadbalancer *networking.LoadBalancerSettings) {
	c.LbPolicy = cluster.Cluster_ROUND_ROBIN

	switch {
	case loadbalancer.GetWarmup() != nil:
		c.LbConfig = &cluster.Cluster_RoundRobinLbConfig_{
			RoundRobinLbConfig: &cluster.Cluster_RoundRobinLbConfig{
				SlowStartConfig: setWarmup(loadbalancer.GetWarmup()),
			},
		}

	// Deprecated: uses setWarmup instead
	case loadbalancer.GetWarmupDurationSecs() != nil:
		c.LbConfig = &cluster.Cluster_RoundRobinLbConfig_{
			RoundRobinLbConfig: &cluster.Cluster_RoundRobinLbConfig{
				SlowStartConfig: setSlowStartConfig(loadbalancer.GetWarmupDurationSecs()),
			},
		}
	}
}

// applyLeastRequestLoadBalancer will set the LbPolicy and create an LbConfig for LEAST_REQUEST if used in LoadBalancerSettings
func applyLeastRequestLoadBalancer(c *cluster.Cluster, loadbalancer *networking.LoadBalancerSettings) {
	c.LbPolicy = cluster.Cluster_LEAST_REQUEST

	switch {
	case loadbalancer.GetWarmup() != nil:
		c.LbConfig = &cluster.Cluster_LeastRequestLbConfig_{
			LeastRequestLbConfig: &cluster.Cluster_LeastRequestLbConfig{
				SlowStartConfig: setWarmup(loadbalancer.GetWarmup()),
			},
		}

	// Deprecated: uses setWarmup instead
	case loadbalancer.GetWarmupDurationSecs() != nil:
		c.LbConfig = &cluster.Cluster_LeastRequestLbConfig_{
			LeastRequestLbConfig: &cluster.Cluster_LeastRequestLbConfig{
				SlowStartConfig: setSlowStartConfig(loadbalancer.GetWarmupDurationSecs()),
			},
		}
	}
}

// setSlowStartConfig will set the warmupDurationSecs for LEAST_REQUEST and ROUND_ROBIN if provided in DestinationRule
//
// Deprecated: use setWarmup instead
func setSlowStartConfig(dur *durationpb.Duration) *cluster.Cluster_SlowStartConfig {
	return &cluster.Cluster_SlowStartConfig{
		SlowStartWindow: dur,
	}
}

// setWarmup will set the warmup configuration for LEAST_REQUEST and ROUND_ROBIN if provided in DestinationRule
// Runtime key is not exposed,the value is fixed
func setWarmup(warmup *networking.WarmupConfiguration) *cluster.Cluster_SlowStartConfig {
	var aggression, minWeightPercent float64

	// If not specified, aggression defaults to 1.0 ensuring a linear traffic rampup
	if a := warmup.Aggression; a == nil {
		aggression = 1
	} else {
		aggression = a.GetValue()
	}

	// If not specified, minWeightPersent default to 10, aligned with envoy default
	if m := warmup.MinimumPercent; m == nil {
		minWeightPercent = 10
	} else {
		minWeightPercent = m.GetValue()
	}

	return &cluster.Cluster_SlowStartConfig{
		SlowStartWindow:  warmup.Duration,
		Aggression:       &core.RuntimeDouble{DefaultValue: aggression, RuntimeKey: "("},
		MinWeightPercent: &xdstype.Percent{Value: minWeightPercent},
	}
}

// getDefaultCircuitBreakerThresholds returns a copy of the default circuit breaker thresholds for the given traffic direction.
func getDefaultCircuitBreakerThresholds() *cluster.CircuitBreakers_Thresholds {
	return &cluster.CircuitBreakers_Thresholds{
		// DefaultMaxRetries specifies the default for the Envoy circuit breaker parameter max_retries. This
		// defines the maximum number of parallel retries a given Envoy will allow to the upstream cluster. Envoy defaults
		// this value to 3, however that has shown to be insufficient during periods of pod churn (e.g. rolling updates),
		// where multiple endpoints in a cluster are terminated. In these scenarios the circuit breaker can kick
		// in before Pilot is able to deliver an updated endpoint list to Envoy, leading to client-facing 503s.
		MaxRetries:         &wrapperspb.UInt32Value{Value: math.MaxUint32},
		MaxRequests:        &wrapperspb.UInt32Value{Value: math.MaxUint32},
		MaxConnections:     &wrapperspb.UInt32Value{Value: math.MaxUint32},
		MaxPendingRequests: &wrapperspb.UInt32Value{Value: math.MaxUint32},
		TrackRemaining:     !features.DisableTrackRemainingMetrics,
	}
}

// FIXME: there isn't a way to distinguish between unset values and zero values
func applyOutlierDetection(service *model.Service, c *cluster.Cluster, outlier *networking.OutlierDetection) {
	if outlier == nil {
		return
	}

	out := &cluster.OutlierDetection{}

	// SuccessRate based outlier detection should be disabled.
	out.EnforcingSuccessRate = &wrapperspb.UInt32Value{Value: 0}

	if e := outlier.Consecutive_5XxErrors; e != nil {
		v := e.GetValue()

		out.Consecutive_5Xx = &wrapperspb.UInt32Value{Value: v}

		if v > 0 {
			v = 100
		}
		out.EnforcingConsecutive_5Xx = &wrapperspb.UInt32Value{Value: v}
	}
	if e := outlier.ConsecutiveGatewayErrors; e != nil {
		v := e.GetValue()

		out.ConsecutiveGatewayFailure = &wrapperspb.UInt32Value{Value: v}

		if v > 0 {
			v = 100
		}
		out.EnforcingConsecutiveGatewayFailure = &wrapperspb.UInt32Value{Value: v}
	}

	if outlier.Interval != nil {
		out.Interval = outlier.Interval
	}
	if outlier.BaseEjectionTime != nil {
		out.BaseEjectionTime = outlier.BaseEjectionTime
	}
	if outlier.MaxEjectionPercent > 0 {
		out.MaxEjectionPercent = &wrapperspb.UInt32Value{Value: uint32(outlier.MaxEjectionPercent)}
	}

	if outlier.SplitExternalLocalOriginErrors {
		out.SplitExternalLocalOriginErrors = true
		if outlier.ConsecutiveLocalOriginFailures.GetValue() > 0 {
			out.ConsecutiveLocalOriginFailure = &wrapperspb.UInt32Value{Value: outlier.ConsecutiveLocalOriginFailures.Value}
			out.EnforcingConsecutiveLocalOriginFailure = &wrapperspb.UInt32Value{Value: 100}
		}
		// SuccessRate based outlier detection should be disabled.
		out.EnforcingLocalOriginSuccessRate = &wrapperspb.UInt32Value{Value: 0}
	}

	c.OutlierDetection = out

	// Disable panic threshold by default as its not typically applicable in k8s environments
	// with few pods per service.
	// To do so, set the healthy_panic_threshold field even if its value is 0 (defaults to 50 in Envoy).
	// FIXME: we can't distinguish between it being unset or being explicitly set to 0
	minHealthPercent := outlier.MinHealthPercent
	if minHealthPercent >= 0 {
		// When we are sending unhealthy endpoints, we should disable Panic Threshold. Otherwise
		// Envoy will send traffic to "Unready" pods when the percentage of healthy hosts fall
		// below minimum health percentage.
		if service.SupportsUnhealthyEndpoints() {
			minHealthPercent = 0
		}
		c.CommonLbConfig.HealthyPanicThreshold = &xdstype.Percent{Value: float64(minHealthPercent)}
	}
}

// ApplyRingHashLoadBalancer will set the LbPolicy and create an LbConfig for RING_HASH if  used in LoadBalancerSettings
func ApplyRingHashLoadBalancer(c *cluster.Cluster, lb *networking.LoadBalancerSettings) {
	consistentHash := lb.GetConsistentHash()
	if consistentHash == nil {
		return
	}

	switch {
	case consistentHash.GetMaglev() != nil:
		c.LbPolicy = cluster.Cluster_MAGLEV
		if consistentHash.GetMaglev().TableSize != 0 {
			c.LbConfig = &cluster.Cluster_MaglevLbConfig_{
				MaglevLbConfig: &cluster.Cluster_MaglevLbConfig{
					TableSize: &wrapperspb.UInt64Value{Value: consistentHash.GetMaglev().TableSize},
				},
			}
		}
	case consistentHash.GetRingHash() != nil:
		c.LbPolicy = cluster.Cluster_RING_HASH
		if consistentHash.GetRingHash().MinimumRingSize != 0 {
			c.LbConfig = &cluster.Cluster_RingHashLbConfig_{
				RingHashLbConfig: &cluster.Cluster_RingHashLbConfig{
					MinimumRingSize: &wrapperspb.UInt64Value{Value: consistentHash.GetRingHash().MinimumRingSize},
				},
			}
		}
	default:
		// Check the deprecated MinimumRingSize.
		// TODO: MinimumRingSize is an int, and zero could potentially
		// be a valid value unable to distinguish between set and unset
		// case currently.
		// 1024 is the default value for envoy.
		minRingSize := &wrapperspb.UInt64Value{Value: 1024}

		if consistentHash.MinimumRingSize != 0 { // nolint: staticcheck
			minRingSize = &wrapperspb.UInt64Value{Value: consistentHash.GetMinimumRingSize()} // nolint: staticcheck
		}
		c.LbPolicy = cluster.Cluster_RING_HASH
		c.LbConfig = &cluster.Cluster_RingHashLbConfig_{
			RingHashLbConfig: &cluster.Cluster_RingHashLbConfig{
				MinimumRingSize: minRingSize,
			},
		}
	}
}

func (cb *ClusterBuilder) applyUpstreamProxyProtocol(
	opts *buildClusterOpts,
	proxyProtocol *networking.TrafficPolicy_ProxyProtocol,
) {
	if proxyProtocol == nil {
		return
	}
	c := opts.mutable

	// No existing transport; wrap RawBuffer.
	if c.cluster.TransportSocket == nil && len(c.cluster.TransportSocketMatches) == 0 {
		c.cluster.TransportSocket = &core.TransportSocket{
			Name: "envoy.transport_sockets.upstream_proxy_protocol",
			ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: protoconv.MessageToAny(&proxyprotocol.ProxyProtocolUpstreamTransport{
				Config:          &core.ProxyProtocolConfig{Version: core.ProxyProtocolConfig_Version(proxyProtocol.Version)},
				TransportSocket: util.RawBufferTransport(),
			})},
		}
		return
	}

	if c.cluster.TransportSocket != nil {
		// add an upstream proxy protocol wrapper for transportSocket
		c.cluster.TransportSocket = &core.TransportSocket{
			Name: wellknown.TransportSocketPROXY,
			ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: protoconv.MessageToAny(&proxyprotocol.ProxyProtocolUpstreamTransport{
				Config:          &core.ProxyProtocolConfig{Version: core.ProxyProtocolConfig_Version(proxyProtocol.Version)},
				TransportSocket: c.cluster.TransportSocket,
			})},
		}
	}

	// add an upstream proxy protocol wrapper for each transportSocket
	for _, tsm := range c.cluster.TransportSocketMatches {
		tsm.TransportSocket = &core.TransportSocket{
			Name: wellknown.TransportSocketPROXY,
			ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: protoconv.MessageToAny(&proxyprotocol.ProxyProtocolUpstreamTransport{
				Config:          &core.ProxyProtocolConfig{Version: core.ProxyProtocolConfig_Version(proxyProtocol.Version)},
				TransportSocket: tsm.TransportSocket,
			})},
		}
	}
}
