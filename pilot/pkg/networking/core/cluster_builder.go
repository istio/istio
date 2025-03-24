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

package core

import (
	"fmt"
	"math"
	"time"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	cares "github.com/envoyproxy/go-control-plane/envoy/extensions/network/dns_resolver/cares/v3"
	http "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	anypb "google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/telemetry"
	"istio.io/istio/pilot/pkg/networking/util"
	networkutil "istio.io/istio/pilot/pkg/util/network"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pilot/pkg/xds/endpoints"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

var maxSecondsValue = int64((math.MaxInt64 - 999999999) / (1000 * 1000 * 1000)) // 9223372035, which is about 292 years.

// passthroughHttpProtocolOptions are http protocol options used for pass through clusters.
// nolint
// revive:disable-next-line
var passthroughHttpProtocolOptions = protoconv.MessageToAny(&http.HttpProtocolOptions{
	CommonHttpProtocolOptions: &core.HttpProtocolOptions{
		IdleTimeout: durationpb.New(5 * time.Minute),
	},
	UpstreamProtocolOptions: &http.HttpProtocolOptions_UseDownstreamProtocolConfig{
		UseDownstreamProtocolConfig: &http.HttpProtocolOptions_UseDownstreamHttpConfig{
			HttpProtocolOptions:  &core.Http1ProtocolOptions{},
			Http2ProtocolOptions: http2ProtocolOptions(),
		},
	},
})

var preserveCaseFormatterConfig = &core.Http1ProtocolOptions{
	HeaderKeyFormat: &core.Http1ProtocolOptions_HeaderKeyFormat{
		HeaderFormat: &core.Http1ProtocolOptions_HeaderKeyFormat_StatefulFormatter{
			StatefulFormatter: &core.TypedExtensionConfig{
				Name: "preserve_case",
				TypedConfig: &anypb.Any{
					TypeUrl: "type.googleapis.com/envoy.extensions.http.header_formatters.preserve_case.v3.PreserveCaseFormatterConfig",
				},
			},
		},
	},
}

// clusterWrapper wraps Cluster object along with upstream protocol options.
type clusterWrapper struct {
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
	// The fields below that influence cluster configuration must be reflected in clusterCache
	// to ensure accurate differentiation and caching of clusters.
	serviceTargets     []model.ServiceTarget // Service targets of Proxy.
	metadataCerts      *metadataCerts        // Client certificates specified in metadata.
	clusterID          string                // Cluster in which proxy is running.
	proxyID            string                // Identifier that uniquely identifies a proxy.
	proxyMetadata      *model.NodeMetadata   // Metadata of the proxy.
	proxyVersion       *model.IstioVersion   // Version of Proxy.
	proxyType          model.NodeType        // Indicates whether the proxy is sidecar or gateway.
	sidecarScope       *model.SidecarScope   // Computed sidecar for the proxy.
	passThroughBindIPs []string              // Passthrough IPs to be used while building clusters.
	supportsIPv4       bool                  // Whether Proxy IPs has IPv4 address.
	supportsIPv6       bool                  // Whether Proxy IPs has IPv6 address.
	sendHbone          bool                  // Does the proxy support HBONE
	locality           *core.Locality        // Locality information of proxy.
	proxyLabels        map[string]string     // Proxy labels.
	proxyView          model.ProxyView       // Proxy view of endpoints.
	proxyIPAddresses   []string              // IP addresses on which proxy is listening on.
	configNamespace    string                // Proxy config namespace.
	// PushRequest to look for updates.
	req                       *model.PushRequest
	cache                     model.XdsCache
	credentialSocketExist     bool
	fileCredentialSocketExist bool
}

// NewClusterBuilder builds an instance of ClusterBuilder.
func NewClusterBuilder(proxy *model.Proxy, req *model.PushRequest, cache model.XdsCache) *ClusterBuilder {
	cb := &ClusterBuilder{
		serviceTargets:     proxy.ServiceTargets,
		proxyID:            proxy.ID,
		proxyMetadata:      proxy.Metadata,
		proxyType:          proxy.Type,
		proxyVersion:       model.ParseIstioVersion(proxy.Metadata.IstioVersion),
		sidecarScope:       proxy.SidecarScope,
		passThroughBindIPs: getPassthroughBindIPs(proxy.GetIPMode()),
		supportsIPv4:       proxy.SupportsIPv4(),
		supportsIPv6:       proxy.SupportsIPv6(),
		sendHbone:          features.EnableHBONESend || proxy.IsWaypointProxy(),
		locality:           proxy.Locality,
		proxyLabels:        proxy.Labels,
		proxyView:          proxy.GetView(),
		proxyIPAddresses:   proxy.IPAddresses,
		configNamespace:    proxy.ConfigNamespace,
		req:                req,
		cache:              cache,
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
		if proxy.Metadata.Raw[security.CredentialMetaDataName] == "true" {
			cb.credentialSocketExist = true
		}
		if proxy.Metadata.Raw[security.CredentialFileMetaDataName] == "true" {
			cb.fileCredentialSocketExist = true
		}
	}
	return cb
}

func (m *metadataCerts) String() string {
	return m.tlsClientCertChain + "~" + m.tlsClientKey + "~" + m.tlsClientRootCert
}

// newClusterWrapper initializes clusterWrapper with the cluster passed.
func newClusterWrapper(cluster *cluster.Cluster) *clusterWrapper {
	return &clusterWrapper{
		cluster: cluster,
	}
}

func (cb *ClusterBuilder) applyLbSubsetConfig(cw *clusterWrapper, subsetSelectorKey string) {
	// TODO(liorlieberman) handle a case where subsetConfig already exist?
	if cw.cluster.LbSubsetConfig == nil {
		cw.cluster.LbSubsetConfig = &cluster.Cluster_LbSubsetConfig{}
	}
	cw.cluster.LbSubsetConfig.FallbackPolicy = cluster.Cluster_LbSubsetConfig_ANY_ENDPOINT
	if len(cw.cluster.LbSubsetConfig.SubsetSelectors) == 0 {
		cw.cluster.LbSubsetConfig.SubsetSelectors = []*cluster.Cluster_LbSubsetConfig_LbSubsetSelector{}
	}

	// TODO(liorlieberman) think about this logic - should we handle a case where subsetSelectors already exist
	// and do we put the selector needed for inference before?
	// TODO(liorlieberman) Maybe this function should be more Inference related and not generic "applySubsetConfig"?
	addKey := true
	for _, ss := range cw.cluster.LbSubsetConfig.SubsetSelectors {
		if slices.Contains(ss.Keys, subsetSelectorKey) {
			addKey = false
		}
	}
	if addKey {
		cw.cluster.LbSubsetConfig.SubsetSelectors = append(cw.cluster.LbSubsetConfig.SubsetSelectors, &cluster.Cluster_LbSubsetConfig_LbSubsetSelector{
			Keys: []string{
				subsetSelectorKey,
			},
		})
	}
}

// sidecarProxy returns true if the clusters are being built for sidecar proxy otherwise false.
func (cb *ClusterBuilder) sidecarProxy() bool {
	return cb.proxyType == model.SidecarProxy
}

func (cb *ClusterBuilder) buildSubsetCluster(
	opts buildClusterOpts, destRule *config.Config, subset *networking.Subset, service *model.Service,
	endpointBuilder *endpoints.EndpointBuilder,
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
		lbEndpoints = endpointBuilder.WithSubset(subset.Name).FromServiceEndpoints()
	}

	subsetCluster := cb.buildCluster(subsetClusterName, clusterType, lbEndpoints, model.TrafficDirectionOutbound, opts.port, service, nil, subset.Name)
	if subsetCluster == nil {
		return nil
	}

	// Apply traffic policy for subset cluster with the destination rule traffic policy.
	opts.mutable = subsetCluster
	opts.istioMtlsSni = defaultSni

	// If subset has a traffic policy, apply it so that it overrides the destination rule traffic policy.
	opts.policy = util.MergeSubsetTrafficPolicy(opts.policy, subset.TrafficPolicy, opts.port)

	if destRule != nil {
		destinationRule := CastDestinationRule(destRule)
		opts.isDrWithSelector = destinationRule.GetWorkloadSelector() != nil
	}
	// Apply traffic policy for the subset cluster.
	cb.applyTrafficPolicy(service, opts)

	maybeApplyEdsConfig(subsetCluster.cluster)

	cb.applyMetadataExchange(opts.mutable.cluster)

	// Add the DestinationRule+subsets metadata. Metadata here is generated on a per-cluster
	// basis in buildCluster, so we can just insert without a copy.
	subsetCluster.cluster.Metadata = util.AddConfigInfoMetadata(subsetCluster.cluster.Metadata, destRule.Meta)
	util.AddSubsetToMetadata(subsetCluster.cluster.Metadata, subset.Name)
	subsetCluster.cluster.Metadata = util.AddALPNOverrideToMetadata(subsetCluster.cluster.Metadata, opts.policy.GetTls().GetMode())
	return subsetCluster.build()
}

