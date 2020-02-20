// Copyright 2019 Istio Authors
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

package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"istio.io/istio/pkg/config/constants"

	"github.com/gogo/protobuf/types"
	"golang.org/x/oauth2/google"

	meshAPI "istio.io/api/mesh/v1alpha1"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/bootstrap/option"
	"istio.io/istio/pkg/bootstrap/platform"
	"istio.io/istio/pkg/spiffe"
)

const (
	// IstioMetaPrefix is used to pass env vars as node metadata.
	IstioMetaPrefix = "ISTIO_META_"

	// IstioMetaJSONPrefix is used to pass annotations and similar environment info.
	IstioMetaJSONPrefix = "ISTIO_METAJSON_"

	lightstepAccessTokenBase = "lightstep_access_token.txt"

	// required stats are used by readiness checks.
	requiredEnvoyStatsMatcherInclusionPrefixes = "cluster_manager,listener_manager,http_mixer_filter,tcp_mixer_filter,server,cluster.xds-grpc"
	requiredEnvoyStatsMatcherInclusionSuffix   = "ssl_context_update_by_sds"

	// Prefixes of V2 metrics.
	// "reporter" prefix is for istio standard metrics.
	// "component" prefix is for istio_build metric.
	v2Prefixes = "reporter=,component,"
)

var (
	// These must match the json field names in model.nodeMetadata
	metadataExchangeKeys = []string{
		"NAME",
		"NAMESPACE",
		"INSTANCE_IPS",
		"LABELS",
		"OWNER",
		"PLATFORM_METADATA",
		"WORKLOAD_NAME",
		"CANONICAL_TELEMETRY_SERVICE",
		"MESH_ID",
		"SERVICE_ACCOUNT",
	}
)

// Config for creating a bootstrap file.
type Config struct {
	Node                string
	DNSRefreshRate      string
	Proxy               *meshAPI.ProxyConfig
	PlatEnv             platform.Environment
	PilotSubjectAltName []string
	MixerSubjectAltName []string
	LocalEnv            []string
	NodeIPs             []string
	PodName             string
	PodNamespace        string
	PodIP               net.IP
	SDSUDSPath          string
	SDSTokenPath        string
	STSPort             int
	ControlPlaneAuth    bool
	DisableReportCalls  bool
	OutlierLogPath      string
	PilotCertProvider   string
}

// newTemplateParams creates a new template configuration for the given configuration.
func (cfg Config) toTemplateParams() (map[string]interface{}, error) {
	opts := make([]option.Instance, 0)

	// Fill in default config values.
	if cfg.PilotSubjectAltName == nil {
		cfg.PilotSubjectAltName = defaultPilotSAN()
	}
	if cfg.PlatEnv == nil {
		cfg.PlatEnv = platform.Discover()
	}

	// Remove duplicates from the node IPs.
	cfg.NodeIPs = removeDuplicates(cfg.NodeIPs)

	opts = append(opts,
		option.NodeID(cfg.Node),
		option.PodName(cfg.PodName),
		option.PodNamespace(cfg.PodNamespace),
		option.PodIP(cfg.PodIP),
		option.PilotSubjectAltName(cfg.PilotSubjectAltName),
		option.MixerSubjectAltName(cfg.MixerSubjectAltName),
		option.DNSRefreshRate(cfg.DNSRefreshRate),
		option.SDSTokenPath(cfg.SDSTokenPath),
		option.SDSUDSPath(cfg.SDSUDSPath),
		option.ControlPlaneAuth(cfg.ControlPlaneAuth),
		option.DisableReportCalls(cfg.DisableReportCalls),
		option.PilotCertProvider(cfg.PilotCertProvider),
		option.OutlierLogPath(cfg.OutlierLogPath))

	// Support passing extra info from node environment as metadata
	sdsEnabled := cfg.SDSTokenPath != "" && cfg.SDSUDSPath != ""
	meta, rawMeta, err := getNodeMetaData(cfg.LocalEnv, cfg.PlatEnv, cfg.NodeIPs, sdsEnabled, cfg.STSPort)
	if err != nil {
		return nil, err
	}
	opts = append(opts, getNodeMetadataOptions(meta, rawMeta, cfg.PlatEnv)...)

	// Check if nodeIP carries IPv4 or IPv6 and set up proxy accordingly
	if isIPv6Proxy(cfg.NodeIPs) {
		opts = append(opts,
			option.Localhost(option.LocalhostIPv6),
			option.Wildcard(option.WildcardIPv6),
			option.DNSLookupFamily(option.DNSLookupFamilyIPv6))
	} else {
		opts = append(opts,
			option.Localhost(option.LocalhostIPv4),
			option.Wildcard(option.WildcardIPv4),
			option.DNSLookupFamily(option.DNSLookupFamilyIPv4))
	}

	proxyOpts, err := getProxyConfigOptions(cfg.Proxy, meta)
	if err != nil {
		return nil, err
	}
	opts = append(opts, proxyOpts...)

	// TODO: allow reading a file with additional metadata (for example if created with
	// 'envref'. This will allow Istio to generate the right config even if the pod info
	// is not available (in particular in some multi-cluster cases)
	return option.NewTemplateParams(opts...)
}

