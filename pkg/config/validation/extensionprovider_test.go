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

package validation

import (
	"testing"

	meshconfig "istio.io/api/mesh/v1alpha1"
)

func TestValidateExtensionProviderService(t *testing.T) {
	tests := []struct {
		service string
		valid   bool
		name    string
	}{
		{
			service: "127.0.0.1",
			valid:   true,
			name:    "pure ip4 address",
		},
		{
			service: "2001:1::1",
			valid:   true,
			name:    "pure ip6 address",
		},
		{
			service: "istio-pilot.istio-system.svc.cluster.local",
			valid:   true,
			name:    "standard kubernetes FQDN",
		},
		{
			service: "istio-pilot.istio-system.svc.cluster.local:3000",
			valid:   false,
			name:    "standard kubernetes FQDN with port",
		},
		{
			service: "bar/istio.io",
			valid:   true,
			name:    "extension provider service with namespace",
		},
		{
			service: "bar/istio.io:3000",
			valid:   false,
			name:    "extension provider service with namespace and port",
		},
		{
			service: "bar/foo/istio.io",
			valid:   false,
			name:    "extension provider service with namespace",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			err := validateExtensionProviderService(tt.service)
			valid := err == nil
			if valid != tt.valid {
				t.Errorf("Expected valid=%v, got valid=%v for %v", tt.valid, valid, tt.service)
			}
		})
	}
}

func TestValidateExtensionProviderTracingZipkin(t *testing.T) {
	cases := []struct {
		name   string
		config *meshconfig.MeshConfig_ExtensionProvider_ZipkinTracingProvider
		valid  bool
	}{
		{
			name: "zipkin normal",
			config: &meshconfig.MeshConfig_ExtensionProvider_ZipkinTracingProvider{
				Service: "zipkin.istio-system",
				Port:    9411,
			},
			valid: true,
		},
		{
			name: "zipkin service with namespace",
			config: &meshconfig.MeshConfig_ExtensionProvider_ZipkinTracingProvider{
				Service: "namespace/zipkin.istio-system",
				Port:    9411,
			},
			valid: true,
		},
		{
			name: "zipkin service with invalid namespace",
			config: &meshconfig.MeshConfig_ExtensionProvider_ZipkinTracingProvider{
				Service: "name/space/zipkin.istio-system",
				Port:    9411,
			},
			valid: false,
		},
		{
			name: "zipkin service with port",
			config: &meshconfig.MeshConfig_ExtensionProvider_ZipkinTracingProvider{
				Service: "zipkin.istio-system:9411",
				Port:    9411,
			},
			valid: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateExtensionProviderTracingZipkin(c.config)
			valid := err == nil
			if valid != c.valid {
				t.Errorf("Expected valid=%v, got valid=%v for %v", c.valid, valid, c.config)
			}
		})
	}
}

func TestValidateExtensionProviderTracingLightstep(t *testing.T) {
	cases := []struct {
		name   string
		config *meshconfig.MeshConfig_ExtensionProvider_LightstepTracingProvider
		valid  bool
	}{
		{
			name: "lightstep normal",
			config: &meshconfig.MeshConfig_ExtensionProvider_LightstepTracingProvider{
				Service:     "collector.lightstep",
				Port:        8080,
				AccessToken: "abcdefg1234567",
			},
			valid: true,
		},
		{
			name: "lightstep service with namespace",
			config: &meshconfig.MeshConfig_ExtensionProvider_LightstepTracingProvider{
				Service:     "namespace/collector.lightstep",
				Port:        8080,
				AccessToken: "abcdefg1234567",
			},
			valid: true,
		},
		{
			name: "lightstep service with missing port",
			config: &meshconfig.MeshConfig_ExtensionProvider_LightstepTracingProvider{
				Service:     "10.0.0.100",
				AccessToken: "abcdefg1234567",
			},
			valid: false,
		},
		{
			name: "lightstep service with missing accesstoken",
			config: &meshconfig.MeshConfig_ExtensionProvider_LightstepTracingProvider{
				Service: "namespace/collector.lightstep",
				Port:    8080,
			},
			valid: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateExtensionProviderTracingLightStep(c.config)
			valid := err == nil
			if valid != c.valid {
				t.Errorf("Expected valid=%v, got valid=%v for %v", c.valid, valid, c.config)
			}
		})
	}
}

