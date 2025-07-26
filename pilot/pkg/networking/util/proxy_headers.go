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

package util

import (
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"google.golang.org/protobuf/types/known/wrapperspb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pkg/proto"
)

// ProxyHeaders contains configuration for HTTP proxy headers
type ProxyHeaders struct {
	ServerName                 string
	ServerHeaderTransformation hcm.HttpConnectionManager_ServerHeaderTransformation
	ForwardedClientCert        hcm.HttpConnectionManager_ForwardClientCertDetails
	SetCurrentCertDetails      *meshconfig.ProxyConfig_ProxyHeaders_SetCurrentClientCertDetails
	IncludeRequestAttemptCount bool
	GenerateRequestID          *wrapperspb.BoolValue
	SuppressDebugHeaders       bool
	SkipIstioMXHeaders         bool
}

// GetProxyHeaders returns proxy headers configuration for the given node and listener class
func GetProxyHeaders(node *model.Proxy, push *model.PushContext, class istionetworking.ListenerClass) ProxyHeaders {
	pc := node.Metadata.ProxyConfigOrDefault(push.Mesh.DefaultConfig)
	return GetProxyHeadersFromProxyConfig(pc, class)
}

// GetProxyHeadersFromProxyConfig returns proxy headers configuration from proxy config and listener class
func GetProxyHeadersFromProxyConfig(pc *meshconfig.ProxyConfig, class istionetworking.ListenerClass) ProxyHeaders {
	base := ProxyHeaders{
		ServerName:                 "istio-envoy", // EnvoyServerName constant
		ServerHeaderTransformation: hcm.HttpConnectionManager_OVERWRITE,
		ForwardedClientCert:        hcm.HttpConnectionManager_APPEND_FORWARD,
		IncludeRequestAttemptCount: true,
		SuppressDebugHeaders:       false,
		GenerateRequestID:          nil, // Envoy default is to enable them, so set nil
		SkipIstioMXHeaders:         false,
	}
	if class == istionetworking.ListenerClassSidecarOutbound {
		// Likely due to a mistake, outbound uses "envoy" while inbound uses "istio-envoy". Bummer.
		// We keep it for backwards compatibility.
		base.ServerName = "" // Envoy default is "envoy" so no need to set it explicitly.
	}
	ph := pc.GetProxyHeaders()
	if ph == nil {
		return base
	}
	if ph.AttemptCount.GetDisabled().GetValue() {
		base.IncludeRequestAttemptCount = false
	}
	if ph.ForwardedClientCert != meshconfig.ForwardClientCertDetails_UNDEFINED {
		base.ForwardedClientCert = MeshConfigToEnvoyForwardClientCertDetails(ph.ForwardedClientCert)
	}
	if ph.Server != nil {
		if ph.Server.Disabled.GetValue() {
			base.ServerName = ""
			base.ServerHeaderTransformation = hcm.HttpConnectionManager_PASS_THROUGH
		} else if ph.Server.Value != "" {
			base.ServerName = ph.Server.Value
		}
	}
	if ph.RequestId.GetDisabled().GetValue() {
		base.GenerateRequestID = proto.BoolFalse
	}
	if ph.EnvoyDebugHeaders.GetDisabled().GetValue() {
		base.SuppressDebugHeaders = true
	}
	if ph.MetadataExchangeHeaders != nil && ph.MetadataExchangeHeaders.GetMode() == meshconfig.ProxyConfig_ProxyHeaders_IN_MESH {
		base.SkipIstioMXHeaders = true
	}
	base.SetCurrentCertDetails = ph.SetCurrentClientCertDetails
	return base
}