// substituteValues substitutes variables known to the bootstrap like pod_ip.
// "http.{pod_ip}_" with pod_id = [10.3.3.3,10.4.4.4] --> [http.10.3.3.3_,http.10.4.4.4_]
func substituteValues(patterns []string, varName string, values []string) []string {
	ret := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		if !strings.Contains(pattern, varName) {
			ret = append(ret, pattern)
			continue
		}

		for _, val := range values {
			ret = append(ret, strings.Replace(pattern, varName, val, -1))
		}
	}
	return ret
}

func getStatsOptions(meta *model.NodeMetadata, nodeIPs []string) []option.Instance {
	parseOption := func(metaOption string, required string) []string {
		var inclusionOption []string
		if len(metaOption) > 0 {
			inclusionOption = strings.Split(metaOption, ",")
		}

		if len(required) > 0 {
			inclusionOption = append(inclusionOption, strings.Split(required, ",")...)
		}

		// At the sidecar we can limit downstream metrics collection to the inbound listener.
		// Inbound downstream metrics are named as: http.{pod_ip}_{port}.downstream_rq_*
		// Other outbound downstream metrics are numerous and not very interesting for a sidecar.
		// specifying http.{pod_ip}_  as a prefix will capture these downstream metrics.
		return substituteValues(inclusionOption, "{pod_ip}", nodeIPs)
	}

	return []option.Instance{
		option.EnvoyStatsMatcherInclusionPrefix(parseOption(meta.StatsInclusionPrefixes, requiredEnvoyStatsMatcherInclusionPrefixes)),
		option.EnvoyStatsMatcherInclusionSuffix(parseOption(meta.StatsInclusionSuffixes, requiredEnvoyStatsMatcherInclusionSuffix)),
		option.EnvoyStatsMatcherInclusionRegexp(parseOption(meta.StatsInclusionRegexps, "")),
		option.EnvoyExtraStatTags(parseOption(meta.ExtraStatTags, "")),
	}
}

func defaultPilotSAN() []string {
	return []string{
		spiffe.MustGenSpiffeURI("istio-system", "istio-pilot-service-account")}
}

func lightstepAccessTokenFile(config string) string {
	return path.Join(config, lightstepAccessTokenBase)
}

// convertDuration converts to golang duration and logs errors
func convertDuration(d *types.Duration) time.Duration {
	if d == nil {
		return 0
	}
	dur, _ := types.DurationFromProto(d)
	return dur
}

func getNodeMetadataOptions(meta *model.NodeMetadata, rawMeta map[string]interface{},
	platEnv platform.Environment) []option.Instance {
	// Add locality options.
	opts := getLocalityOptions(meta, platEnv)

	opts = append(opts, getStatsOptions(meta, meta.InstanceIPs)...)

	opts = append(opts, option.NodeMetadata(meta, rawMeta))
	return opts
}

func getLocalityOptions(meta *model.NodeMetadata, platEnv platform.Environment) []option.Instance {
	l := util.ConvertLocality(model.GetLocalityOrDefault(meta.LocalityLabel, ""))
	if l == nil {
		// Populate the platform locality if available.
		l = platEnv.Locality()
	}

	return []option.Instance{option.Region(l.Region), option.Zone(l.Zone), option.SubZone(l.SubZone)}
}

