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

package options

import (
	"os"
	"strings"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	istioagent "istio.io/istio/pkg/istio-agent"
)

// Similar with ISTIO_META_, which is used to customize the node metadata - this customizes extra header.
const xdsHeaderPrefix = "XDS_HEADER_"

func NewAgentOptions(proxy *model.Proxy) *istioagent.AgentOptions {
	o := &istioagent.AgentOptions{
		XDSRootCerts: xdsRootCA,
		CARootCerts:  caRootCA,
		XDSHeaders:   map[string]string{},
		XdsUdsPath:   constants.DefaultXdsUdsPath,
		IsIPv6:       proxy.SupportsIPv6(),
		ProxyType:    proxy.Type,
	}
	extractXDSHeadersFromEnv(o)
	if proxyXDSViaAgent {
		o.ProxyXDSViaAgent = true
		o.DNSCapture = dnsCaptureByAgent
		o.ProxyNamespace = PodNamespaceVar.Get()
		o.ProxyDomain = proxy.DNSDomain
	}

	return o
}

// Simplified extraction of gRPC headers from environment.
// Unlike ISTIO_META, where we need JSON and advanced features - this is just for small string headers.
func extractXDSHeadersFromEnv(o *istioagent.AgentOptions) {
	envs := os.Environ()
	for _, e := range envs {
		if strings.HasPrefix(e, xdsHeaderPrefix) {
			parts := strings.SplitN(e, "=", 2)
			if len(parts) != 2 {
				continue
			}
			o.XDSHeaders[parts[0][len(xdsHeaderPrefix):]] = parts[1]
		}
	}
}
