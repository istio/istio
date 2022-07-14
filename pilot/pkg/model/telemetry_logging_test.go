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
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	otlpcommon "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/protobuf/types/known/structpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	tpb "istio.io/api/telemetry/v1alpha1"
	"istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/protomarshal"
)

func TestAccessLogging(t *testing.T) {
	labels := map[string]string{"app": "test"}
	sidecar := &Proxy{ConfigNamespace: "default", Metadata: &NodeMetadata{Labels: labels}}
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
	nonExistant := &tpb.Telemetry{
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
			nil,
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
			[]config.Config{newTelemetry("istio-system", envoy), newTelemetry("default", stackdriver)},
			networking.ListenerClassSidecarOutbound,
			sidecar,
			nil,
			[]string{"stackdriver"},
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
			[]config.Config{newTelemetry("default", nonExistant)},
			networking.ListenerClassSidecarOutbound,
			sidecar,
			[]string{"envoy"},
			[]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			telemetry, ctx := createTestTelemetries(tt.cfgs, t)
			telemetry.meshConfig.DefaultProviders.AccessLogging = tt.defaultProviders
			al := telemetry.AccessLogging(ctx, tt.proxy, tt.class)
			var got []string
			if al != nil {
				got = []string{} // We distinguish between nil vs empty in the test
				for _, p := range al.Providers {
					got = append(got, p.Name)
				}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestAccessLoggingWithFilter(t *testing.T) {
	sidecar := &Proxy{ConfigNamespace: "default", Metadata: &NodeMetadata{Labels: map[string]string{"app": "test"}}}
	filter1 := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "custom-provider",
					},
				},
				Filter: &tpb.AccessLogging_Filter{
					Expression: "response.code >= 400",
				},
			},
		},
	}
	filter2 := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "custom-provider",
					},
				},
				Filter: &tpb.AccessLogging_Filter{
					Expression: "response.code >= 500",
				},
			},
		},
	}
	tests := []struct {
		name             string
		cfgs             []config.Config
		proxy            *Proxy
		defaultProviders []string
		want             *LoggingConfig
	}{
		{
			"filter",
			[]config.Config{newTelemetry("default", filter1)},
			sidecar,
			[]string{"custom-provider"},
			&LoggingConfig{
				Filter: &tpb.AccessLogging_Filter{
					Expression: "response.code >= 400",
				},
			},
		},
		{
			"multi-filter",
			[]config.Config{newTelemetry("default", filter2), newTelemetry("default", filter1)},
			sidecar,
			[]string{"custom-provider"},
			&LoggingConfig{
				Filter: &tpb.AccessLogging_Filter{
					Expression: "response.code >= 500",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			telemetry, ctx := createTestTelemetries(tt.cfgs, t)
			telemetry.meshConfig.DefaultProviders.AccessLogging = tt.defaultProviders
			got := telemetry.AccessLogging(ctx, tt.proxy, networking.ListenerClassSidecarOutbound)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestBuildOpenTelemetryAccessLogConfig(t *testing.T) {
	fakeCluster := "outbound|55680||otel-collector.monitoring.svc.cluster.local"
	fakeAuthority := "otel-collector.monitoring.svc.cluster.local"
	for _, tc := range []struct {
		name        string
		logName     string
		clusterName string
		hostname    string
		body        string
		labels      *structpb.Struct
		expected    *otelaccesslog.OpenTelemetryAccessLogConfig
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
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := buildOpenTelemetryAccessLogConfig(tc.logName, tc.hostname, tc.clusterName, tc.body, tc.labels)
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestTelemetryAccessLog(t *testing.T) {
	singleCfg := &LoggingConfig{
		Providers: []*meshconfig.MeshConfig_ExtensionProvider{
			{
				Name: "",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
					EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
						Path: DevStdout,
					},
				},
			},
		},
	}

	customTextFormat := &LoggingConfig{
		Providers: []*meshconfig.MeshConfig_ExtensionProvider{
			{
				Name: "",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
					EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
						Path: DevStdout,
						LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat{
							LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat_Text{
								Text: "%LOCAL_REPLY_BODY%:%RESPONSE_CODE%:path=%REQ(:path)%\n",
							},
						},
					},
				},
			},
		},
	}

	defaultJSONFormat := &LoggingConfig{
		Providers: []*meshconfig.MeshConfig_ExtensionProvider{
			{
				Name: "",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
					EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
						Path: DevStdout,
						LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat{
							LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat_Labels{
								Labels: &structpb.Struct{},
							},
						},
					},
				},
			},
		},
	}

	customLabelsFormat := &LoggingConfig{
		Providers: []*meshconfig.MeshConfig_ExtensionProvider{
			{
				Name: "",
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
			},
		},
	}

	multiCfg := &LoggingConfig{
		Providers: []*meshconfig.MeshConfig_ExtensionProvider{
			{
				Name: "stdout",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
					EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
						Path: DevStdout,
					},
				},
			},
			{
				Name: "stderr",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
					EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
						Path: "/dev/stderr",
					},
				},
			},
		},
	}

	fakeFilterStateObjects := []string{"fake-filter-state-object1", "fake-filter-state-object1"}
	grpcCfg := &LoggingConfig{
		Providers: []*meshconfig.MeshConfig_ExtensionProvider{
			{
				Name: "stdout",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
					EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
						Path: DevStdout,
					},
				},
			},
			{
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
			},
		},
	}

	grpcTCPCfg := &LoggingConfig{
		Providers: []*meshconfig.MeshConfig_ExtensionProvider{
			{
				Name: "stdout",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
					EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
						Path: DevStdout,
					},
				},
			},
			{
				Name: "grpc-tcp-als",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyTcpAls{
					EnvoyTcpAls: &meshconfig.MeshConfig_ExtensionProvider_EnvoyTcpGrpcV3LogProvider{
						LogName:                 "grpc-tcp-als",
						Service:                 "grpc-als.foo.svc.cluster.local",
						Port:                    9811,
						FilterStateObjectsToLog: fakeFilterStateObjects,
					},
				},
			},
		},
	}

	labels := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"protocol": {Kind: &structpb.Value_StringValue{StringValue: "%PROTOCOL%"}},
		},
	}

	multiWithOtelCfg := &LoggingConfig{
		Providers: []*meshconfig.MeshConfig_ExtensionProvider{
			{
				Name: "stdout",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
					EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
						Path: DevStdout,
					},
				},
			},
			{
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
			},
		},
	}

	defaultEnvoyProvider := &LoggingConfig{
		Providers: []*meshconfig.MeshConfig_ExtensionProvider{
			{
				Name: "envoy",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
					EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
						Path: "/dev/stdout",
					},
				},
			},
		},
	}

	grpcBackendClusterName := "outbound|9811||grpc-als.foo.svc.cluster.local"
	grpcBackendAuthority := "grpc-als.foo.svc.cluster.local"
	otelCfg := &otelaccesslog.OpenTelemetryAccessLogConfig{
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
		Body: &otlpcommon.AnyValue{
			Value: &otlpcommon.AnyValue_StringValue{
				StringValue: EnvoyTextLogFormat,
			},
		},
		Attributes: &otlpcommon.KeyValueList{
			Values: ConvertStructToAttributeKeyValues(labels.Fields),
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

	defaultJSONLabelsOut := &fileaccesslog.FileAccessLog{
		Path: DevStdout,
		AccessLogFormat: &fileaccesslog.FileAccessLog_LogFormat{
			LogFormat: &core.SubstitutionFormatString{
				Format: &core.SubstitutionFormatString_JsonFormat{
					JsonFormat: EnvoyJSONLogFormatIstio,
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
			},
		},
	}

	errout := &fileaccesslog.FileAccessLog{
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

	for _, tc := range []struct {
		name       string
		ctx        *PushContext
		meshConfig *meshconfig.MeshConfig
		spec       *LoggingConfig
		expected   []*accesslog.AccessLog
	}{
		{
			name: "single",
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			spec: singleCfg,
			expected: []*accesslog.AccessLog{
				{
					Name:       wellknown.FileAccessLog,
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(stdout)},
				},
			},
		},
		{
			name: "custom-text",
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			spec: customTextFormat,
			expected: []*accesslog.AccessLog{
				{
					Name:       wellknown.FileAccessLog,
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(customTextOut)},
				},
			},
		},
		{
			name: "default-labels",
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			spec: defaultJSONFormat,
			expected: []*accesslog.AccessLog{
				{
					Name:       wellknown.FileAccessLog,
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(defaultJSONLabelsOut)},
				},
			},
		},
		{
			name: "custom-labels",
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			spec: customLabelsFormat,
			expected: []*accesslog.AccessLog{
				{
					Name:       wellknown.FileAccessLog,
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(customLabelsOut)},
				},
			},
		},
		{
			name: "multi",
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			spec: multiCfg,
			expected: []*accesslog.AccessLog{
				{
					Name:       wellknown.FileAccessLog,
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(stdout)},
				},
				{
					Name:       wellknown.FileAccessLog,
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(errout)},
				},
			},
		},
		{
			name: "multi-with-open-telemetry",
			ctx:  ctx,
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			spec: multiWithOtelCfg,
			expected: []*accesslog.AccessLog{
				{
					Name:       wellknown.FileAccessLog,
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(stdout)},
				},
				{
					Name:       OtelEnvoyALSName,
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(otelCfg)},
				},
			},
		},
		{
			name: "grpc-als",
			spec: grpcCfg,
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			expected: []*accesslog.AccessLog{
				{
					Name:       wellknown.FileAccessLog,
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(stdout)},
				},
				{
					Name:       wellknown.HTTPGRPCAccessLog,
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(grpcHTTPout)},
				},
			},
		},
		{
			name: "grpc-tcp-als",
			spec: grpcTCPCfg,
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			expected: []*accesslog.AccessLog{
				{
					Name:       wellknown.FileAccessLog,
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(stdout)},
				},
				{
					Name:       TCPEnvoyALSName,
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(grpcTCPOut)},
				},
			},
		},
		{
			name: "default-envoy-provider",
			ctx:  ctx,
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_JSON,
				AccessLogFormat:   defaultFormatJSON,
				ExtensionProviders: []*meshconfig.MeshConfig_ExtensionProvider{
					{
						Name: "envoy",
						Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
							EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
								Path: "/dev/stdout",
							},
						},
					},
				},
			},
			spec: defaultEnvoyProvider,
			expected: []*accesslog.AccessLog{
				{
					Name:       wellknown.FileAccessLog,
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(defaultJSONLabelsOut)},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			push := tc.ctx
			if push == nil {
				push = NewPushContext()
			}
			push.Mesh = tc.meshConfig

			got := make([]*accesslog.AccessLog, 0, len(tc.spec.Providers))
			for _, p := range tc.spec.Providers {
				al := telemetryAccessLog(push, p)
				if al == nil {
					t.Fatalf("get nil accesslog")
				}
				got = append(got, al)
			}

			assert.Equal(t, tc.expected, got)
		})
	}
}
