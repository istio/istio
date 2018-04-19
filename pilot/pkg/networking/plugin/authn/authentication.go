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

package authn

import (
	"crypto/sha1"
	"encoding/base64"
	"fmt"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	jwtfilter "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/jwt_authn/v2alpha"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"

	authn "istio.io/api/authentication/v1alpha1"
	authn_filter "istio.io/api/envoy/config/filter/http/authn/v2alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/log"
)

const (
	// JwtFilterName is the name for the Jwt filter. This should be the same
	// as the name defined in
	// https://github.com/istio/proxy/blob/master/src/envoy/http/jwt_auth/http_filter_factory.cc#L50
	JwtFilterName = "jwt-auth"

	// AuthnFilterName is the name for the Istio AuthN filter. This should be the same
	// as the name defined in
	// https://github.com/istio/proxy/blob/master/src/envoy/http/authn/http_filter_factory.cc#L30
	AuthnFilterName = "istio_authn"

	// Defautl cache duration for JWT public key. This should be moved to a global config.
	jwtPublicKeyCacheSeconds = 60 * 5
)

// Plugin implements Istio mTLS auth
type Plugin struct{}

// NewPlugin returns an instance of the authn plugin
func NewPlugin() plugin.Plugin {
	return Plugin{}
}

// RequireTLS returns true and pointer to mTLS params if the policy use mTLS for (peer) authentication.
// (note that mTLS params can still be nil). Otherwise, return (false, nil).
func RequireTLS(policy *authn.Policy) (bool, *authn.MutualTls) {
	if policy == nil {
		return false, nil
	}
	if len(policy.Peers) > 0 {
		for _, method := range policy.Peers {
			switch method.GetParams().(type) {
			case *authn.PeerAuthenticationMethod_Mtls:
				return true, method.GetMtls()
			default:
				continue
			}
		}
	}
	return false, nil
}

// JwksURIClusterName returns cluster name for the jwks URI. This should be used
// to override the name for outbound cluster that are added for Jwks URI so that they
// can be referred correctly in the JWT filter config.
func JwksURIClusterName(hostname string, port *model.Port) string {
	const clusterPrefix = "jwks."
	const maxClusterNameLength = 189 - len(clusterPrefix)
	name := hostname + "|" + port.Name
	if len(name) > maxClusterNameLength {
		prefix := name[:maxClusterNameLength-sha1.Size*2]
		sum := sha1.Sum([]byte(name))
		name = fmt.Sprintf("%s%x", prefix, sum)
	}
	return clusterPrefix + name
}

// CollectJwtSpecs returns a list of all JWT specs (ponters) defined the policy. This
// provides a convenient way to iterate all Jwt specs.
func CollectJwtSpecs(policy *authn.Policy) []*authn.Jwt {
	ret := []*authn.Jwt{}
	if policy == nil {
		return ret
	}
	for _, method := range policy.Peers {
		switch method.GetParams().(type) {
		case *authn.PeerAuthenticationMethod_Jwt:
			ret = append(ret, method.GetJwt())
		}
	}
	for _, method := range policy.Origins {
		ret = append(ret, method.Jwt)
	}
	return ret
}

// OutputLocationForJwtIssuer returns the header location that should be used to output payload if
// authentication succeeds.
func OutputLocationForJwtIssuer(issuer string) string {
	const locationPrefix = "istio-sec-"
	sum := sha1.Sum([]byte(issuer))
	return locationPrefix + fmt.Sprintf("%x", sum)
}

