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

package bootstrap

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"istio.io/api/annotation"
	meshAPI "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/util/network"
	"istio.io/istio/pkg/bootstrap/option"
	"istio.io/istio/pkg/bootstrap/platform"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/env"
	common_features "istio.io/istio/pkg/features"
	"istio.io/istio/pkg/kube/labels"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/model"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/version"
)

const (
	// IstioMetaPrefix is used to pass env vars as node metadata.
	IstioMetaPrefix = "ISTIO_META_"

	// IstioMetaJSONPrefix is used to pass annotations and similar environment info.
	IstioMetaJSONPrefix = "ISTIO_METAJSON_"

	lightstepAccessTokenBase = "lightstep_access_token.txt"

	// required stats are used by readiness checks.
	requiredEnvoyStatsMatcherInclusionPrefixes = "cluster_manager,listener_manager,server,cluster.xds-grpc,wasm"

	rbacEnvoyStatsMatcherInclusionSuffix = "rbac.allowed,rbac.denied,shadow_allowed,shadow_denied"

	requiredEnvoyStatsMatcherInclusionSuffixes = rbacEnvoyStatsMatcherInclusionSuffix + ",downstream_cx_active" // Needed for draining.

	// required for metrics based on stat_prefix in virtual service.
	requiredEnvoyStatsMatcherInclusionRegexes = `vhost\..*\.route\..*`

	// Prefixes of V2 metrics.
	// "reporter" prefix is for istio standard metrics.
	// "component" suffix is for istio_build metric.
	v2Prefixes = "reporter=,"
	v2Suffix   = ",component,istio"
)

var envoyWellKnownCompressorLibrary = sets.String{
	"gzip":   {},
	"zstd":   {},
	"brotli": {},
}

// Config for creating a bootstrap file.
type Config struct {
	*model.Node
	// CompliancePolicy to decouple the environment variable dependency.
	CompliancePolicy string
	LogAsJSON        bool
}

