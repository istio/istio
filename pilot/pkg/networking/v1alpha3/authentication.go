// Copyright 2018 Istio Authors
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
	"time"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	"github.com/golang/protobuf/ptypes/duration"

	authn "istio.io/api/authentication/v1alpha2"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

const (
	// jwtFilterName is the name for the Jwt filter. This should be the same
	// as the name defined in
	// https://github.com/istio/proxy/blob/master/src/envoy/http/jwt_auth/http_filter_factory.cc#L50
	jwtFilterName = "jwt-auth"
)

// buildJwtFilter returns a Jwt filter for all Jwt specs in the policy.
func buildJwtFilter(policy *authn.Policy) *http_conn.HttpFilter {
	filterConfigProto := model.ConvertPolicyToJwtConfig(policy)
	if filterConfigProto == nil {
		return nil
	}
	return &http_conn.HttpFilter{
		Name:   jwtFilterName,
		Config: messageToStruct(filterConfigProto),
	}
}

// buildJwksURIClustersForProxyInstances checks the authentication policy for the
// input proxyInstances, and generates (outbound) clusters for all JwksURIs.
func buildJwksURIClustersForProxyInstances(mesh *meshconfig.MeshConfig,
	store model.IstioConfigStore, proxyInstances []*model.ServiceInstance) []*v2.Cluster {
	if len(proxyInstances) == 0 {
		return nil
	}
	var jwtSpecs []*authn.Jwt
	for _, instance := range proxyInstances {
		authnPolicy := model.GetConsolidateAuthenticationPolicy(mesh, store, instance.Service.Hostname, instance.Endpoint.ServicePort)
		jwtSpecs = append(jwtSpecs, model.CollectJwtSpecs(authnPolicy)...)
	}

	return buildJwksURIClusters(jwtSpecs, mesh.ConnectTimeout)
}

// buildJwksURIClusters returns a list of clusters for each unique JwksUri from
// the input list of Jwt specs. This function is to support
// buildJwksURIClustersForProxyInstances above.
func buildJwksURIClusters(jwtSpecs []*authn.Jwt, timeout *duration.Duration) []*v2.Cluster {
	type jwksCluster struct {
		hostname string
		port     *model.Port
		useSSL   bool
	}
	jwksClusters := map[string]jwksCluster{}
	for _, jwt := range jwtSpecs {
		if _, exist := jwksClusters[jwt.JwksUri]; exist {
			continue
		}
		if hostname, port, ssl, err := model.ParseJwksURI(jwt.JwksUri); err != nil {
			log.Warnf("Could not build envoy cluster and address from jwks_uri %q: %v",
				jwt.JwksUri, err)
		} else {
			jwksClusters[jwt.JwksUri] = jwksCluster{hostname, port, ssl}
		}
	}

	clusters := make([]*v2.Cluster, 0)
	for _, auth := range jwksClusters {
		host := &core.Address{Address: &core.Address_SocketAddress{
			SocketAddress: &core.SocketAddress{
				Address:  auth.hostname,
				Protocol: core.TCP,
				PortSpecifier: &core.SocketAddress_PortValue{
					PortValue: uint32(auth.port.Port),
				},
			},
		}}
		cluster := &v2.Cluster{
			Name:           model.JwksURIClusterName(auth.hostname, auth.port),
			Type:           v2.Cluster_STRICT_DNS,
			Hosts:          []*core.Address{host},
			ConnectTimeout: time.Duration(timeout.Seconds) * time.Second,
		}
		// TODO(diemtvu): add TlsContext, circuitbreaker etc.
		// cluster.CircuitBreaker = &v1.CircuitBreaker{
		// 	Default: v1.DefaultCBPriority{
		// 		MaxPendingRequests: 10000,
		// 		MaxRequests:        10000,
		// 	},
		// }
		// cluster.ConnectTimeoutMs = timeout.GetSeconds() * 1000
		// if auth.useSSL {
		// 	cluster.SslContext = &v1.SSLContextExternal{}
		// }

		clusters = append(clusters, cluster)
	}
	return clusters
}