// ConvertPolicyToJwtConfig converts policy into Jwt filter config for envoy.
func ConvertPolicyToJwtConfig(policy *authn.Policy, useInlinePublicKey bool) *jwtfilter.JwtAuthentication {
	policyJwts := CollectJwtSpecs(policy)
	if len(policyJwts) == 0 {
		return nil
	}
	ret := &jwtfilter.JwtAuthentication{
		AllowMissingOrFailed: true,
	}
	for _, policyJwt := range policyJwts {
		jwt := &jwtfilter.JwtRule{
			Issuer:               policyJwt.Issuer,
			Audiences:            policyJwt.Audiences,
			ForwardPayloadHeader: OutputLocationForJwtIssuer(policyJwt.Issuer),
			Forward:              true,
		}

		for _, location := range policyJwt.JwtHeaders {
			jwt.FromHeaders = append(jwt.FromHeaders, &jwtfilter.JwtHeader{
				Name: location,
			})
		}
		jwt.FromParams = policyJwt.JwtParams

		if useInlinePublicKey {
			jwtPubKey, err := model.JwtKeyResolver.GetPublicKey(policyJwt.JwksUri)
			if err != nil {
				log.Warnf("Failed to fetch jwt public key from %q", policyJwt.JwksUri)
			}

			// Put empty string in config even if above ResolveJwtPubKey fails.
			jwt.JwksSourceSpecifier = &jwtfilter.JwtRule_LocalJwks{
				LocalJwks: &core.DataSource{
					Specifier: &core.DataSource_InlineString{
						InlineString: base64.StdEncoding.EncodeToString([]byte(jwtPubKey)),
					},
				},
			}
		} else {
			hostname, port, _, err := model.ParseJwksURI(policyJwt.JwksUri)
			if err != nil {
				log.Warnf("Cannot parse jwks_uri %q: %v", policyJwt.JwksUri, err)
			}

			jwt.JwksSourceSpecifier = &jwtfilter.JwtRule_RemoteJwks{
				RemoteJwks: &jwtfilter.RemoteJwks{
					HttpUri: &core.HttpUri{
						Uri: policyJwt.JwksUri,
						HttpUpstreamType: &core.HttpUri_Cluster{
							Cluster: JwksURIClusterName(hostname, port),
						},
					},
					CacheDuration: &types.Duration{Seconds: jwtPublicKeyCacheSeconds},
				},
			}
		}

		ret.Rules = append(ret.Rules, jwt)
	}
	return ret
}

// ConvertPolicyToAuthNFilterConfig returns an authn filter config corresponding for the input policy.
func ConvertPolicyToAuthNFilterConfig(policy *authn.Policy) *authn_filter.FilterConfig {
	if policy == nil || (len(policy.Peers) == 0 && len(policy.Origins) == 0) {
		return nil
	}
	filterConfig := &authn_filter.FilterConfig{
		Policy: proto.Clone(policy).(*authn.Policy),
	}
	// Create default mTLS params for params type mTLS but value is nil.
	// This walks around the issue https://github.com/istio/istio/issues/4763
	for _, peer := range filterConfig.Policy.Peers {
		switch peer.GetParams().(type) {
		case *authn.PeerAuthenticationMethod_Mtls:
			if peer.GetMtls() == nil {
				peer.Params = &authn.PeerAuthenticationMethod_Mtls{&authn.MutualTls{}}
			}
		}
	}
	// Remove targets part.
	filterConfig.Policy.Targets = nil
	locations := make(map[string]string)
	for _, jwt := range CollectJwtSpecs(policy) {
		locations[jwt.Issuer] = OutputLocationForJwtIssuer(jwt.Issuer)
	}
	if len(locations) > 0 {
		filterConfig.JwtOutputPayloadLocations = locations
	}
	return filterConfig
}

// BuildJwtFilter returns a Jwt filter for all Jwt specs in the policy.
func BuildJwtFilter(policy *authn.Policy) *http_conn.HttpFilter {
	// v2 api will use inline public key.
	filterConfigProto := ConvertPolicyToJwtConfig(policy, true /*useInlinePublicKey*/)
	if filterConfigProto == nil {
		return nil
	}
	return &http_conn.HttpFilter{
		Name:   JwtFilterName,
		Config: util.MessageToStruct(filterConfigProto),
	}
}

// BuildAuthNFilter returns authn filter for the given policy. If policy is nil, returns nil.
func BuildAuthNFilter(policy *authn.Policy) *http_conn.HttpFilter {
	filterConfigProto := ConvertPolicyToAuthNFilterConfig(policy)
	if filterConfigProto == nil {
		return nil
	}
	return &http_conn.HttpFilter{
		Name:   AuthnFilterName,
		Config: util.MessageToStruct(filterConfigProto),
	}
}

func isDestinationExcludedForMTLS(destService string, mtlsExcludedServices []string) bool {
	for _, serviceName := range mtlsExcludedServices {
		if destService == serviceName {
			return true
		}
	}
	return false
}

// buildSidecarListenerTLSContext adds TLS to the listener if the policy requires one.
func buildSidecarListenerTLSContext(authenticationPolicy *authn.Policy) *auth.DownstreamTlsContext {
	if requireTLS, mTLSParams := RequireTLS(authenticationPolicy); requireTLS {
		return &auth.DownstreamTlsContext{
			CommonTlsContext: &auth.CommonTlsContext{
				TlsCertificates: []*auth.TlsCertificate{
					{
						CertificateChain: &core.DataSource{
							Specifier: &core.DataSource_Filename{
								Filename: model.AuthCertsPath + model.CertChainFilename,
							},
						},
						PrivateKey: &core.DataSource{
							Specifier: &core.DataSource_Filename{
								Filename: model.AuthCertsPath + model.KeyFilename,
							},
						},
					},
				},
				ValidationContext: &auth.CertificateValidationContext{
					TrustedCa: &core.DataSource{
						Specifier: &core.DataSource_Filename{
							Filename: model.AuthCertsPath + model.RootCertFilename,
						},
					},
				},
				// Same as ListenersALPNProtocols defined in listener. Need to move that constant else where in order to share.
				AlpnProtocols: []string{"h2", "http/1.1"},
			},
			RequireClientCertificate: &types.BoolValue{
				Value: !(mTLSParams != nil && mTLSParams.AllowTls),
			},
		}
	}
	return nil
}

