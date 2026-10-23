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

package kube

import (
	"net/http"
	"time"

	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"

	"istio.io/istio/pkg/env"
)

// Must stay above the API server's own --request-timeout (60s by default).
var kubeHeaderTimeout = env.Register("ISTIO_KUBE_HEADER_TIMEOUT", 90*time.Second,
	"How long a Kubernetes API request may wait for its response headers. "+
		"Must exceed the API server's --request-timeout (60s by default). 0 disables the timeout.").Get()

// setResponseHeaderTimeout sets ResponseHeaderTimeout on the transport client-go builds for config.
// Protocol upgrades (SPDY/websocket) use their own transports and are not affected.
func setResponseHeaderTimeout(config *rest.Config, timeout time.Duration) {
	if timeout <= 0 || config.Transport != nil {
		return
	}
	// An explicit proxy function disables client-go's shared transport cache, so
	// setting the timeout below cannot race with another client's requests.
	// The CIDR-aware proxier keeps NO_PROXY CIDR support, like client-go's default.
	if config.Proxy == nil {
		config.Proxy = utilnet.NewProxierWithNoProxyCIDR(http.ProxyFromEnvironment)
	}
	config.WrapTransport = transport.Wrappers(func(rt http.RoundTripper) http.RoundTripper {
		// Walk through client-go's CA rotation and lifecycle wrappers. CA rotation clones
		// this transport, which preserves the timeout.
		for base := rt; base != nil; {
			switch t := base.(type) {
			case *http.Transport:
				t.ResponseHeaderTimeout = timeout
				return rt
			case utilnet.RoundTripperWrapper:
				base = t.WrappedRoundTripper()
			default:
				return rt
			}
		}
		return rt
	}, config.WrapTransport)
}