// applyDestinationRule applies the destination rule if it exists for the Service.
// It returns the subset clusters if any created as it applies the destination rule.
func (cb *ClusterBuilder) applyDestinationRule(mc *clusterWrapper, clusterMode ClusterMode, service *model.Service,
	port *model.Port, eb *endpoints.EndpointBuilder, destRule *config.Config, serviceAccounts []string,
) []*cluster.Cluster {
	destinationRule := CastDestinationRule(destRule)
	// merge applicable port level traffic policy settings
	trafficPolicy, _ := util.GetPortLevelTrafficPolicy(destinationRule.GetTrafficPolicy(), port)
	opts := buildClusterOpts{
		mesh:                      cb.req.Push.Mesh,
		serviceTargets:            cb.serviceTargets,
		mutable:                   mc,
		policy:                    trafficPolicy,
		port:                      port,
		clusterMode:               clusterMode,
		direction:                 model.TrafficDirectionOutbound,
		credentialSocketExist:     cb.credentialSocketExist,
		fileCredentialSocketExist: cb.fileCredentialSocketExist,
	}

	if clusterMode == DefaultClusterMode {
		opts.serviceAccounts = serviceAccounts
		opts.istioMtlsSni = model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, "", service.Hostname, port.Port)
		opts.meshExternal = service.MeshExternal
		opts.serviceRegistry = service.Attributes.ServiceRegistry
		opts.serviceMTLSMode = cb.req.Push.BestEffortInferServiceMTLSMode(destinationRule.GetTrafficPolicy(), service, port)
		opts.allInstancesHBONE = cb.req.Push.AllInstancesSupportHBONE(service, port)
	}

	if destRule != nil {
		opts.isDrWithSelector = destinationRule.GetWorkloadSelector() != nil
	}
	// Apply traffic policy for the main default cluster.
	cb.applyTrafficPolicy(service, opts)

	// Apply EdsConfig if needed. This should be called after traffic policy is applied because, traffic policy might change
	// discovery type.
	maybeApplyEdsConfig(mc.cluster)

	cb.applyMetadataExchange(opts.mutable.cluster)

	if service.MeshExternal || opts.allInstancesHBONE {
		// Conditionally skips based on config
		key := "external"
		if opts.allInstancesHBONE {
			// Unconditionally skips
			key = "disable_mx"
		}
		im := getOrCreateIstioMetadata(mc.cluster)
		im.Fields[key] = &structpb.Value{
			Kind: &structpb.Value_BoolValue{
				BoolValue: true,
			},
		}
	}

	if destRule != nil {
		mc.cluster.Metadata = util.AddConfigInfoMetadata(mc.cluster.Metadata, destRule.Meta)
		mc.cluster.Metadata = util.AddALPNOverrideToMetadata(mc.cluster.Metadata, opts.policy.GetTls().GetMode())
	}
	subsetClusters := make([]*cluster.Cluster, 0)
	for _, subset := range destinationRule.GetSubsets() {
		subsetCluster := cb.buildSubsetCluster(opts, destRule, subset, service, eb)
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

// buildCluster builds the default cluster and also applies global options.
// It is used for building both inbound and outbound cluster.
func (cb *ClusterBuilder) buildCluster(name string, discoveryType cluster.Cluster_DiscoveryType,
	localityLbEndpoints []*endpoint.LocalityLbEndpoints, direction model.TrafficDirection,
	port *model.Port, service *model.Service, inboundServices []model.ServiceTarget,
	subset string,
) *clusterWrapper {
	c := &cluster.Cluster{
		Name:                 name,
		ClusterDiscoveryType: &cluster.Cluster_Type{Type: discoveryType},
		CommonLbConfig:       &cluster.Cluster_CommonLbConfig{},
	}

	// Build default alt stat name - This may be overwritten by the MeshConfig options.
	c.AltStatName = util.DelimitedStatsPrefix(name)

	switch discoveryType {
	case cluster.Cluster_STRICT_DNS, cluster.Cluster_LOGICAL_DNS:
		if networkutil.AllIPv4(cb.proxyIPAddresses) {
			// IPv4 only
			c.DnsLookupFamily = cluster.Cluster_V4_ONLY
		} else if networkutil.AllIPv6(cb.proxyIPAddresses) {
			// IPv6 only
			c.DnsLookupFamily = cluster.Cluster_V6_ONLY
			// If we are in this mode, Istio sees ourselves as only have IPv6 addresses, but there is actually a link-local
			// interface that serves the IPv4. Allow both families.
			// This ensures we do not break DNS resolution to destinations that are IPv4 only.
			if features.EnableAdditionalIpv4OutboundListenerForIpv6Only {
				c.DnsLookupFamily = cluster.Cluster_ALL
			}
		} else {
			// Dual Stack
			if features.EnableDualStack {
				// using Cluster_ALL to enable Happy Eyeballsfor upstream connections
				c.DnsLookupFamily = cluster.Cluster_ALL
			} else {
				// keep the original logic if Dual Stack is disable
				c.DnsLookupFamily = cluster.Cluster_V4_ONLY
			}
		}
		dnsResolverConfig, err := anypb.New(&cares.CaresDnsResolverConfig{
			UdpMaxQueries: wrappers.UInt32(features.PilotDNSCaresUDPMaxQueries),
		})
		if err != nil {
			log.Warnf("Could not create typed_dns_cluster_config for %s: %s. Using default configuration.", name, err)
		} else {
			c.TypedDnsResolverConfig = &core.TypedExtensionConfig{
				Name:        "envoy.network.dns_resolver.cares",
				TypedConfig: dnsResolverConfig,
			}
		}
		// 0 disables jitter.
		c.DnsJitter = durationpb.New(features.PilotDNSJitterDurationEnv) //nolint:staticcheck // DnsJitter is deprecated
		c.DnsRefreshRate = cb.req.Push.Mesh.DnsRefreshRate               //nolint:staticcheck // DnsRefreshRate is deprecated
		c.RespectDnsTtl = true                                           //nolint:staticcheck // RespectDnsTtl is deprecated
		// we want to run all the STATIC parts as well to build the load assignment
		fallthrough
	case cluster.Cluster_STATIC:
		if len(localityLbEndpoints) == 0 {
			log.Debugf("locality endpoints missing for cluster %s", c.Name)
			cb.req.Push.AddMetric(model.DNSNoEndpointClusters, c.Name, cb.proxyID,
				fmt.Sprintf("%s cluster without endpoints %s found while pushing CDS", discoveryType.String(), c.Name))
			return nil
		}
		c.LoadAssignment = &endpoint.ClusterLoadAssignment{
			ClusterName: name,
			Endpoints:   localityLbEndpoints,
		}
	case cluster.Cluster_ORIGINAL_DST:
		if override, f := service.Attributes.PassthroughTargetPorts[uint32(port.Port)]; f {
			c.LbConfig = &cluster.Cluster_OriginalDstLbConfig_{
				OriginalDstLbConfig: &cluster.Cluster_OriginalDstLbConfig{
					UpstreamPortOverride: wrappers.UInt32(override),
				},
			}
		}
	}

	ec := newClusterWrapper(c)
	cb.setUpstreamProtocol(ec, port)
	addTelemetryMetadata(c, port, service, direction, inboundServices)
	if direction == model.TrafficDirectionOutbound {
		// If stat name is configured, build the alternate stats name.
		if len(cb.req.Push.Mesh.OutboundClusterStatName) != 0 {
			statPrefix := telemetry.BuildStatPrefix(cb.req.Push.Mesh.OutboundClusterStatName, string(service.Hostname), subset, port, 0, &service.Attributes)
			ec.cluster.AltStatName = util.DelimitedStatsPrefix(statPrefix)
		}
	}

	return ec
}

// buildInboundCluster constructs a single inbound cluster. The cluster will be bound to
// `inbound|clusterPort||`, and send traffic to <bind>:<instance.Endpoint.EndpointPort>. A workload
// will have a single inbound cluster per port. In general this works properly, with the exception of
// the Service-oriented DestinationRule, and upstream protocol selection. Our documentation currently
// requires a single protocol per port, and the DestinationRule issue is slated to move to Sidecar.
// Note: clusterPort and instance.Endpoint.EndpointPort are identical for standard Services; however,
// Sidecar.Ingress allows these to be different.
func (cb *ClusterBuilder) buildInboundCluster(clusterPort int, bind string,
	proxy *model.Proxy, inboundServices []model.ServiceTarget,
) *clusterWrapper {
	// should not happen
	if len(inboundServices) == 0 {
		return nil
	}
	instance := inboundServices[0]
	localityLbEndpoints := buildInboundLocalityLbEndpoints(bind, instance.Port.TargetPort)
	clusterType := cluster.Cluster_ORIGINAL_DST
	if len(localityLbEndpoints) > 0 {
		clusterType = cluster.Cluster_STATIC
	}
	clusterName := model.BuildInboundSubsetKey(clusterPort)
	localCluster := cb.buildCluster(clusterName, clusterType, localityLbEndpoints,
		model.TrafficDirectionInbound, instance.Port.ServicePort, instance.Service, inboundServices, "")
	// If stat name is configured, build the alt statname.
	if len(cb.req.Push.Mesh.InboundClusterStatName) != 0 {
		statPrefix := telemetry.BuildStatPrefix(cb.req.Push.Mesh.InboundClusterStatName,
			string(instance.Service.Hostname), "", instance.Port.ServicePort, clusterPort,
			&instance.Service.Attributes)
		localCluster.cluster.AltStatName = util.DelimitedStatsPrefix(statPrefix)
	}
	if clusterType == cluster.Cluster_ORIGINAL_DST {
		// Disable cleanup for inbound clusters - set to Max possible duration.
		localCluster.cluster.CleanupInterval = durationpb.New(time.Duration(maxSecondsValue) * time.Second)
	}

	opts := buildClusterOpts{
		mesh:            cb.req.Push.Mesh,
		mutable:         localCluster,
		policy:          nil,
		port:            instance.Port.ServicePort,
		serviceAccounts: nil,
		serviceTargets:  cb.serviceTargets,
		istioMtlsSni:    "",
		clusterMode:     DefaultClusterMode,
		direction:       model.TrafficDirectionInbound,
	}
	// When users specify circuit breakers, they need to be set on the receiver end
	// (server side) as well as client side, so that the server has enough capacity
	// (not the defaults) to handle the increased traffic volume
	// TODO: This is not foolproof - if instance is part of multiple services listening on same port,
	// choice of inbound cluster is arbitrary. So the connection pool settings may not apply cleanly.
	cfg := proxy.SidecarScope.DestinationRule(model.TrafficDirectionInbound, proxy, instance.Service.Hostname).GetRule()
	if cfg != nil {
		destinationRule := CastDestinationRule(cfg)
		opts.isDrWithSelector = destinationRule.GetWorkloadSelector() != nil
		if destinationRule.TrafficPolicy != nil {
			opts.policy, _ = util.GetPortLevelTrafficPolicy(destinationRule.TrafficPolicy, instance.Port.ServicePort)
			util.AddConfigInfoMetadata(localCluster.cluster.Metadata, cfg.Meta)
		}
	}
	// If there's a connection pool set on the Sidecar then override any settings derived from the DestinationRule
	// with those set by Sidecar resource. This allows the user to resolve any ambiguity, e.g. in the case that
	// multiple services are listening on the same port.
	if sidecarConnPool := proxy.SidecarScope.InboundConnectionPoolForPort(clusterPort); sidecarConnPool != nil {
		if opts.policy == nil {
			// There was no destination rule, so no inbound traffic policy; we'll create a default
			opts.policy = &networking.TrafficPolicy{}
		} else {
			// copy policy to prevent mutating the original destinationRule trafficPolicy
			opts.policy = util.ShallowCopyTrafficPolicy(opts.policy)
		}
		opts.policy.ConnectionPool = sidecarConnPool
	}
	cb.applyTrafficPolicy(nil, opts)

	if bind != LocalhostAddress && bind != LocalhostIPv6Address {
		// iptables will redirect our own traffic to localhost back to us if we do not use the "magic" upstream bind
		// config which will be skipped.
		localCluster.cluster.UpstreamBindConfig = &core.BindConfig{
			SourceAddress: &core.SocketAddress{
				Address: cb.passThroughBindIPs[0],
				PortSpecifier: &core.SocketAddress_PortValue{
					PortValue: uint32(0),
				},
			},
		}
		// There is a usage doc here:
		// https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/core/v3/address.proto#config-core-v3-bindconfig
		// to support Dual Stack via Envoy BindConfig, and below is the related issue/PR in Envoy:
		// https://github.com/envoyproxy/envoy/issues/9811
		// https://github.com/envoyproxy/envoy/pull/22639.
		// The extra source address for UpstreamBindConfig should be added if dual stack is enabled and there is
		// more than one IP for the proxy.
		if features.EnableDualStack && len(cb.passThroughBindIPs) > 1 {
			// add extra source addresses to cluster builder
			var extraSrcAddrs []*core.ExtraSourceAddress
			for _, extraBdIP := range cb.passThroughBindIPs[1:] {
				extraSrcAddr := &core.ExtraSourceAddress{
					Address: &core.SocketAddress{
						Address: extraBdIP,
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: uint32(0),
						},
					},
				}
				extraSrcAddrs = append(extraSrcAddrs, extraSrcAddr)
			}
			localCluster.cluster.UpstreamBindConfig.ExtraSourceAddresses = extraSrcAddrs
		}
	}
	return localCluster
}

