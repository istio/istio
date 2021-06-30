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
	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	"github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	tlsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/config/host"
)

// BuildClusters handles a gRPC CDS request, used with the 'ApiListener' style of requests.
// The main difference is that the request includes Resources to filter.
func (g *GrpcConfigGenerator) BuildClusters(node *model.Proxy, push *model.PushContext, names []string) model.Resources {
	var clusters = make([]*clusterv3.Cluster, 0, len(names))
	for _, defaultClusterName := range names {
		builder, err := newClusterBuilder(node, push, defaultClusterName)
		if err != nil {
			log.Warn(err)
			continue
		}
		clusters = append(clusters, builder.build()...)
	}

	resp := make(model.Resources, 0, len(clusters))
	for _, c := range clusters {
		resp = append(resp, &envoy_service_discovery_v3.Resource{
			Name:     c.Name,
			Resource: util.MessageToAny(c),
		})
	}
	return resp
}

// clusterBuilder is responsible for building a single default and subset clusters for a service
// TODO re-use the v1alpha3.ClusterBuilder:
// Most of the logic is similar, I think we can just share the code if we expose:
// * BuildSubsetCluster
// * BuildDefaultCluster
// * BuildClusterOpts and members
// * Add something to allow us to override how tlscontext is built
type clusterBuilder struct {
	// conveinence
	push *model.PushContext
	node *model.Proxy

	// guaranteed to be set in init
	name           string
	defaultCluster *clusterv3.Cluster
	hostname       host.Name
	portNum        int

	// may not be set
	svc  *model.Service
	port *model.Port
}

func newClusterBuilder(node *model.Proxy, push *model.PushContext, defaultClusterName string) (*clusterBuilder, error) {
	_, _, hostname, portNum := model.ParseSubsetKey(defaultClusterName)
	if hostname == "" || portNum == 0 {
		return nil, fmt.Errorf("failed parsing subset key: %s", defaultClusterName)
	}

	defaultCluster := edsCluster(defaultClusterName)
	if defaultCluster == nil {
		// should never happen
		return nil, fmt.Errorf("failed building default cluster %s", defaultClusterName)
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
		node:           node,
		push:           push,
		hostname:       hostname,
		portNum:        portNum,
		defaultCluster: defaultCluster,
		svc:            svc,
		port:           port,
	}, nil
}

func (b *clusterBuilder) build() []*clusterv3.Cluster {
	subsetClusters := b.applyDestinationRule()
	out := make([]*clusterv3.Cluster, 0, 1+len(subsetClusters))
	return append(append(out, b.defaultCluster), subsetClusters...)
}

// edsCluster creates a simple cluster to read endpoints from ads/eds.
func edsCluster(name string) *clusterv3.Cluster {
	return &clusterv3.Cluster{
		Name:                 name,
		ClusterDiscoveryType: &clusterv3.Cluster_Type{Type: clusterv3.Cluster_EDS},
		EdsClusterConfig: &clusterv3.Cluster_EdsClusterConfig{
			ServiceName: name,
			EdsConfig: &envoy_config_core_v3.ConfigSource{
				ConfigSourceSpecifier: &envoy_config_core_v3.ConfigSource_Ads{
					Ads: &envoy_config_core_v3.AggregatedConfigSource{},
				},
			},
		},
	}
}

// applyDestinationRule mutates the default cluster to reflect traffic policies, and returns a set of additional
// subset clusters if specified by a destination rule
func (b *clusterBuilder) applyDestinationRule() (subsetClusters []*clusterv3.Cluster) {
	if b.svc != nil || b.port != nil {
		return nil
	}

	// resolve policy from context
	destinationRule := v1alpha3.CastDestinationRuleOrDefault(b.push.DestinationRule(b.node, b.svc))
	trafficPolicy := v1alpha3.MergeTrafficPolicy(nil, destinationRule.TrafficPolicy, b.port)

	// setup default cluster
	b.applyPolicy(b.defaultCluster, trafficPolicy)

	// subset clusters
	if len(destinationRule.Subsets) > 0 {
		subsetClusters = make([]*clusterv3.Cluster, 0, len(destinationRule.Subsets))
		for _, subset := range destinationRule.Subsets {
			c := edsCluster(subsetClusterKey(subset.Name, string(b.hostname), b.portNum))
			trafficPolicy := v1alpha3.MergeTrafficPolicy(trafficPolicy, subset.TrafficPolicy, b.port)
			b.applyPolicy(c, trafficPolicy)
		}
	}

	return
}

func (b *clusterBuilder) applyPolicy(c *clusterv3.Cluster, trafficPolicy *networking.TrafficPolicy) {
	b.applyTLS(c, trafficPolicy)
	b.applyLoadBalancing(c, trafficPolicy)
	// TODO status or log when unsupported features are included
}

func (b *clusterBuilder) applyLoadBalancing(c *clusterv3.Cluster, policy *networking.TrafficPolicy) {
	// TODO implement me
}

func (b *clusterBuilder) applyTLS(c *clusterv3.Cluster, policy *networking.TrafficPolicy) {
	// TODO check for automtls
	mode := networking.ClientTLSSettings_ISTIO_MUTUAL
	if settings := policy.GetTls(); settings != nil {
		mode = settings.GetMode()
	}

	switch mode {
	case networking.ClientTLSSettings_DISABLE:
		// nothing to do
	case networking.ClientTLSSettings_SIMPLE:
		// TODO support this
	case networking.ClientTLSSettings_MUTUAL:
		// TODO support this
	case networking.ClientTLSSettings_ISTIO_MUTUAL:
		tlsCtx := buildTLSContext(b.push.ServiceAccounts[b.hostname][b.portNum])
		c.TransportSocket = &envoy_config_core_v3.TransportSocket{
			Name:       transportSocketName,
			ConfigType: &envoy_config_core_v3.TransportSocket_TypedConfig{TypedConfig: util.MessageToAny(tlsCtx)},
		}
	}

}

// TransportSocket proto message has a `name` field which is expected to be set to exactly this value by the
// management server (see grpc/xds/internal/client/xds.go securityConfigFromCluster).
const transportSocketName = "envoy.transport_sockets.tls"

// buildTLSContext creates a TLS context that assumes 'default' name, and credentials/tls/certprovider/pemfile
// (see grpc/xds/internal/client/xds.go securityConfigFromCluster).
func buildTLSContext(sans []string) *tlsv3.UpstreamTlsContext {
	return &tlsv3.UpstreamTlsContext{
		CommonTlsContext: &tlsv3.CommonTlsContext{
			TlsCertificateCertificateProviderInstance: &tlsv3.CommonTlsContext_CertificateProviderInstance{
				InstanceName:    "default",
				CertificateName: "default",
			},
			ValidationContextType: &tlsv3.CommonTlsContext_CombinedValidationContext{
				CombinedValidationContext: &tlsv3.CommonTlsContext_CombinedCertificateValidationContext{
					ValidationContextCertificateProviderInstance: &tlsv3.CommonTlsContext_CertificateProviderInstance{
						InstanceName:    "default",
						CertificateName: "ROOTCA",
					},
					DefaultValidationContext: &tlsv3.CertificateValidationContext{
						MatchSubjectAltNames: util.StringToExactMatch(sans),
					},
				},
			},
		},
	}
}
