// Copyright Istio Authors. All Rights Reserved.
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

package core

import (
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"google.golang.org/protobuf/types/known/durationpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/util/protomarshal"
)

// applyEdgeProfileDefaults fills in nil fields with Envoy edge best-practice defaults
// when the ConnectionSettings profile is EDGE and the proxy is a Router (gateway).
// Returns the original ConnectionSettings unchanged for non-EDGE profiles or non-Router proxies.
func applyEdgeProfileDefaults(cs *meshconfig.ProxyConfig_ConnectionSettings, nodeType model.NodeType) *meshconfig.ProxyConfig_ConnectionSettings {
	if cs == nil || cs.GetProfile() != meshconfig.ProxyConfig_ConnectionSettings_EDGE {
		return cs
	}
	if nodeType != model.Router {
		return cs
	}

	// Clone to avoid mutating the shared ProxyConfig (e.g., MeshConfig.DefaultConfig).
	cs = protomarshal.Clone(cs)

	if cs.ListenerPerConnectionBufferLimitBytes == nil {
		cs.ListenerPerConnectionBufferLimitBytes = &wrappers.Int32Value{Value: 32768} // 32 KiB
	}
	if cs.HttpIdleTimeout == nil {
		cs.HttpIdleTimeout = durationpb.New(time.Hour)
	}
	if cs.HttpStreamIdleTimeout == nil {
		cs.HttpStreamIdleTimeout = durationpb.New(5 * time.Minute)
	}
	if cs.HttpRequestTimeout == nil {
		cs.HttpRequestTimeout = durationpb.New(5 * time.Minute)
	}
	if cs.HttpRequestHeadersTimeout == nil {
		cs.HttpRequestHeadersTimeout = durationpb.New(60 * time.Second)
	}
	if cs.HttpMaxConcurrentStreams == nil {
		cs.HttpMaxConcurrentStreams = &wrappers.Int32Value{Value: 100}
	}
	if cs.Http2InitialStreamWindowSize == nil {
		cs.Http2InitialStreamWindowSize = &wrappers.Int32Value{Value: 65536} // 64 KiB
	}
	if cs.Http2InitialConnectionWindowSize == nil {
		cs.Http2InitialConnectionWindowSize = &wrappers.Int32Value{Value: 1048576} // 1 MiB
	}
	if cs.HttpHeadersWithUnderscoresAction == meshconfig.ProxyConfig_ConnectionSettings_HEADERS_WITH_UNDERSCORES_UNSPECIFIED {
		cs.HttpHeadersWithUnderscoresAction = meshconfig.ProxyConfig_ConnectionSettings_HEADERS_WITH_UNDERSCORES_REJECT_REQUEST
	}
	if cs.HttpMergeSlashes == nil {
		cs.HttpMergeSlashes = &wrappers.BoolValue{Value: true}
	}
	if cs.HttpPathWithEscapedSlashesAction == meshconfig.ProxyConfig_ConnectionSettings_PATH_WITH_ESCAPED_SLASHES_UNSPECIFIED {
		cs.HttpPathWithEscapedSlashesAction = meshconfig.ProxyConfig_ConnectionSettings_UNESCAPE_AND_REDIRECT
	}

	return cs
}

// resolveConnectionSettings returns the effective ConnectionSettings for a proxy,
// with EDGE profile defaults applied for Router proxies. The result is safe to
// use without further cloning (applyEdgeProfileDefaults clones internally).
func resolveConnectionSettings(node *model.Proxy, push *model.PushContext) *meshconfig.ProxyConfig_ConnectionSettings {
	cs := node.Metadata.ProxyConfigOrDefault(push.Mesh.GetDefaultConfig()).GetConnectionSettings()
	if cs == nil {
		cs = push.Mesh.GetDefaultConfig().GetConnectionSettings()
	}
	if cs != nil {
		cs = applyEdgeProfileDefaults(cs, node.Type)
	}
	return cs
}

// safeUint32 converts an Int32Value to a UInt32Value, returning nil if the input is nil or negative.
// This guards against negative values becoming ~4 GiB values after an unsigned cast.
func safeUint32(v *wrappers.Int32Value) *wrappers.UInt32Value {
	if v == nil || v.GetValue() < 0 {
		return nil
	}
	return &wrappers.UInt32Value{Value: uint32(v.GetValue())}
}

