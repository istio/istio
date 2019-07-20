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

package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path"
	"strings"
	"text/template"
	"time"

	"github.com/gogo/protobuf/types"
	"github.com/pkg/errors"

	"golang.org/x/oauth2/google"

	"istio.io/api/annotation"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/bootstrap/platform"
	"istio.io/istio/pkg/spiffe"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

// Generate the envoy v2 bootstrap configuration, using template.
const (
	// EpochFileTemplate is a template for the root config JSON
	EpochFileTemplate = "envoy-rev%d.json"
	DefaultCfgDir     = "/var/lib/istio/envoy/envoy_bootstrap_tmpl.json"

	// IstioMetaPrefix is used to pass env vars as node metadata.
	IstioMetaPrefix = "ISTIO_META_"

	// IstioMetaJSONPrefix is used to pass annotations and similar environment info.
	IstioMetaJSONPrefix = "ISTIO_METAJSON_"

	lightstepAccessTokenBase = "lightstep_access_token.txt"

	// Options are used in the boostrap template.
	envoyStatsMatcherInclusionPrefixOption = "inclusionPrefix"
	envoyStatsMatcherInclusionSuffixOption = "inclusionSuffix"
	envoyStatsMatcherInclusionRegexpOption = "inclusionRegexps"
)

var (
	// required stats are used by readiness checks.
	requiredEnvoyStatsMatcherInclusionPrefixes = "cluster_manager,listener_manager,http_mixer_filter,tcp_mixer_filter,server,cluster.xds-grpc"
	requiredEnvoyStatsMatcherInclusionSuffix   = "ssl_context_update_by_sds"
)

// substituteValues substitutes variables known to the boostrap like pod_ip.
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

// setStatsOptions configures stats inclusion list based on annotations.
func setStatsOptions(opts map[string]interface{}, meta map[string]interface{}, nodeIPs []string) {

	setStatsOption := func(metaKey string, optKey string, required string) {
		var inclusionOption []string
		if inclusionPatterns, ok := meta[metaKey]; ok {
			inclusionOption = strings.Split(inclusionPatterns.(string), ",")
		}

		if len(required) > 0 {
			inclusionOption = append(inclusionOption,
				strings.Split(required, ",")...)
		}

		// At the sidecar we can limit downstream metrics collection to the inbound listener.
		// Inbound downstream metrics are named as: http.{pod_ip}_{port}.downstream_rq_*
		// Other outbound downstream metrics are numerous and not very interesting for a sidecar.
		// specifying http.{pod_ip}_  as a prefix will capture these downstream metrics.
		inclusionOption = substituteValues(inclusionOption, "{pod_ip}", nodeIPs)

		if len(inclusionOption) > 0 {
			opts[optKey] = inclusionOption
		}
	}
	setStatsOption(annotation.SidecarStatsInclusionPrefixes.Name, envoyStatsMatcherInclusionPrefixOption, requiredEnvoyStatsMatcherInclusionPrefixes)

	setStatsOption(annotation.SidecarStatsInclusionSuffixes.Name, envoyStatsMatcherInclusionSuffixOption, requiredEnvoyStatsMatcherInclusionSuffix)

	setStatsOption(annotation.SidecarStatsInclusionRegexps.Name, envoyStatsMatcherInclusionRegexpOption, "")
}

func defaultPilotSan() []string {
	return []string{
		spiffe.MustGenSpiffeURI("istio-system", "istio-pilot-service-account")}
}

