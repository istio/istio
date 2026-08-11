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
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"google.golang.org/protobuf/types/known/durationpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/util/assert"
)

func TestApplyEdgeProfileDefaults(t *testing.T) {
	tests := []struct {
		name     string
		cs       *meshconfig.ProxyConfig_ConnectionSettings
		nodeType model.NodeType
		verify   func(t *testing.T, cs *meshconfig.ProxyConfig_ConnectionSettings)
	}{
		{
			name:     "nil ConnectionSettings returns nil",
			cs:       nil,
			nodeType: model.Router,
			verify: func(t *testing.T, cs *meshconfig.ProxyConfig_ConnectionSettings) {
				assert.Equal(t, (*meshconfig.ProxyConfig_ConnectionSettings)(nil), cs)
			},
		},
		{
			name:     "empty ConnectionSettings passes through without error",
			cs:       &meshconfig.ProxyConfig_ConnectionSettings{},
			nodeType: model.Router,
			verify: func(t *testing.T, cs *meshconfig.ProxyConfig_ConnectionSettings) {
				// No profile set (zero value), so no defaults applied
				assert.Equal(t, (*wrappers.Int32Value)(nil), cs.GetListenerPerConnectionBufferLimitBytes())
			},
		},
		{
			name: "SIDECAR profile returns unchanged",
			cs: &meshconfig.ProxyConfig_ConnectionSettings{
				Profile: meshconfig.ProxyConfig_ConnectionSettings_SIDECAR,
			},
			nodeType: model.Router,
			verify: func(t *testing.T, cs *meshconfig.ProxyConfig_ConnectionSettings) {
				assert.Equal(t, (*wrappers.Int32Value)(nil), cs.GetListenerPerConnectionBufferLimitBytes())
			},
		},
		{
			name: "EDGE profile on sidecar does not apply defaults",
			cs: &meshconfig.ProxyConfig_ConnectionSettings{
				Profile: meshconfig.ProxyConfig_ConnectionSettings_EDGE,
			},
			nodeType: model.SidecarProxy,
			verify: func(t *testing.T, cs *meshconfig.ProxyConfig_ConnectionSettings) {
				assert.Equal(t, (*wrappers.Int32Value)(nil), cs.GetListenerPerConnectionBufferLimitBytes())
			},
		},
		{
			name: "EDGE profile on Router applies all defaults",
			cs: &meshconfig.ProxyConfig_ConnectionSettings{
				Profile: meshconfig.ProxyConfig_ConnectionSettings_EDGE,
			},
			nodeType: model.Router,
			verify: func(t *testing.T, cs *meshconfig.ProxyConfig_ConnectionSettings) {
				assert.Equal(t, int32(32768), cs.GetListenerPerConnectionBufferLimitBytes().GetValue())
				assert.Equal(t, time.Hour, cs.GetHttpIdleTimeout().AsDuration())
				assert.Equal(t, int32(100), cs.GetHttpMaxConcurrentStreams().GetValue())
				assert.Equal(t, int32(65536), cs.GetHttp2InitialStreamWindowSize().GetValue())
				assert.Equal(t, int32(1048576), cs.GetHttp2InitialConnectionWindowSize().GetValue())
				assert.Equal(t, true, cs.GetHttpMergeSlashes().GetValue())
				assert.Equal(t, meshconfig.ProxyConfig_ConnectionSettings_UNESCAPE_AND_REDIRECT, cs.GetHttpPathWithEscapedSlashesAction())
				assert.Equal(t, meshconfig.ProxyConfig_ConnectionSettings_HEADERS_WITH_UNDERSCORES_REJECT_REQUEST, cs.GetHttpHeadersWithUnderscoresAction())
			},
		},
		{
			name: "EDGE profile on Router preserves explicit overrides",
			cs: &meshconfig.ProxyConfig_ConnectionSettings{
				Profile:                               meshconfig.ProxyConfig_ConnectionSettings_EDGE,
				ListenerPerConnectionBufferLimitBytes: &wrappers.Int32Value{Value: 16384},
				HttpMaxConcurrentStreams:              &wrappers.Int32Value{Value: 50},
				HttpHeadersWithUnderscoresAction:      meshconfig.ProxyConfig_ConnectionSettings_HEADERS_WITH_UNDERSCORES_DROP_HEADER,
				HttpPathWithEscapedSlashesAction:      meshconfig.ProxyConfig_ConnectionSettings_UNESCAPE_AND_FORWARD,
			},
			nodeType: model.Router,
			verify: func(t *testing.T, cs *meshconfig.ProxyConfig_ConnectionSettings) {
				// Explicit overrides preserved
				assert.Equal(t, int32(16384), cs.GetListenerPerConnectionBufferLimitBytes().GetValue())
				assert.Equal(t, int32(50), cs.GetHttpMaxConcurrentStreams().GetValue())
				assert.Equal(t, meshconfig.ProxyConfig_ConnectionSettings_HEADERS_WITH_UNDERSCORES_DROP_HEADER, cs.GetHttpHeadersWithUnderscoresAction())
				assert.Equal(t, meshconfig.ProxyConfig_ConnectionSettings_UNESCAPE_AND_FORWARD, cs.GetHttpPathWithEscapedSlashesAction())
				// Defaults still applied for unset fields
				assert.Equal(t, true, cs.GetHttpMergeSlashes().GetValue())
			},
		},
		{
			name: "EDGE profile preserves explicit ALLOW and KEEP_UNCHANGED enum values",
			cs: &meshconfig.ProxyConfig_ConnectionSettings{
				Profile:                          meshconfig.ProxyConfig_ConnectionSettings_EDGE,
				HttpHeadersWithUnderscoresAction: meshconfig.ProxyConfig_ConnectionSettings_HEADERS_WITH_UNDERSCORES_ALLOW,
				HttpPathWithEscapedSlashesAction: meshconfig.ProxyConfig_ConnectionSettings_KEEP_UNCHANGED,
			},
			nodeType: model.Router,
			verify: func(t *testing.T, cs *meshconfig.ProxyConfig_ConnectionSettings) {
				// Explicit ALLOW / KEEP_UNCHANGED must NOT be overridden to EDGE defaults
				assert.Equal(t, meshconfig.ProxyConfig_ConnectionSettings_HEADERS_WITH_UNDERSCORES_ALLOW, cs.GetHttpHeadersWithUnderscoresAction())
				assert.Equal(t, meshconfig.ProxyConfig_ConnectionSettings_KEEP_UNCHANGED, cs.GetHttpPathWithEscapedSlashesAction())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := applyEdgeProfileDefaults(tt.cs, tt.nodeType)
			tt.verify(t, result)
		})
	}
}