// applyConnectionSettingsToHCM applies ConnectionSettings fields to an HTTP Connection Manager.
// When ConnectionSettings normalization fields are set, they take precedence over MeshConfig values.
func applyConnectionSettingsToHCM(connectionManager *hcm.HttpConnectionManager, cs *meshconfig.ProxyConfig_ConnectionSettings) {
	if cs == nil {
		return
	}

	// --- Timeouts ---

	if cs.GetHttpIdleTimeout() != nil || cs.GetHttpMaxConnectionDuration() != nil || cs.GetHttpMaxStreamDuration() != nil {
		if connectionManager.CommonHttpProtocolOptions == nil {
			connectionManager.CommonHttpProtocolOptions = &core.HttpProtocolOptions{}
		}
	}
	if cs.GetHttpIdleTimeout() != nil {
		connectionManager.CommonHttpProtocolOptions.IdleTimeout = cs.GetHttpIdleTimeout()
	}
	if cs.GetHttpMaxConnectionDuration() != nil {
		connectionManager.CommonHttpProtocolOptions.MaxConnectionDuration = cs.GetHttpMaxConnectionDuration()
	}
	if cs.GetHttpMaxStreamDuration() != nil {
		connectionManager.CommonHttpProtocolOptions.MaxStreamDuration = cs.GetHttpMaxStreamDuration()
	}

	if cs.GetHttpStreamIdleTimeout() != nil {
		connectionManager.StreamIdleTimeout = cs.GetHttpStreamIdleTimeout()
	}
	if cs.GetHttpRequestTimeout() != nil {
		connectionManager.RequestTimeout = cs.GetHttpRequestTimeout()
	}
	if cs.GetHttpRequestHeadersTimeout() != nil {
		connectionManager.RequestHeadersTimeout = cs.GetHttpRequestHeadersTimeout()
	}
	if cs.GetHttpDrainTimeout() != nil {
		connectionManager.DrainTimeout = cs.GetHttpDrainTimeout()
	}

	// --- HTTP/2 options ---

	if cs.GetHttpMaxConcurrentStreams() != nil || cs.GetHttp2InitialStreamWindowSize() != nil || cs.GetHttp2InitialConnectionWindowSize() != nil {
		if connectionManager.Http2ProtocolOptions == nil {
			connectionManager.Http2ProtocolOptions = &core.Http2ProtocolOptions{}
		}
		if v := safeUint32(cs.GetHttpMaxConcurrentStreams()); v != nil {
			connectionManager.Http2ProtocolOptions.MaxConcurrentStreams = v
		}
		if v := safeUint32(cs.GetHttp2InitialStreamWindowSize()); v != nil {
			connectionManager.Http2ProtocolOptions.InitialStreamWindowSize = v
		}
		if v := safeUint32(cs.GetHttp2InitialConnectionWindowSize()); v != nil {
			connectionManager.Http2ProtocolOptions.InitialConnectionWindowSize = v
		}
	}

	// --- Header/path normalization ---
	// ProxyConfig ConnectionSettings take precedence over MeshConfig when set.

	if cs.GetHttpHeadersWithUnderscoresAction() != meshconfig.ProxyConfig_ConnectionSettings_HEADERS_WITH_UNDERSCORES_UNSPECIFIED {
		if connectionManager.CommonHttpProtocolOptions == nil {
			connectionManager.CommonHttpProtocolOptions = &core.HttpProtocolOptions{}
		}
		connectionManager.CommonHttpProtocolOptions.HeadersWithUnderscoresAction = toEnvoyHeadersWithUnderscoresAction(cs.GetHttpHeadersWithUnderscoresAction())
	}
	if cs.GetHttpMergeSlashes() != nil {
		connectionManager.MergeSlashes = cs.GetHttpMergeSlashes().GetValue()
	}
	if cs.GetHttpPathWithEscapedSlashesAction() != meshconfig.ProxyConfig_ConnectionSettings_PATH_WITH_ESCAPED_SLASHES_UNSPECIFIED {
		connectionManager.PathWithEscapedSlashesAction = toEnvoyPathWithEscapedSlashesAction(cs.GetHttpPathWithEscapedSlashesAction())
	}
}

// toEnvoyHeadersWithUnderscoresAction converts the Istio API enum to the Envoy core proto enum.
func toEnvoyHeadersWithUnderscoresAction(
	action meshconfig.ProxyConfig_ConnectionSettings_HeadersWithUnderscoresAction,
) core.HttpProtocolOptions_HeadersWithUnderscoresAction {
	switch action {
	case meshconfig.ProxyConfig_ConnectionSettings_HEADERS_WITH_UNDERSCORES_REJECT_REQUEST:
		return core.HttpProtocolOptions_REJECT_REQUEST
	case meshconfig.ProxyConfig_ConnectionSettings_HEADERS_WITH_UNDERSCORES_DROP_HEADER:
		return core.HttpProtocolOptions_DROP_HEADER
	default:
		return core.HttpProtocolOptions_ALLOW
	}
}

// toEnvoyPathWithEscapedSlashesAction converts the Istio API enum to the Envoy HCM proto enum.
func toEnvoyPathWithEscapedSlashesAction(
	action meshconfig.ProxyConfig_ConnectionSettings_PathWithEscapedSlashesAction,
) hcm.HttpConnectionManager_PathWithEscapedSlashesAction {
	switch action {
	case meshconfig.ProxyConfig_ConnectionSettings_REJECT_REQUEST:
		return hcm.HttpConnectionManager_REJECT_REQUEST
	case meshconfig.ProxyConfig_ConnectionSettings_UNESCAPE_AND_REDIRECT:
		return hcm.HttpConnectionManager_UNESCAPE_AND_REDIRECT
	case meshconfig.ProxyConfig_ConnectionSettings_UNESCAPE_AND_FORWARD:
		return hcm.HttpConnectionManager_UNESCAPE_AND_FORWARD
	default:
		return hcm.HttpConnectionManager_KEEP_UNCHANGED
	}
}
