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
	"crypto/sha256"
	"encoding/base64"
	"net"
	"net/url"

	meshconfig "istio.io/api/mesh/v1alpha1"
	mpb "istio.io/api/mixer/v1"
	mccpb "istio.io/api/mixer/v1/config/client"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

const (
	// MixerCheckClusterName is the name of the mixer cluster used for policy checks
	MixerCheckClusterName = "mixer_check_server"

	// MixerReportClusterName is the name of the mixer cluster used for telemetry
	MixerReportClusterName = "mixer_report_server"

	// MixerFilter name and its attributes
	MixerFilter = "mixer"

	// AttrSourcePrefix all source attributes start with this prefix
	AttrSourcePrefix = "source"

	// AttrSourceIP is client source IP
	AttrSourceIP = "source.ip"

	// AttrSourceUID is platform-specific unique identifier for the client instance of the source service
	AttrSourceUID = "source.uid"

	// AttrDestinationPrefix all destination attributes start with this prefix
	AttrDestinationPrefix = "destination"

	// AttrDestinationIP is the server source IP
	AttrDestinationIP = "destination.ip"

	// AttrDestinationUID is platform-specific unique identifier for the server instance of the target service
	AttrDestinationUID = "destination.uid"

	// AttrDestinationLabels is Labels associated with the destination
	AttrDestinationLabels = "destination.labels"

	// AttrDestinationService is name of the target service
	AttrDestinationService = "destination.service"

	// AttrIPSuffix represents IP address suffix.
	AttrIPSuffix = "ip"

	// AttrUIDSuffix is the uid suffix of with source or destination.
	AttrUIDSuffix = "uid"

	// AttrLabelsSuffix is the suffix for labels associated with source or destination.
	AttrLabelsSuffix = "labels"

	// keyConfigMixer is a key in the opaque_config. It is base64(json.Marshal(ServiceConfig)).
	keyConfigMixer = "mixer"

	// keyConfigMixerSha is the sha of keyConfigMixer. It is used for equality check.
	// MixerClient uses it to avoid decoding and processing keyConfigMixer on every request.
	keyConfigMixerSha = "mixer_sha"

	// MixerRequestCount is the quota bucket name
	MixerRequestCount = "RequestCount"

	// MixerCheck switches Check call on and off
	MixerCheck = "mixer_check"

	// MixerReport switches Report call on and off
	MixerReport = "mixer_report"

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

// IsNetworkFilterConfig marks FilterMixerConfig as an implementation of NetworkFilterConfig
func (*FilterMixerConfig) IsNetworkFilterConfig() {}

// buildMixerCluster builds an outbound mixer cluster of a given name
func buildMixerCluster(mesh *meshconfig.MeshConfig, mixerSAN []string, server, clusterName string) *Cluster {
	cluster := buildCluster(server, clusterName, mesh.ConnectTimeout)
	cluster.CircuitBreaker = &CircuitBreaker{
		Default: DefaultCBPriority{
			MaxPendingRequests: 10000,
			MaxRequests:        10000,
		},
	}

	cluster.Features = ClusterFeatureHTTP2
	// apply auth policies
	switch mesh.DefaultConfig.ControlPlaneAuthPolicy {
	case meshconfig.AuthenticationPolicy_NONE:
		// do nothing
	case meshconfig.AuthenticationPolicy_MUTUAL_TLS:
		// apply SSL context to enable mutual TLS between Envoy proxies between app and mixer
		cluster.SSLContext = buildClusterSSLContext(model.AuthCertsPath, mixerSAN)
	}

	return cluster
}

// BuildMixerClusters builds an outbound mixer cluster with configured check/report clusters
func BuildMixerClusters(mesh *meshconfig.MeshConfig, role model.Proxy, mixerSAN []string) []*Cluster {
	mixerClusters := make([]*Cluster, 0)

	if mesh.MixerCheckServer != "" {
		mixerClusters = append(mixerClusters, buildMixerCluster(mesh, mixerSAN, mesh.MixerCheckServer, MixerCheckClusterName))
	}

	if mesh.MixerReportServer != "" {
		mixerClusters = append(mixerClusters, buildMixerCluster(mesh, mixerSAN, mesh.MixerReportServer, MixerReportClusterName))
	}

	return mixerClusters
}

// BuildMixerConfig build per route mixer config to be deployed at the `model.Proxy` workload
// with destination of Service `dest` and `destName` as the service name
func BuildMixerConfig(source model.Proxy, destName string, dest *model.Service, instances []*model.ServiceInstance, config model.IstioConfigStore,
	disableCheck bool, disableReport bool) map[string]string {
	sc := serviceConfig(destName, &model.ServiceInstance{Service: dest}, config, disableCheck, disableReport)
	var labels map[string]string
	// Note: instances are all running on mode.Node named 'role'
	// So instance labels are the workload / Node labels.
	if len(instances) > 0 {
		labels = instances[0].Labels
	}
	addStandardNodeAttributes(sc.MixerAttributes.Attributes, AttrSourcePrefix, source.IPAddress, source.ID, labels)

	addStandardNodeAttributes(sc.MixerAttributes.Attributes, AttrDestinationPrefix, "", destName, nil)

	oc := map[string]string{
		AttrDestinationService: destName,
	}

	if cfg, err := model.ToJSON(sc); err == nil {
		ba := []byte(cfg)
		oc[keyConfigMixer] = base64.StdEncoding.EncodeToString(ba)
		h := sha256.New()
		h.Write(ba) //nolint: errcheck
		oc[keyConfigMixerSha] = base64.StdEncoding.EncodeToString(h.Sum(nil))
	} else {
		log.Warnf("Unable to convert %#v to json: %v", sc, err)
	}
	return oc
}

// BuildMixerOpaqueConfig builds a mixer opaque config.
func BuildMixerOpaqueConfig(check, forward bool, destinationService string) map[string]string {
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

// BuildHTTPMixerFilterConfig builds a mixer HTTP filter config. Mixer filter uses outbound configuration by default
// (forward attributes, but not invoke check calls)  ServiceInstances belong to the Node.
func BuildHTTPMixerFilterConfig(mesh *meshconfig.MeshConfig, role model.Proxy, nodeInstances []*model.ServiceInstance, outboundRoute bool, config model.IstioConfigStore) *FilterMixerConfig { // nolint: lll
	transport := &mccpb.TransportConfig{
		CheckCluster:  MixerCheckClusterName,
		ReportCluster: MixerReportClusterName,
	}

	v2 := &mccpb.HttpClientConfig{
		MixerAttributes: &mpb.Attributes{
			Attributes: map[string]*mpb.Attributes_AttributeValue{},
		},
		ServiceConfigs: map[string]*mccpb.ServiceConfig{},
		Transport:      transport,
	}

	filter := &FilterMixerConfig{}

	var labels map[string]string
	// Note: instances are all running on mode.Node named 'role'
	// So instance labels are the workload / Node labels.
	if len(nodeInstances) > 0 {
		labels = nodeInstances[0].Labels
		v2.DefaultDestinationService = nodeInstances[0].Service.Hostname
	}

	if !outboundRoute {
		// for outboundRoutes there are no default MixerAttributes
		// specific MixerAttributes are in per route configuration.
		addStandardNodeAttributes(v2.MixerAttributes.Attributes, AttrDestinationPrefix, role.IPAddress, role.ID, labels)
	}

	if role.Type == model.Sidecar && !outboundRoute {
		// Don't forward mixer attributes to the app from inbound sidecar routes
	} else {
		v2.ForwardAttributes = &mpb.Attributes{
			Attributes: map[string]*mpb.Attributes_AttributeValue{},
		}
		addStandardNodeAttributes(v2.ForwardAttributes.Attributes, AttrSourcePrefix, role.IPAddress, role.ID, labels)
	}

	for _, instance := range nodeInstances {
		v2.ServiceConfigs[instance.Service.Hostname] = serviceConfig(instance.Service.Hostname, instance, config,
			outboundRoute || mesh.DisablePolicyChecks, outboundRoute)
	}

	if v2JSONMap, err := model.ToJSONMap(v2); err != nil {
		log.Warnf("Could not encode v2 HTTP mixerclient filter for node %q: %v", role, err)
	} else {
		filter.V2 = v2JSONMap
	}
	return filter
}

// addStandardNodeAttributes add standard node attributes with the given prefix
func addStandardNodeAttributes(attr map[string]*mpb.Attributes_AttributeValue, prefix string, IPAddress string, ID string, labels map[string]string) {
	if len(IPAddress) > 0 {
		attr[prefix+"."+AttrIPSuffix] = &mpb.Attributes_AttributeValue{
			Value: &mpb.Attributes_AttributeValue_BytesValue{net.ParseIP(IPAddress)},
		}
	}

	attr[prefix+"."+AttrUIDSuffix] = &mpb.Attributes_AttributeValue{
		Value: &mpb.Attributes_AttributeValue_StringValue{"kubernetes://" + ID},
	}

	if len(labels) > 0 {
		attr[prefix+"."+AttrLabelsSuffix] = &mpb.Attributes_AttributeValue{
			Value: &mpb.Attributes_AttributeValue_StringMapValue{
				StringMapValue: &mpb.Attributes_StringMap{Entries: labels},
			},
		}
	}
}

func standardNodeAttributes(prefix string, IPAddress string, ID string, labels map[string]string) map[string]*mpb.Attributes_AttributeValue {
	attrs := make(map[string]*mpb.Attributes_AttributeValue)
	addStandardNodeAttributes(attrs, prefix, IPAddress, ID, labels)
	return attrs
}

// generate serviceConfig for a given instance
func serviceConfig(serviceName string, dest *model.ServiceInstance, config model.IstioConfigStore, disableCheck, disableReport bool) *mccpb.ServiceConfig {
	sc := &mccpb.ServiceConfig{
		MixerAttributes: &mpb.Attributes{
			Attributes: map[string]*mpb.Attributes_AttributeValue{
				AttrDestinationService: {
					Value: &mpb.Attributes_AttributeValue_StringValue{StringValue: serviceName},
				},
			},
		},
		DisableCheckCalls:  disableCheck,
		DisableReportCalls: disableReport,
	}

	if len(dest.Labels) > 0 {
		sc.MixerAttributes.Attributes[AttrDestinationLabels] = &mpb.Attributes_AttributeValue{
			Value: &mpb.Attributes_AttributeValue_StringMapValue{
				StringMapValue: &mpb.Attributes_StringMap{Entries: dest.Labels},
			},
		}
	}

	apiSpecs := config.HTTPAPISpecByDestination(dest)
	model.SortHTTPAPISpec(apiSpecs)
	for _, config := range apiSpecs {
		sc.HttpApiSpec = append(sc.HttpApiSpec, config.Spec.(*mccpb.HTTPAPISpec))
	}

	quotaSpecs := config.QuotaSpecByDestination(dest)
	model.SortQuotaSpec(quotaSpecs)
	for _, config := range quotaSpecs {
		sc.QuotaSpec = append(sc.QuotaSpec, config.Spec.(*mccpb.QuotaSpec))
	}

	authSpecs := config.EndUserAuthenticationPolicySpecByDestination(dest)
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
				dest.Service, spec)
		}
	}

	return sc
}