func TestApplyConnectionSettingsToHCM(t *testing.T) {
	tests := []struct {
		name   string
		cs     *meshconfig.ProxyConfig_ConnectionSettings
		verify func(t *testing.T, cm *hcm.HttpConnectionManager)
	}{
		{
			name: "nil ConnectionSettings is a no-op",
			cs:   nil,
			verify: func(t *testing.T, cm *hcm.HttpConnectionManager) {
				assert.Equal(t, (*core.HttpProtocolOptions)(nil), cm.CommonHttpProtocolOptions)
			},
		},
		{
			name: "idle timeout sets CommonHttpProtocolOptions",
			cs: &meshconfig.ProxyConfig_ConnectionSettings{
				HttpIdleTimeout: durationpb.New(time.Hour),
			},
			verify: func(t *testing.T, cm *hcm.HttpConnectionManager) {
				assert.Equal(t, int64(3600), cm.CommonHttpProtocolOptions.IdleTimeout.GetSeconds())
			},
		},
		{
			name: "stream idle timeout overrides HCM default",
			cs: &meshconfig.ProxyConfig_ConnectionSettings{
				HttpStreamIdleTimeout: durationpb.New(5 * time.Minute),
			},
			verify: func(t *testing.T, cm *hcm.HttpConnectionManager) {
				assert.Equal(t, int64(300), cm.StreamIdleTimeout.GetSeconds())
			},
		},
		{
			name: "HTTP/2 settings create Http2ProtocolOptions",
			cs: &meshconfig.ProxyConfig_ConnectionSettings{
				HttpMaxConcurrentStreams:         &wrappers.Int32Value{Value: 100},
				Http2InitialStreamWindowSize:     &wrappers.Int32Value{Value: 65536},
				Http2InitialConnectionWindowSize: &wrappers.Int32Value{Value: 1048576},
			},
			verify: func(t *testing.T, cm *hcm.HttpConnectionManager) {
				assert.Equal(t, uint32(100), cm.Http2ProtocolOptions.MaxConcurrentStreams.GetValue())
				assert.Equal(t, uint32(65536), cm.Http2ProtocolOptions.InitialStreamWindowSize.GetValue())
				assert.Equal(t, uint32(1048576), cm.Http2ProtocolOptions.InitialConnectionWindowSize.GetValue())
			},
		},
		{
			name: "merge slashes and escaped slashes action",
			cs: &meshconfig.ProxyConfig_ConnectionSettings{
				HttpMergeSlashes:                 &wrappers.BoolValue{Value: true},
				HttpPathWithEscapedSlashesAction: meshconfig.ProxyConfig_ConnectionSettings_UNESCAPE_AND_REDIRECT,
			},
			verify: func(t *testing.T, cm *hcm.HttpConnectionManager) {
				assert.Equal(t, true, cm.MergeSlashes)
				assert.Equal(t, hcm.HttpConnectionManager_UNESCAPE_AND_REDIRECT, cm.PathWithEscapedSlashesAction)
			},
		},
		{
			name: "headers with underscores action sets CommonHttpProtocolOptions",
			cs: &meshconfig.ProxyConfig_ConnectionSettings{
				HttpHeadersWithUnderscoresAction: meshconfig.ProxyConfig_ConnectionSettings_HEADERS_WITH_UNDERSCORES_REJECT_REQUEST,
			},
			verify: func(t *testing.T, cm *hcm.HttpConnectionManager) {
				assert.Equal(t, core.HttpProtocolOptions_REJECT_REQUEST, cm.CommonHttpProtocolOptions.HeadersWithUnderscoresAction)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := &hcm.HttpConnectionManager{}
			applyConnectionSettingsToHCM(cm, tt.cs)
			tt.verify(t, cm)
		})
	}
}

