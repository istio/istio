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

package grpcgen

import (
	"fmt"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	corexds "istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/util/sets"
)

// BuildClusters handles a gRPC CDS request, used with the 'ApiListener' style of requests.
// The main difference is that the request includes Resources to filter.
func (g *GrpcConfigGenerator) BuildClusters(node *model.Proxy, push *model.PushContext, names []string) model.Resources {
	filter := newClusterFilter(names)
	clusters := make([]*cluster.Cluster, 0, len(names))
	for defaultClusterName, subsetFilter := range filter {
		builder, err := newClusterBuilder(node, push, defaultClusterName, subsetFilter)
		if err != nil {
			log.Warn(err)
			continue
		}
		clusters = append(clusters, builder.build()...)
	}

	resp := make(model.Resources, 0, len(clusters))
	for _, c := range clusters {
		resp = append(resp, &discovery.Resource{
			Name:     c.Name,
			Resource: util.MessageToAny(c),
		})
	}
	if len(resp) == 0 && len(names) == 0 {
		log.Warnf("did not generate any cds for %s; no names provided", node.ID)
	}
	return resp
}

// newClusterFilter maps a non-subset cluster name to the list of actual cluster names (default or subset) actually
// requested by the client. gRPC will usually request a group of clusters that are used in the same route; in some
// cases this means subsets associated with the same default cluster aren't all expected in the same CDS response.
func newClusterFilter(names []string) map[string]sets.Set {
	filter := map[string]sets.Set{}
	for _, name := range names {
		dir, _, hn, p := model.ParseSubsetKey(name)
		defaultKey := model.BuildSubsetKey(dir, "", hn, p)
		if _, ok := filter[defaultKey]; !ok {
			filter[defaultKey] = sets.New()
		}
		filter[defaultKey].Insert(name)
	}
	return filter
}

// clusterBuilder is responsible for building a single default and subset clusters for a service
// TODO re-use the v1alpha3.ClusterBuilder:
// Most of the logic is similar, I think we can just share the code if we expose:
// * BuildSubsetCluster
// * BuildDefaultCluster
// * BuildClusterOpts and members
// * Add something to allow us to override how tlscontext is built
type clusterBuilder struct {
	push *model.PushContext
	node *model.Proxy

	// guaranteed to be set in init
	defaultClusterName   string
	requestedClusterName string
	hostname             host.Name
	portNum              int

	// may not be set
	svc    *model.Service
	port   *model.Port
	filter sets.Set
}

func newClusterBuilder(node *model.Proxy, push *model.PushContext, defaultClusterName string, filter sets.Set) (*clusterBuilder, error) {
	_, _, hostname, portNum := model.ParseSubsetKey(defaultClusterName)
	if hostname == "" || portNum == 0 {
		return nil, fmt.Errorf("failed parsing subset key: %s", defaultClusterName)
	}

	// try to resolve the service and port
	var port *model.Port
	svc := push.ServiceForHostname(node, hostname)
	if svc == nil {
		log.Warnf("cds gen for %s: did not find service for cluster %s", node.ID, defaultClusterName)
	} else {
		var ok bool
		port, ok = svc.Ports.GetByPort(portNum)
		if !ok {
			log.Warnf("cds gen for %s: did not find port %d in service for cluster %s", node.ID, portNum, defaultClusterName)
		}
	}

	return &clusterBuilder{
		node: node,
		push: push,

		defaultClusterName: defaultClusterName,
		hostname:           hostname,
		portNum:            portNum,
		filter:             filter,

		svc:  svc,
		port: port,
	}, nil
}

// subsetFilter returns the requestedClusterName if it isn't the default cluster
// for subset clusters, gRPC may request them individually
func (b *clusterBuilder) subsetFilter() string {
	if b.defaultClusterName == b.requestedClusterName {
		return ""
	}
	return b.requestedClusterName
}

func (b *clusterBuilder) build() []*cluster.Cluster {
	var defaultCluster *cluster.Cluster
	if b.filter.Contains(b.defaultClusterName) {
		defaultCluster = edsCluster(b.defaultClusterName)
	}

	subsetClusters := b.applyDestinationRule(defaultCluster)
	out := make([]*cluster.Cluster, 0, 1+len(subsetClusters))
	if defaultCluster != nil {
		out = append(out, defaultCluster)
	}
	return append(out, subsetClusters...)
}