func getProxyConfigOptions(config *meshAPI.ProxyConfig, metadata *model.NodeMetadata) ([]option.Instance, error) {
	// Add a few misc options.
	opts := make([]option.Instance, 0)

	opts = append(opts, option.ProxyConfig(config),
		option.ConnectTimeout(config.ConnectTimeout),
		option.Cluster(config.ServiceCluster),
		option.PilotGRPCAddress(config.DiscoveryAddress),
		option.DiscoveryAddress(config.DiscoveryAddress),
		option.StatsdAddress(config.StatsdUdpAddress))

	// Add tracing options.
	if config.Tracing != nil {
		switch tracer := config.Tracing.Tracer.(type) {
		case *meshAPI.Tracing_Zipkin_:
			opts = append(opts, option.ZipkinAddress(tracer.Zipkin.Address))
		case *meshAPI.Tracing_Lightstep_:
			// Create the token file.
			lightstepAccessTokenPath := lightstepAccessTokenFile(config.ConfigPath)
			lsConfigOut, err := os.Create(lightstepAccessTokenPath)
			if err != nil {
				return nil, err
			}
			_, err = lsConfigOut.WriteString(tracer.Lightstep.AccessToken)
			if err != nil {
				return nil, err
			}

			opts = append(opts, option.LightstepAddress(tracer.Lightstep.Address),
				option.LightstepToken(lightstepAccessTokenPath),
				option.LightstepSecure(tracer.Lightstep.Secure),
				option.LightstepCACertPath(tracer.Lightstep.CacertPath))
		case *meshAPI.Tracing_Datadog_:
			opts = append(opts, option.DataDogAddress(tracer.Datadog.Address))
		case *meshAPI.Tracing_Stackdriver_:
			var cred *google.Credentials
			var err error
			// in-cluster credentials are fetched by using the GCE metadata server.
			// You may also specify environment variable GOOGLE_APPLICATION_CREDENTIALS to point a GCP credentials file.
			if cred, err = google.FindDefaultCredentials(context.Background()); err != nil {
				return nil, fmt.Errorf("unable to process Stackdriver tracer: %v", err)
			}

			opts = append(opts, option.StackDriverEnabled(true),
				option.StackDriverProjectID(cred.ProjectID),
				option.StackDriverDebug(tracer.Stackdriver.Debug),
				option.StackDriverMaxAnnotations(getInt64ValueOrDefault(tracer.Stackdriver.MaxNumberOfAnnotations, 200)),
				option.StackDriverMaxAttributes(getInt64ValueOrDefault(tracer.Stackdriver.MaxNumberOfAttributes, 200)),
				option.StackDriverMaxEvents(getInt64ValueOrDefault(tracer.Stackdriver.MaxNumberOfMessageEvents, 200)))
		}
	}

	// Add options for Envoy metrics.
	if config.EnvoyMetricsService != nil && config.EnvoyMetricsService.Address != "" {
		opts = append(opts, option.EnvoyMetricsServiceAddress(config.EnvoyMetricsService.Address),
			option.EnvoyMetricsServiceTLS(config.EnvoyMetricsService.TlsSettings, metadata),
			option.EnvoyMetricsServiceTCPKeepalive(config.EnvoyMetricsService.TcpKeepalive))
	} else if config.EnvoyMetricsServiceAddress != "" {
		opts = append(opts, option.EnvoyMetricsServiceAddress(config.EnvoyMetricsService.Address))
	}

	// Add options for Envoy access log.
	if config.EnvoyAccessLogService != nil && config.EnvoyAccessLogService.Address != "" {
		opts = append(opts, option.EnvoyAccessLogServiceAddress(config.EnvoyAccessLogService.Address),
			option.EnvoyAccessLogServiceTLS(config.EnvoyAccessLogService.TlsSettings, metadata),
			option.EnvoyAccessLogServiceTCPKeepalive(config.EnvoyAccessLogService.TcpKeepalive))
	}

	return opts, nil
}

func getInt64ValueOrDefault(src *types.Int64Value, defaultVal int64) int64 {
	val := defaultVal
	if src != nil {
		val = src.Value
	}
	return val
}

// isIPv6Proxy check the addresses slice and returns true for a valid IPv6 address
// for all other cases it returns false
func isIPv6Proxy(ipAddrs []string) bool {
	for i := 0; i < len(ipAddrs); i++ {
		addr := net.ParseIP(ipAddrs[i])
		if addr == nil {
			// Should not happen, invalid IP in proxy's IPAddresses slice should have been caught earlier,
			// skip it to prevent a panic.
			continue
		}
		if addr.To4() != nil {
			return false
		}
	}
	return true
}

type setMetaFunc func(m map[string]interface{}, key string, val string)

func extractMetadata(envs []string, prefix string, set setMetaFunc, meta map[string]interface{}) {
	metaPrefixLen := len(prefix)
	for _, e := range envs {
		if !shouldExtract(e, prefix) {
			continue
		}
		v := e[metaPrefixLen:]
		if !isEnvVar(v) {
			continue
		}
		metaKey, metaVal := parseEnvVar(v)
		set(meta, metaKey, metaVal)
	}
}

func shouldExtract(envVar, prefix string) bool {
	if strings.HasPrefix(envVar, "ISTIO_META_WORKLOAD") {
		return false
	}
	return strings.HasPrefix(envVar, prefix)
}

func isEnvVar(str string) bool {
	return strings.Contains(str, "=")
}