// buildInboundPassthroughCluster builds passthrough cluster for inbound.
func (cb *ClusterBuilder) buildInboundPassthroughCluster() *cluster.Cluster {
	// We need to set a local bind address, which we will match in iptables to avoid looping back to ourselves.
	// This needs a per-IP-version, since we cannot bind to IPv4 and send to IPv6 (or the inverse).
	// Fortunately, Envoy can natively handle this by giving it a local v4 and v6 address, and it will pick which to use for us.
	src := InboundPassthroughBindIpv4
	if !cb.supportsIPv4 {
		src = InboundPassthroughBindIpv6
	}
	var extraSrc []*core.ExtraSourceAddress
	if cb.supportsIPv4 && cb.supportsIPv6 {
		extraSrc = append(extraSrc, &core.ExtraSourceAddress{
			Address: &core.SocketAddress{
				Address:       InboundPassthroughBindIpv6,
				PortSpecifier: &core.SocketAddress_PortValue{PortValue: uint32(0)},
			},
		})
	}
	c := cb.buildDefaultPassthroughCluster()
	c.Name = util.InboundPassthroughCluster
	c.Filters = nil
	c.UpstreamBindConfig = &core.BindConfig{
		SourceAddress: &core.SocketAddress{
			Address:       src,
			PortSpecifier: &core.SocketAddress_PortValue{PortValue: uint32(0)},
		},
		ExtraSourceAddresses: extraSrc,
	}
	return c
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
	c.AltStatName = util.DelimitedStatsPrefix(util.BlackHoleCluster)
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
		TypedExtensionProtocolOptions: map[string]*anypb.Any{
			v3.HttpProtocolOptionsType: passthroughHttpProtocolOptions,
		},
	}
	cluster.AltStatName = util.DelimitedStatsPrefix(util.PassthroughCluster)
	cb.applyConnectionPool(cb.req.Push.Mesh, newClusterWrapper(cluster), &networking.ConnectionPoolSettings{}, nil)
	cb.applyMetadataExchange(cluster)
	return cluster
}