// BuildTCPMixerFilterConfig builds a TCP filter config for inbound requests.
func BuildTCPMixerFilterConfig(mesh *meshconfig.MeshConfig, role model.Proxy, instance *model.ServiceInstance) *FilterMixerConfig {
	filter := &FilterMixerConfig{
		MixerAttributes: map[string]string{
			AttrDestinationIP:  role.IPAddress,
			AttrDestinationUID: "kubernetes://" + role.ID,
		},
	}

	attrs := standardNodeAttributes(AttrDestinationPrefix, role.IPAddress, role.ID, nil)
	attrs[AttrDestinationService] = &mpb.Attributes_AttributeValue{Value: &mpb.Attributes_AttributeValue_StringValue{instance.Service.Hostname}}

	v2 := &mccpb.TcpClientConfig{
		MixerAttributes: &mpb.Attributes{
			Attributes: attrs,
		},
		Transport: &mccpb.TransportConfig{
			CheckCluster:  MixerCheckClusterName,
			ReportCluster: MixerReportClusterName,
		},
		DisableCheckCalls: mesh.DisablePolicyChecks,
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

	return TruncateClusterName(OutboundJWTURIClusterPrefix + name), address, useSSL, nil
}

// BuildMixerAuthFilterClusters builds the necessary clusters for the
// JWT auth filter to fetch public keys from the specified jwks_uri.
func BuildMixerAuthFilterClusters(config model.IstioConfigStore, mesh *meshconfig.MeshConfig, proxyInstances []*model.ServiceInstance) Clusters {
	type authCluster struct {
		name   string
		useSSL bool
	}
	authClusters := map[string]authCluster{}
	for _, instance := range proxyInstances {
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
