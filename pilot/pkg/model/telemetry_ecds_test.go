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

package model

import (
	"encoding/json"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	meshv1a1 "istio.io/api/mesh/v1alpha1"
	telemetryv1a1 "istio.io/api/telemetry/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestHTTPTypedExtensionConfigFilters(t *testing.T) {
	t.Setenv("ENABLE_ECDS_FOR_STATS", "true")

	sidecar := &Proxy{
		ConfigNamespace: "default",
		Labels:          map[string]string{"app": "test"},
		Metadata:        &NodeMetadata{Labels: map[string]string{"app": "test"}},
		Type:            SidecarProxy,
	}

	tests := []struct {
		name             string
		cfgs             []config.Config
		proxy            *Proxy
		class            networking.ListenerClass
		defaultProviders *meshv1a1.MeshConfig_DefaultProviders
		want             string
	}{
		{
			"empty",
			nil,
			sidecar,
			networking.ListenerClassSidecarOutbound,
			nil,
			"[]",
		},
		{
			"router",
			nil,
			&Proxy{
				ConfigNamespace: "default",
				Labels:          map[string]string{"app": "test"},
				Metadata:        &NodeMetadata{Labels: map[string]string{"app": "test"}},
				Type:            Router,
			},
			networking.ListenerClassGateway,
			&meshv1a1.MeshConfig_DefaultProviders{
				Metrics: []string{"prometheus"},
			},
			// nolint: lll
			"[{\"name\":\"istio.io/telemetry/stats/prometheus/router/Gateway/HTTP\",\"typed_config\":{\"type_url\":\"type.googleapis.com/stats.PluginConfig\",\"value\":\"MAE=\"}}]",
		},
		{
			"sidecar-outbound",
			nil,
			sidecar,
			networking.ListenerClassSidecarOutbound,
			&meshv1a1.MeshConfig_DefaultProviders{
				Metrics: []string{"prometheus"},
			},
			// nolint: lll
			"[{\"name\":\"istio.io/telemetry/stats/prometheus/sidecar/Outbound/HTTP\",\"typed_config\":{\"type_url\":\"type.googleapis.com/stats.PluginConfig\"}}]",
		},
		{
			"sidecar-inbound",
			[]config.Config{newTelemetry("istio-system", &telemetryv1a1.Telemetry{
				Metrics: []*telemetryv1a1.Metrics{
					{
						Providers: []*telemetryv1a1.ProviderRef{{Name: "prometheus"}},
						Overrides: []*telemetryv1a1.MetricsOverrides{
							{
								Match: &telemetryv1a1.MetricSelector{
									MetricMatch: &telemetryv1a1.MetricSelector_Metric{
										Metric: telemetryv1a1.MetricSelector_REQUEST_COUNT,
									},
								},
								Disabled: &wrapperspb.BoolValue{Value: true},
							},
						},
						ReportingInterval: durationpb.New(10 * time.Second),
					},
				},
			})},
			sidecar,
			networking.ListenerClassSidecarInbound,
			nil,
			// nolint: lll
			"[{\"name\":\"istio.io/telemetry/stats/prometheus/sidecar/Inbound/HTTP\",\"typed_config\":{\"type_url\":\"type.googleapis.com/stats.PluginConfig\",\"value\":\"MAE6AggKQhISDnJlcXVlc3RzX3RvdGFsKAE=\"}}]",
		},
		{
			"stackdriver-inbound",
			nil,
			sidecar,
			networking.ListenerClassSidecarInbound,
			&meshv1a1.MeshConfig_DefaultProviders{
				Metrics:       []string{"stackdriver"},
				AccessLogging: []string{"stackdriver"},
			},
			// nolint: lll
			"[{\"name\":\"istio.io/telemetry/stats/stackdriver/sidecar/Inbound/HTTP\",\"typed_config\":{\"type_url\":\"type.googleapis.com/envoy.extensions.filters.http.wasm.v3.Wasm\",\"value\":\"CvwBEhNzdGFja2RyaXZlcl9pbmJvdW5kIpMBCi90eXBlLmdvb2dsZWFwaXMuY29tL2dvb2dsZS5wcm90b2J1Zi5TdHJpbmdWYWx1ZRJgCl57ImRpc2FibGVfaG9zdF9oZWFkZXJfZmFsbGJhY2siOnRydWUsImFjY2Vzc19sb2dnaW5nIjoiRlVMTCIsIm1ldHJpY19leHBpcnlfZHVyYXRpb24iOiIzNjAwcyJ9Gk8KE3N0YWNrZHJpdmVyX2luYm91bmQSF2Vudm95Lndhc20ucnVudGltZS5udWxsGh8KHRobZW52b3kud2FzbS5udWxsLnN0YWNrZHJpdmVy\"}}]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			test.SetEnvForTest(t, features.EnableECDSForStats.Name, "true")
			telemetry, _ := createTestTelemetries(tc.cfgs, t)
			telemetry.meshConfig.DefaultProviders = tc.defaultProviders
			got := telemetry.HTTPTypedExtensionConfigFilters(tc.proxy, tc.class)
			b, err := json.Marshal(got)
			assert.NoError(t, err)
			assert.Equal(t, tc.want, string(b))
		})
	}
}