// toTemplateParams creates a new template configuration for the given configuration.
func (cfg Config) toTemplateParams() (map[string]any, error) {
	opts := make([]option.Instance, 0)

	discHost := strings.Split(cfg.Metadata.ProxyConfig.DiscoveryAddress, ":")[0]

	xdsType := "GRPC"
	if features.DeltaXds {
		xdsType = "DELTA_GRPC"
	}

	// Waypoint overrides
	metadataDiscovery := cfg.Metadata.MetadataDiscovery
	if strings.HasPrefix(cfg.ID, "waypoint~") {
		xdsType = "DELTA_GRPC"
		metadataDiscovery = ptr.Of(model.StringBool(true))
	}

	var mDiscovery bool
	if metadataDiscovery != nil && *metadataDiscovery {
		mDiscovery = true
	}
	if mDiscovery && xdsType == "GRPC" {
		log.Warnf("disabling metadata discovery; not supported on SotW XDS")
		// Not supported on legacy SotW protocol
		mDiscovery = false
	}
	customSDSPath := ""
	if _, f := cfg.RawMetadata[security.CredentialFileMetaDataName]; f {
		customSDSPath = security.FileCredentialNameSocketPath
	}
	opts = append(opts,
		option.NodeID(cfg.ID),
		option.NodeType(cfg.ID),
		option.PilotSubjectAltName(cfg.Metadata.PilotSubjectAltName),
		option.OutlierLogPath(cfg.Metadata.OutlierLogPath),
		option.CustomFileSDSPath(customSDSPath),
		option.ApplicationLogJSON(cfg.LogAsJSON),
		option.DiscoveryHost(discHost),
		option.Metadata(cfg.Metadata),
		option.XdsType(xdsType),
		option.MetadataDiscovery(mDiscovery),
		option.MetricsLocalhostAccessOnly(cfg.Metadata.ProxyConfig.ProxyMetadata),
	)

	// Add GCPProjectNumber to access in bootstrap template.
	md := cfg.Metadata.PlatformMetadata
	if projectNumber, found := md[platform.GCPProjectNumber]; found {
		opts = append(opts, option.GCPProjectNumber(projectNumber))
	}

	if cfg.Metadata.StsPort != "" {
		stsPort, err := strconv.Atoi(cfg.Metadata.StsPort)
		if err == nil && stsPort > 0 {
			opts = append(opts,
				option.STSEnabled(true),
				option.STSPort(stsPort))
			md := cfg.Metadata.PlatformMetadata
			if projectID, found := md[platform.GCPProject]; found {
				opts = append(opts, option.GCPProjectID(projectID))
			}
		}
	}

	opts = append(opts, option.WorkloadIdentitySocketFile(cfg.Metadata.WorkloadIdentitySocketFile))

	// Support passing extra info from node environment as metadata
	opts = append(opts, getNodeMetadataOptions(cfg.Node, cfg.CompliancePolicy)...)

	// Check if nodeIP carries IPv4 or IPv6 and set up proxy accordingly
	if network.AllIPv4(cfg.Metadata.InstanceIPs) {
		// IPv4 only
		opts = append(opts,
			option.Localhost(option.LocalhostIPv4),
			option.Wildcard(option.WildcardIPv4),
			option.DNSLookupFamily(option.DNSLookupFamilyIPv4))
	} else if network.AllIPv6(cfg.Metadata.InstanceIPs) {
		// IPv6 only
		opts = append(opts,
			option.Localhost(option.LocalhostIPv6),
			option.Wildcard(option.WildcardIPv6),
			option.DNSLookupFamily(option.DNSLookupFamilyIPv6))
	} else {
		// Dual Stack
		if features.EnableDualStack {
			// If dual-stack, it may be [IPv4, IPv6] or [IPv6, IPv4]
			// So let the first ip family policy to decide its DNSLookupFamilyIP policy
			ipFamily, err := network.CheckIPFamilyTypeForFirstIPs(cfg.Metadata.InstanceIPs)
			if err != nil {
				return nil, err
			}
			if ipFamily == network.IPv6 {
				opts = append(opts,
					option.Localhost(option.LocalhostIPv6),
					option.AdditionalLocalhost(option.LocalhostIPv4),
					option.Wildcard(option.WildcardIPv6),
					option.AdditionalWildCard(option.WildcardIPv4),
					option.DNSLookupFamily(option.DNSLookupFamilyIPS))
			} else {
				opts = append(opts,
					option.Localhost(option.LocalhostIPv4),
					option.AdditionalLocalhost(option.LocalhostIPv6),
					option.Wildcard(option.WildcardIPv4),
					option.AdditionalWildCard(option.WildcardIPv6),
					option.DNSLookupFamily(option.DNSLookupFamilyIPS))
			}
			opts = append(opts, option.DualStack(true))
		} else {
			// keep the original logic if Dual Stack is disabled
			opts = append(opts,
				option.Localhost(option.LocalhostIPv4),
				option.Wildcard(option.WildcardIPv4),
				option.DNSLookupFamily(option.DNSLookupFamilyIPv4))
		}
	}

	if features.EnvoyStatusPortEnableProxyProtocol {
		opts = append(opts, option.EnvoyStatusPortEnableProxyProtocol(true))
	}

	proxyOpts, err := getProxyConfigOptions(cfg.Metadata)
	if err != nil {
		return nil, err
	}
	opts = append(opts, proxyOpts...)

	// Append LRS related options.
	opts = append(opts, option.LoadStatsConfigJSONStr(cfg.Node))

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

func getStatsOptions(meta *model.BootstrapNodeMetadata) []option.Instance {
	nodeIPs := meta.InstanceIPs
	config := meta.ProxyConfig

	tagAnno := meta.Annotations[annotation.SidecarExtraStatTags.Name]
	prefixAnno := meta.Annotations[annotation.SidecarStatsInclusionPrefixes.Name]
	RegexAnno := meta.Annotations[annotation.SidecarStatsInclusionRegexps.Name]
	suffixAnno := meta.Annotations[annotation.SidecarStatsInclusionSuffixes.Name]

	parseOption := func(metaOption string, required string, proxyConfigOption []string) []string {
		var inclusionOption []string
		if len(metaOption) > 0 {
			inclusionOption = strings.Split(metaOption, ",")
		} else if proxyConfigOption != nil {
			// In case user relies on mixed usage of annotation and proxy config,
			// only consider proxy config if annotation is not set instead of merging.
			inclusionOption = proxyConfigOption
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

	extraStatTags := make([]string, 0, len(config.ExtraStatTags))
	for _, tag := range config.ExtraStatTags {
		if tag != "" {
			extraStatTags = append(extraStatTags, tag)
		}
	}
	for _, tag := range strings.Split(tagAnno, ",") {
		if tag != "" {
			extraStatTags = append(extraStatTags, tag)
		}
	}
	extraStatTags = removeDuplicates(extraStatTags)

	var proxyConfigPrefixes, proxyConfigSuffixes, proxyConfigRegexps []string
	if config.ProxyStatsMatcher != nil {
		proxyConfigPrefixes = config.ProxyStatsMatcher.InclusionPrefixes
		proxyConfigSuffixes = config.ProxyStatsMatcher.InclusionSuffixes
		proxyConfigRegexps = config.ProxyStatsMatcher.InclusionRegexps
	}
	inclusionSuffixes := rbacEnvoyStatsMatcherInclusionSuffix
	if meta.ExitOnZeroActiveConnections {
		inclusionSuffixes = requiredEnvoyStatsMatcherInclusionSuffixes
	}

	var buckets []option.HistogramBucket
	if bucketsAnno, ok := meta.Annotations[annotation.SidecarStatsHistogramBuckets.Name]; ok {
		js := map[string][]float64{}
		err := json.Unmarshal([]byte(bucketsAnno), &js)
		if err == nil {
			for prefix, value := range js {
				buckets = append(buckets, option.HistogramBucket{Match: option.HistogramMatch{Prefix: prefix}, Buckets: value})
			}
			sort.Slice(buckets, func(i, j int) bool {
				return buckets[i].Match.Prefix < buckets[j].Match.Prefix
			})
		} else {
			log.Warnf("Failed to unmarshal histogram buckets %v: %v", bucketsAnno, err)
		}
	}

	var compression string
	if statsCompression, ok := meta.Annotations[annotation.SidecarStatsCompression.Name]; ok &&
		envoyWellKnownCompressorLibrary.Contains(statsCompression) {
		compression = statsCompression
	}

	options := []option.Instance{
		option.EnvoyStatsMatcherInclusionPrefix(parseOption(prefixAnno,
			requiredEnvoyStatsMatcherInclusionPrefixes, proxyConfigPrefixes)),
		option.EnvoyStatsMatcherInclusionSuffix(parseOption(suffixAnno,
			inclusionSuffixes, proxyConfigSuffixes)),
		option.EnvoyStatsMatcherInclusionRegexp(parseOption(RegexAnno, requiredEnvoyStatsMatcherInclusionRegexes, proxyConfigRegexps)),
		option.EnvoyExtraStatTags(extraStatTags),
		option.EnvoyHistogramBuckets(buckets),
		option.EnvoyStatsCompression(compression),
	}

	statsFlushInterval := 5 * time.Second // Default value is 5s.
	if v, exits := meta.Annotations[annotation.SidecarStatsFlushInterval.Name]; exits {
		d, err := time.ParseDuration(v)
		if err == nil {
			statsFlushInterval = d
			options = append(options, option.EnvoyStatsFlushInterval(statsFlushInterval))
		} else {
			log.Warnf("Failed to parse stats flush interval %v: %v", v, err)
		}
	}

	if eviction, exits := meta.Annotations[annotation.SidecarStatsEvictionInterval.Name]; exits {
		statsEvictionInterval, err := time.ParseDuration(eviction)
		if err != nil {
			log.Warnf("Failed to parse stats eviction interval %v: %v", eviction, err)
		} else if statsEvictionInterval.Microseconds()%statsFlushInterval.Microseconds() != 0 {
			log.Warnf("StatsEvictionInterval must be a multiple of the StatsFlushInterval")
		} else {
			options = append(options, option.EnvoyStatsEvictionInterval(statsEvictionInterval))
		}
	}

	return options
}

func lightstepAccessTokenFile(config string) string {
	return path.Join(config, lightstepAccessTokenBase)
}

func getNodeMetadataOptions(node *model.Node, policy string) []option.Instance {
	// Add locality options.
	opts := getLocalityOptions(node.Locality)

	opts = append(opts, getStatsOptions(node.Metadata)...)

	opts = append(opts,
		option.NodeMetadata(node.Metadata, node.RawMetadata),
		option.RuntimeFlags(extractRuntimeFlags(node.Metadata.ProxyConfig, policy)),
		option.EnvoyStatusPort(node.Metadata.EnvoyStatusPort),
		option.EnvoyPrometheusPort(node.Metadata.EnvoyPrometheusPort))
	return opts
}

var StripFragment = env.Register("HTTP_STRIP_FRAGMENT_FROM_PATH_UNSAFE_IF_DISABLED", true, "").Get()

func extractRuntimeFlags(cfg *model.NodeMetaProxyConfig, policy string) map[string]any {
	// Setup defaults
	runtimeFlags := map[string]any{
		"overload.global_downstream_max_connections": "2147483647",
		"re2.max_program_size.error_level":           "32768",
		"envoy.deprecated_features:envoy.config.listener.v3.Listener.hidden_envoy_deprecated_use_original_dst": true,
		"envoy.reloadable_features.http_reject_path_with_fragment":                                             false,
	}
	if policy == common_features.FIPS_140_2 {
		// This flag limits google_grpc client in Envoy to TLSv1.2 as the maximum version.
		runtimeFlags["envoy.reloadable_features.google_grpc_disable_tls_13"] = true
	}
	if !StripFragment {
		// Note: the condition here is basically backwards. This was a mistake in the initial commit and cannot be reverted
		runtimeFlags["envoy.reloadable_features.http_strip_fragment_from_path_unsafe_if_disabled"] = "false"
	}
	for k, v := range cfg.RuntimeValues {
		if v == "" {
			// Envoy runtime doesn't see "" as a special value, so we use it to mean 'unset default flag'
			delete(runtimeFlags, k)
			continue
		}
		// Envoy used to allow everything as string but stopped in https://github.com/envoyproxy/envoy/issues/27434
		// However, our API always takes in strings.
		// Convert strings to bools for backwards compat.
		switch v {
		case "false":
			runtimeFlags[k] = false
		case "true":
			runtimeFlags[k] = true
		default:
			runtimeFlags[k] = v
		}
	}
	return runtimeFlags
}

func getLocalityOptions(l *core.Locality) []option.Instance {
	return []option.Instance{option.Region(l.Region), option.Zone(l.Zone), option.SubZone(l.SubZone)}
}

func getServiceCluster(metadata *model.BootstrapNodeMetadata) string {
	switch name := metadata.ProxyConfig.ClusterName.(type) {
	case *meshAPI.ProxyConfig_ServiceCluster:
		return serviceClusterOrDefault(name.ServiceCluster, metadata)

	case *meshAPI.ProxyConfig_TracingServiceName_:
		workloadName := metadata.WorkloadName
		if workloadName == "" {
			workloadName = "istio-proxy"
		}

		switch name.TracingServiceName {
		case meshAPI.ProxyConfig_APP_LABEL_AND_NAMESPACE:
			return serviceClusterOrDefault("istio-proxy", metadata)
		case meshAPI.ProxyConfig_CANONICAL_NAME_ONLY:
			cs, _ := labels.CanonicalService(metadata.Labels, workloadName)
			return serviceClusterOrDefault(cs, metadata)
		case meshAPI.ProxyConfig_CANONICAL_NAME_AND_NAMESPACE:
			cs, _ := labels.CanonicalService(metadata.Labels, workloadName)
			if metadata.Namespace != "" {
				return cs + "." + metadata.Namespace
			}
			return serviceClusterOrDefault(cs, metadata)
		default:
			return serviceClusterOrDefault("istio-proxy", metadata)
		}

	default:
		return serviceClusterOrDefault("istio-proxy", metadata)
	}
}

func serviceClusterOrDefault(name string, metadata *model.BootstrapNodeMetadata) string {
	if name != "" && name != "istio-proxy" {
		return name
	}
	if app, ok := metadata.Labels["app"]; ok {
		return app + "." + metadata.Namespace
	}
	if metadata.WorkloadName != "" {
		return metadata.WorkloadName + "." + metadata.Namespace
	}
	if metadata.Namespace != "" {
		return "istio-proxy." + metadata.Namespace
	}
	return "istio-proxy"
}

func getProxyConfigOptions(metadata *model.BootstrapNodeMetadata) ([]option.Instance, error) {
	config := metadata.ProxyConfig

	// Add a few misc options.
	opts := make([]option.Instance, 0)

	opts = append(opts, option.ProxyConfig(config),
		option.Cluster(getServiceCluster(metadata)),
		option.PilotGRPCAddress(config.DiscoveryAddress),
		option.DiscoveryAddress(config.DiscoveryAddress),
		option.StatsdAddress(config.StatsdUdpAddress),
		option.XDSRootCert(metadata.XDSRootCert))

	// Add tracing options.
	if config.Tracing != nil {
		isH2 := false
		switch tracer := config.Tracing.Tracer.(type) {
		case *meshAPI.Tracing_Zipkin_:
			opts = append(opts, option.ZipkinAddress(tracer.Zipkin.Address))
		case *meshAPI.Tracing_Lightstep_:
			isH2 = true
			// Write the token file.
			lightstepAccessTokenPath := lightstepAccessTokenFile(config.ConfigPath)
			//nolint: staticcheck  // Lightstep deprecated
			err := os.WriteFile(lightstepAccessTokenPath, []byte(tracer.Lightstep.AccessToken), 0o666)
			if err != nil {
				return nil, err
			}
			opts = append(opts, option.LightstepAddress(tracer.Lightstep.Address),
				option.LightstepToken(lightstepAccessTokenPath))
		case *meshAPI.Tracing_Datadog_:
			opts = append(opts, option.DataDogAddress(tracer.Datadog.Address))
		case *meshAPI.Tracing_Stackdriver_:
			projectID, projFound := metadata.PlatformMetadata[platform.GCPProject]
			if !projFound {
				return nil, errors.New("unable to process Stackdriver tracer: missing GCP Project")
			}

			opts = append(opts, option.StackDriverEnabled(true),
				option.StackDriverProjectID(projectID),
				option.StackDriverDebug(tracer.Stackdriver.Debug),
				option.StackDriverMaxAnnotations(getInt64ValueOrDefault(tracer.Stackdriver.MaxNumberOfAnnotations, 200)),
				option.StackDriverMaxAttributes(getInt64ValueOrDefault(tracer.Stackdriver.MaxNumberOfAttributes, 200)),
				option.StackDriverMaxEvents(getInt64ValueOrDefault(tracer.Stackdriver.MaxNumberOfMessageEvents, 200)))
		}

		opts = append(opts, option.TracingTLS(config.Tracing.TlsSettings, metadata, isH2))
	}

	// Add options for Envoy metrics.
	if config.EnvoyMetricsService != nil && config.EnvoyMetricsService.Address != "" {
		opts = append(opts, option.EnvoyMetricsServiceAddress(config.EnvoyMetricsService.Address),
			option.EnvoyMetricsServiceTLS(config.EnvoyMetricsService.TlsSettings, metadata),
			option.EnvoyMetricsServiceTCPKeepalive(config.EnvoyMetricsService.TcpKeepalive))
	} else if config.EnvoyMetricsServiceAddress != "" { // nolint: staticcheck
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

func getInt64ValueOrDefault(src *wrapperspb.Int64Value, defaultVal int64) int64 {
	val := defaultVal
	if src != nil {
		val = src.Value
	}
	return val
}

type setMetaFunc func(m map[string]any, key string, val string)

func extractMetadata(envs []string, prefix string, set setMetaFunc, meta map[string]any) {
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

func extractAttributesMetadata(envVars []string, plat platform.Environment, meta *model.BootstrapNodeMetadata) {
	for _, varStr := range envVars {
		name, val := parseEnvVar(varStr)
		switch name {
		case "ISTIO_METAJSON_LABELS":
			m := jsonStringToMap(val)
			if len(m) > 0 {
				meta.Labels = m
				meta.StaticLabels = m
			}
		case "POD_NAME":
			meta.InstanceName = val
		case "POD_NAMESPACE":
			meta.Namespace = val
		case "SERVICE_ACCOUNT":
			meta.ServiceAccount = val
		}
	}
	if plat != nil && len(plat.Metadata()) > 0 {
		meta.PlatformMetadata = plat.Metadata()
	}
}

// MetadataOptions for constructing node metadata.
type MetadataOptions struct {
	Envs                        []string
	Platform                    platform.Environment
	InstanceIPs                 []string
	StsPort                     int
	ID                          string
	ProxyConfig                 *meshAPI.ProxyConfig
	PilotSubjectAltName         []string
	CredentialSocketExists      bool
	CustomCredentialsFileExists bool
	XDSRootCert                 string
	OutlierLogPath              string
	annotationFilePath          string
	EnvoyStatusPort             int
	EnvoyPrometheusPort         int
	ExitOnZeroActiveConnections bool
	MetadataDiscovery           *bool
	EnvoySkipDeprecatedLogs     bool
	WorkloadIdentitySocketFile  string
}

const (
	// DefaultDeploymentUniqueLabelKey is the default key of the selector that is added
	// to existing ReplicaSets (and label key that is added to its pods) to prevent the existing ReplicaSets
	// to select new pods (and old pods being select by new ReplicaSet).
	DefaultDeploymentUniqueLabelKey string = "pod-template-hash"
)

// GetNodeMetaData function uses an environment variable contract
// ISTIO_METAJSON_* env variables contain json_string in the value.
// The name of variable is ignored.
// ISTIO_META_* env variables are passed through
func GetNodeMetaData(options MetadataOptions) (*model.Node, error) {
	meta := &model.BootstrapNodeMetadata{}
	untypedMeta := map[string]any{}

	for k, v := range options.ProxyConfig.GetProxyMetadata() {
		if strings.HasPrefix(k, IstioMetaPrefix) {
			untypedMeta[strings.TrimPrefix(k, IstioMetaPrefix)] = v
		}
	}

	extractMetadata(options.Envs, IstioMetaPrefix, func(m map[string]any, key string, val string) {
		m[key] = val
	}, untypedMeta)

	extractMetadata(options.Envs, IstioMetaJSONPrefix, func(m map[string]any, key string, val string) {
		err := json.Unmarshal([]byte(val), &m)
		if err != nil {
			log.Warnf("Env variable %s [%s] failed json unmarshal: %v", key, val, err)
		}
	}, untypedMeta)

	j, err := json.Marshal(untypedMeta)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(j, meta); err != nil {
		return nil, err
	}

	meta = SetIstioVersion(meta)

	// Support multiple network interfaces, removing duplicates.
	meta.InstanceIPs = removeDuplicates(options.InstanceIPs)

	// Add STS port into node metadata if it is not 0. This is read by envoy telemetry filters
	if options.StsPort != 0 {
		meta.StsPort = strconv.Itoa(options.StsPort)
	}
	meta.EnvoyStatusPort = options.EnvoyStatusPort
	meta.EnvoyPrometheusPort = options.EnvoyPrometheusPort
	meta.ExitOnZeroActiveConnections = model.StringBool(options.ExitOnZeroActiveConnections)
	if options.MetadataDiscovery == nil {
		meta.MetadataDiscovery = nil
	} else {
		meta.MetadataDiscovery = ptr.Of(model.StringBool(*options.MetadataDiscovery))
	}
	meta.EnvoySkipDeprecatedLogs = model.StringBool(options.EnvoySkipDeprecatedLogs)

	meta.WorkloadIdentitySocketFile = options.WorkloadIdentitySocketFile

	meta.ProxyConfig = (*model.NodeMetaProxyConfig)(options.ProxyConfig)

	extractAttributesMetadata(options.Envs, options.Platform, meta)
	// Add all instance labels with lower precedence than pod labels
	extractInstanceLabels(options.Platform, meta)

	// Add all pod labels found from filesystem
	// These are typically volume mounted by the downward API
	lbls, err := readPodLabels()
	if err == nil {
		meta.Labels = map[string]string{}
		for k, v := range meta.StaticLabels {
			meta.Labels[k] = v
		}
		for k, v := range lbls {
			// ignore `pod-template-hash` label
			if k == DefaultDeploymentUniqueLabelKey {
				continue
			}
			meta.Labels[k] = v
		}
	} else {
		if os.IsNotExist(err) {
			log.Debugf("failed to read pod labels: %v", err)
		} else {
			log.Warnf("failed to read pod labels: %v", err)
		}
	}

	// Add all pod annotations found from filesystem
	// These are typically volume mounted by the downward API
	annos, err := ReadPodAnnotations(options.annotationFilePath)
	if err == nil {
		if meta.Annotations == nil {
			meta.Annotations = map[string]string{}
		}
		for k, v := range annos {
			meta.Annotations[k] = v
		}
	} else {
		if os.IsNotExist(err) {
			log.Debugf("failed to read pod annotations: %v", err)
		} else {
			log.Warnf("failed to read pod annotations: %v", err)
		}
	}

	var l *core.Locality
	if meta.Labels[model.LocalityLabel] == "" && options.Platform != nil {
		// The locality string was not set, try to get locality from platform
		l = options.Platform.Locality()
	} else {
		// replace "." with "/"
		localityString := model.GetLocalityLabel(meta.Labels[model.LocalityLabel])
		if localityString != "" {
			// override the label with the sanitized value
			meta.Labels[model.LocalityLabel] = localityString
		}
		l = model.ConvertLocality(localityString)
	}

	meta.PilotSubjectAltName = options.PilotSubjectAltName
	meta.XDSRootCert = options.XDSRootCert
	meta.OutlierLogPath = options.OutlierLogPath
	if options.CredentialSocketExists {
		untypedMeta[security.CredentialMetaDataName] = "true"
	}
	if options.CustomCredentialsFileExists {
		untypedMeta[security.CredentialFileMetaDataName] = "true"
	}

	if meta.MetadataDiscovery == nil {
		// If it's disabled, set it if ambient is enabled
		meta.MetadataDiscovery = ptr.Of(meta.EnableHBONE)
		log.Debugf("metadata discovery is disabled, setting it to %s based on if ambient HBONE is enabled", meta.EnableHBONE)
	}

	return &model.Node{
		ID:          options.ID,
		Metadata:    meta,
		RawMetadata: untypedMeta,
		Locality:    l,
	}, nil
}

func SetIstioVersion(meta *model.BootstrapNodeMetadata) *model.BootstrapNodeMetadata {
	if meta.IstioVersion == "" {
		meta.IstioVersion = version.Info.Version
	}
	return meta
}

// Extracts instance labels for the platform into model.NodeMetadata.Labels
// only if not running on Kubernetes
func extractInstanceLabels(plat platform.Environment, meta *model.BootstrapNodeMetadata) {
	if plat == nil || meta == nil || plat.IsKubernetes() {
		return
	}
	instanceLabels := plat.Labels()
	if meta.StaticLabels == nil {
		meta.StaticLabels = map[string]string{}
	}
	for k, v := range instanceLabels {
		meta.StaticLabels[k] = v
	}
}

func readPodLabels() (map[string]string, error) {
	b, err := os.ReadFile(constants.PodInfoLabelsPath)
	if err != nil {
		return nil, err
	}
	return ParseDownwardAPI(string(b))
}

func ReadPodAnnotations(path string) (map[string]string, error) {
	if path == "" {
		path = constants.PodInfoAnnotationsPath
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseDownwardAPI(string(b))
}

// ParseDownwardAPI parses fields which are stored as format `%s=%q` back to a map
func ParseDownwardAPI(i string) (map[string]string, error) {
	res := map[string]string{}
	for _, line := range strings.Split(i, "\n") {
		sl := strings.SplitN(line, "=", 2)
		if len(sl) != 2 {
			continue
		}
		key := sl[0]
		// Strip the leading/trailing quotes
		val, err := strconv.Unquote(sl[1])
		if err != nil {
			return nil, fmt.Errorf("failed to unquote %v: %v", sl[1], err)
		}
		res[key] = val
	}
	return res, nil
}

func removeDuplicates(values []string) []string {
	set := sets.New[string]()
	newValues := make([]string, 0, len(values))
	for _, v := range values {
		if !set.InsertContains(v) {
			newValues = append(newValues, v)
		}
	}
	return newValues
}