func configFile(config string, epoch int) string {
	return path.Join(config, fmt.Sprintf(EpochFileTemplate, epoch))
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

func createArgs(config *meshconfig.ProxyConfig, node, fname string, epoch int, cliarg []string) []string {
	startupArgs := []string{"-c", fname,
		"--restart-epoch", fmt.Sprint(epoch),
		"--drain-time-s", fmt.Sprint(int(convertDuration(config.DrainDuration) / time.Second)),
		"--parent-shutdown-time-s", fmt.Sprint(int(convertDuration(config.ParentShutdownDuration) / time.Second)),
		"--service-cluster", config.ServiceCluster,
		"--service-node", node,
		"--max-obj-name-len", fmt.Sprint(config.StatNameLength),
		"--allow-unknown-fields",
	}

	startupArgs = append(startupArgs, cliarg...)

	if config.Concurrency > 0 {
		startupArgs = append(startupArgs, "--concurrency", fmt.Sprint(config.Concurrency))
	}

	return startupArgs
}

// RunProxy will run sidecar with a v2 config generated from template according with the config.
// The doneChan will be notified if the envoy process dies.
// The returned process can be killed by the caller to terminate the proxy.
func RunProxy(config *meshconfig.ProxyConfig, node string, epoch int, configFname string, doneChan chan error,
	outWriter io.Writer, errWriter io.Writer, cliarg []string) (*os.Process, error) {

	// spin up a new Envoy process
	args := createArgs(config, node, configFname, epoch, cliarg)

	/* #nosec */
	cmd := exec.Command(config.BinaryPath, args...)
	cmd.Stdout = outWriter
	cmd.Stderr = errWriter
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	// Set if the caller is monitoring envoy, for example in tests or if envoy runs in same
	// container with the app.
	// Caller passed a channel, will wait itself for termination
	if doneChan != nil {
		go func() {
			doneChan <- cmd.Wait()
		}()
	}

	return cmd.Process, nil
}

// GetHostPort separates out the host and port portions of an address. The
// host portion may be a name, IPv4 address, or IPv6 address (with square brackets).
func GetHostPort(name, addr string) (host string, port string, err error) {
	host, port, err = net.SplitHostPort(addr)
	if err != nil {
		return "", "", fmt.Errorf("unable to parse %s address %q: %v", name, addr, err)
	}
	return host, port, nil
}

// StoreHostPort encodes the host and port as key/value pair strings in
// the provided map.
func StoreHostPort(host, port, field string, opts map[string]interface{}) {
	opts[field] = fmt.Sprintf("{\"address\": \"%s\", \"port_value\": %s}", host, port)
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

type istioMetadata struct {
	CanonicalTelemetryService string            `json:"canonical_telemetry_service,omitempty"`
	IP                        string            `json:"ip,omitempty"`
	Labels                    map[string]string `json:"labels,omitempty"`
	Name                      string            `json:"name,omitempty"`
	Namespace                 string            `json:"namespace,omitempty"`
	ServiceAccount            string            `json:"service_account,omitempty"`
	PlatformMetadata          map[string]string `json:"platform_metadata,omitempty"`
}

func shouldExtract(envVar, prefix string) bool {
	// this will allow transition from current method of exposition in the future
	// Example:
	// if strings.HasPrefix(envVar, "ISTIO_METAJSON_LABELS") {
	// 	return false
	// }
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

func extractIstioMetadata(envVars []string, plat platform.Environment) istioMetadata {
	im := istioMetadata{}
	for _, varStr := range envVars {
		name, val := parseEnvVar(varStr)
		switch name {
		case "INSTANCE_IP":
			im.IP = val
		case "ISTIO_METAJSON_LABELS":
			m := jsonStringToMap(val)
			im.Labels = m
			im.CanonicalTelemetryService = m["istioTelemetryService"]
		case "POD_NAME":
			im.Name = val
		case "POD_NAMESPACE":
			im.Namespace = val
		}
	}
	if plat != nil {
		im.PlatformMetadata = plat.Metadata()
	}
	return im
}

// getNodeMetaData function uses an environment variable contract
// ISTIO_METAJSON_* env variables contain json_string in the value.
// 					The name of variable is ignored.
// ISTIO_META_* env variables are passed thru
func getNodeMetaData(envs []string, plat platform.Environment) map[string]interface{} {
	meta := map[string]interface{}{}

	extractMetadata(envs, IstioMetaPrefix, func(m map[string]interface{}, key string, val string) {
		m[key] = val
	}, meta)

	extractMetadata(envs, IstioMetaJSONPrefix, func(m map[string]interface{}, key string, val string) {
		err := json.Unmarshal([]byte(val), &m)
		if err != nil {
			log.Warnf("Env variable %s [%s] failed json unmarshal: %v", key, val, err)
		}
	}, meta)
	meta["istio"] = "sidecar"

	meta["istio.io/metadata"] = extractIstioMetadata(envs, plat)

	return meta
}

var overrideVar = env.RegisterStringVar("ISTIO_BOOTSTRAP", "", "")

// WriteBootstrap generates an envoy config based on config and epoch, and returns the filename.
// TODO: in v2 some of the LDS ports (port, http_port) should be configured in the bootstrap.
func WriteBootstrap(config *meshconfig.ProxyConfig, node string, epoch int, pilotSAN []string,
	opts map[string]interface{}, localEnv []string, nodeIPs []string, dnsRefreshRate string) (string, error) {
	// currently, only the GCP Platform is supported, so this is hardcorded and the writeBootstrapForPlatform method is private.
	return writeBootstrapForPlatform(config, node, epoch, pilotSAN, opts, localEnv, nodeIPs, dnsRefreshRate, platform.NewGCP())
}

func writeBootstrapForPlatform(config *meshconfig.ProxyConfig, node string, epoch int, pilotSAN []string,
	opts map[string]interface{}, localEnv []string, nodeIPs []string, dnsRefreshRate string, platEnv platform.Environment) (string, error) {

	if opts == nil {
		opts = map[string]interface{}{}
	}
	if err := os.MkdirAll(config.ConfigPath, 0700); err != nil {
		return "", err
	}
	// attempt to write file
	fname := configFile(config.ConfigPath, epoch)

	cfg := config.CustomConfigFile
	if cfg == "" {
		cfg = config.ProxyBootstrapTemplatePath
	}
	if cfg == "" {
		cfg = DefaultCfgDir
	}

	override := overrideVar.Get()
	if len(override) > 0 {
		cfg = override
	}

	cfgTmpl, err := ioutil.ReadFile(cfg)
	if err != nil {
		return "", err
	}

	t, err := template.New("bootstrap").Parse(string(cfgTmpl))
	if err != nil {
		return "", err
	}

	opts["config"] = config

	if pilotSAN == nil {
		pilotSAN = defaultPilotSan()
	}
	opts["pilot_SAN"] = pilotSAN

	// Simplify the template
	opts["connect_timeout"] = (&types.Duration{Seconds: config.ConnectTimeout.Seconds, Nanos: config.ConnectTimeout.Nanos}).String()
	opts["cluster"] = config.ServiceCluster
	opts["nodeID"] = node

	// Support passing extra info from node environment as metadata
	meta := getNodeMetaData(localEnv, platEnv)

	localityOverride := ""
	if locality, ok := meta[model.LocalityLabel]; ok {
		localityOverride = model.GetLocalityOrDefault(locality.(string), localityOverride)
	}
	l := util.ConvertLocality(localityOverride)
	if l == nil {
		// Populate the platform locality if available.
		l = platEnv.Locality()
	}
	if l.Region != "" {
		opts["region"] = l.Region
	}
	if l.Zone != "" {
		opts["zone"] = l.Zone
	}
	if l.SubZone != "" {
		opts["sub_zone"] = l.SubZone
	}

	// Remove duplicate nodeIPs, but preserve the original ordering.
	ipSet := make(map[string]struct{})
	newNodeIPs := make([]string, 0, len(nodeIPs))
	for _, ip := range nodeIPs {
		if _, ok := ipSet[ip]; !ok {
			ipSet[ip] = struct{}{}
			newNodeIPs = append(newNodeIPs, ip)
		}
	}
	nodeIPs = newNodeIPs

	setStatsOptions(opts, meta, nodeIPs)

	// Support multiple network interfaces
	meta[model.NodeMetadataInstanceIPs] = strings.Join(nodeIPs, ",")

	ba, err := json.Marshal(meta)
	if err != nil {
		return "", err
	}
	opts["meta_json_str"] = string(ba)

	// TODO: allow reading a file with additional metadata (for example if created with
	// 'envref'. This will allow Istio to generate the right config even if the pod info
	// is not available (in particular in some multi-cluster cases)

	h, p, err := GetHostPort("Discovery", config.DiscoveryAddress)
	if err != nil {
		return "", err
	}
	StoreHostPort(h, p, "pilot_grpc_address", opts)

	// Pass unmodified config.DiscoveryAddress for Google gRPC Envoy client target_uri parameter
	opts["discovery_address"] = config.DiscoveryAddress

	opts["dns_refresh_rate"] = dnsRefreshRate

	// Setting default to ipv4 local host, wildcard and dns policy
	opts["localhost"] = "127.0.0.1"
	opts["wildcard"] = "0.0.0.0"
	opts["dns_lookup_family"] = "V4_ONLY"

	// Check if nodeIP carries IPv4 or IPv6 and set up proxy accordingly
	if isIPv6Proxy(nodeIPs) {
		opts["localhost"] = "::1"
		opts["wildcard"] = "::"
		opts["dns_lookup_family"] = "AUTO"
	}

	if config.Tracing != nil {
		switch tracer := config.Tracing.Tracer.(type) {
		case *meshconfig.Tracing_Zipkin_:
			h, p, err = GetHostPort("Zipkin", tracer.Zipkin.Address)
			if err != nil {
				return "", err
			}
			StoreHostPort(h, p, "zipkin", opts)
		case *meshconfig.Tracing_Lightstep_:
			h, p, err = GetHostPort("Lightstep", tracer.Lightstep.Address)
			if err != nil {
				return "", err
			}
			StoreHostPort(h, p, "lightstep", opts)

			lightstepAccessTokenPath := lightstepAccessTokenFile(config.ConfigPath)
			lsConfigOut, err := os.Create(lightstepAccessTokenPath)
			if err != nil {
				return "", err
			}
			_, err = lsConfigOut.WriteString(tracer.Lightstep.AccessToken)
			if err != nil {
				return "", err
			}
			opts["lightstepToken"] = lightstepAccessTokenPath
			opts["lightstepSecure"] = tracer.Lightstep.Secure
			opts["lightstepCacertPath"] = tracer.Lightstep.CacertPath
		case *meshconfig.Tracing_Datadog_:
			h, p, err = GetHostPort("Datadog", tracer.Datadog.Address)
			if err != nil {
				return "", err
			}
			StoreHostPort(h, p, "datadog", opts)
		case *meshconfig.Tracing_Stackdriver_:
			var cred *google.Credentials
			// in-cluster credentials are fetched by using the GCE metadata server.
			// You may also specify environment variable GOOGLE_APPLICATION_CREDENTIALS to point a GCP credentials file.
			if cred, err = google.FindDefaultCredentials(context.Background()); err != nil {
				return "", errors.Errorf("Unable to process Stackdriver tracer: %v", err)
			}
			opts["stackdriver"] = true
			opts["stackdriverProjectID"] = cred.ProjectID
			opts["stackdriverDebug"] = tracer.Stackdriver.Debug
			setOptsWithDefaults(tracer.Stackdriver.MaxNumberOfAnnotations, "stackdriverMaxAnnotations", opts, 200)
			setOptsWithDefaults(tracer.Stackdriver.MaxNumberOfAttributes, "stackdriverMaxAttributes", opts, 200)
			setOptsWithDefaults(tracer.Stackdriver.MaxNumberOfMessageEvents, "stackdriverMaxEvents", opts, 200)
		}
	}

	if config.StatsdUdpAddress != "" {
		h, p, err = GetHostPort("statsd UDP", config.StatsdUdpAddress)
		if err != nil {
			return "", err
		}
		StoreHostPort(h, p, "statsd", opts)
	}

	if config.EnvoyMetricsServiceAddress != "" {
		h, p, err = GetHostPort("envoy metrics service", config.EnvoyMetricsServiceAddress)
		if err != nil {
			return "", err
		}
		StoreHostPort(h, p, "envoy_metrics_service", opts)
	}

	if config.EnvoyAccessLogServiceAddress != "" {
		h, p, err = GetHostPort("envoy accesslog service", config.EnvoyAccessLogServiceAddress)
		if err != nil {
			return "", err
		}
		StoreHostPort(h, p, "envoy_accesslog_service", opts)
	}

	fout, err := os.Create(fname)
	if err != nil {
		return "", err
	}
	defer fout.Close()

	// Execute needs some sort of io.Writer
	err = t.Execute(fout, opts)
	return fname, err
}

func setOptsWithDefaults(src *types.Int64Value, name string, opts map[string]interface{}, defaultVal int64) {
	val := defaultVal
	if src != nil {
		val = src.Value
	}
	opts[name] = val
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