func parseEnvVar(varStr string) (string, string) {
	parts := strings.SplitN(varStr, "=", 2)
	if len(parts) != 2 {
		return varStr, ""
	}
	return parts[0], parts[1]
}

func jsonStringToMap(jsonStr string) (m map[string]string) {
	err := json.Unmarshal([]byte(jsonStr), &m)
	if err != nil {
		log.Warnf("Env variable with value %q failed json unmarshal: %v", jsonStr, err)
	}
	return
}

func extractAttributesMetadata(envVars []string, plat platform.Environment, meta *model.NodeMetadata) {
	for _, varStr := range envVars {
		name, val := parseEnvVar(varStr)
		switch name {
		case "ISTIO_METAJSON_LABELS":
			m := jsonStringToMap(val)
			if len(m) > 0 {
				meta.Labels = m
				if telemetrySvc := m["istioTelemetryService"]; len(telemetrySvc) > 0 {
					meta.CanonicalTelemetryService = m["istioTelemetryService"]
				}
			}
		case "POD_NAME":
			meta.InstanceName = val
		case "POD_NAMESPACE":
			meta.Namespace = val
		case "ISTIO_META_OWNER":
			meta.Owner = val
		case "ISTIO_META_WORKLOAD_NAME":
			meta.WorkloadName = val
		case "SERVICE_ACCOUNT":
			meta.ServiceAccount = val
		}
	}
	if plat != nil && len(plat.Metadata()) > 0 {
		meta.PlatformMetadata = plat.Metadata()
	}
	meta.ExchangeKeys = metadataExchangeKeys
}

// getNodeMetaData function uses an environment variable contract
// ISTIO_METAJSON_* env variables contain json_string in the value.
// 					The name of variable is ignored.
// ISTIO_META_* env variables are passed thru
func getNodeMetaData(envs []string, plat platform.Environment, nodeIPs []string,
	sdsEnabled bool, stsPort int) (*model.NodeMetadata, map[string]interface{}, error) {
	meta := &model.NodeMetadata{}
	untypedMeta := map[string]interface{}{}

	extractMetadata(envs, IstioMetaPrefix, func(m map[string]interface{}, key string, val string) {
		m[key] = val
	}, untypedMeta)

	extractMetadata(envs, IstioMetaJSONPrefix, func(m map[string]interface{}, key string, val string) {
		err := json.Unmarshal([]byte(val), &m)
		if err != nil {
			log.Warnf("Env variable %s [%s] failed json unmarshal: %v", key, val, err)
		}
	}, untypedMeta)

	j, err := json.Marshal(untypedMeta)
	if err != nil {
		return nil, nil, err
	}
	if err := meta.UnmarshalJSON(j); err != nil {
		return nil, nil, err
	}
	extractAttributesMetadata(envs, plat, meta)

	// Support multiple network interfaces, removing duplicates.
	meta.InstanceIPs = nodeIPs

	// Set SDS configuration on the metadata, if provided.
	if sdsEnabled {
		// sds is enabled
		meta.SdsEnabled = true
		meta.SdsTrustJwt = true
	}

	// Add STS port into node metadata if it is not 0.
	if stsPort != 0 {
		meta.StsPort = strconv.Itoa(stsPort)
	}

	// Add all pod labels found from filesystem
	// These are typically volume mounted by the downward API
	lbls, err := readPodLabels()
	if err == nil {
		if meta.Labels == nil {
			meta.Labels = map[string]string{}
		}
		for k, v := range lbls {
			meta.Labels[k] = v
		}
	} else {
		log.Warnf("failed to read pod labels: %v", err)
	}

	return meta, untypedMeta, nil
}

func readPodLabels() (map[string]string, error) {
	b, err := ioutil.ReadFile(constants.PodInfoLabelsPath)
	if err != nil {
		return nil, err
	}
	return ParseDownwardAPI(string(b))
}

// Fields are stored as format `%s=%q`, we will parse this back to a map
func ParseDownwardAPI(i string) (map[string]string, error) {
	res := map[string]string{}
	for _, line := range strings.Split(i, "\n") {
		sl := strings.SplitN(line, "=", 2)
		if len(sl) != 2 {
			continue
		}
		key := sl[0]
		// Strip the leading/trailing quotes
		val := sl[1][1 : len(sl[1])-1]
		res[key] = val
	}
	return res, nil
}

func removeDuplicates(values []string) []string {
	set := make(map[string]struct{})
	newValues := make([]string, 0, len(values))
	for _, v := range values {
		if _, ok := set[v]; !ok {
			set[v] = struct{}{}
			newValues = append(newValues, v)
		}
	}
	return newValues
}
