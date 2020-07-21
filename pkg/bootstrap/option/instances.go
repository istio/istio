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

package option

import (
	"net"

	"github.com/gogo/protobuf/types"

	meshAPI "istio.io/api/mesh/v1alpha1"
	networkingAPI "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/model"
)

type LocalhostValue string
type WildcardValue string
type DNSLookupFamilyValue string

const (
	LocalhostIPv4       LocalhostValue       = "127.0.0.1"
	LocalhostIPv6       LocalhostValue       = "::1"
	WildcardIPv4        WildcardValue        = "0.0.0.0"
	WildcardIPv6        WildcardValue        = "::"
	DNSLookupFamilyIPv4 DNSLookupFamilyValue = "V4_ONLY"
	DNSLookupFamilyIPv6 DNSLookupFamilyValue = "AUTO"
)

func ProxyConfig(value *meshAPI.ProxyConfig) Instance {
	return newOption("config", value)
}

func PilotSubjectAltName(value []string) Instance {
	return newOption("pilot_SAN", value).withConvert(sanConverter(value))
}

func MixerSubjectAltName(value []string) Instance {
	san := ""
	if len(value) > 0 {
		san = value[0]
	}
	return newOptionOrSkipIfZero("MixerSubjectAltName", san)
}

func ConnectTimeout(value *types.Duration) Instance {
	return newDurationOption("connect_timeout", value)
}

func Cluster(value string) Instance {
	return newOption("cluster", value)
}

func NodeID(value string) Instance {
	return newOption("nodeID", value)
}

func Region(value string) Instance {
	return newOptionOrSkipIfZero("region", value)
}

func Zone(value string) Instance {
	return newOptionOrSkipIfZero("zone", value)
}

func SubZone(value string) Instance {
	return newOptionOrSkipIfZero("sub_zone", value)
}

func NodeMetadata(meta *model.BootstrapNodeMetadata, rawMeta map[string]interface{}) Instance {
	return newOptionOrSkipIfZero("meta_json_str", meta).withConvert(nodeMetadataConverter(meta, rawMeta))
}

func DiscoveryAddress(value string) Instance {
	return newOption("discovery_address", value)
}

func Localhost(value LocalhostValue) Instance {
	return newOption("localhost", value)
}

func Wildcard(value WildcardValue) Instance {
	return newOption("wildcard", value)
}

func DNSLookupFamily(value DNSLookupFamilyValue) Instance {
	return newOption("dns_lookup_family", value)
}

func PodName(value string) Instance {
	return newOptionOrSkipIfZero("PodName", value)
}

func PodNamespace(value string) Instance {
	return newOptionOrSkipIfZero("PodNamespace", value)
}

func PodIP(value net.IP) Instance {
	return newOption("PodIP", value).withConvert(podIPConverter(value))
}

func ControlPlaneAuth(value bool) Instance {
	strVal := ""
	if value {
		strVal = "enable"
	}
	return newOptionOrSkipIfZero("ControlPlaneAuth", strVal)
}

func DisableReportCalls(value bool) Instance {
	strVal := ""
	if value {
		strVal = "true"
	}
	return newOptionOrSkipIfZero("DisableReportCalls", strVal)
}

func OutlierLogPath(value string) Instance {
	return newOptionOrSkipIfZero("outlier_log_path", value)
}

func LightstepAddress(value string) Instance {
	return newOptionOrSkipIfZero("lightstep", value).withConvert(addressConverter(value))
}

func LightstepToken(value string) Instance {
	return newOption("lightstepToken", value)
}

func StackDriverEnabled(value bool) Instance {
	return newOption("stackdriver", value)
}

func StackDriverProjectID(value string) Instance {
	return newOption("stackdriverProjectID", value)
}

func StackDriverDebug(value bool) Instance {
	return newOption("stackdriverDebug", value)
}

func StackDriverMaxAnnotations(value int64) Instance {
	return newOption("stackdriverMaxAnnotations", value)
}