func TestToEnvoyHeadersWithUnderscoresAction(t *testing.T) {
	assert.Equal(t, core.HttpProtocolOptions_ALLOW,
		toEnvoyHeadersWithUnderscoresAction(meshconfig.ProxyConfig_ConnectionSettings_HEADERS_WITH_UNDERSCORES_ALLOW))
	assert.Equal(t, core.HttpProtocolOptions_REJECT_REQUEST,
		toEnvoyHeadersWithUnderscoresAction(meshconfig.ProxyConfig_ConnectionSettings_HEADERS_WITH_UNDERSCORES_REJECT_REQUEST))
	assert.Equal(t, core.HttpProtocolOptions_DROP_HEADER,
		toEnvoyHeadersWithUnderscoresAction(meshconfig.ProxyConfig_ConnectionSettings_HEADERS_WITH_UNDERSCORES_DROP_HEADER))
}

func TestToEnvoyPathWithEscapedSlashesAction(t *testing.T) {
	assert.Equal(t, hcm.HttpConnectionManager_KEEP_UNCHANGED,
		toEnvoyPathWithEscapedSlashesAction(meshconfig.ProxyConfig_ConnectionSettings_KEEP_UNCHANGED))
	assert.Equal(t, hcm.HttpConnectionManager_REJECT_REQUEST,
		toEnvoyPathWithEscapedSlashesAction(meshconfig.ProxyConfig_ConnectionSettings_REJECT_REQUEST))
	assert.Equal(t, hcm.HttpConnectionManager_UNESCAPE_AND_REDIRECT,
		toEnvoyPathWithEscapedSlashesAction(meshconfig.ProxyConfig_ConnectionSettings_UNESCAPE_AND_REDIRECT))
	assert.Equal(t, hcm.HttpConnectionManager_UNESCAPE_AND_FORWARD,
		toEnvoyPathWithEscapedSlashesAction(meshconfig.ProxyConfig_ConnectionSettings_UNESCAPE_AND_FORWARD))
}
