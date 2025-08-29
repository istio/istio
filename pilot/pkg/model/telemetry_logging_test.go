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
	"reflect"
	"testing"

	accesslog "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	fileaccesslog "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/file/v3"
	grpcaccesslog "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/grpc/v3"
	otelaccesslog "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/open_telemetry/v3"
	otlpcommon "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/protobuf/types/known/structpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	"istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	tpb "istio.io/api/telemetry/v1alpha1"
	"istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/wellknown"
)

func TestFileAccessLogFormat(t *testing.T) {
	cases := []struct {
		name         string
		formatString string
		expected     string
	}{
		{
			name:     "empty",
			expected: EnvoyTextLogFormat,
		},
		{
			name:         "contains newline",
			formatString: "[%START_TIME%] %REQ(:METHOD)% %REQ(X-ENVOY-ORIGINAL-PATH?:PATH)% \n",
			expected:     "[%START_TIME%] %REQ(:METHOD)% %REQ(X-ENVOY-ORIGINAL-PATH?:PATH)% \n",
		},
		{
			name:         "miss newline",
			formatString: "[%START_TIME%] %REQ(:METHOD)% %REQ(X-ENVOY-ORIGINAL-PATH?:PATH)%",
			expected:     "[%START_TIME%] %REQ(:METHOD)% %REQ(X-ENVOY-ORIGINAL-PATH?:PATH)%\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := fileAccessLogFormat(tc.formatString)
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestAccessLogging(t *testing.T) {
	labels := map[string]string{"app": "test"}
	sidecar := &Proxy{
		ConfigNamespace: "default",
		Labels:          labels,
		Metadata:        &NodeMetadata{Labels: labels},
		IstioVersion:    &IstioVersion{Major: 1, Minor: 23},
	}
	waypoint := &Proxy{
		ConfigNamespace: "default",
		Type:            Waypoint,
		Labels:          map[string]string{label.IoK8sNetworkingGatewayGatewayName.Name: "waypoint"},
		Metadata:        &NodeMetadata{Labels: map[string]string{label.IoK8sNetworkingGatewayGatewayName.Name: "waypoint"}},
		IstioVersion:    &IstioVersion{Major: 1, Minor: 23},
	}
	prometheus := &tpb.Telemetry{
		Metrics: []*tpb.Metrics{
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy",
					},
				},
			},
		},
	}
	envoy := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy",
					},
				},
			},
		},
	}
	client := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{
				Match: &tpb.AccessLogging_LogSelector{
					Mode: tpb.WorkloadMode_CLIENT,
				},
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy",
					},
				},
			},
		},
	}
	clientDisabled := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{
				Match: &tpb.AccessLogging_LogSelector{
					Mode: tpb.WorkloadMode_CLIENT,
				},
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy",
					},
				},
				Disabled: &wrappers.BoolValue{
					Value: true,
				},
			},
		},
	}
	targetRefClient := &tpb.Telemetry{
		TargetRef: &v1beta1.PolicyTargetReference{
			Group: gvk.KubernetesGateway.Group,
			Kind:  gvk.KubernetesGateway.Kind,
			Name:  "waypoint",
		},
		AccessLogging: []*tpb.AccessLogging{
			{
				Match: &tpb.AccessLogging_LogSelector{
					Mode: tpb.WorkloadMode_CLIENT,
				},
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy",
					},
				},
			},
		},
	}
	sidecarClient := &tpb.Telemetry{
		Selector: &v1beta1.WorkloadSelector{
			MatchLabels: labels,
		},
		AccessLogging: []*tpb.AccessLogging{
			{
				Match: &tpb.AccessLogging_LogSelector{
					Mode: tpb.WorkloadMode_CLIENT,
				},
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy",
					},
				},
			},
		},
	}
	server := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{
				Match: &tpb.AccessLogging_LogSelector{
					Mode: tpb.WorkloadMode_SERVER,
				},
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy",
					},
				},
			},
		},
	}
	serverDisabled := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{
				Match: &tpb.AccessLogging_LogSelector{
					Mode: tpb.WorkloadMode_SERVER,
				},
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy",
					},
				},
				Disabled: &wrappers.BoolValue{
					Value: true,
				},
			},
		},
	}
	serverAndClient := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{
				Match: &tpb.AccessLogging_LogSelector{
					Mode: tpb.WorkloadMode_CLIENT_AND_SERVER,
				},
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy",
					},
				},
			},
		},
	}
	stackdriver := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "stackdriver",
					},
				},
			},
		},
	}
	empty := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{{}},
	}
	defaultJSON := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy-json",
					},
				},
			},
		},
	}
	disabled := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{
				Disabled: &wrappers.BoolValue{Value: true},
			},
		},
	}
	nonExistent := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "custom-provider",
					},
				},
			},
		},
	}
	multiAccessLogging := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy",
					},
				},
			},
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy-json",
					},
				},
			},
		},
	}
	multiAccessLoggingWithDisabled := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy",
					},
				},
			},
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy-json",
					},
				},
				Disabled: &wrappers.BoolValue{
					Value: true,
				},
			},
		},
	}
	multiAccessLoggingAndProviders := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy",
					},
					{
						Name: "envoy-json",
					},
				},
			},
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy",
					},
				},
			},
		},
	}
	multiFilters := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{
				Match: &tpb.AccessLogging_LogSelector{
					Mode: tpb.WorkloadMode_SERVER,
				},
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy",
					},
				},
			},
			{
				Match: &tpb.AccessLogging_LogSelector{
					Mode: tpb.WorkloadMode_CLIENT_AND_SERVER, // pickup last filter
				},
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy",
					},
				},
			},
		},
	}
	multiFiltersDisabled := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{
				Match: &tpb.AccessLogging_LogSelector{
					Mode: tpb.WorkloadMode_SERVER,
				},
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy",
					},
				},
			},
			{
				Match: &tpb.AccessLogging_LogSelector{
					Mode: tpb.WorkloadMode_CLIENT_AND_SERVER, // pickup last filter
				},
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy",
					},
				},
				Disabled: &wrappers.BoolValue{
					Value: true,
				},
			},
		},
	}
	serverAndClientDifferent := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{
				Match: &tpb.AccessLogging_LogSelector{
					Mode: tpb.WorkloadMode_SERVER,
				},
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy",
					},
				},
			},
			{
				Match: &tpb.AccessLogging_LogSelector{
					Mode: tpb.WorkloadMode_CLIENT,
				},
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy-json",
					},
				},
			},
		},
	}
	tests := []struct {
		name             string
		cfgs             []config.Config
		class            networking.ListenerClass
		proxy            *Proxy
		defaultProviders []string
		want             []string
	}{
		{
			"empty",
			nil,
			networking.ListenerClassSidecarOutbound,
			sidecar,
			nil,
			nil, // No Telemetry API configured, fall back to legacy mesh config setting
		},
		{
			"prometheus-mesh",
			[]config.Config{newTelemetry("istio-system", prometheus)},
			networking.ListenerClassSidecarOutbound,
			sidecar,
			nil,
			nil, // No Telemetry API configured, fall back to legacy mesh config setting
		},
		{
			"prometheus-namespace",
			[]config.Config{newTelemetry("default", prometheus)},
			networking.ListenerClassSidecarOutbound,
			sidecar,
			nil,
			nil, // No Telemetry API configured, fall back to legacy mesh config setting
		},
		{
			"prometheus-workload",
			[]config.Config{newTelemetry("default", &tpb.Telemetry{
				Selector: &v1beta1.WorkloadSelector{
					MatchLabels: labels,
				},
				Metrics: []*tpb.Metrics{
					{
						Providers: []*tpb.ProviderRef{
							{
								Name: "envoy",
							},
						},
					},
				},
			})},
			networking.ListenerClassSidecarOutbound,
			sidecar,
			nil,
			nil, // No Telemetry API configured, fall back to legacy mesh config setting
		},
		{
			"default provider only",
			nil,
			networking.ListenerClassSidecarOutbound,
			sidecar,
			[]string{"envoy"},
			[]string{"envoy"},
		},
		{
			"provider only",
			[]config.Config{newTelemetry("istio-system", envoy)},
			networking.ListenerClassSidecarOutbound,
			sidecar,
			nil,
			[]string{"envoy"},
		},
		{
			"client - gateway",
			[]config.Config{newTelemetry("istio-system", client)},
			networking.ListenerClassGateway,
			sidecar,
			nil,
			[]string{"envoy"},
		},
		{
			"client - gateway defined by targetRef",
			[]config.Config{newTelemetry("default", targetRefClient)},
			networking.ListenerClassGateway,
			waypoint,
			nil,
			[]string{"envoy"},
		},
		{
			"client - outbound",
			[]config.Config{newTelemetry("istio-system", client)},
			networking.ListenerClassSidecarOutbound,
			sidecar,
			nil,
			[]string{"envoy"},
		},
		{
			"client - inbound",
			[]config.Config{newTelemetry("istio-system", client)},
			networking.ListenerClassSidecarInbound,
			sidecar,
			nil,
			[]string{},
		},
		{
			"client - disabled server",
			[]config.Config{newTelemetry("istio-system", client), newTelemetry("default", serverDisabled)},
			networking.ListenerClassSidecarOutbound,
			sidecar,
			nil,
			[]string{"envoy"},
		},
		{
			"client - disabled client",
			[]config.Config{newTelemetry("istio-system", client), newTelemetry("default", clientDisabled)},
			networking.ListenerClassSidecarOutbound,
			sidecar,
			nil,
			[]string{},
		},
		{
			"client - disabled - enabled",
			[]config.Config{newTelemetry("istio-system", client), newTelemetry("default", clientDisabled), newTelemetry("default", sidecarClient)},
			networking.ListenerClassSidecarOutbound,
			sidecar,
			nil,
			[]string{"envoy"},
		},
		{
			"server - gateway",
			[]config.Config{newTelemetry("istio-system", server)},
			networking.ListenerClassGateway,
			sidecar,
			nil,
			[]string{},
		},
		{
			"server - inbound",
			[]config.Config{newTelemetry("istio-system", server)},
			networking.ListenerClassSidecarInbound,
			sidecar,
			nil,
			[]string{"envoy"},
		},
		{
			"server - outbound",
			[]config.Config{newTelemetry("istio-system", server)},
			networking.ListenerClassSidecarOutbound,
			sidecar,
			nil,
			[]string{},
		},
		{
			"server and client - gateway",
			[]config.Config{newTelemetry("istio-system", serverAndClient)},
			networking.ListenerClassGateway,
			sidecar,
			nil,
			[]string{"envoy"},
		},
		{
			"server and client - inbound",
			[]config.Config{newTelemetry("istio-system", serverAndClient)},
			networking.ListenerClassSidecarInbound,
			sidecar,
			nil,
			[]string{"envoy"},
		},
		{
			"server and client - outbound",
			[]config.Config{newTelemetry("istio-system", serverAndClient)},
			networking.ListenerClassSidecarOutbound,
			sidecar,
			nil,
			[]string{"envoy"},
		},
		{
			"override default",
			[]config.Config{newTelemetry("istio-system", envoy)},
			networking.ListenerClassSidecarOutbound,
			sidecar,
			[]string{"stackdriver"},
			[]string{"envoy"},
		},
		{
			"override namespace",
			[]config.Config{newTelemetry("istio-system", envoy), newTelemetry("default", defaultJSON)},
			networking.ListenerClassSidecarOutbound,
			sidecar,
			nil,
			[]string{"envoy-json"},
		},
		{
			"empty config inherits",
			[]config.Config{newTelemetry("istio-system", envoy), newTelemetry("default", empty)},
			networking.ListenerClassSidecarOutbound,
			sidecar,
			nil,
			[]string{"envoy"},
		},
		{
			"stackdriver",
			[]config.Config{newTelemetry("istio-system", envoy), newTelemetry("default", stackdriver)},
			networking.ListenerClassSidecarOutbound,
			sidecar,
			nil,
			[]string{},
		},
		{
			"default envoy JSON",
			[]config.Config{newTelemetry("istio-system", defaultJSON)},
			networking.ListenerClassSidecarOutbound,
			sidecar,
			nil,
			[]string{"envoy-json"},
		},
		{
			"disable config",
			[]config.Config{newTelemetry("istio-system", envoy), newTelemetry("default", disabled)},
			networking.ListenerClassSidecarOutbound,
			sidecar,
			nil,
			[]string{},
		},
		{
			"disable default",
			[]config.Config{newTelemetry("default", disabled)},
			networking.ListenerClassSidecarOutbound,
			sidecar,
			[]string{"envoy"},
			[]string{},
		},
		{
			"non existing",
			[]config.Config{newTelemetry("default", nonExistent)},
			networking.ListenerClassSidecarOutbound,
			sidecar,
			[]string{"envoy"},
			[]string{},
		},
		{
			"server - multi filters",
			[]config.Config{newTelemetry("istio-system", multiFilters)},
			networking.ListenerClassSidecarOutbound,
			sidecar,
			nil,
			[]string{"envoy"},
		},
		{
			"server - multi filters disabled",
			[]config.Config{newTelemetry("istio-system", multiFiltersDisabled)},
			networking.ListenerClassSidecarOutbound,
			sidecar,
			nil,
			[]string{},
		},
		{
			"multi accesslogging",
			[]config.Config{newTelemetry("istio-system", envoy), newTelemetry("default", multiAccessLogging)},
			networking.ListenerClassSidecarOutbound,
			sidecar,
			nil,
			[]string{"envoy", "envoy-json"},
		},
		{
			"multi accesslogging with disabled",
			[]config.Config{newTelemetry("istio-system", envoy), newTelemetry("default", multiAccessLoggingWithDisabled)},
			networking.ListenerClassSidecarOutbound,
			sidecar,
			nil,
			[]string{"envoy"},
		},
		{
			"multi accesslogging - multi providers",
			[]config.Config{newTelemetry("istio-system", envoy), newTelemetry("default", multiAccessLoggingAndProviders)},
			networking.ListenerClassSidecarOutbound,
			sidecar,
			nil,
			[]string{"envoy", "envoy-json"},
		},
		{
			"server and client different - inbound",
			[]config.Config{newTelemetry("istio-system", serverAndClientDifferent)},
			networking.ListenerClassSidecarInbound,
			sidecar,
			nil,
			[]string{"envoy"},
		},
		{
			"server and client different - outbound",
			[]config.Config{newTelemetry("istio-system", serverAndClientDifferent)},
			networking.ListenerClassSidecarOutbound,
			sidecar,
			nil,
			[]string{"envoy-json"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			telemetry, ctx := createTestTelemetries(tt.cfgs, t)
			telemetry.meshConfig.DefaultProviders.AccessLogging = tt.defaultProviders
			var got []string
			cfgs := telemetry.AccessLogging(ctx, tt.proxy, tt.class, nil)
			if cfgs != nil {
				got = []string{}
				for _, p := range cfgs {
					if p.Disabled {
						continue
					}
					got = append(got, p.Provider.Name)
				}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestAccessLoggingWithFilter(t *testing.T) {
	sidecar := &Proxy{
		ConfigNamespace: "default",
		Labels:          map[string]string{"app": "test"},
		Metadata:        &NodeMetadata{},
		IstioVersion:    &IstioVersion{Major: 1, Minor: 23},
	}
	code400filter := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy-json",
					},
				},
				Filter: &tpb.AccessLogging_Filter{
					Expression: "response.code >= 400",
				},
			},
		},
	}
	code500filter := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy-json",
					},
				},
				Filter: &tpb.AccessLogging_Filter{
					Expression: "response.code >= 500",
				},
			},
		},
	}
	multiAccessLoggingFilter := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy-json",
					},
				},
				Filter: &tpb.AccessLogging_Filter{
					Expression: "response.code >= 500",
				},
			},
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy-json",
					},
				},
				Filter: &tpb.AccessLogging_Filter{
					Expression: "response.code >= 400",
				},
			},
		},
	}
	multiAccessLoggingNilFilter := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy-json",
					},
				},
				Filter: &tpb.AccessLogging_Filter{
					Expression: "response.code >= 500",
				},
			},
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy-json",
					},
				},
			},
		},
	}
	serverAndClientDifferent := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{
				Match: &tpb.AccessLogging_LogSelector{
					Mode: tpb.WorkloadMode_CLIENT,
				},
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy-json",
					},
				},
				Filter: &tpb.AccessLogging_Filter{
					Expression: "response.code >= 500",
				},
			},
			{
				Match: &tpb.AccessLogging_LogSelector{
					Mode: tpb.WorkloadMode_SERVER,
				},
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy-json",
					},
				},
				Filter: &tpb.AccessLogging_Filter{
					Expression: "response.code >= 400",
				},
			},
		},
	}

	tests := []struct {
		name             string
		cfgs             []config.Config
		proxy            *Proxy
		defaultProviders []string
		excepted         []LoggingConfig
	}{
		{
			"filter",
			[]config.Config{newTelemetry("default", code400filter)},
			sidecar,
			[]string{"envoy"},
			[]LoggingConfig{
				{
					AccessLog: &accesslog.AccessLog{
						Name:       wellknown.FileAccessLog,
						ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(defaultJSONLabelsOut)},
					},
					Provider: jsonTextProvider,
					Filter: &tpb.AccessLogging_Filter{
						Expression: "response.code >= 400",
					},
				},
			},
		},
		{
			"namespace-filter",
			[]config.Config{newTelemetry("istio-system", code400filter), newTelemetry("default", code500filter)},
			sidecar,
			[]string{"envoy"},
			[]LoggingConfig{
				{
					AccessLog: &accesslog.AccessLog{
						Name:       wellknown.FileAccessLog,
						ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(defaultJSONLabelsOut)},
					},
					Provider: jsonTextProvider,
					Filter: &tpb.AccessLogging_Filter{
						Expression: "response.code >= 500",
					},
				},
			},
		},
		{
			"multi-accesslogging",
			[]config.Config{newTelemetry("default", multiAccessLoggingFilter)},
			sidecar,
			[]string{"envoy"},
			[]LoggingConfig{
				{
					AccessLog: &accesslog.AccessLog{
						Name:       wellknown.FileAccessLog,
						ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(defaultJSONLabelsOut)},
					},
					Provider: jsonTextProvider,
					Filter: &tpb.AccessLogging_Filter{
						Expression: "response.code >= 400",
					},
				},
			},
		},
		{
			"multi-accesslogging-nil",
			[]config.Config{newTelemetry("default", multiAccessLoggingNilFilter)},
			sidecar,
			[]string{"envoy"},
			[]LoggingConfig{
				{
					AccessLog: &accesslog.AccessLog{
						Name:       wellknown.FileAccessLog,
						ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(defaultJSONLabelsOut)},
					},
					Provider: jsonTextProvider,
				},
			},
		},
		{
			"server-and-client-different",
			[]config.Config{newTelemetry("default", serverAndClientDifferent)},
			sidecar,
			[]string{"envoy"},
			[]LoggingConfig{
				{
					AccessLog: &accesslog.AccessLog{
						Name:       wellknown.FileAccessLog,
						ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(defaultJSONLabelsOut)},
					},
					Provider: jsonTextProvider,
					Filter: &tpb.AccessLogging_Filter{
						Expression: "response.code >= 500",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			telemetry, ctx := createTestTelemetries(tt.cfgs, t)
			telemetry.meshConfig.DefaultProviders.AccessLogging = tt.defaultProviders
			got := telemetry.AccessLogging(ctx, tt.proxy, networking.ListenerClassSidecarOutbound, nil)
			assert.Equal(t, tt.excepted, got)
		})
	}
}

