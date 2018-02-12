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
	"io/ioutil"
	"os"
	"path"
	"strings"
	"text/template"

	meshconfig "istio.io/api/mesh/v1alpha1"
)

// Generate the envoy v2 bootstrap configuration, using template.
const (
	// EpochFileTemplate is a template for the root config JSON
	EpochFileTemplate = "envoy-rev%d.json"
	DefaultCfgDir     = "/var/lib/istio/envoy/envoy_bootstrap_tmpl.json"
)

func configFile(config string, epoch int) string {
	return path.Join(config, fmt.Sprintf(EpochFileTemplate, epoch))
}

// WriteBootstrap generates an envoy config based on config and epoch, and returns the filename.
// TODO: in v2 some of the LDS ports (port, http_port) should be configured in the bootstrap.
func WriteBootstrap(config *meshconfig.ProxyConfig, epoch int, pilotSAN []string) (string, error) {
	if err := os.MkdirAll(config.ConfigPath, 0700); err != nil {
		return "", err
	}
	// attempt to write file
	fname := configFile(config.ConfigPath, epoch)

	cfg := config.CustomConfigFile
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

	opts := map[string]interface{}{
		"config": config,
	}

	if pilotSAN != nil {
		opts["pilot_SAN"] = pilotSAN
	}

	// Simplify the template
	opts["refresh_delay"] = fmt.Sprintf("{\"seconds\": %d, \"nanos\": %d}", config.DiscoveryRefreshDelay.Seconds, config.DiscoveryRefreshDelay.Nanos)
	opts["connect_timeout"] = fmt.Sprintf("{\"seconds\": %d, \"nanos\": %d}", config.ConnectTimeout.Seconds, config.ConnectTimeout.Nanos)

	addPort := strings.Split(config.DiscoveryAddress, ":")
	opts["pilot_address"] = fmt.Sprintf("{\"address\": \"%s\", \"port_value\": %s}", addPort[0], addPort[1])

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
