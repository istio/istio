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

// AuthN filter configuration

package v1

import (
	"github.com/golang/protobuf/ptypes/duration"

	authn "istio.io/api/authentication/v1alpha1"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	authn_plugin "istio.io/istio/pilot/pkg/networking/plugin/authn"
	"istio.io/istio/pkg/log"
)

// buildJwtFilter returns a Jwt filter for all Jwt specs in the policy.
func buildJwtFilter(policy *authn.Policy) *HTTPFilter {
	filterConfigProto := authn_plugin.ConvertPolicyToJwtConfig(policy, false /*fetchPubKey*/)
	if filterConfigProto == nil {
		return nil
	}
	config, err := model.ToJSONMap(filterConfigProto)
	if err != nil {
		log.Errorf("Unable to convert Jwt filter config proto: %v", err)
		return nil
	}
	return &HTTPFilter{
		Type:   decoder,
		Name:   authn_plugin.JwtFilterName,
		Config: config,
	}
}

// buildAuthnFilter returns a authN filter for the policy.
func buildAuthnFilter(policy *authn.Policy) *HTTPFilter {
	filterConfigProto := authn_plugin.ConvertPolicyToAuthNFilterConfig(policy)
	if filterConfigProto == nil {
		return nil
	}
	config, err := model.ToJSONMap(filterConfigProto)
	if err != nil {
		log.Errorf("Unable to convert authn filter config proto: %v", err)
		return nil
	}
	return &HTTPFilter{
		Type:   decoder,
		Name:   authn_plugin.AuthnFilterName,
		Config: config,
	}
}

// buildJwksURIClustersForProxyInstances checks the authentication policy for the
// input proxyInstances, and generates (outbound) clusters for all JwksURIs.
func buildJwksURIClustersForProxyInstances(mesh *meshconfig.MeshConfig,
	store model.IstioConfigStore, proxyInstances []*model.ServiceInstance) Clusters {
	if len(proxyInstances) == 0 {
		return nil
	}
	var jwtSpecs []*authn.Jwt
	for _, instance := range proxyInstances {
		authnPolicy := model.GetConsolidateAuthenticationPolicy(mesh, store, instance.Service.Hostname, instance.Endpoint.ServicePort)
		jwtSpecs = append(jwtSpecs, authn_plugin.CollectJwtSpecs(authnPolicy)...)
	}

	return buildJwksURIClusters(jwtSpecs, mesh.ConnectTimeout)
}

// buildJwksURIClusters returns a list of clusters for each unique JwksUri from
// the input list of Jwt specs. This function is to support
// buildJwksURIClustersForProxyInstances above.
func buildJwksURIClusters(jwtSpecs []*authn.Jwt, timeout *duration.Duration) Clusters {
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

	var clusters Clusters
	for _, auth := range jwksClusters {
		cluster := BuildOutboundCluster(auth.hostname, auth.port, nil /* labels */, true /* external */)
		cluster.Name = authn_plugin.JwksURIClusterName(auth.hostname, auth.port)
		cluster.CircuitBreaker = &CircuitBreaker{
			Default: DefaultCBPriority{
				MaxPendingRequests: 10000,
				MaxRequests:        10000,
			},
		}
		cluster.ConnectTimeoutMs = timeout.GetSeconds() * 1000
		if auth.useSSL {
			cluster.SSLContext = &SSLContextExternal{}
		}

		clusters = append(clusters, cluster)
	}
	return clusters
}