func TestAccessLoggingCache(t *testing.T) {
	sidecar := &Proxy{
		ConfigNamespace: "default",
		Metadata:        &NodeMetadata{Labels: map[string]string{"app": "test"}},
		IstioVersion:    &IstioVersion{Major: 1, Minor: 23},
	}
	otherNamespace := &Proxy{
		ConfigNamespace: "common",
		Metadata:        &NodeMetadata{Labels: map[string]string{"app": "test"}},
		IstioVersion:    &IstioVersion{Major: 1, Minor: 23},
	}
	cfgs := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy-json",
					},
				},
				Filter: &tpb.AccessLogging_Filter{
					Expression: "response.code >= 400",
				},
			},
		},
	}

	telemetry, ctx := createTestTelemetries([]config.Config{newTelemetry("default", cfgs)}, t)
	for _, s := range []*Proxy{sidecar, otherNamespace} {
		t.Run(s.ConfigNamespace, func(t *testing.T) {
			first := telemetry.AccessLogging(ctx, s, networking.ListenerClassSidecarOutbound, nil)
			second := telemetry.AccessLogging(ctx, s, networking.ListenerClassSidecarOutbound, nil)
			assert.Equal(t, first, second)
		})
	}
}

func TestBuildOpenTelemetryAccessLogConfig(t *testing.T) {
	sidecar := &Proxy{
		ConfigNamespace: "default",
		Labels:          map[string]string{"app": "test"},
		Metadata:        &NodeMetadata{},
		IstioVersion:    &IstioVersion{Major: 1, Minor: 23},
	}

	fakeCluster := "outbound|55680||otel-collector.monitoring.svc.cluster.local"
	fakeAuthority := "otel-collector.monitoring.svc.cluster.local"
	for _, tc := range []struct {
		name                 string
		logName              string
		clusterName          string
		hostname             string
		body                 string
		labels               *structpb.Struct
		skipBuiltInFormatter bool
		expected             *otelaccesslog.OpenTelemetryAccessLogConfig
		proxyVersion         *IstioVersion
	}{
		{
			name:        "default",
			logName:     OtelEnvoyAccessLogFriendlyName,
			clusterName: fakeCluster,
			hostname:    fakeAuthority,
			body:        EnvoyTextLogFormat,
			expected: &otelaccesslog.OpenTelemetryAccessLogConfig{
				CommonConfig: &grpcaccesslog.CommonGrpcAccessLogConfig{
					LogName: OtelEnvoyAccessLogFriendlyName,
					GrpcService: &core.GrpcService{
						TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
							EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
								ClusterName: fakeCluster,
								Authority:   fakeAuthority,
							},
						},
					},
					TransportApiVersion:     core.ApiVersion_V3,
					FilterStateObjectsToLog: envoyWasmStateToLog,
				},
				DisableBuiltinLabels: true,
				Body: &otlpcommon.AnyValue{
					Value: &otlpcommon.AnyValue_StringValue{
						StringValue: EnvoyTextLogFormat,
					},
				},
			},
		},
		{
			name:        "with attrs",
			logName:     OtelEnvoyAccessLogFriendlyName,
			clusterName: fakeCluster,
			hostname:    fakeAuthority,
			body:        EnvoyTextLogFormat,
			labels: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"protocol": {Kind: &structpb.Value_StringValue{StringValue: "%PROTOCOL%"}},
					"host":     {Kind: &structpb.Value_StringValue{StringValue: "%CEL(request.host)%"}},
				},
			},
			expected: &otelaccesslog.OpenTelemetryAccessLogConfig{
				CommonConfig: &grpcaccesslog.CommonGrpcAccessLogConfig{
					LogName: OtelEnvoyAccessLogFriendlyName,
					GrpcService: &core.GrpcService{
						TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
							EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
								ClusterName: fakeCluster,
								Authority:   fakeAuthority,
							},
						},
					},
					TransportApiVersion:     core.ApiVersion_V3,
					FilterStateObjectsToLog: envoyWasmStateToLog,
				},
				DisableBuiltinLabels: true,
				Body: &otlpcommon.AnyValue{
					Value: &otlpcommon.AnyValue_StringValue{
						StringValue: EnvoyTextLogFormat,
					},
				},
				Formatters: []*core.TypedExtensionConfig{
					celFormatter,
				},
				Attributes: &otlpcommon.KeyValueList{
					Values: []*otlpcommon.KeyValue{
						{
							Key:   "host",
							Value: &otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_StringValue{StringValue: "%CEL(request.host)%"}},
						},
						{
							Key:   "protocol",
							Value: &otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_StringValue{StringValue: "%PROTOCOL%"}},
						},
					},
				},
			},
		},
		{
			name:        "skip builtin",
			logName:     OtelEnvoyAccessLogFriendlyName,
			clusterName: fakeCluster,
			hostname:    fakeAuthority,
			body:        EnvoyTextLogFormat,
			labels: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"protocol": {Kind: &structpb.Value_StringValue{StringValue: "%PROTOCOL%"}},
					"host":     {Kind: &structpb.Value_StringValue{StringValue: "%CEL(request.host)%"}},
				},
			},
			skipBuiltInFormatter: true,
			expected: &otelaccesslog.OpenTelemetryAccessLogConfig{
				CommonConfig: &grpcaccesslog.CommonGrpcAccessLogConfig{
					LogName: OtelEnvoyAccessLogFriendlyName,
					GrpcService: &core.GrpcService{
						TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
							EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
								ClusterName: fakeCluster,
								Authority:   fakeAuthority,
							},
						},
					},
					TransportApiVersion:     core.ApiVersion_V3,
					FilterStateObjectsToLog: envoyWasmStateToLog,
				},
				DisableBuiltinLabels: true,
				Body: &otlpcommon.AnyValue{
					Value: &otlpcommon.AnyValue_StringValue{
						StringValue: EnvoyTextLogFormat,
					},
				},
				Attributes: &otlpcommon.KeyValueList{
					Values: []*otlpcommon.KeyValue{
						{
							Key:   "host",
							Value: &otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_StringValue{StringValue: "%CEL(request.host)%"}},
						},
						{
							Key:   "protocol",
							Value: &otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_StringValue{StringValue: "%PROTOCOL%"}},
						},
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := buildOpenTelemetryAccessLogConfig(sidecar, tc.logName, tc.hostname,
				tc.clusterName, tc.body, tc.labels, tc.skipBuiltInFormatter)
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestTelemetryAccessLogExhaustiveness(t *testing.T) {
	AssertProvidersHandled(telemetryAccessLogHandled)
}