func StackDriverMaxAttributes(value int64) Instance {
	return newOption("stackdriverMaxAttributes", value)
}

func StackDriverMaxEvents(value int64) Instance {
	return newOption("stackdriverMaxEvents", value)
}

func PilotGRPCAddress(value string) Instance {
	return newOptionOrSkipIfZero("pilot_grpc_address", value).withConvert(addressConverter(value))
}

func ZipkinAddress(value string) Instance {
	return newOptionOrSkipIfZero("zipkin", value).withConvert(addressConverter(value))
}

func DataDogAddress(value string) Instance {
	return newOptionOrSkipIfZero("datadog", value).withConvert(addressConverter(value))
}

func StatsdAddress(value string) Instance {
	return newOptionOrSkipIfZero("statsd", value).withConvert(addressConverter(value))
}

func TracingTLS(value *networkingAPI.ClientTLSSettings, metadata *model.BootstrapNodeMetadata, isH2 bool) Instance {
	return newOptionOrSkipIfZero("tracing_tls", value).
		withConvert(transportSocketConverter(value, "tracer", metadata, isH2))
}

func EnvoyMetricsServiceAddress(value string) Instance {
	return newOptionOrSkipIfZero("envoy_metrics_service_address", value).withConvert(addressConverter(value))
}

func EnvoyMetricsServiceTLS(value *networkingAPI.ClientTLSSettings, metadata *model.BootstrapNodeMetadata) Instance {
	return newOptionOrSkipIfZero("envoy_metrics_service_tls", value).
		withConvert(transportSocketConverter(value, "envoy_metrics_service", metadata, true))
}

func EnvoyMetricsServiceTCPKeepalive(value *networkingAPI.ConnectionPoolSettings_TCPSettings_TcpKeepalive) Instance {
	return newTCPKeepaliveOption("envoy_metrics_service_tcp_keepalive", value)
}

func EnvoyAccessLogServiceAddress(value string) Instance {
	return newOptionOrSkipIfZero("envoy_accesslog_service_address", value).withConvert(addressConverter(value))
}

func EnvoyAccessLogServiceTLS(value *networkingAPI.ClientTLSSettings, metadata *model.BootstrapNodeMetadata) Instance {
	return newOptionOrSkipIfZero("envoy_accesslog_service_tls", value).
		withConvert(transportSocketConverter(value, "envoy_accesslog_service", metadata, true))
}

func EnvoyAccessLogServiceTCPKeepalive(value *networkingAPI.ConnectionPoolSettings_TCPSettings_TcpKeepalive) Instance {
	return newTCPKeepaliveOption("envoy_accesslog_service_tcp_keepalive", value)
}

func EnvoyExtraStatTags(value []string) Instance {
	return newStringArrayOptionOrSkipIfEmpty("extraStatTags", value)
}

func EnvoyStatsMatcherInclusionPrefix(value []string) Instance {
	return newStringArrayOptionOrSkipIfEmpty("inclusionPrefix", value)
}

func EnvoyStatsMatcherInclusionSuffix(value []string) Instance {
	return newStringArrayOptionOrSkipIfEmpty("inclusionSuffix", value)
}

func EnvoyStatsMatcherInclusionRegexp(value []string) Instance {
	return newStringArrayOptionOrSkipIfEmpty("inclusionRegexps", value)
}

func PilotCertProvider(value string) Instance {
	return newOption("pilot_cert_provider", value)
}

func STSPort(value int) Instance {
	return newOption("sts_port", value)
}

func GCPProjectID(value string) Instance {
	return newOption("gcp_project_id", value)
}

func STSEnabled(value bool) Instance {
	return newOption("sts", value)
}

func ProvCert(value string) Instance {
	return newOption("provisioned_cert", value)
}

func DiscoveryHost(value string) Instance {
	return newOption("discovery_host", value)
}