// OnOutboundListener is called whenever a new outbound listener is added to the LDS output for a given service
// Can be used to add additional filters on the outbound path
func (Plugin) OnOutboundListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	// TODO: implementation
	return nil
}

// OnInboundListener is called whenever a new listener is added to the LDS output for a given service
// Can be used to add additional filters (e.g., mixer filter) or add more stuff to the HTTP connection manager
// on the inbound path
func (Plugin) OnInboundListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	if in.Node.Type != model.Sidecar {
		// Only care about sidecar.
		return nil
	}
	authnPolicy := model.GetConsolidateAuthenticationPolicy(
		in.Env.Mesh, in.Env.IstioConfigStore, in.ServiceInstance.Service.Hostname, in.ServiceInstance.Endpoint.ServicePort)

	if mutable.Listener == nil || (len(mutable.Listener.FilterChains) != len(mutable.FilterChains)) {
		return fmt.Errorf("expected same number of filter chains in listener (%d) and mutable (%d)", len(mutable.Listener.FilterChains), len(mutable.FilterChains))
	}

	for i := range mutable.Listener.FilterChains {
		mutable.Listener.FilterChains[i].TlsContext = buildSidecarListenerTLSContext(authnPolicy)
		if in.ListenerType == plugin.ListenerTypeHTTP {
			// Adding Jwt filter and authn filter, if needed.
			if filter := BuildJwtFilter(authnPolicy); filter != nil {
				mutable.FilterChains[i].HTTP = append(mutable.FilterChains[i].HTTP, filter)
			}
			if filter := BuildAuthNFilter(authnPolicy); filter != nil {
				mutable.FilterChains[i].HTTP = append(mutable.FilterChains[i].HTTP, filter)
			}
		}
	}
	return nil
}

// OnInboundCluster implements the Plugin interface method.
func (Plugin) OnInboundCluster(env model.Environment, node model.Proxy, service *model.Service,
	servicePort *model.Port, cluster *xdsapi.Cluster) {
}

// OnOutboundRouteConfiguration implements the Plugin interface method.
func (Plugin) OnOutboundRouteConfiguration(in *plugin.InputParams, route *xdsapi.RouteConfiguration) {
}

// OnInboundRouteConfiguration implements the Plugin interface method.
func (Plugin) OnInboundRouteConfiguration(in *plugin.InputParams, route *xdsapi.RouteConfiguration) {
}

// OnOutboundCluster implements the Plugin interface method.
func (Plugin) OnOutboundCluster(env model.Environment, node model.Proxy, service *model.Service,
	servicePort *model.Port, cluster *xdsapi.Cluster) {
	mesh := env.Mesh
	config := env.IstioConfigStore

	// Original DST cluster are used to route to services outside the mesh
	// where Istio auth does not apply.
	if cluster.Type == xdsapi.Cluster_ORIGINAL_DST {
		return
	}

	required, _ := RequireTLS(model.GetConsolidateAuthenticationPolicy(mesh, config, service.Hostname, servicePort))
	if isDestinationExcludedForMTLS(service.Hostname, mesh.MtlsExcludedServices) || !required {
		return
	}

	// apply auth policies
	serviceAccounts := env.ServiceAccounts.GetIstioServiceAccounts(service.Hostname, []string{servicePort.Name})

	cluster.TlsContext = &auth.UpstreamTlsContext{
		CommonTlsContext: &auth.CommonTlsContext{
			TlsCertificates: []*auth.TlsCertificate{
				{
					CertificateChain: &core.DataSource{
						Specifier: &core.DataSource_Filename{
							Filename: model.AuthCertsPath + model.CertChainFilename,
						},
					},
					PrivateKey: &core.DataSource{
						Specifier: &core.DataSource_Filename{
							Filename: model.AuthCertsPath + model.KeyFilename,
						},
					},
				},
			},
			ValidationContext: &auth.CertificateValidationContext{
				TrustedCa: &core.DataSource{
					Specifier: &core.DataSource_Filename{
						Filename: model.AuthCertsPath + model.RootCertFilename,
					},
				},
				VerifySubjectAltName: serviceAccounts,
			},
		},
	}
}