func TestValidateExtensionProviderTracingDatadog(t *testing.T) {
	cases := []struct {
		name   string
		config *meshconfig.MeshConfig_ExtensionProvider_DatadogTracingProvider
		valid  bool
	}{
		{
			name: "datadog normal",
			config: &meshconfig.MeshConfig_ExtensionProvider_DatadogTracingProvider{
				Service: "datadog-agent.com",
				Port:    8126,
			},
			valid: true,
		},
		{
			name: "datadog service with namespace",
			config: &meshconfig.MeshConfig_ExtensionProvider_DatadogTracingProvider{
				Service: "namespace/datadog-agent.com",
				Port:    8126,
			},
			valid: true,
		},
		{
			name: "datadog service with invalid namespace",
			config: &meshconfig.MeshConfig_ExtensionProvider_DatadogTracingProvider{
				Service: "name/space/datadog-agent.com",
				Port:    8126,
			},
			valid: false,
		},
		{
			name: "datadog service with port",
			config: &meshconfig.MeshConfig_ExtensionProvider_DatadogTracingProvider{
				Service: "datadog-agent.com:8126",
				Port:    8126,
			},
			valid: false,
		},
		{
			name: "datadog missing port",
			config: &meshconfig.MeshConfig_ExtensionProvider_DatadogTracingProvider{
				Service: "datadog-agent.com",
			},
			valid: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateExtensionProviderTracingDatadog(c.config)
			valid := err == nil
			if valid != c.valid {
				t.Errorf("Expected valid=%v, got valid=%v for %v", c.valid, valid, c.config)
			}
		})
	}
}

func TestValidateExtensionProviderTracingOpenCensusAgent(t *testing.T) {
	cases := []struct {
		name   string
		config *meshconfig.MeshConfig_ExtensionProvider_OpenCensusAgentTracingProvider
		valid  bool
	}{
		{
			name: "opencensus normal",
			config: &meshconfig.MeshConfig_ExtensionProvider_OpenCensusAgentTracingProvider{
				Service: "opencensus-agent.com",
				Port:    4000,
			},
			valid: true,
		},
		{
			name: "opencensus service with namespace",
			config: &meshconfig.MeshConfig_ExtensionProvider_OpenCensusAgentTracingProvider{
				Service: "namespace/opencensus-agent.com",
				Port:    4000,
			},
			valid: true,
		},
		{
			name: "opencensus service with invalid namespace",
			config: &meshconfig.MeshConfig_ExtensionProvider_OpenCensusAgentTracingProvider{
				Service: "name/space/opencensus-agent.com",
				Port:    4000,
			},
			valid: false,
		},
		{
			name: "opencensus service with port",
			config: &meshconfig.MeshConfig_ExtensionProvider_OpenCensusAgentTracingProvider{
				Service: "opencensus-agent.com:4000",
				Port:    4000,
			},
			valid: false,
		},
		{
			name: "opencensus missing port",
			config: &meshconfig.MeshConfig_ExtensionProvider_OpenCensusAgentTracingProvider{
				Service: "opencensus-agent.com",
			},
			valid: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateExtensionProviderTracingOpenCensusAgent(c.config)
			valid := err == nil
			if valid != c.valid {
				t.Errorf("Expected valid=%v, got valid=%v for %v", c.valid, valid, c.config)
			}
		})
	}
}

