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

package plugin

import (
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"

	"istio.io/istio/pilot/pkg/model"
)

const (
	// AuthzCustom is the name of the authorization plugin (CUSTOM action) passed through the command line
	AuthzCustom = "ext_authz"
	// Authn is the name of the authentication plugin passed through the command line
	Authn = "authn"
	// Authz is the name of the authorization plugin (ALLOW/DENY/AUDIT action) passed through the command line
	Authz = "authz"
	// MetadataExchange is the name of the telemetry plugin passed through the command line
	MetadataExchange = "metadata_exchange"
)

// InputParams is a set of values passed to Plugin callback methods. Not all fields are guaranteed to
// be set, it's up to the callee to validate required fields are set and emit error if they are not.
// These are for reading only and should not be modified.
type InputParams struct {
	// Node is the node the response is for.
	Node *model.Proxy
	// ServiceInstance is the service instance colocated with the listener (applies to sidecar).
	ServiceInstance *model.ServiceInstance
	// Push holds stats and other information about the current push.
	Push *model.PushContext
}

// MTLSSettings describes the mTLS options for a filter chain
type MTLSSettings struct {
	// Port is the port this option applies for
	Port uint32
	// Mode is the mTLS  mode to use
	Mode model.MutualTLSMode
	// TCP describes the tls context to use for TCP filter chains
	TCP *tls.DownstreamTlsContext
	// HTTP describes the tls context to use for HTTP filter chains
	HTTP *tls.DownstreamTlsContext
}
