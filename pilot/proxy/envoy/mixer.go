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

package envoy

import (
	"net"
	"strings"

	"github.com/golang/glog"

	mpb "istio.io/api/mixer/v1"
	mccpb "istio.io/api/mixer/v1/config/client"
	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/istio/pilot/model"
	"istio.io/istio/pilot/proxy"
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
func buildMixerCluster(mesh *proxyconfig.MeshConfig, role proxy.Node, mixerSAN []string) *Cluster {
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
	case proxyconfig.AuthenticationPolicy_NONE:
		// do nothing
	case proxyconfig.AuthenticationPolicy_MUTUAL_TLS:
		// apply SSL context to enable mutual TLS between Envoy proxies between app and mixer
		mixerCluster.SSLContext = buildClusterSSLContext(proxy.AuthCertsPath, mixerSAN)
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
func mixerHTTPRouteConfig(role proxy.Node, services []string) *FilterMixerConfig {
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
		ForwardAttributes: &mpb.Attributes{
			Attributes: map[string]*mpb.Attributes_AttributeValue{
				AttrSourceIP:  {Value: &mpb.Attributes_AttributeValue_BytesValue{net.ParseIP(role.IPAddress)}},
				AttrSourceUID: {Value: &mpb.Attributes_AttributeValue_StringValue{"kubernetes://" + role.ID}},
			},
		},
		ServiceConfigs: map[string]*mccpb.ServiceConfig{},
	}
	if len(services) > 0 {
		// legacy mixerclient behavior is a comma separated list of
		// services. Can this be removed?
		filter.MixerAttributes[AttrDestinationService] = strings.Join(services, ",")

		// arbitrarily make the first service in the list the default
		v2.DefaultDestinationService = services[0]
	}

	for _, service := range services {
		v2.ServiceConfigs[service] = &mccpb.ServiceConfig{
			MixerAttributes: &mpb.Attributes{
				Attributes: map[string]*mpb.Attributes_AttributeValue{
					AttrDestinationService: {Value: &mpb.Attributes_AttributeValue_StringValue{service}},
				},
			},
			// TODO per-service HttpApiApsec, QuotaSpec
		}
	}

	if v2JSONMap, err := model.ToJSONMap(v2); err != nil {
		glog.Warningf("Could not encode v2 HTTP mixerclient filter for node %q: %v", role, err)
	} else {
		filter.V2 = v2JSONMap
	}
	return filter
}

// Mixer TCP filter config for inbound requests.
func mixerTCPConfig(role proxy.Node, check bool) *FilterMixerConfig {
	filter := &FilterMixerConfig{
		MixerAttributes: map[string]string{
			AttrDestinationIP:  role.IPAddress,
			AttrDestinationUID: "kubernetes://" + role.ID,
		},
	}
	v2 := &mccpb.TcpClientConfig{
		MixerAttributes: &mpb.Attributes{
			Attributes: map[string]*mpb.Attributes_AttributeValue{
				AttrDestinationIP:  {Value: &mpb.Attributes_AttributeValue_StringValue{role.IPAddress}},
				AttrDestinationUID: {Value: &mpb.Attributes_AttributeValue_StringValue{"kubernetes://" + role.ID}},
			},
		},
	}
	if v2JSONMap, err := model.ToJSONMap(v2); err != nil {
		glog.Warningf("Could not encode v2 TCP mixerclient filter for node %q: %v", role, err)
	} else {
		filter.V2 = v2JSONMap

	}
	return filter
}
