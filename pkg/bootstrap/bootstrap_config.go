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

	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/duration"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"encoding/base64"
	"istio.io/istio/pkg/log"
)

// Generate the envoy v2 bootstrap configuration, using template.
const (
	// EpochFileTemplate is a template for the root config JSON
	EpochFileTemplate = "envoy-rev%d.json"
	DefaultCfgDir     = "/var/lib/istio/envoy/envoy_bootstrap_tmpl.json"

	// MaxClusterNameLength is the maximum cluster name length
	MaxClusterNameLength = 189 // TODO: use MeshConfig.StatNameLength instead

	// IstioMetaPrefix is used to pass env vars as node metadata.
	IstioMetaPrefix = "ISTIO_META_"

	// IstioMetaB64Prefix is used to pass annotations and other info that cannot be directly passed in as env vars.
	// this data is base64 encoded.
	IstioMetaB64Prefix =  "ISTIO_METAB64_"
)

var (
	defaultPilotSan = []string{
		"spiffe://cluster.local/ns/istio-system/sa/istio-pilot-service-account"}
)

func configFile(config string, epoch int) string {
	return path.Join(config, fmt.Sprintf(EpochFileTemplate, epoch))
}

// convertDuration converts to golang duration and logs errors
func convertDuration(d *duration.Duration) time.Duration {
	if d == nil {
		return 0
	}
	dur, _ := ptypes.Duration(d)
	return dur
}

func args(config *meshconfig.ProxyConfig, node, fname string, epoch int, cliarg []string) []string {
	startupArgs := []string{"-c", fname,
		"--restart-epoch", fmt.Sprint(epoch),
		"--drain-time-s", fmt.Sprint(int(convertDuration(config.DrainDuration) / time.Second)),
		"--parent-shutdown-time-s", fmt.Sprint(int(convertDuration(config.ParentShutdownDuration) / time.Second)),
		"--service-cluster", config.ServiceCluster,
		"--service-node", node,
		"--max-obj-name-len", fmt.Sprint(MaxClusterNameLength), // TODO: use MeshConfig.StatNameLength instead
		"--allow-unknown-fields",
	}

	for _, v := range cliarg {
		startupArgs = append(startupArgs, v)
	}

	if config.Concurrency > 0 {
		startupArgs = append(startupArgs, "--concurrency", fmt.Sprint(config.Concurrency))
	}

	if len(config.AvailabilityZone) > 0 {
		startupArgs = append(startupArgs, []string{"--service-zone", config.AvailabilityZone}...)
	}

	return startupArgs
}

// RunProxy will run sidecar with a v2 config generated from template according with the config.
// The doneChan will be notified if the envoy process dies.
// The returned process can be killed by the caller to terminate the proxy.
func RunProxy(config *meshconfig.ProxyConfig, node string, epoch int, configFname string, doneChan chan error,
	outWriter io.Writer, errWriter io.Writer, cliarg []string) (*os.Process, error) {

	// spin up a new Envoy process
	args := args(config, node, configFname, epoch, cliarg)
	args = append(args, "--v2-config-only")

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


type decodeFunc func(string) (string, error)
type filterFunc func(string) bool


func extractMetadata(envs []string, prefix string, decode decodeFunc, filter filterFunc, meta map[string]string) {
	metaPrefixLen := len(prefix)
	for _, env := range envs {
		if strings.HasPrefix(env, prefix) {
			v := env[metaPrefixLen:]
			parts := strings.SplitN(v, "=", 2)
			if len(parts) != 2 {
				continue
			}
			metaKey, metaVal := parts[0], parts[1]

			if decode != nil {
				var err error
				if metaKey, err = decode(metaKey); err != nil {
					log.Warnf("Unable to process %s: %v", env, err)
					continue
				}
				if metaVal, err = decode(metaVal); err != nil {
					log.Warnf("Unable to process %s: %v", env, err)
					continue
				}
			}

			if filter!=nil && !filter(metaKey) {
				continue
			}

			meta[metaKey] = metaVal
		}
	}
}

// This function uses an environment variable contract
// ISTIO_METAB64_* env variables are base64decoded and only used if key contains istio.io
// ISTIO_META_* env variables are passed thru
func getNodeMetaData(envs []string) map[string]string {
	meta := map[string]string{}

	extractMetadata(envs, IstioMetaPrefix, nil, nil, meta)

	extractMetadata(envs, IstioMetaB64Prefix, func(s string) (string, error) {
		ba, err := base64.StdEncoding.DecodeString(s)
		return string(ba), err
	}, func(s string) bool {
		return strings.Contains(s, "istio.io")
	},meta)

	return meta
}

// WriteBootstrap generates an envoy config based on config and epoch, and returns the filename.
// TODO: in v2 some of the LDS ports (port, http_port) should be configured in the bootstrap.
func WriteBootstrap(config *meshconfig.ProxyConfig, node string, epoch int, pilotSAN []string, opts map[string]interface{}) (string, error) {
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

	override := os.Getenv("ISTIO_BOOTSTRAP")
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
		pilotSAN = defaultPilotSan
	}
	opts["pilot_SAN"] = pilotSAN

	// Simplify the template
	opts["refresh_delay"] = fmt.Sprintf("{\"seconds\": %d, \"nanos\": %d}", config.DiscoveryRefreshDelay.Seconds, config.DiscoveryRefreshDelay.Nanos)
	opts["connect_timeout"] = fmt.Sprintf("{\"seconds\": %d, \"nanos\": %d}", config.ConnectTimeout.Seconds, config.ConnectTimeout.Nanos)

	opts["cluster"] = config.ServiceCluster
	opts["nodeID"] = node

	// Support passing extra info from node environment as metadata
	opts["meta"] = getNodeMetaData(os.Environ())

	// TODO: allow reading a file with additional metadata (for example if created with
	// 'envref'. This will allow Istio to generate the right config even if the pod info
	// is not available (in particular in some multi-cluster cases)

	if len(config.AvailabilityZone) > 0 {
		opts["az"] = config.AvailabilityZone
	}

	h, p, err := GetHostPort("Discovery", config.DiscoveryAddress)
	if err != nil {
		return "", err
	}
	StoreHostPort(h, p, "pilot_address", opts)

	// Default values for the grpc address.
	// TODO: take over the DiscoveryAddress or add a separate mesh config option
	// Default value
	grpcPort := "15010"
	if config.ControlPlaneAuthPolicy == meshconfig.AuthenticationPolicy_MUTUAL_TLS {
		grpcPort = "15011"
	}
	grpcHost := h // Use pilot host

	grpcAddress := opts["pilot_grpc"]
	if grpcAddress != nil {
		grpcHost, grpcPort, err = GetHostPort("gRPC", grpcAddress.(string))
		if err != nil {
			return "", err
		}
	}
	StoreHostPort(grpcHost, grpcPort, "pilot_grpc_address", opts)

	if config.ZipkinAddress != "" {
		h, p, err = GetHostPort("Zipkin", config.ZipkinAddress)
		if err != nil {
			return "", err
		}
		StoreHostPort(h, p, "zipkin", opts)
	}

	if config.StatsdUdpAddress != "" {
		h, p, err = GetHostPort("statsd UDP", config.StatsdUdpAddress)
		if err != nil {
			return "", err
		}
		StoreHostPort(h, p, "statsd", opts)
	}

	fout, err := os.Create(fname)
	if err != nil {
		return "", err
	}

	// Execute needs some sort of io.Writer
	err = t.Execute(fout, opts)
	return fname, err
}
