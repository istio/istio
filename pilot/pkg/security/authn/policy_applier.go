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

package authn

import (
	http_conn "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
)

type TLSModeGetter interface {
	Get(PolicyApplier) model.MutualTLSMode
}

type TLSPortNumber struct {
	port uint32
}

func (p TLSPortNumber) Get(a PolicyApplier) model.MutualTLSMode {
	return a.GetMutualTLSMode(p.port)
}
func NewTlsPortNumber(p uint32) TLSPortNumber {
	return TLSPortNumber{p}
}

type StrictTLSModeValue struct {
}

func (v StrictTLSModeValue) Get(PolicyApplier) model.MutualTLSMode {
	return model.MTLSStrict
}

// PolicyApplier is the interface provides essential functionalities to help config Envoy (xDS) to enforce
// authentication policy. Each version of authentication policy will implement this interface.
type PolicyApplier interface {
	// InboundFilterChain returns inbound filter chain(s) for the given endpoint (aka workload) port to
	// enforce the underlying authentication policy.
	InboundFilterChain(tlsModeGetter TLSModeGetter, sdsUdsPath string, node *model.Proxy,
		listenerProtocol networking.ListenerProtocol) []networking.FilterChain

	// AuthNFilter returns the JWT HTTP filter to enforce the underlying authentication policy.
	// It may return nil, if no JWT validation is needed.
	JwtFilter() *http_conn.HttpFilter

	// AuthNFilter returns the (authn) HTTP filter to enforce the underlying authentication policy.
	// It may return nil, if no authentication is needed.
	AuthNFilter(proxyType model.NodeType, tlsModeGetter TLSModeGetter) *http_conn.HttpFilter

	// Return the MutualTLSMode given the endpoint port.
	GetMutualTLSMode(endpointPort uint32) model.MutualTLSMode
}
