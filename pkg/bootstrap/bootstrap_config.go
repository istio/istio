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
	"os"
	"os/exec"
	"path"
	"strings"
	"text/template"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/duration"

	meshconfig "istio.io/api/mesh/v1alpha1"
)

// Generate the envoy v2 bootstrap configuration, using template.
const (
	// EpochFileTemplate is a template for the root config JSON
	EpochFileTemplate = "envoy-rev%d.json"
	DefaultCfgDir     = "/var/lib/istio/envoy/envoy_bootstrap_tmpl.json"

	// MaxClusterNameLength is the maximum cluster name length
	MaxClusterNameLength = 189 // TODO: use MeshConfig.StatNameLength instead
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

func args(config *meshconfig.ProxyConfig, node, fname string, epoch int) []string {
	startupArgs := []string{"-c", fname,
		"--restart-epoch", fmt.Sprint(epoch),
		"--drain-time-s", fmt.Sprint(int(convertDuration(config.DrainDuration) / time.Second)),
		"--parent-shutdown-time-s", fmt.Sprint(int(convertDuration(config.ParentShutdownDuration) / time.Second)),
		"--service-cluster", config.ServiceCluster,
		"--service-node", node,
		"--max-obj-name-len", fmt.Sprint(MaxClusterNameLength), // TODO: use MeshConfig.StatNameLength instead
	}

	//startupArgs = append(startupArgs, proxy.extraArgs...)

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
	outWriter io.Writer, errWriter io.Writer) (*os.Process, error) {

	// spin up a new Envoy process
	args := args(config, node, configFname, epoch)
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

// WriteBootstrap generates an envoy config based on config and epoch, and returns the filename.
// TODO: in v2 some of the LDS ports (port, http_port) should be configured in the bootstrap.
func WriteBootstrap(config *meshconfig.ProxyConfig, epoch int, pilotSAN []string, opts map[string]interface{}) (string, error) {
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

	addPort := strings.Split(config.DiscoveryAddress, ":")
	pilotHost := addPort[0]
	pilotPort := addPort[1]
	opts["pilot_address"] = fmt.Sprintf("{\"address\": \"%s\", \"port_value\": %s}", pilotHost, pilotPort)

	grpcAddress := opts["pilot_grpc"]
	// Default values for the grpc address.
	grpcPort := "15010"
	grpcHost := pilotHost
	// Default value
	if grpcAddress != nil {
		addPort = strings.Split(grpcAddress.(string), ":")
		grpcHost = addPort[0]
		grpcPort = addPort[1]
	}
	opts["pilot_grpc_address"] = fmt.Sprintf("{\"address\": \"%s\", \"port_value\": %s}", grpcHost, grpcPort)

	// Failsafe for EDSv2. In case of bugs of problems, the injection template can be modified to
	// add this env variable. This is short lived, EDSv1 will be deprecated/removed.
	if os.Getenv("USE_EDS_V1") == "1" {
		opts["edsv1"] = "1"
	}

	if config.ZipkinAddress != "" {
		addPort = strings.Split(config.ZipkinAddress, ":")
		opts["zipkin"] = fmt.Sprintf("{\"address\": \"%s\", \"port_value\": %s}", addPort[0], addPort[1])
	}
	if config.StatsdUdpAddress != "" {
		addPort = strings.Split(config.StatsdUdpAddress, ":")
		opts["statsd"] = fmt.Sprintf("{\"address\": \"%s\", \"port_value\": %s}", addPort[0], addPort[1])
	}
	fout, err := os.Create(fname)
	if err != nil {
		return "", err
	}

	// Execute needs some sort of io.Writer
	err = t.Execute(fout, opts)
	return fname, err
}