func TestValidateExtensionProviderEnvoyOtelAls(t *testing.T) {
	cases := []struct {
		name     string
		provider *meshconfig.MeshConfig_ExtensionProvider_EnvoyOpenTelemetryLogProvider
		valid    bool
	}{
		{
			name: "otel normal",
			provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyOpenTelemetryLogProvider{
				Service: "otel.istio-syste.svc",
				Port:    4000,
			},
			valid: true,
		},
		{
			name: "otel service with namespace",
			provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyOpenTelemetryLogProvider{
				Service: "namespace/otel.istio-syste.svc",
				Port:    4000,
			},
			valid: true,
		},
		{
			name: "otel service with invalid namespace",
			provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyOpenTelemetryLogProvider{
				Service: "name/space/otel.istio-syste.svc",
				Port:    4000,
			},
			valid: false,
		},
		{
			name: "otel service with port",
			provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyOpenTelemetryLogProvider{
				Service: "otel.istio-syste.svc:4000",
				Port:    4000,
			},
			valid: false,
		},
		{
			name: "otel missing port",
			provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyOpenTelemetryLogProvider{
				Service: "otel.istio-syste.svc",
			},
			valid: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateExtensionProviderEnvoyOtelAls(c.provider)
			valid := err == nil
			if valid != c.valid {
				t.Errorf("Expected valid=%v, got valid=%v for %v", c.valid, valid, c.provider)
			}
		})
	}
}

func TestValidateExtensionProviderEnvoyHTTPAls(t *testing.T) {
	cases := []struct {
		name     string
		provider *meshconfig.MeshConfig_ExtensionProvider_EnvoyHttpGrpcV3LogProvider
		valid    bool
	}{
		{
			name: "normal",
			provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyHttpGrpcV3LogProvider{
				Service: "grpc-als.istio-syste.svc",
				Port:    4000,
			},
			valid: true,
		},
		{
			name: "service with namespace",
			provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyHttpGrpcV3LogProvider{
				Service: "namespace/grpc-als.istio-syste.svc",
				Port:    4000,
			},
			valid: true,
		},
		{
			name: "service with invalid namespace",
			provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyHttpGrpcV3LogProvider{
				Service: "name/space/grpc-als.istio-syste.svc",
				Port:    4000,
			},
			valid: false,
		},
		{
			name: "service with port",
			provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyHttpGrpcV3LogProvider{
				Service: "grpc-als.istio-syste.svc:4000",
				Port:    4000,
			},
			valid: false,
		},
		{
			name: "missing port",
			provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyHttpGrpcV3LogProvider{
				Service: "grpc-als.istio-syste.svc",
			},
			valid: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateExtensionProviderEnvoyHTTPAls(c.provider)
			valid := err == nil
			if valid != c.valid {
				t.Errorf("Expected valid=%v, got valid=%v for %v", c.valid, valid, c.provider)
			}
		})
	}
}

func TestValidateExtensionProviderEnvoyTCPAls(t *testing.T) {
	cases := []struct {
		name     string
		provider *meshconfig.MeshConfig_ExtensionProvider_EnvoyTcpGrpcV3LogProvider
		valid    bool
	}{
		{
			name: "normal",
			provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyTcpGrpcV3LogProvider{
				Service: "grpc-als.istio-syste.svc",
				Port:    4000,
			},
			valid: true,
		},
		{
			name: "service with namespace",
			provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyTcpGrpcV3LogProvider{
				Service: "namespace/grpc-als.istio-syste.svc",
				Port:    4000,
			},
			valid: true,
		},
		{
			name: "service with invalid namespace",
			provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyTcpGrpcV3LogProvider{
				Service: "name/space/grpc-als.istio-syste.svc",
				Port:    4000,
			},
			valid: false,
		},
		{
			name: "service with port",
			provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyTcpGrpcV3LogProvider{
				Service: "grpc-als.istio-syste.svc:4000",
				Port:    4000,
			},
			valid: false,
		},
		{
			name: "missing port",
			provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyTcpGrpcV3LogProvider{
				Service: "grpc-als.istio-syste.svc",
			},
			valid: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateExtensionProviderEnvoyTCPAls(c.provider)
			valid := err == nil
			if valid != c.valid {
				t.Errorf("Expected valid=%v, got valid=%v for %v", c.valid, valid, c.provider)
			}
		})
	}
}
