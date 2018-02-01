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

// Mixer filter configuration

package v1

import (
	"net"
	"net/url"
	"sort"
	"strings"
	// TODO(nmittler): Remove this
	_ "github.com/golang/glog"

	meshconfig "istio.io/api/mesh/v1alpha1"
	mpb "istio.io/api/mixer/v1"
	mccpb "istio.io/api/mixer/v1/config/client"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

const (
	// MixerCluster is the name of the mixer cluster
	MixerCluster = "mixer_server"

	// MixerFilter name and its attributes
	MixerFilter = "mixer"

	// AttrSourceIP is client source IP
	AttrSourceIP = "source.ip"

	// AttrSourceUID is platform-specific unique identifier for the client instance of the source service
	AttrSourceUID = "source.uid"

	// AttrDestinationIP is the server source IP
	AttrDestinationIP = "destination.ip"

	// AttrDestinationUID is platform-specific unique identifier for the server instance of the target service
	AttrDestinationUID = "destination.uid"

	// AttrDestinationService is name of the target service
	AttrDestinationService = "destination.service"

	// MixerRequestCount is the quota bucket name
	MixerRequestCount = "RequestCount"

	// MixerCheck switches Check call on and off
	MixerCheck = "mixer_check"

	// MixerReport switches Report call on and off
	MixerReport = "mixer_report"

	// DisableTCPCheckCalls switches Check call on and off for tcp listeners
	DisableTCPCheckCalls = "disable_tcp_check_calls"

	// MixerForward switches attribute forwarding on and off
	MixerForward = "mixer_forward"
)

// FilterMixerConfig definition.
//
// NOTE: all fields marked as DEPRECATED are part of the original v1
// mixerclient configuration. They are deprecated and will be
// eventually removed once proxies are updated.
//
// Going forwards all mixerclient configuration should represeted by
// istio.io/api/mixer/v1/config/client/mixer_filter_config.proto and
// encoded in the `V2` field below.
//
type FilterMixerConfig struct {
	// DEPRECATED: MixerAttributes specifies the static list of attributes that are sent with
	// each request to Mixer.
	MixerAttributes map[string]string `json:"mixer_attributes,omitempty"`

	// DEPRECATED: ForwardAttributes specifies the list of attribute keys and values that
	// are forwarded as an HTTP header to the server side proxy
	ForwardAttributes map[string]string `json:"forward_attributes,omitempty"`

	// DEPRECATED: QuotaName specifies the name of the quota bucket to withdraw tokens from;
	// an empty name means no quota will be charged.
	QuotaName string `json:"quota_name,omitempty"`

	// DEPRECATED: If set to true, disables mixer check calls for TCP connections
	DisableTCPCheckCalls bool `json:"disable_tcp_check_calls,omitempty"`

	// istio.io/api/mixer/v1/config/client configuration protobuf
	// encoded as a generic map using canonical JSON encoding.
	//
	// If `V2` field is not empty, the DEPRECATED fields above should
	// be discarded.
	V2 map[string]interface{} `json:"v2,omitempty"`
}

func (*FilterMixerConfig) isNetworkFilterConfig() {}

// buildMixerCluster builds an outbound mixer cluster
func buildMixerCluster(mesh *meshconfig.MeshConfig, role model.Node, mixerSAN []string) *Cluster {
	mixerCluster := buildCluster(mesh.MixerAddress, MixerCluster, mesh.ConnectTimeout)
	mixerCluster.CircuitBreaker = &CircuitBreaker{
		Default: DefaultCBPriority{
			MaxPendingRequests: 10000,
			MaxRequests:        10000,
		},
	}
	mixerCluster.Features = ClusterFeatureHTTP2

	// apply auth policies
	switch mesh.DefaultConfig.ControlPlaneAuthPolicy {
	case meshconfig.AuthenticationPolicy_NONE:
		// do nothing
	case meshconfig.AuthenticationPolicy_MUTUAL_TLS:
		// apply SSL context to enable mutual TLS between Envoy proxies between app and mixer
		mixerCluster.SSLContext = buildClusterSSLContext(model.AuthCertsPath, mixerSAN)
	}

	return mixerCluster
}

func buildMixerOpaqueConfig(check, forward bool, destinationService string) map[string]string {
	keys := map[bool]string{true: "on", false: "off"}
	m := map[string]string{
		MixerReport:  "on",
		MixerCheck:   keys[check],
		MixerForward: keys[forward],
	}
	if destinationService != "" {
		m[AttrDestinationService] = destinationService
	}
	return m
}

// Mixer filter uses outbound configuration by default (forward attributes,
// but not invoke check calls)
func mixerHTTPRouteConfig(mesh *meshconfig.MeshConfig, role model.Node, instances []*model.ServiceInstance, outboundRoute bool, config model.IstioConfigStore) *FilterMixerConfig { // nolint: lll
	filter := &FilterMixerConfig{
		MixerAttributes: map[string]string{
			AttrDestinationIP:  role.IPAddress,
			AttrDestinationUID: "kubernetes://" + role.ID,
		},
		ForwardAttributes: map[string]string{
			AttrSourceIP:  role.IPAddress,
			AttrSourceUID: "kubernetes://" + role.ID,
		},
		QuotaName: MixerRequestCount,
	}
	v2 := &mccpb.HttpClientConfig{
		MixerAttributes: &mpb.Attributes{
			Attributes: map[string]*mpb.Attributes_AttributeValue{
				AttrDestinationIP:  {Value: &mpb.Attributes_AttributeValue_BytesValue{net.ParseIP(role.IPAddress)}},
				AttrDestinationUID: {Value: &mpb.Attributes_AttributeValue_StringValue{"kubernetes://" + role.ID}},
			},
		},
		ServiceConfigs: map[string]*mccpb.ServiceConfig{},
	}

	if role.Type == model.Sidecar && !outboundRoute {
		// Don't forward mixer attributes to the app from inbound sidecar routes
	} else {
		v2.ForwardAttributes = &mpb.Attributes{
			Attributes: map[string]*mpb.Attributes_AttributeValue{
				AttrSourceIP:  {Value: &mpb.Attributes_AttributeValue_BytesValue{net.ParseIP(role.IPAddress)}},
				AttrSourceUID: {Value: &mpb.Attributes_AttributeValue_StringValue{"kubernetes://" + role.ID}},
			},
		}
	}

	if len(instances) > 0 {
		// legacy mixerclient behavior is a comma separated list of
		// services. When can this be removed?
		var services []string
		if instances != nil {
			serviceSet := make(map[string]bool, len(instances))
			for _, instance := range instances {
				serviceSet[instance.Service.Hostname] = true
			}
			for service := range serviceSet {
				services = append(services, service)
			}
			sort.Strings(services)
		}
		filter.MixerAttributes[AttrDestinationService] = strings.Join(services, ",")

		// first service in the sorted list is the default
		v2.DefaultDestinationService = services[0]
	}

	for _, instance := range instances {
		sc := &mccpb.ServiceConfig{
			MixerAttributes: &mpb.Attributes{
				Attributes: map[string]*mpb.Attributes_AttributeValue{
					AttrDestinationService: {
						Value: &mpb.Attributes_AttributeValue_StringValue{instance.Service.Hostname},
					},
				},
			},
			DisableCheckCalls:  outboundRoute || mesh.DisablePolicyChecks,
			DisableReportCalls: outboundRoute,
		}

		// omit API, Quota, and Auth portion of service config when
		// check and report are disabled.
		if !sc.DisableCheckCalls || !sc.DisableReportCalls {
			apiSpecs := config.HTTPAPISpecByDestination(instance)
			model.SortHTTPAPISpec(apiSpecs)
			for _, config := range apiSpecs {
				sc.HttpApiSpec = append(sc.HttpApiSpec, config.Spec.(*mccpb.HTTPAPISpec))
			}

			quotaSpecs := config.QuotaSpecByDestination(instance)
			model.SortQuotaSpec(quotaSpecs)
			for _, config := range quotaSpecs {
				sc.QuotaSpec = append(sc.QuotaSpec, config.Spec.(*mccpb.QuotaSpec))
			}

			authSpecs := config.EndUserAuthenticationPolicySpecByDestination(instance)
			model.SortEndUserAuthenticationPolicySpec(quotaSpecs)
			if len(authSpecs) > 0 {
				spec := (authSpecs[0].Spec).(*mccpb.EndUserAuthenticationPolicySpec)

				// Update jwks_uri_envoy_cluster This cluster should be
				// created elsewhere using the same host-to-cluster naming
				// scheme, i.e. buildJWKSURIClusterNameAndAddress.
				for _, jwt := range spec.Jwts {
					if name, _, _, err := buildJWKSURIClusterNameAndAddress(jwt.JwksUri); err != nil {
						log.Warnf("Could not set jwks_uri_envoy and address for jwks_uri %q: %v",
							jwt.JwksUri, err)
					} else {
						jwt.JwksUriEnvoyCluster = name
					}
				}

				sc.EndUserAuthnSpec = spec
				if len(authSpecs) > 1 {
					// TODO - validation should catch this problem earlier at config time.
					log.Warnf("Multiple EndUserAuthenticationPolicySpec found for service %q. Selecting %v",
						instance.Service, spec)
				}
			}
		}

		v2.ServiceConfigs[instance.Service.Hostname] = sc
	}

	if v2JSONMap, err := model.ToJSONMap(v2); err != nil {
		log.Warnf("Could not encode v2 HTTP mixerclient filter for node %q: %v", role, err)
	} else {
		filter.V2 = v2JSONMap
	}
	return filter
}

// Mixer TCP filter config for inbound requests.
func mixerTCPConfig(role model.Node, check bool, instance *model.ServiceInstance) *FilterMixerConfig {
	filter := &FilterMixerConfig{
		MixerAttributes: map[string]string{
			AttrDestinationIP:  role.IPAddress,
			AttrDestinationUID: "kubernetes://" + role.ID,
		},
	}
	v2 := &mccpb.TcpClientConfig{
		MixerAttributes: &mpb.Attributes{
			Attributes: map[string]*mpb.Attributes_AttributeValue{
				AttrDestinationIP:      {Value: &mpb.Attributes_AttributeValue_StringValue{role.IPAddress}},
				AttrDestinationUID:     {Value: &mpb.Attributes_AttributeValue_StringValue{"kubernetes://" + role.ID}},
				AttrDestinationService: {Value: &mpb.Attributes_AttributeValue_StringValue{instance.Service.Hostname}},
			},
		},
	}
	if v2JSONMap, err := model.ToJSONMap(v2); err != nil {
		log.Warnf("Could not encode v2 TCP mixerclient filter for node %q: %v", role, err)
	} else {
		filter.V2 = v2JSONMap

	}
	return filter
}

const (
	// OutboundJWTURIClusterPrefix is the prefix for jwt_uri service
	// clusters external to the proxy instance
	OutboundJWTURIClusterPrefix = "jwt."
)

// buildJWKSURIClusterNameAndAddress builds the internal envoy cluster
// name and DNS address from the jwks_uri. The cluster name is used by
// the JWT auth filter to fetch public keys. The cluster name and
// address are used to build an envoy cluster that corresponds to the
// jwks_uri server.
func buildJWKSURIClusterNameAndAddress(raw string) (string, string, bool, error) {
	var useSSL bool

	u, err := url.Parse(raw)
	if err != nil {
		return "", "", useSSL, err
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"

		} else {
			port = "80"
		}
	}
	address := host + ":" + port
	name := host + "|" + port

	if u.Scheme == "https" {
		useSSL = true
	}

	return truncateClusterName(OutboundJWTURIClusterPrefix + name), address, useSSL, nil
}

