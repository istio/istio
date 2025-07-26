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
	"testing"

	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"google.golang.org/protobuf/types/known/wrapperspb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pkg/proto"
)

func TestGetProxyHeadersFromProxyConfig(t *testing.T) {
	tests := []struct {
		name     string
		config   *meshconfig.ProxyConfig
		class    istionetworking.ListenerClass
		expected ProxyHeaders
	}{
		{
			name:   "nil config",
			config: nil,
			class:  istionetworking.ListenerClassSidecarInbound,
			expected: ProxyHeaders{
				ServerName:                 "istio-envoy",
				ServerHeaderTransformation: hcm.HttpConnectionManager_OVERWRITE,
				ForwardedClientCert:        hcm.HttpConnectionManager_APPEND_FORWARD,
				IncludeRequestAttemptCount: true,
				GenerateRequestID:          nil,
				SuppressDebugHeaders:       false,
				SkipIstioMXHeaders:         false,
			},
		},
		{
			name:   "sidecar outbound class",
			config: &meshconfig.ProxyConfig{},
			class:  istionetworking.ListenerClassSidecarOutbound,
			expected: ProxyHeaders{
				ServerName:                 "",
				ServerHeaderTransformation: hcm.HttpConnectionManager_OVERWRITE,
				ForwardedClientCert:        hcm.HttpConnectionManager_APPEND_FORWARD,
				IncludeRequestAttemptCount: true,
				GenerateRequestID:          nil,
				SuppressDebugHeaders:       false,
				SkipIstioMXHeaders:         false,
			},
		},
		{
			name: "with proxy headers config",
			config: &meshconfig.ProxyConfig{
				ProxyHeaders: &meshconfig.ProxyConfig_ProxyHeaders{
					Server: &meshconfig.ProxyConfig_ProxyHeaders_Server{
						Value: "custom-server",
					},
					AttemptCount: &meshconfig.ProxyConfig_ProxyHeaders_AttemptCount{
						Disabled: &wrapperspb.BoolValue{Value: true},
					},
					RequestId: &meshconfig.ProxyConfig_ProxyHeaders_RequestId{
						Disabled: &wrapperspb.BoolValue{Value: true},
					},
					EnvoyDebugHeaders: &meshconfig.ProxyConfig_ProxyHeaders_EnvoyDebugHeaders{
						Disabled: &wrapperspb.BoolValue{Value: true},
					},
				},
			},
			class: istionetworking.ListenerClassSidecarInbound,
			expected: ProxyHeaders{
				ServerName:                 "custom-server",
				ServerHeaderTransformation: hcm.HttpConnectionManager_OVERWRITE,
				ForwardedClientCert:        hcm.HttpConnectionManager_APPEND_FORWARD,
				IncludeRequestAttemptCount: false,
				GenerateRequestID:          proto.BoolFalse,
				SuppressDebugHeaders:       true,
				SkipIstioMXHeaders:         false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetProxyHeadersFromProxyConfig(tt.config, tt.class)
			if result.ServerName != tt.expected.ServerName {
				t.Errorf("ServerName = %v, want %v", result.ServerName, tt.expected.ServerName)
			}
			if result.ServerHeaderTransformation != tt.expected.ServerHeaderTransformation {
				t.Errorf("ServerHeaderTransformation = %v, want %v", result.ServerHeaderTransformation, tt.expected.ServerHeaderTransformation)
			}
			if result.ForwardedClientCert != tt.expected.ForwardedClientCert {
				t.Errorf("ForwardedClientCert = %v, want %v", result.ForwardedClientCert, tt.expected.ForwardedClientCert)
			}
			if result.IncludeRequestAttemptCount != tt.expected.IncludeRequestAttemptCount {
				t.Errorf("IncludeRequestAttemptCount = %v, want %v", result.IncludeRequestAttemptCount, tt.expected.IncludeRequestAttemptCount)
			}
			if result.SuppressDebugHeaders != tt.expected.SuppressDebugHeaders {
				t.Errorf("SuppressDebugHeaders = %v, want %v", result.SuppressDebugHeaders, tt.expected.SuppressDebugHeaders)
			}
			if result.SkipIstioMXHeaders != tt.expected.SkipIstioMXHeaders {
				t.Errorf("SkipIstioMXHeaders = %v, want %v", result.SkipIstioMXHeaders, tt.expected.SkipIstioMXHeaders)
			}
		})
	}
}

func TestGetProxyHeaders(t *testing.T) {
	// Test with a simple proxy and push context
	node := &model.Proxy{
		Metadata: &model.NodeMetadata{},
	}
	push := &model.PushContext{
		Mesh: &meshconfig.MeshConfig{
			DefaultConfig: &meshconfig.ProxyConfig{},
		},
	}

	result := GetProxyHeaders(node, push, istionetworking.ListenerClassSidecarInbound)

	// Verify basic structure
	if result.ServerName != "istio-envoy" {
		t.Errorf("Expected ServerName to be 'istio-envoy', got %s", result.ServerName)
	}
	if result.ServerHeaderTransformation != hcm.HttpConnectionManager_OVERWRITE {
		t.Errorf("Expected ServerHeaderTransformation to be OVERWRITE, got %v", result.ServerHeaderTransformation)
	}
	if result.ForwardedClientCert != hcm.HttpConnectionManager_APPEND_FORWARD {
		t.Errorf("Expected ForwardedClientCert to be APPEND_FORWARD, got %v", result.ForwardedClientCert)
	}
	if !result.IncludeRequestAttemptCount {
		t.Error("Expected IncludeRequestAttemptCount to be true")
	}
}