func TestTelemetryAccessLog(t *testing.T) {
	stdoutFormat := &meshconfig.MeshConfig_ExtensionProvider{
		Name: "stdout",
		Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
			EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
				Path: DevStdout,
			},
		},
	}

	customTextFormat := &meshconfig.MeshConfig_ExtensionProvider{
		Name: "custom-text",
		Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
			EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
				Path: DevStdout,
				LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat{
					LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat_Text{
						Text: "%LOCAL_REPLY_BODY%:%RESPONSE_CODE%:path=%REQ(:path)%",
					},
				},
			},
		},
	}

	customLabelsFormat := &meshconfig.MeshConfig_ExtensionProvider{
		Name: "custom-label",
		Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
			EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
				Path: DevStdout,
				LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat{
					LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat_Labels{
						Labels: &structpb.Struct{
							Fields: map[string]*structpb.Value{
								"start_time":                     {Kind: &structpb.Value_StringValue{StringValue: "%START_TIME%"}},
								"route_name":                     {Kind: &structpb.Value_StringValue{StringValue: "%ROUTE_NAME%"}},
								"method":                         {Kind: &structpb.Value_StringValue{StringValue: "%REQ(:METHOD)%"}},
								"path":                           {Kind: &structpb.Value_StringValue{StringValue: "%REQ(X-ENVOY-ORIGINAL-PATH?:PATH)%"}},
								"protocol":                       {Kind: &structpb.Value_StringValue{StringValue: "%PROTOCOL%"}},
								"response_code":                  {Kind: &structpb.Value_StringValue{StringValue: "%RESPONSE_CODE%"}},
								"response_flags":                 {Kind: &structpb.Value_StringValue{StringValue: "%RESPONSE_FLAGS%"}},
								"response_code_details":          {Kind: &structpb.Value_StringValue{StringValue: "%RESPONSE_CODE_DETAILS%"}},
								"connection_termination_details": {Kind: &structpb.Value_StringValue{StringValue: "%CONNECTION_TERMINATION_DETAILS%"}},
								"bytes_received":                 {Kind: &structpb.Value_StringValue{StringValue: "%BYTES_RECEIVED%"}},
								"bytes_sent":                     {Kind: &structpb.Value_StringValue{StringValue: "%BYTES_SENT%"}},
								"duration":                       {Kind: &structpb.Value_StringValue{StringValue: "%DURATION%"}},
								"upstream_service_time":          {Kind: &structpb.Value_StringValue{StringValue: "%RESP(X-ENVOY-UPSTREAM-SERVICE-TIME)%"}},
								"x_forwarded_for":                {Kind: &structpb.Value_StringValue{StringValue: "%REQ(X-FORWARDED-FOR)%"}},
								"user_agent":                     {Kind: &structpb.Value_StringValue{StringValue: "%REQ(USER-AGENT)%"}},
								"request_id":                     {Kind: &structpb.Value_StringValue{StringValue: "%REQ(X-REQUEST-ID)%"}},
								"authority":                      {Kind: &structpb.Value_StringValue{StringValue: "%REQ(:AUTHORITY)%"}},
								"upstream_host":                  {Kind: &structpb.Value_StringValue{StringValue: "%UPSTREAM_HOST%"}},
								"upstream_cluster":               {Kind: &structpb.Value_StringValue{StringValue: "%UPSTREAM_CLUSTER%"}},
								"upstream_local_address":         {Kind: &structpb.Value_StringValue{StringValue: "%UPSTREAM_LOCAL_ADDRESS%"}},
								"downstream_local_address":       {Kind: &structpb.Value_StringValue{StringValue: "%DOWNSTREAM_LOCAL_ADDRESS%"}},
								"downstream_remote_address":      {Kind: &structpb.Value_StringValue{StringValue: "%DOWNSTREAM_REMOTE_ADDRESS%"}},
								"requested_server_name":          {Kind: &structpb.Value_StringValue{StringValue: "%REQUESTED_SERVER_NAME%"}},
							},
						},
					},
				},
			},
		},
	}

	stderr := &meshconfig.MeshConfig_ExtensionProvider{
		Name: "stderr",
		Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
			EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
				Path: "/dev/stderr",
			},
		},
	}

	fakeFilterStateObjects := []string{"fake-filter-state-object1", "fake-filter-state-object1"}
	grpcHTTPCfg := &meshconfig.MeshConfig_ExtensionProvider{
		Name: "grpc-http-als",
		Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyHttpAls{
			EnvoyHttpAls: &meshconfig.MeshConfig_ExtensionProvider_EnvoyHttpGrpcV3LogProvider{
				LogName:                         "grpc-http-als",
				Service:                         "grpc-als.foo.svc.cluster.local",
				Port:                            9811,
				AdditionalRequestHeadersToLog:   []string{"fake-request-header1"},
				AdditionalResponseHeadersToLog:  []string{"fake-response-header1"},
				AdditionalResponseTrailersToLog: []string{"fake-response-trailer1"},
				FilterStateObjectsToLog:         fakeFilterStateObjects,
			},
		},
	}

	grpcTCPCfg := &meshconfig.MeshConfig_ExtensionProvider{
		Name: "grpc-tcp-als",
		Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyTcpAls{
			EnvoyTcpAls: &meshconfig.MeshConfig_ExtensionProvider_EnvoyTcpGrpcV3LogProvider{
				LogName:                 "grpc-tcp-als",
				Service:                 "grpc-als.foo.svc.cluster.local",
				Port:                    9811,
				FilterStateObjectsToLog: fakeFilterStateObjects,
			},
		},
	}

	labels := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"protocol":   {Kind: &structpb.Value_StringValue{StringValue: "%PROTOCOL%"}},
			"start_time": {Kind: &structpb.Value_StringValue{StringValue: "%START_TIME%"}},
		},
	}

	otelCfg := &meshconfig.MeshConfig_ExtensionProvider{
		Name: OtelEnvoyAccessLogFriendlyName,
		Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyOtelAls{
			EnvoyOtelAls: &meshconfig.MeshConfig_ExtensionProvider_EnvoyOpenTelemetryLogProvider{
				Service: "otel.foo.svc.cluster.local",
				Port:    9811,
				LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyOpenTelemetryLogProvider_LogFormat{
					Labels: labels,
				},
			},
		},
	}

	otelCfgWithFormatters := &meshconfig.MeshConfig_ExtensionProvider{
		Name: OtelEnvoyAccessLogFriendlyName,
		Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyOtelAls{
			EnvoyOtelAls: &meshconfig.MeshConfig_ExtensionProvider_EnvoyOpenTelemetryLogProvider{
				Service: "otel.foo.svc.cluster.local",
				Port:    9811,
				LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyOpenTelemetryLogProvider_LogFormat{
					Text: EnvoyTextLogFormat + " %REQ_WITHOUT_QUERY(key1:val1)% REQ_WITHOUT_QUERY(key2:val1)% %METADATA(UPSTREAM_HOST:istio)% %METADATA(CLUSTER:istio)%",
					Labels: &structpb.Struct{
						Fields: map[string]*structpb.Value{
							"key1": {Kind: &structpb.Value_StringValue{StringValue: "%METADATA(CLUSTER:istio)%"}},
							"key2": {Kind: &structpb.Value_StringValue{StringValue: "%REQ_WITHOUT_QUERY(key1:val1)%"}},
							"key3": {Kind: &structpb.Value_StringValue{StringValue: "%CEL(request.host)%"}},
						},
					},
				},
			},
		},
	}

	defaultEnvoyProvider := &meshconfig.MeshConfig_ExtensionProvider{
		Name: "envoy",
		Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
			EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
				Path: "/dev/stdout",
			},
		},
	}

	grpcBackendClusterName := "outbound|9811||grpc-als.foo.svc.cluster.local"
	grpcBackendAuthority := "grpc-als.foo.svc.cluster.local"
	otelAttributeCfg := &otelaccesslog.OpenTelemetryAccessLogConfig{
		CommonConfig: &grpcaccesslog.CommonGrpcAccessLogConfig{
			LogName: OtelEnvoyAccessLogFriendlyName,
			GrpcService: &core.GrpcService{
				TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
					EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
						ClusterName: grpcBackendClusterName,
						Authority:   grpcBackendAuthority,
					},
				},
			},
			TransportApiVersion:     core.ApiVersion_V3,
			FilterStateObjectsToLog: envoyWasmStateToLog,
		},
		DisableBuiltinLabels: true,
		Body: &otlpcommon.AnyValue{
			Value: &otlpcommon.AnyValue_StringValue{
				StringValue: EnvoyTextLogFormat,
			},
		},
		Attributes: &otlpcommon.KeyValueList{
			Values: []*otlpcommon.KeyValue{
				{
					Key:   "protocol",
					Value: &otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_StringValue{StringValue: "%PROTOCOL%"}},
				},
				{
					Key:   "start_time",
					Value: &otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_StringValue{StringValue: "%START_TIME%"}},
				},
			},
		},
	}
	otelWithFormattersCfg := &otelaccesslog.OpenTelemetryAccessLogConfig{
		CommonConfig: &grpcaccesslog.CommonGrpcAccessLogConfig{
			LogName: OtelEnvoyAccessLogFriendlyName,
			GrpcService: &core.GrpcService{
				TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
					EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
						ClusterName: grpcBackendClusterName,
						Authority:   grpcBackendAuthority,
					},
				},
			},
			TransportApiVersion:     core.ApiVersion_V3,
			FilterStateObjectsToLog: envoyWasmStateToLog,
		},
		DisableBuiltinLabels: true,
		Body: &otlpcommon.AnyValue{
			Value: &otlpcommon.AnyValue_StringValue{
				StringValue: EnvoyTextLogFormat + " %REQ_WITHOUT_QUERY(key1:val1)% REQ_WITHOUT_QUERY(key2:val1)% %METADATA(UPSTREAM_HOST:istio)% %METADATA(CLUSTER:istio)%",
			},
		},
		Attributes: &otlpcommon.KeyValueList{
			Values: []*otlpcommon.KeyValue{
				{
					Key:   "key1",
					Value: &otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_StringValue{StringValue: "%METADATA(CLUSTER:istio)%"}},
				},
				{
					Key:   "key2",
					Value: &otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_StringValue{StringValue: "%REQ_WITHOUT_QUERY(key1:val1)%"}},
				},
				{
					Key:   "key3",
					Value: &otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_StringValue{StringValue: "%CEL(request.host)%"}},
				},
			},
		},
		Formatters: []*core.TypedExtensionConfig{
			celFormatter,
			metadataFormatter,
			reqWithoutQueryFormatter,
		},
	}

	clusterLookupFn = func(push *PushContext, service string, port int) (hostname string, cluster string, err error) {
		return grpcBackendAuthority, grpcBackendClusterName, nil
	}

	stdout := &fileaccesslog.FileAccessLog{
		Path: DevStdout,
		AccessLogFormat: &fileaccesslog.FileAccessLog_LogFormat{
			LogFormat: &core.SubstitutionFormatString{
				Format: &core.SubstitutionFormatString_TextFormatSource{
					TextFormatSource: &core.DataSource{
						Specifier: &core.DataSource_InlineString{
							InlineString: EnvoyTextLogFormat,
						},
					},
				},
			},
		},
	}

	customTextOut := &fileaccesslog.FileAccessLog{
		Path: DevStdout,
		AccessLogFormat: &fileaccesslog.FileAccessLog_LogFormat{
			LogFormat: &core.SubstitutionFormatString{
				Format: &core.SubstitutionFormatString_TextFormatSource{
					TextFormatSource: &core.DataSource{
						Specifier: &core.DataSource_InlineString{
							InlineString: "%LOCAL_REPLY_BODY%:%RESPONSE_CODE%:path=%REQ(:path)%\n",
						},
					},
				},
			},
		},
	}

	customLabelsOut := &fileaccesslog.FileAccessLog{
		Path: DevStdout,
		AccessLogFormat: &fileaccesslog.FileAccessLog_LogFormat{
			LogFormat: &core.SubstitutionFormatString{
				Format: &core.SubstitutionFormatString_JsonFormat{
					JsonFormat: &structpb.Struct{
						Fields: map[string]*structpb.Value{
							"start_time":                     {Kind: &structpb.Value_StringValue{StringValue: "%START_TIME%"}},
							"route_name":                     {Kind: &structpb.Value_StringValue{StringValue: "%ROUTE_NAME%"}},
							"method":                         {Kind: &structpb.Value_StringValue{StringValue: "%REQ(:METHOD)%"}},
							"path":                           {Kind: &structpb.Value_StringValue{StringValue: "%REQ(X-ENVOY-ORIGINAL-PATH?:PATH)%"}},
							"protocol":                       {Kind: &structpb.Value_StringValue{StringValue: "%PROTOCOL%"}},
							"response_code":                  {Kind: &structpb.Value_StringValue{StringValue: "%RESPONSE_CODE%"}},
							"response_flags":                 {Kind: &structpb.Value_StringValue{StringValue: "%RESPONSE_FLAGS%"}},
							"response_code_details":          {Kind: &structpb.Value_StringValue{StringValue: "%RESPONSE_CODE_DETAILS%"}},
							"connection_termination_details": {Kind: &structpb.Value_StringValue{StringValue: "%CONNECTION_TERMINATION_DETAILS%"}},
							"bytes_received":                 {Kind: &structpb.Value_StringValue{StringValue: "%BYTES_RECEIVED%"}},
							"bytes_sent":                     {Kind: &structpb.Value_StringValue{StringValue: "%BYTES_SENT%"}},
							"duration":                       {Kind: &structpb.Value_StringValue{StringValue: "%DURATION%"}},
							"upstream_service_time":          {Kind: &structpb.Value_StringValue{StringValue: "%RESP(X-ENVOY-UPSTREAM-SERVICE-TIME)%"}},
							"x_forwarded_for":                {Kind: &structpb.Value_StringValue{StringValue: "%REQ(X-FORWARDED-FOR)%"}},
							"user_agent":                     {Kind: &structpb.Value_StringValue{StringValue: "%REQ(USER-AGENT)%"}},
							"request_id":                     {Kind: &structpb.Value_StringValue{StringValue: "%REQ(X-REQUEST-ID)%"}},
							"authority":                      {Kind: &structpb.Value_StringValue{StringValue: "%REQ(:AUTHORITY)%"}},
							"upstream_host":                  {Kind: &structpb.Value_StringValue{StringValue: "%UPSTREAM_HOST%"}},
							"upstream_cluster":               {Kind: &structpb.Value_StringValue{StringValue: "%UPSTREAM_CLUSTER%"}},
							"upstream_local_address":         {Kind: &structpb.Value_StringValue{StringValue: "%UPSTREAM_LOCAL_ADDRESS%"}},
							"downstream_local_address":       {Kind: &structpb.Value_StringValue{StringValue: "%DOWNSTREAM_LOCAL_ADDRESS%"}},
							"downstream_remote_address":      {Kind: &structpb.Value_StringValue{StringValue: "%DOWNSTREAM_REMOTE_ADDRESS%"}},
							"requested_server_name":          {Kind: &structpb.Value_StringValue{StringValue: "%REQUESTED_SERVER_NAME%"}},
						},
					},
				},
				JsonFormatOptions: &core.JsonFormatOptions{SortProperties: false},
			},
		},
	}

	stderrout := &fileaccesslog.FileAccessLog{
		Path: "/dev/stderr",
		AccessLogFormat: &fileaccesslog.FileAccessLog_LogFormat{
			LogFormat: &core.SubstitutionFormatString{
				Format: &core.SubstitutionFormatString_TextFormatSource{
					TextFormatSource: &core.DataSource{
						Specifier: &core.DataSource_InlineString{
							InlineString: EnvoyTextLogFormat,
						},
					},
				},
			},
		},
	}

	grpcHTTPout := &grpcaccesslog.HttpGrpcAccessLogConfig{
		CommonConfig: &grpcaccesslog.CommonGrpcAccessLogConfig{
			LogName: "grpc-http-als",
			GrpcService: &core.GrpcService{
				TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
					EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
						ClusterName: grpcBackendClusterName,
						Authority:   grpcBackendAuthority,
					},
				},
			},
			TransportApiVersion:     core.ApiVersion_V3,
			FilterStateObjectsToLog: fakeFilterStateObjects,
		},
		AdditionalRequestHeadersToLog:   []string{"fake-request-header1"},
		AdditionalResponseHeadersToLog:  []string{"fake-response-header1"},
		AdditionalResponseTrailersToLog: []string{"fake-response-trailer1"},
	}

	grpcTCPOut := &grpcaccesslog.TcpGrpcAccessLogConfig{
		CommonConfig: &grpcaccesslog.CommonGrpcAccessLogConfig{
			LogName: "grpc-tcp-als",
			GrpcService: &core.GrpcService{
				TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
					EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
						ClusterName: grpcBackendClusterName,
						Authority:   grpcBackendAuthority,
					},
				},
			},
			TransportApiVersion:     core.ApiVersion_V3,
			FilterStateObjectsToLog: fakeFilterStateObjects,
		},
	}

	defaultFormatJSON, _ := protomarshal.ToJSON(EnvoyJSONLogFormatIstio)

	ctx := NewPushContext()
	ctx.ServiceIndex.HostnameAndNamespace["otel-collector.foo.svc.cluster.local"] = map[string]*Service{
		"foo": {
			Hostname:       "otel-collector.foo.svc.cluster.local",
			DefaultAddress: "172.217.0.0/16",
			Ports: PortList{
				&Port{
					Name:     "grpc-port",
					Port:     3417,
					Protocol: protocol.TCP,
				},
				&Port{
					Name:     "http-port",
					Port:     3418,
					Protocol: protocol.HTTP,
				},
			},
			Resolution: ClientSideLB,
			Attributes: ServiceAttributes{
				Name:            "otel-collector",
				Namespace:       "foo",
				ServiceRegistry: provider.Kubernetes,
			},
		},
	}

	sidecar := &Proxy{
		ConfigNamespace: "default",
		Labels:          map[string]string{"app": "test"},
		Metadata:        &NodeMetadata{},
		IstioVersion:    &IstioVersion{Major: 1, Minor: 23},
	}

	for _, tc := range []struct {
		name       string
		ctx        *PushContext
		meshConfig *meshconfig.MeshConfig
		fp         *meshconfig.MeshConfig_ExtensionProvider
		expected   *accesslog.AccessLog
	}{
		{
			name: "stdout",
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			fp: stdoutFormat,
			expected: &accesslog.AccessLog{
				Name:       wellknown.FileAccessLog,
				ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(stdout)},
			},
		},
		{
			name: "stderr",
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			fp: stderr,
			expected: &accesslog.AccessLog{
				Name:       wellknown.FileAccessLog,
				ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(stderrout)},
			},
		},
		{
			name: "custom-text",
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			fp: customTextFormat,
			expected: &accesslog.AccessLog{
				Name:       wellknown.FileAccessLog,
				ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(customTextOut)},
			},
		},
		{
			name: "default-labels",
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			fp: jsonTextProvider,
			expected: &accesslog.AccessLog{
				Name:       wellknown.FileAccessLog,
				ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(defaultJSONLabelsOut)},
			},
		},
		{
			name: "custom-labels",
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			fp: customLabelsFormat,
			expected: &accesslog.AccessLog{
				Name:       wellknown.FileAccessLog,
				ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(customLabelsOut)},
			},
		},
		{
			name: "otel",
			ctx:  ctx,
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			fp: otelCfg,
			expected: &accesslog.AccessLog{
				Name:       OtelEnvoyALSName,
				ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(otelAttributeCfg)},
			},
		},
		{
			name: "otel-with-formatters",
			ctx:  ctx,
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			fp: otelCfgWithFormatters,
			expected: &accesslog.AccessLog{
				Name:       OtelEnvoyALSName,
				ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(otelWithFormattersCfg)},
			},
		},
		{
			name: "grpc-http",
			fp:   grpcHTTPCfg,
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			expected: &accesslog.AccessLog{
				Name:       wellknown.HTTPGRPCAccessLog,
				ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(grpcHTTPout)},
			},
		},
		{
			name: "grpc-tcp",
			fp:   grpcTCPCfg,
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			expected: &accesslog.AccessLog{
				Name:       TCPEnvoyALSName,
				ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(grpcTCPOut)},
			},
		},
		{
			name: "builtin-fallback",
			ctx:  ctx,
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_JSON,
				AccessLogFormat:   defaultFormatJSON,
			},
			fp: defaultEnvoyProvider,
			expected: &accesslog.AccessLog{
				Name:       wellknown.FileAccessLog,
				ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(defaultJSONLabelsOut)},
			},
		},
		{
			name: "enable-omit-empty-values",
			ctx:  ctx,
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			fp: &meshconfig.MeshConfig_ExtensionProvider{
				Name: "envoy",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
					EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
						Path:            "/dev/stdout",
						OmitEmptyValues: true,
					},
				},
			},
			expected: &accesslog.AccessLog{
				Name: wellknown.FileAccessLog,
				ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(&fileaccesslog.FileAccessLog{
					Path: DevStdout,
					AccessLogFormat: &fileaccesslog.FileAccessLog_LogFormat{
						LogFormat: &core.SubstitutionFormatString{
							Format: &core.SubstitutionFormatString_TextFormatSource{
								TextFormatSource: &core.DataSource{
									Specifier: &core.DataSource_InlineString{
										InlineString: EnvoyTextLogFormat,
									},
								},
							},
							OmitEmptyValues: true,
						},
					},
				})},
			},
		},
		{
			name: "json-enable-omit-empty-values",
			ctx:  ctx,
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_JSON,
				AccessLogFormat:   defaultFormatJSON,
			},
			fp: &meshconfig.MeshConfig_ExtensionProvider{
				Name: "envoy",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
					EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
						Path: "/dev/stdout",
						LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat{
							LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat_Labels{
								Labels: &structpb.Struct{},
							},
						},
						OmitEmptyValues: true,
					},
				},
			},
			expected: &accesslog.AccessLog{
				Name: wellknown.FileAccessLog,
				ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(&fileaccesslog.FileAccessLog{
					Path: "/dev/stdout",
					AccessLogFormat: &fileaccesslog.FileAccessLog_LogFormat{
						LogFormat: &core.SubstitutionFormatString{
							Format: &core.SubstitutionFormatString_JsonFormat{
								JsonFormat: EnvoyJSONLogFormatIstio,
							},
							JsonFormatOptions: &core.JsonFormatOptions{SortProperties: false},
							OmitEmptyValues:   true,
						},
					},
				})},
			},
		},
		{
			name: "builtin-not-fallback",
			ctx:  ctx,
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_JSON,
				AccessLogFormat:   defaultFormatJSON,
			},
			fp: &meshconfig.MeshConfig_ExtensionProvider{
				Name: "envoy",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
					EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
						Path: "/dev/stdout",
						LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat{
							LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat_Text{
								Text: "%LOCAL_REPLY_BODY%:%RESPONSE_CODE%:path=%REQ(:path)%",
							},
						},
					},
				},
			},
			expected: &accesslog.AccessLog{
				Name: wellknown.FileAccessLog,
				ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(&fileaccesslog.FileAccessLog{
					Path: DevStdout,
					AccessLogFormat: &fileaccesslog.FileAccessLog_LogFormat{
						LogFormat: &core.SubstitutionFormatString{
							Format: &core.SubstitutionFormatString_TextFormatSource{
								TextFormatSource: &core.DataSource{
									Specifier: &core.DataSource_InlineString{
										InlineString: "%LOCAL_REPLY_BODY%:%RESPONSE_CODE%:path=%REQ(:path)%\n",
									},
								},
							},
						},
					},
				})},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			push := tc.ctx
			if push == nil {
				push = NewPushContext()
			}
			push.Mesh = tc.meshConfig

			got := telemetryAccessLog(push, sidecar, tc.fp)
			if got == nil {
				t.Fatal("get nil accesslog")
			}
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestAccessLogJSONFormatters(t *testing.T) {
	cases := []struct {
		name                 string
		json                 *structpb.Struct
		skipBuiltInFormatter bool
		expected             []*core.TypedExtensionConfig
	}{
		{
			name:     "default",
			json:     EnvoyJSONLogFormatIstio,
			expected: []*core.TypedExtensionConfig{},
		},
		{
			name: "with-req-without-query",
			json: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"key1": {Kind: &structpb.Value_StringValue{StringValue: "%REQ_WITHOUT_QUERY(key1:val1)%"}},
				},
			},
			expected: []*core.TypedExtensionConfig{
				reqWithoutQueryFormatter,
			},
		},
		{
			name: "with-metadata",
			json: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"key1": {Kind: &structpb.Value_StringValue{StringValue: "%METADATA(CLUSTER:istio)%"}},
				},
			},
			expected: []*core.TypedExtensionConfig{
				metadataFormatter,
			},
		},
		{
			name: "with-both",
			json: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"key1": {Kind: &structpb.Value_StringValue{StringValue: "%REQ_WITHOUT_QUERY(key1:val1)%"}},
					"key2": {Kind: &structpb.Value_StringValue{StringValue: "%METADATA(UPSTREAM_HOST:istio)%"}},
				},
			},
			expected: []*core.TypedExtensionConfig{
				reqWithoutQueryFormatter,
				metadataFormatter,
			},
		},
		{
			name: "with-multi-metadata",
			json: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"key1": {Kind: &structpb.Value_StringValue{StringValue: "%METADATA(CLUSTER:istio)%"}},
					"key2": {Kind: &structpb.Value_StringValue{StringValue: "%METADATA(UPSTREAM_HOST:istio)%"}},
				},
			},
			expected: []*core.TypedExtensionConfig{
				metadataFormatter,
			},
		},
		{
			name: "more-complex",
			json: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"req1": {Kind: &structpb.Value_StringValue{StringValue: "%REQ_WITHOUT_QUERY(key1:val1)%"}},
					"req2": {Kind: &structpb.Value_StringValue{StringValue: "%REQ_WITHOUT_QUERY(key2:val1)%"}},
					"key1": {Kind: &structpb.Value_StringValue{StringValue: "%METADATA(CLUSTER:istio)%"}},
					"key2": {Kind: &structpb.Value_StringValue{StringValue: "%METADATA(UPSTREAM_HOST:istio)%"}},
				},
			},
			expected: []*core.TypedExtensionConfig{
				reqWithoutQueryFormatter,
				metadataFormatter,
			},
		},
		{
			name: "cel",
			json: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"req1": {Kind: &structpb.Value_StringValue{StringValue: "%CEL(request.host)%"}},
				},
			},
			expected: []*core.TypedExtensionConfig{
				celFormatter,
			},
		},
		{
			name: "skip builtin",
			json: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"req1": {Kind: &structpb.Value_StringValue{StringValue: "%REQ_WITHOUT_QUERY(key1:val1)%"}},
					"req2": {Kind: &structpb.Value_StringValue{StringValue: "%REQ_WITHOUT_QUERY(key2:val1)%"}},
					"key1": {Kind: &structpb.Value_StringValue{StringValue: "%METADATA(CLUSTER:istio)%"}},
					"key2": {Kind: &structpb.Value_StringValue{StringValue: "%METADATA(UPSTREAM_HOST:istio)%"}},
					"host": {Kind: &structpb.Value_StringValue{StringValue: "%CEL(request.host)%"}},
				},
			},
			skipBuiltInFormatter: true,
			expected: []*core.TypedExtensionConfig{
				reqWithoutQueryFormatter,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := accessLogJSONFormatters(tc.json, tc.skipBuiltInFormatter)
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestAccessLogTextFormatters(t *testing.T) {
	cases := []struct {
		name                 string
		text                 string
		skipBuiltInFormatter bool
		expected             []*core.TypedExtensionConfig
	}{
		{
			name:     "default",
			text:     EnvoyTextLogFormat,
			expected: []*core.TypedExtensionConfig{},
		},
		{
			name: "with-req-without-query",
			text: EnvoyTextLogFormat + " %REQ_WITHOUT_QUERY(key1:val1)%",
			expected: []*core.TypedExtensionConfig{
				reqWithoutQueryFormatter,
			},
		},
		{
			name: "with-metadata",
			text: EnvoyTextLogFormat + " %METADATA(CLUSTER:istio)%",
			expected: []*core.TypedExtensionConfig{
				metadataFormatter,
			},
		},
		{
			name: "with-both",
			text: EnvoyTextLogFormat + " %REQ_WITHOUT_QUERY(key1:val1)% %METADATA(CLUSTER:istio)%",
			expected: []*core.TypedExtensionConfig{
				reqWithoutQueryFormatter,
				metadataFormatter,
			},
		},
		{
			name: "with-multi-metadata",
			text: EnvoyTextLogFormat + " %METADATA(UPSTREAM_HOST:istio)% %METADATA(CLUSTER:istio)%",
			expected: []*core.TypedExtensionConfig{
				metadataFormatter,
			},
		},
		{
			name: "more-complex",
			text: EnvoyTextLogFormat + " %REQ_WITHOUT_QUERY(key1:val1)% REQ_WITHOUT_QUERY(key2:val1)% %METADATA(UPSTREAM_HOST:istio)% %METADATA(CLUSTER:istio)%",
			expected: []*core.TypedExtensionConfig{
				reqWithoutQueryFormatter,
				metadataFormatter,
			},
		},
		{
			name: "skip-built-in",
			// nolint: lll
			text:                 EnvoyTextLogFormat + "%CEL(request.host)% %REQ_WITHOUT_QUERY(key1:val1)% REQ_WITHOUT_QUERY(key2:val1)% %METADATA(UPSTREAM_HOST:istio)% %METADATA(CLUSTER:istio)%",
			skipBuiltInFormatter: true,
			expected: []*core.TypedExtensionConfig{
				reqWithoutQueryFormatter,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := accessLogTextFormatters(tc.text, tc.skipBuiltInFormatter)
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestTelemetryAccessLogWithFormatter(t *testing.T) {
	sidecar := &Proxy{
		ConfigNamespace: "default",
		Labels:          map[string]string{"app": "test"},
		Metadata:        &NodeMetadata{},
		IstioVersion:    &IstioVersion{Major: 1, Minor: 23},
	}

	textFormatters := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy-text-formatters",
					},
				},
			},
		},
	}

	jsonFormatters := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy-json-formatters",
					},
				},
			},
		},
	}

	tests := []struct {
		name             string
		cfgs             []config.Config
		proxy            *Proxy
		defaultProviders []string
		excepted         []LoggingConfig
	}{
		{
			"text",
			[]config.Config{newTelemetry("default", textFormatters)},
			sidecar,
			[]string{},
			[]LoggingConfig{
				{
					AccessLog: &accesslog.AccessLog{
						Name:       wellknown.FileAccessLog,
						ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(formattersTextLabelsOut)},
					},
					Provider: textFormattersProvider,
				},
			},
		},
		{
			"json",
			[]config.Config{newTelemetry("default", jsonFormatters)},
			sidecar,
			[]string{},
			[]LoggingConfig{
				{
					AccessLog: &accesslog.AccessLog{
						Name:       wellknown.FileAccessLog,
						ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(formattersJSONLabelsOut)},
					},
					Provider: jsonFormattersProvider,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			telemetry, ctx := createTestTelemetries(tt.cfgs, t)
			telemetry.meshConfig.DefaultProviders.AccessLogging = tt.defaultProviders
			got := telemetry.AccessLogging(ctx, tt.proxy, networking.ListenerClassSidecarOutbound, nil)
			assert.Equal(t, tt.excepted, got)
		})
	}
}