func TestTCPTypedExtensionConfigFilters(t *testing.T) {
	t.Setenv("ENABLE_ECDS_FOR_STATS", "true")

	sidecar := &Proxy{
		ConfigNamespace: "default",
		Labels:          map[string]string{"app": "test"},
		Metadata:        &NodeMetadata{Labels: map[string]string{"app": "test"}},
		Type:            SidecarProxy,
	}

	emptyPrometheus := &telemetryv1a1.Telemetry{
		Metrics: []*telemetryv1a1.Metrics{
			{
				Providers: []*telemetryv1a1.ProviderRef{{Name: "prometheus"}},
			},
		},
	}

	tests := []struct {
		name             string
		cfgs             []config.Config
		proxy            *Proxy
		class            networking.ListenerClass
		defaultProviders *meshv1a1.MeshConfig_DefaultProviders
		want             string
	}{
		{
			"empty",
			nil,
			sidecar,
			networking.ListenerClassSidecarOutbound,
			nil,
			"[]",
		},
		{
			"router",
			[]config.Config{newTelemetry("istio-system", emptyPrometheus)},
			&Proxy{
				ConfigNamespace: "default",
				Labels:          map[string]string{"app": "test"},
				Metadata:        &NodeMetadata{Labels: map[string]string{"app": "test"}},
				Type:            Router,
			},
			networking.ListenerClassGateway,
			nil,
			// nolint: lll
			"[{\"name\":\"istio.io/telemetry/stats/prometheus/router/Gateway/TCP\",\"typed_config\":{\"type_url\":\"type.googleapis.com/stats.PluginConfig\",\"value\":\"MAE=\"}}]",
		},
		{
			"sidecar-outbound",
			[]config.Config{newTelemetry("istio-system", emptyPrometheus)},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			nil,
			// nolint: lll
			"[{\"name\":\"istio.io/telemetry/stats/prometheus/sidecar/Outbound/TCP\",\"typed_config\":{\"type_url\":\"type.googleapis.com/stats.PluginConfig\"}}]",
		},
		{
			"sidecar-inbound",
			[]config.Config{newTelemetry("istio-system", emptyPrometheus)},
			sidecar,
			networking.ListenerClassSidecarInbound,
			nil,
			// nolint: lll
			"[{\"name\":\"istio.io/telemetry/stats/prometheus/sidecar/Inbound/TCP\",\"typed_config\":{\"type_url\":\"type.googleapis.com/stats.PluginConfig\",\"value\":\"MAE=\"}}]",
		},
		{
			"stackdriver-inbound",
			nil,
			sidecar,
			networking.ListenerClassSidecarInbound,
			&meshv1a1.MeshConfig_DefaultProviders{
				Metrics:       []string{"stackdriver"},
				AccessLogging: []string{"stackdriver"},
			},
			// nolint: lll
			"[{\"name\":\"istio.io/telemetry/stats/stackdriver/sidecar/Inbound/TCP\",\"typed_config\":{\"type_url\":\"type.googleapis.com/envoy.extensions.filters.network.wasm.v3.Wasm\",\"value\":\"CvwBEhNzdGFja2RyaXZlcl9pbmJvdW5kIpMBCi90eXBlLmdvb2dsZWFwaXMuY29tL2dvb2dsZS5wcm90b2J1Zi5TdHJpbmdWYWx1ZRJgCl57ImRpc2FibGVfaG9zdF9oZWFkZXJfZmFsbGJhY2siOnRydWUsImFjY2Vzc19sb2dnaW5nIjoiRlVMTCIsIm1ldHJpY19leHBpcnlfZHVyYXRpb24iOiIzNjAwcyJ9Gk8KE3N0YWNrZHJpdmVyX2luYm91bmQSF2Vudm95Lndhc20ucnVudGltZS5udWxsGh8KHRobZW52b3kud2FzbS5udWxsLnN0YWNrZHJpdmVy\"}}]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			test.SetEnvForTest(t, features.EnableECDSForStats.Name, "true")
			telemetry, _ := createTestTelemetries(tc.cfgs, t)
			telemetry.meshConfig.DefaultProviders = tc.defaultProviders
			got := telemetry.TCPTypedExtensionConfigFilters(tc.proxy, tc.class)
			b, err := json.Marshal(got)
			assert.NoError(t, err)
			assert.Equal(t, tc.want, string(b))
		})
	}
}

func TestStatsECDSResourceName(t *testing.T) {
	cases := []struct {
		in       StatsConfig
		expected string
	}{
		{
			in: StatsConfig{
				Provider:         StatsProviderPrometheus,
				NodeType:         SidecarProxy,
				ListenerClass:    networking.ListenerClassSidecarInbound,
				ListenerProtocol: networking.ListenerProtocolHTTP,
			},
			expected: "istio.io/telemetry/stats/prometheus/sidecar/Inbound/HTTP",
		},
		{
			in: StatsConfig{
				Provider:         StatsProviderPrometheus,
				NodeType:         Waypoint,
				ListenerClass:    networking.ListenerClassSidecarInbound,
				ListenerProtocol: networking.ListenerProtocolHTTP,
			},
			expected: "istio.io/telemetry/stats/prometheus/waypoint/Inbound/HTTP",
		},
		{
			in: StatsConfig{
				Provider:         StatsProviderStackdriver,
				NodeType:         SidecarProxy,
				ListenerClass:    networking.ListenerClassSidecarInbound,
				ListenerProtocol: networking.ListenerProtocolHTTP,
			},
			expected: "istio.io/telemetry/stats/stackdriver/sidecar/Inbound/HTTP",
		},
		{
			in: StatsConfig{
				Provider:         StatsProviderStackdriver,
				NodeType:         Waypoint,
				ListenerClass:    networking.ListenerClassSidecarInbound,
				ListenerProtocol: networking.ListenerProtocolHTTP,
			},
			expected: "istio.io/telemetry/stats/stackdriver/waypoint/Inbound/HTTP",
		},
	}

	for _, tc := range cases {
		t.Run("", func(t *testing.T) {
			got := StatsECDSResourceName(tc.in)
			assert.Equal(t, tc.expected, got)
		})
	}
}
