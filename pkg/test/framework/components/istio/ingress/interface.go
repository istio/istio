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

package ingress

import (
	"net"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/util/retry"
)

// Instance represents a deployed Ingress Gateway instance.
type Instance interface {
	// HTTPAddress returns the external HTTP (80) address of the ingress gateway ((or the NodePort address,
	//	// when in an environment that doesn't support LoadBalancer).
	HTTPAddress() net.TCPAddr
	// HTTPSAddress returns the external HTTPS (443) address of the ingress gateway (or the NodePort address,
	//	// when in an environment that doesn't support LoadBalancer).
	HTTPSAddress() net.TCPAddr
	// TCPAddress returns the external TCP (31400) address of the ingress gateway (or the NodePort address,
	// when in an environment that doesn't support LoadBalancer).
	TCPAddress() net.TCPAddr
	// DiscoveryAddress returns the external XDS (!5012) address on the ingress gateway (or the NodePort address,
	// when in an evnironment that doesn't support LoadBalancer).
	DiscoveryAddress() net.TCPAddr

	// CallEcho makes a call through ingress using the echo call and response types.
	CallEcho(options echo.CallOptions) (client.ParsedResponses, error)
	CallEchoOrFail(t test.Failer, options echo.CallOptions) client.ParsedResponses
	CallEchoWithRetry(options echo.CallOptions, retryOptions ...retry.Option) (client.ParsedResponses, error)
	CallEchoWithRetryOrFail(t test.Failer, options echo.CallOptions, retryOptions ...retry.Option) client.ParsedResponses

	// ProxyStats returns proxy stats, or error if failure happens.
	ProxyStats() (map[string]int, error)

	// PodID returns the name of the ingress gateway pod of index i. Returns error if failed to get the pod
	// or the index is out of boundary.
	PodID(i int) (string, error)
}