// edsCluster creates a simple cluster to read endpoints from ads/eds.
func edsCluster(name string) *cluster.Cluster {
	return &cluster.Cluster{
		Name:                 name,
		ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS},
		EdsClusterConfig: &cluster.Cluster_EdsClusterConfig{
			ServiceName: name,
			EdsConfig: &core.ConfigSource{
				ConfigSourceSpecifier: &core.ConfigSource_Ads{
					Ads: &core.AggregatedConfigSource{},
				},
			},
		},
	}
}

// applyDestinationRule mutates the default cluster to reflect traffic policies, and returns a set of additional
// subset clusters if specified by a destination rule
func (b *clusterBuilder) applyDestinationRule(defaultCluster *cluster.Cluster) (subsetClusters []*cluster.Cluster) {
	if b.svc == nil || b.port == nil {
		return nil
	}

	// resolve policy from context
	destinationRule := corexds.CastDestinationRule(b.node.SidecarScope.DestinationRule(
		model.TrafficDirectionOutbound, b.node, b.svc.Hostname).GetRule())
	trafficPolicy := corexds.MergeTrafficPolicy(nil, destinationRule.GetTrafficPolicy(), b.port)

	// setup default cluster
	b.applyTrafficPolicy(defaultCluster, trafficPolicy)

	// subset clusters
	if len(destinationRule.GetSubsets()) > 0 {
		subsetClusters = make([]*cluster.Cluster, 0, len(destinationRule.GetSubsets()))
		for _, subset := range destinationRule.GetSubsets() {
			subsetKey := subsetClusterKey(subset.Name, string(b.hostname), b.portNum)
			if !b.filter.Contains(subsetKey) {
				continue
			}
			c := edsCluster(subsetKey)
			trafficPolicy := corexds.MergeTrafficPolicy(trafficPolicy, subset.TrafficPolicy, b.port)
			b.applyTrafficPolicy(c, trafficPolicy)
			subsetClusters = append(subsetClusters, c)
		}
	}

	return
}

// applyTrafficPolicy mutates the give cluster (if not-nil) so that the given merged traffic policy applies.
func (b *clusterBuilder) applyTrafficPolicy(c *cluster.Cluster, trafficPolicy *networking.TrafficPolicy) {
	// cluster can be nil if it wasn't requested
	if c == nil {
		return
	}
	b.applyTLS(c, trafficPolicy)
	b.applyLoadBalancing(c, trafficPolicy)
	// TODO status or log when unsupported features are included
}

func (b *clusterBuilder) applyLoadBalancing(c *cluster.Cluster, policy *networking.TrafficPolicy) {
	switch policy.GetLoadBalancer().GetSimple() {
	case networking.LoadBalancerSettings_ROUND_ROBIN, networking.LoadBalancerSettings_UNSPECIFIED:
	// ok
	default:
		log.Warnf("cannot apply LbPolicy %s to %s", policy.LoadBalancer.GetSimple(), b.node.ID)
	}
	corexds.ApplyRingHashLoadBalancer(c, policy.GetLoadBalancer())
}

func (b *clusterBuilder) applyTLS(c *cluster.Cluster, policy *networking.TrafficPolicy) {
	// TODO for now, we leave mTLS *off* by default:
	// 1. We don't know if the client uses xds.NewClientCredentials; these settings will be ignored if not
	// 2. We cannot reach servers in PERMISSIVE mode; gRPC doesn't allow us to override the alpn to one of Istio's
	// 3. Once we support gRPC servers, we have no good way to detect if a server is implemented with xds.NewGrpcServer and will actually support our config
	// For these reasons, support only explicit tls configuration.
	switch policy.GetTls().GetMode() {
	case networking.ClientTLSSettings_DISABLE:
		// nothing to do
	case networking.ClientTLSSettings_SIMPLE:
		// TODO support this
	case networking.ClientTLSSettings_MUTUAL:
		// TODO support this
	case networking.ClientTLSSettings_ISTIO_MUTUAL:
		tlsCtx := buildUpstreamTLSContext(b.push.ServiceAccounts[b.hostname][b.portNum])
		c.TransportSocket = &core.TransportSocket{
			Name:       transportSocketName,
			ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: util.MessageToAny(tlsCtx)},
		}
	}
}

// TransportSocket proto message has a `name` field which is expected to be set to exactly this value by the
// management server (see grpc/xds/internal/client/xds.go securityConfigFromCluster).
const transportSocketName = "envoy.transport_sockets.tls"

func buildUpstreamTLSContext(sans []string) *tls.UpstreamTlsContext {
	return &tls.UpstreamTlsContext{
		CommonTlsContext: buildCommonTLSContext(sans),
	}
}