// setH2Options make the cluster an h2 cluster by setting http2ProtocolOptions.
func setH2Options(mc *clusterWrapper) {
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

type mtlsContextType int

const (
	userSupplied mtlsContextType = iota
	autoDetected
)

func (cb *ClusterBuilder) setUseDownstreamProtocol(mc *clusterWrapper) {
	if mc.httpProtocolOptions == nil {
		mc.httpProtocolOptions = &http.HttpProtocolOptions{}
	}
	options := mc.httpProtocolOptions
	// Inherit the HTTP1.x protocol options from the explicit HTTP1.x config.
	explicitHTTP1Options := &core.Http1ProtocolOptions{}
	if opts := options.GetExplicitHttpConfig().GetHttpProtocolOptions(); opts != nil {
		explicitHTTP1Options = opts
	}
	options.UpstreamProtocolOptions = &http.HttpProtocolOptions_UseDownstreamProtocolConfig{
		UseDownstreamProtocolConfig: &http.HttpProtocolOptions_UseDownstreamHttpConfig{
			HttpProtocolOptions:  explicitHTTP1Options,
			Http2ProtocolOptions: http2ProtocolOptions(),
		},
	}
}

func http2ProtocolOptions() *core.Http2ProtocolOptions {
	return &core.Http2ProtocolOptions{}
}

// nolint
// revive:disable-next-line
func isHttp2Cluster(mc *clusterWrapper) bool {
	options := mc.httpProtocolOptions
	return options != nil && options.GetExplicitHttpConfig().GetHttp2ProtocolOptions() != nil
}

// This is called after traffic policy applied
func (cb *ClusterBuilder) setUpstreamProtocol(cluster *clusterWrapper, port *model.Port) {
	if port.Protocol.IsHTTP2() {
		setH2Options(cluster)
		return
	}
	// Preserve HTTP/1.x traffic header case
	isExplicitHTTP := port.Protocol.IsHTTP()
	isAutoProtocol := port.Protocol.IsUnsupported()
	effectiveProxyConfig := cb.proxyMetadata.ProxyConfigOrDefault(cb.req.Push.Mesh.GetDefaultConfig())
	preserveHeaderCase := effectiveProxyConfig.GetProxyHeaders().GetPreserveHttp1HeaderCase().GetValue()

	if (isExplicitHTTP || isAutoProtocol) && preserveHeaderCase {
		// Apply the stateful formatter for HTTP/1.x headers
		if cluster.httpProtocolOptions == nil {
			cluster.httpProtocolOptions = &http.HttpProtocolOptions{}
		}
		options := cluster.httpProtocolOptions
		options.UpstreamProtocolOptions = &http.HttpProtocolOptions_ExplicitHttpConfig_{
			ExplicitHttpConfig: &http.HttpProtocolOptions_ExplicitHttpConfig{
				ProtocolConfig: &http.HttpProtocolOptions_ExplicitHttpConfig_HttpProtocolOptions{
					HttpProtocolOptions: preserveCaseFormatterConfig,
				},
			},
		}
	}

	// Add use_downstream_protocol for sidecar proxy only if protocol sniffing is enabled. Since
	// protocol detection is disabled for gateway and use_downstream_protocol is used under protocol
	// detection for cluster to select upstream connection protocol when the service port is unnamed.
	// use_downstream_protocol should be disabled for gateway; while it sort of makes sense there, even
	// without sniffing, a concern is that clients will do ALPN negotiation, and we always advertise
	// h2. Clients would then connect with h2, while the upstream may not support it. This is not a
	// concern for plaintext, but we do not have a way to distinguish https vs http here. If users of
	// gateway want this behavior, they can configure UseClientProtocol explicitly.
	if (cb.sidecarProxy() || cb.proxyType == model.Waypoint) && isAutoProtocol {
		// Use downstream protocol. If the incoming traffic use HTTP 1.1, the
		// upstream cluster will use HTTP 1.1, if incoming traffic use HTTP2,
		// the upstream cluster will use HTTP2.
		cb.setUseDownstreamProtocol(cluster)
	}
}

// normalizeClusters normalizes clusters to avoid duplicate clusters. This should be called
// at the end before adding the cluster to list of clusters.
func (cb *ClusterBuilder) normalizeClusters(clusters []*cluster.Cluster) []*cluster.Cluster {
	// resolve cluster name conflicts. there can be duplicate cluster names if there are conflicting service definitions.
	// for any clusters that share the same name the first cluster is kept and the others are discarded.
	have := sets.String{}
	out := make([]*cluster.Cluster, 0, len(clusters))
	for _, c := range clusters {
		if !have.InsertContains(c.Name) {
			out = append(out, c)
		} else {
			cb.req.Push.AddMetric(model.DuplicatedClusters, c.Name, cb.proxyID,
				fmt.Sprintf("Duplicate cluster %s found while pushing CDS", c.Name))
		}
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
	destinationRule := CastDestinationRule(clusterKey.destinationRule.GetRule())
	res := make([]*discovery.Resource, 0, 1+len(destinationRule.GetSubsets()))
	cachedCluster := cb.cache.Get(&clusterKey)
	allFound := cachedCluster != nil
	res = append(res, cachedCluster)
	dir, _, host, port := model.ParseSubsetKey(clusterKey.clusterName)
	for _, ss := range destinationRule.GetSubsets() {
		clusterKey.clusterName = model.BuildSubsetKey(dir, ss.Name, host, port)
		cachedCluster := cb.cache.Get(&clusterKey)
		if cachedCluster == nil {
			allFound = false
		}
		res = append(res, cachedCluster)
	}
	return res, allFound
}

// build does any final build operations needed, like marshaling etc.
func (mc *clusterWrapper) build() *cluster.Cluster {
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
		mc.cluster.TypedExtensionProtocolOptions = map[string]*anypb.Any{
			v3.HttpProtocolOptionsType: protoconv.MessageToAny(mc.httpProtocolOptions),
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

// buildExternalSDSCluster generates a cluster that acts as external SDS server
func (cb *ClusterBuilder) buildExternalSDSCluster(addr string) *cluster.Cluster {
	ep := &endpoint.LbEndpoint{
		HostIdentifier: &endpoint.LbEndpoint_Endpoint{
			Endpoint: &endpoint.Endpoint{
				Address: &core.Address{
					Address: &core.Address_Pipe{
						Pipe: &core.Pipe{
							Path: addr,
						},
					},
				},
			},
		},
	}
	options := &http.HttpProtocolOptions{}
	options.UpstreamProtocolOptions = &http.HttpProtocolOptions_ExplicitHttpConfig_{
		ExplicitHttpConfig: &http.HttpProtocolOptions_ExplicitHttpConfig{
			ProtocolConfig: &http.HttpProtocolOptions_ExplicitHttpConfig_Http2ProtocolOptions{
				Http2ProtocolOptions: http2ProtocolOptions(),
			},
		},
	}
	c := &cluster.Cluster{
		Name:                 security.SDSExternalClusterName,
		ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_STATIC},
		ConnectTimeout:       cb.req.Push.Mesh.ConnectTimeout,
		LoadAssignment: &endpoint.ClusterLoadAssignment{
			ClusterName: security.SDSExternalClusterName,
			Endpoints: []*endpoint.LocalityLbEndpoints{
				{
					LbEndpoints: []*endpoint.LbEndpoint{ep},
				},
			},
		},
		TypedExtensionProtocolOptions: map[string]*anypb.Any{
			v3.HttpProtocolOptionsType: protoconv.MessageToAny(options),
		},
	}
	c.AltStatName = util.DelimitedStatsPrefix(security.SDSExternalClusterName)
	return c
}

func addTelemetryMetadata(cluster *cluster.Cluster,
	port *model.Port, service *model.Service,
	direction model.TrafficDirection, inboundServices []model.ServiceTarget,
) {
	if !features.EnableTelemetryLabel {
		return
	}
	if cluster == nil {
		return
	}
	if direction == model.TrafficDirectionInbound &&
		(len(inboundServices) == 0 || inboundServices[0].Service.MeshExternal || port == nil) {
		// At inbound, port and local service instance has to be provided
		return
	}
	if direction == model.TrafficDirectionOutbound && service == nil {
		// At outbound, the service corresponding to the cluster has to be provided.
		return
	}

	im := getOrCreateIstioMetadata(cluster)

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
		have := sets.New[host.Name]()
		for _, svc := range inboundServices {
			if svc.Port.Port != port.Port {
				// If the service port is different from the port of the cluster that is being built,
				// skip adding telemetry metadata for the service to the cluster.
				continue
			}
			if have.Contains(svc.Service.Hostname) {
				// Skip adding metadata for instance with the same host name.
				// This could happen when a service has multiple IPs.
				continue
			}
			svcMetaList.Values = append(svcMetaList.Values, buildServiceMetadata(svc.Service))
			have.Insert(svc.Service.Hostname)
		}
	} else if direction == model.TrafficDirectionOutbound {
		// For outbound cluster, add telemetry metadata based on the service that the cluster is built for.
		svcMetaList.Values = append(svcMetaList.Values, buildServiceMetadata(service))
	}
}
