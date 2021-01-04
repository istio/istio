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
	"istio.io/istio/pilot/pkg/model"
	istionetworking "istio.io/istio/pilot/pkg/networking"
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
	// ListenerProtocol is the protocol/class of listener (TCP, HTTP etc.). Must be set.
	// This is valid only for the inbound listener
	// Outbound listeners could have multiple filter chains, where one filter chain could be
	// a HTTP connection manager with TLS context, while the other could be a tcp proxy with sni
	ListenerProtocol istionetworking.ListenerProtocol
	// Node is the node the response is for.
	Node *model.Proxy
	// ServiceInstance is the service instance colocated with the listener (applies to sidecar).
	ServiceInstance *model.ServiceInstance
	// Push holds stats and other information about the current push.
	Push *model.PushContext
}

// Plugin is called during the construction of a listener.Listener which may alter the Listener in any
// way. Examples include AuthenticationPlugin that sets up mTLS authentication on the inbound Listener
// and outbound Cluster, etc.
type Plugin interface {
	// OnOutboundListener is called whenever a new outbound listener is added to the LDS output for a given service.
	// Can be used to add additional filters on the outbound path.
	OnOutboundListener(in *InputParams, mutable *istionetworking.MutableObjects) error

	// OnInboundListener is called whenever a new listener is added to the LDS output for a given service
	// Can be used to add additional filters.
	OnInboundListener(in *InputParams, mutable *istionetworking.MutableObjects) error

	// OnInboundFilterChains is called whenever a plugin needs to setup the filter chains, including relevant filter chain
	// configuration, like FilterChainMatch and TLSContext.
	OnInboundFilterChains(in *InputParams) []istionetworking.FilterChain

	// OnInboundPassthrough is called whenever a new passthrough filter chain is added to the LDS output.
	// Can be used to add additional filters.
	OnInboundPassthrough(in *InputParams, mutable *istionetworking.MutableObjects) error

	// OnInboundPassthroughFilterChains is called whenever a plugin needs to setup custom pass through filter chain.
	OnInboundPassthroughFilterChains(in *InputParams) []istionetworking.FilterChain
}
