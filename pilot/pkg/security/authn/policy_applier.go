// Copyright 2019 Istio Authors
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
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
)

// PolicyApplier is the interface provides essential functionalities to help config Envoy (xDS) to enforce
// authentication policy. Each version of authentication policy will implement this interface.
type PolicyApplier interface {
	// InboundFilterChain returns inbound filter chain(s) to enforce the underlying authentication policy.
	InboundFilterChain(sdsUdsPath string, sdsUseTrustworthyJwt, sdsUseNormalJwt bool, meta map[string]string) []plugin.FilterChain

	// AuthNFilter returns the JWT HTTP filter to enforce the underlying authentication policy.
	// It may return nil, if no JWT validation is needed.
	JwtFilter(isXDSMarshalingToAnyEnabled bool) *http_conn.HttpFilter

	// AuthNFilter returns the (authn) HTTP filter to enforce the underlying authentication policy.
	// It may return nil, if no authentication is needed.
	AuthNFilter(proxyType model.NodeType, isXDSMarshalingToAnyEnabled bool) *http_conn.HttpFilter
}