// buildMixerAuthFilterClusters builds the necessary clusters for the
// JWT auth filter to fetch public keys from the specified jwks_uri.
func buildMixerAuthFilterClusters(config model.IstioConfigStore, mesh *meshconfig.MeshConfig, instances []*model.ServiceInstance) Clusters {
	type authCluster struct {
		name   string
		useSSL bool
	}
	authClusters := map[string]authCluster{}
	for _, instance := range instances {
		for _, policy := range config.EndUserAuthenticationPolicySpecByDestination(instance) {
			for _, jwt := range policy.Spec.(*mccpb.EndUserAuthenticationPolicySpec).Jwts {
				if name, address, ssl, err := buildJWKSURIClusterNameAndAddress(jwt.JwksUri); err != nil {
					log.Warnf("Could not build envoy cluster and address from jwks_uri %q: %v",
						jwt.JwksUri, err)
				} else {
					authClusters[address] = authCluster{name, ssl}
				}
			}
		}
	}

	var clusters Clusters
	for address, auth := range authClusters {
		cluster := buildCluster(address, auth.name, mesh.ConnectTimeout)
		cluster.CircuitBreaker = &CircuitBreaker{
			Default: DefaultCBPriority{
				MaxPendingRequests: 10000,
				MaxRequests:        10000,
			},
		}
		if auth.useSSL {
			cluster.SSLContext = &SSLContextExternal{}
		}
		clusters = append(clusters, cluster)
	}
	return clusters
}
