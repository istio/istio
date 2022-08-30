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

package status

import (
	"fmt"
	"net"

	"istio.io/pkg/env"
)

const (
	// KubeAppProberEnvName is the name of the command line flag for pilot agent to pass app prober config.
	// The json encoded string to pass app HTTP probe information from injector(istioctl or webhook).
	// For example, ISTIO_KUBE_APP_PROBERS='{"/app-health/httpbin/livez":{"httpGet":{"path": "/hello", "port": 8080}}.
	// indicates that httpbin container liveness prober port is 8080 and probing path is /hello.
	// This environment variable should never be set manually.
	KubeAppProberEnvName = "ISTIO_KUBE_APP_PROBERS"
)

var (
	PrometheusScrapingConfig = env.Register("ISTIO_PROMETHEUS_ANNOTATIONS", "", "")

	EnableHTTP2Probing = env.Register("ISTIO_ENABLE_HTTP2_PROBING", true,
		"If enabled, HTTP2 probes will be enabled for HTTPS probes, following Kubernetes").Get()

	LegacyLocalhostProbeDestination = env.Register("REWRITE_PROBE_LEGACY_LOCALHOST_DESTINATION", false,
		"If enabled, readiness probes will be sent to 'localhost'. Otherwise, they will be sent to the Pod's IP, matching Kubernetes' behavior.")

	ProbeKeepaliveConnections = env.Register("ENABLE_PROBE_KEEPALIVE_CONNECTIONS", false,
		"If enabled, readiness probes will keep the connection from pilot-agent to the application alive. "+
			"This mirrors older Istio versions' behaviors, but not kubelet's.").Get()
)

var (
	UpstreamLocalAddressIPv4 = &net.TCPAddr{IP: net.ParseIP("127.0.0.6")}
	UpstreamLocalAddressIPv6 = &net.TCPAddr{IP: net.ParseIP("::6")}
)

type PrometheusScrapeConfiguration struct {
	Scrape string `json:"scrape"`
	Path   string `json:"path"`
	Port   string `json:"port"`
}

// FormatProberURL returns a set of HTTP URLs that pilot agent will serve to take over Kubernetes
// app probers.
func FormatProberURL(container string) (string, string, string) {
	return fmt.Sprintf("/app-health/%v/readyz", container),
		fmt.Sprintf("/app-health/%v/livez", container),
		fmt.Sprintf("/app-health/%v/startupz", container)
}