func TestAccessLogFormatters(t *testing.T) {
	cases := []struct {
		name                 string
		text                 string
		labels               *structpb.Struct
		skipBuiltInFormatter bool
		expected             []*core.TypedExtensionConfig
	}{
		{
			name:     "default",
			text:     EnvoyTextLogFormat,
			expected: []*core.TypedExtensionConfig{},
		},
		{
			name: "with-req-without-query",
			text: EnvoyTextLogFormat + " %REQ_WITHOUT_QUERY(key1:val1)%",
			labels: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"key1": {Kind: &structpb.Value_StringValue{StringValue: "%REQ_WITHOUT_QUERY(key1:val1)%"}},
				},
			},
			expected: []*core.TypedExtensionConfig{
				reqWithoutQueryFormatter,
			},
		},
		{
			name: "with-metadata",
			text: EnvoyTextLogFormat + " %METADATA(CLUSTER:istio)%",
			labels: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"key1": {Kind: &structpb.Value_StringValue{StringValue: "%METADATA(CLUSTER:istio)%"}},
				},
			},
			expected: []*core.TypedExtensionConfig{
				metadataFormatter,
			},
		},
		{
			name: "with-both",
			text: EnvoyTextLogFormat + " %REQ_WITHOUT_QUERY(key1:val1)% %METADATA(CLUSTER:istio)%",
			labels: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"key1": {Kind: &structpb.Value_StringValue{StringValue: "%METADATA(CLUSTER:istio)%"}},
					"key2": {Kind: &structpb.Value_StringValue{StringValue: "%REQ_WITHOUT_QUERY(key1:val1)%"}},
				},
			},
			expected: []*core.TypedExtensionConfig{
				metadataFormatter,
				reqWithoutQueryFormatter,
			},
		},
		{
			name: "all",
			text: EnvoyTextLogFormat + " %REQ_WITHOUT_QUERY(key1:val1)% REQ_WITHOUT_QUERY(key2:val1)% %METADATA(UPSTREAM_HOST:istio)% %METADATA(CLUSTER:istio)%",
			labels: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"key1": {Kind: &structpb.Value_StringValue{StringValue: "%METADATA(CLUSTER:istio)%"}},
					"key2": {Kind: &structpb.Value_StringValue{StringValue: "%REQ_WITHOUT_QUERY(key1:val1)%"}},
					"key3": {Kind: &structpb.Value_StringValue{StringValue: "%CEL(request.host)%"}},
				},
			},
			expected: []*core.TypedExtensionConfig{
				celFormatter,
				metadataFormatter,
				reqWithoutQueryFormatter,
			},
		},
		{
			name: "skip builtin",
			text: EnvoyTextLogFormat + " %REQ_WITHOUT_QUERY(key1:val1)% REQ_WITHOUT_QUERY(key2:val1)% %METADATA(UPSTREAM_HOST:istio)% %METADATA(CLUSTER:istio)%",
			labels: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"key1": {Kind: &structpb.Value_StringValue{StringValue: "%METADATA(CLUSTER:istio)%"}},
					"key2": {Kind: &structpb.Value_StringValue{StringValue: "%REQ_WITHOUT_QUERY(key1:val1)%"}},
					"key3": {Kind: &structpb.Value_StringValue{StringValue: "%CEL(request.host)%"}},
				},
			},
			skipBuiltInFormatter: true,
			expected: []*core.TypedExtensionConfig{
				reqWithoutQueryFormatter,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := accessLogFormatters(tc.text, tc.labels, tc.skipBuiltInFormatter)
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestFilterStateObjectsToLog(t *testing.T) {
	cases := []struct {
		proxy    *Proxy
		expected []string
	}{
		{
			proxy: &Proxy{
				IstioVersion: &IstioVersion{Major: 1, Minor: 23},
			},
			expected: []string{"wasm.upstream_peer", "wasm.upstream_peer_id", "wasm.downstream_peer", "wasm.downstream_peer_id"},
		},
		{
			proxy: &Proxy{
				IstioVersion: &IstioVersion{Major: 1, Minor: 24},
			},
			expected: []string{"upstream_peer", "downstream_peer"},
		},
		{
			proxy: &Proxy{
				IstioVersion: &IstioVersion{Major: 1, Minor: 25},
			},
			expected: []string{"upstream_peer", "downstream_peer"},
		},
	}

	for _, tc := range cases {
		t.Run("", func(t *testing.T) {
			got := filterStateObjectsToLog(tc.proxy)
			assert.Equal(t, tc.expected, got)
		})
	}
}
