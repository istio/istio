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

package core

import (
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	tracingcfg "github.com/envoyproxy/go-control-plane/envoy/config/trace/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	resourcedetectors "github.com/envoyproxy/go-control-plane/envoy/extensions/tracers/opentelemetry/resource_detectors/v3"
	otelsamplers "github.com/envoyproxy/go-control-plane/envoy/extensions/tracers/opentelemetry/samplers/v3"
	tracing "github.com/envoyproxy/go-control-plane/envoy/type/tracing/v3"
	xdstype "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	tpb "istio.io/api/telemetry/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pilot/pkg/xds/requestidextension"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test/util/assert"
)

const DefaultZipkinEndpoint = "/api/v2/spans"

func TestConfigureTracingExhaustiveness(t *testing.T) {
	model.AssertProvidersHandled(configureFromProviderConfigHandled)
}

func TestConfigureTracing(t *testing.T) {
	clusterName := "testcluster"
	authority := "testhost"

	clusterLookupFn = func(push *model.PushContext, service string, port int) (hostname string, cluster string, err error) {
		return authority, clusterName, nil
	}
	defer func() {
		clusterLookupFn = model.LookupCluster
	}()

	defaultUUIDExtensionCtx := requestidextension.UUIDRequestIDExtensionContext{
		UseRequestIDForTraceSampling: true,
	}

	testcases := []struct {
		name            string
		opts            gatewayListenerOpts
		inSpec          *model.TracingConfig
		want            *hcm.HttpConnectionManager_Tracing
		wantReqIDExtCtx *requestidextension.UUIDRequestIDExtensionContext
	}{
		{
			name:            "no telemetry api",
			opts:            fakeOptsNoTelemetryAPI(),
			want:            fakeTracingConfigNoProvider(55.55, 13, append(defaultTracingTags(), fakeEnvTag)),
			wantReqIDExtCtx: nil,
		},
		{
			name: "default providers",
			inSpec: &model.TracingConfig{
				ClientSpec: model.TracingSpec{
					Disabled:        true,
					EnableIstioTags: true,
				},
				ServerSpec: model.TracingSpec{
					Provider:        fakeZipkin(),
					EnableIstioTags: true,
				},
			},
			opts:            fakeOptsWithDefaultProviders(),
			want:            fakeTracingConfig(fakeZipkinProvider(clusterName, authority, DefaultZipkinEndpoint, true), 55.5, 256, defaultTracingTags()),
			wantReqIDExtCtx: &requestidextension.UUIDRequestIDExtensionContext{},
		},
		{
			name:            "no telemetry api and nil custom tag",
			opts:            fakeOptsNoTelemetryAPIWithNilCustomTag(),
			want:            fakeTracingConfigNoProvider(55.55, 13, defaultTracingTags()),
			wantReqIDExtCtx: nil,
		},
		{
			name:            "only telemetry api (no provider)",
			inSpec:          fakeTracingSpecNoProvider(99.999, false, true, true),
			opts:            fakeOptsOnlyZipkinTelemetryAPI(),
			want:            fakeTracingConfigNoProvider(99.999, 0, append(defaultTracingTags(), fakeEnvTag)),
			wantReqIDExtCtx: &defaultUUIDExtensionCtx,
		},
		{
			name:            "only telemetry api (no provider) and disabled istio tags",
			inSpec:          fakeTracingSpecNoProvider(99.999, false, true, false),
			opts:            fakeOptsOnlyZipkinTelemetryAPI(),
			want:            fakeTracingConfigNoProvider(99.999, 0, append([]*tracing.CustomTag{}, fakeEnvTag)),
			wantReqIDExtCtx: &defaultUUIDExtensionCtx,
		},
		{
			name:   "only telemetry api (no provider) with nil custom tag",
			inSpec: fakeTracingSpecNoProviderWithNilCustomTag(99.999, false, true),
			opts:   fakeOptsOnlyZipkinTelemetryAPI(),
			want:   fakeTracingConfigNoProvider(99.999, 0, defaultTracingTags()),

			wantReqIDExtCtx: &defaultUUIDExtensionCtx,
		},
		{
			name:   "only telemetry api (with provider)",
			inSpec: fakeTracingSpec(fakeZipkin(), 99.999, false, true, true),
			opts:   fakeOptsOnlyZipkinTelemetryAPI(),
			want: fakeTracingConfig(fakeZipkinProvider(clusterName, authority, DefaultZipkinEndpoint, true),
				99.999, 256, append(defaultTracingTags(), fakeEnvTag)),
			wantReqIDExtCtx: &defaultUUIDExtensionCtx,
		},
		{
			name:   "zipkin enable 64bit trace id",
			inSpec: fakeTracingSpec(fakeZipkinEnable64bitTraceID(), 99.999, false, true, true),
			opts:   fakeOptsOnlyZipkinTelemetryAPI(),
			want: fakeTracingConfig(fakeZipkinProvider(clusterName, authority, DefaultZipkinEndpoint, false),
				99.999, 256, append(defaultTracingTags(), fakeEnvTag)),
			wantReqIDExtCtx: &defaultUUIDExtensionCtx,
		},
		{
			name:            "both tracing enabled (no provider)",
			inSpec:          fakeTracingSpecNoProvider(99.999, false, true, true),
			opts:            fakeOptsMeshAndTelemetryAPI(true /* enable tracing */),
			want:            fakeTracingConfigNoProvider(99.999, 13, append(defaultTracingTags(), fakeEnvTag)),
			wantReqIDExtCtx: &defaultUUIDExtensionCtx,
		},
		{
			name:            "both tracing disabled (no provider)",
			inSpec:          fakeTracingSpecNoProvider(99.999, false, true, true),
			opts:            fakeOptsMeshAndTelemetryAPI(false /* no enable tracing */),
			want:            fakeTracingConfigNoProvider(99.999, 13, append(defaultTracingTags(), fakeEnvTag)),
			wantReqIDExtCtx: &defaultUUIDExtensionCtx,
		},
		{
			name:   "both tracing enabled (with provider)",
			inSpec: fakeTracingSpec(fakeZipkin(), 99.999, false, true, true),
			opts:   fakeOptsMeshAndTelemetryAPI(true /* enable tracing */),
			want: fakeTracingConfig(fakeZipkinProvider(clusterName, authority, DefaultZipkinEndpoint, true),
				99.999, 256, append(defaultTracingTags(), fakeEnvTag)),
			wantReqIDExtCtx: &defaultUUIDExtensionCtx,
		},
		{
			name:   "both tracing disabled (with provider)",
			inSpec: fakeTracingSpec(fakeZipkin(), 99.999, false, true, true),
			opts:   fakeOptsMeshAndTelemetryAPI(false /* no enable tracing */),
			want: fakeTracingConfig(fakeZipkinProvider(clusterName, authority, DefaultZipkinEndpoint, true),
				99.999, 256, append(defaultTracingTags(), fakeEnvTag)),

			wantReqIDExtCtx: &defaultUUIDExtensionCtx,
		},
		{
			name:   "basic config (with datadog provider)",
			inSpec: fakeTracingSpec(fakeDatadog(), 99.999, false, true, true),
			opts:   fakeOptsOnlyDatadogTelemetryAPI(),
			want:   fakeTracingConfig(fakeDatadogProvider("fake-cluster", "testhost", clusterName), 99.999, 256, append(defaultTracingTags(), fakeEnvTag)),

			wantReqIDExtCtx: &defaultUUIDExtensionCtx,
		},
		{
			name:            "basic config (with skywalking provider)",
			inSpec:          fakeTracingSpec(fakeSkywalking(), 99.999, false, false, true),
			opts:            fakeOptsOnlySkywalkingTelemetryAPI(),
			want:            fakeTracingConfigForSkywalking(fakeSkywalkingProvider(clusterName, authority), 99.999, 0, append(defaultTracingTags(), fakeEnvTag)),
			wantReqIDExtCtx: &requestidextension.UUIDRequestIDExtensionContext{UseRequestIDForTraceSampling: false},
		},
		{
			name:   "basic config (with opentelemetry provider via grpc)",
			inSpec: fakeTracingSpec(fakeOpenTelemetryGrpc(), 99.999, false, true, true),
			opts:   fakeOptsOnlyOpenTelemetryGrpcTelemetryAPI(),
			want:   fakeTracingConfig(fakeOpenTelemetryGrpcProvider(clusterName, authority), 99.999, 256, append(defaultTracingTags(), fakeEnvTag)),

			wantReqIDExtCtx: &defaultUUIDExtensionCtx,
		},
		{
			name:            "basic config (with opentelemetry provider via http)",
			inSpec:          fakeTracingSpec(fakeOpenTelemetryHTTP(), 99.999, false, true, true),
			opts:            fakeOptsOnlyOpenTelemetryHTTPTelemetryAPI(),
			want:            fakeTracingConfig(fakeOpenTelemetryHTTPProvider(clusterName, authority), 99.999, 256, append(defaultTracingTags(), fakeEnvTag)),
			wantReqIDExtCtx: &defaultUUIDExtensionCtx,
		},
		{
			name:   "basic config (with opentelemetry provider with resource detectors)",
			inSpec: fakeTracingSpec(fakeOpenTelemetryResourceDetectors(), 99.999, false, true, true),
			opts:   fakeOptsOnlyOpenTelemetryResourceDetectorsTelemetryAPI(),
			want: fakeTracingConfig(
				fakeOpenTelemetryResourceDetectorsProvider(clusterName, authority), 99.999, 256, append(defaultTracingTags(), fakeEnvTag)),

			wantReqIDExtCtx: &defaultUUIDExtensionCtx,
		},
		{
			name:            "client-only config for server",
			inSpec:          fakeClientOnlyTracingSpec(fakeSkywalking(), 99.999, false, false),
			opts:            fakeInboundOptsOnlySkywalkingTelemetryAPI(),
			want:            nil,
			wantReqIDExtCtx: nil,
		},
		{
			name:            "server-only config for skywalking server",
			inSpec:          fakeServerOnlyTracingSpec(fakeSkywalking(), 99.999, false, false),
			opts:            fakeInboundOptsOnlySkywalkingTelemetryAPI(),
			want:            fakeTracingConfigForSkywalking(fakeSkywalkingProvider(clusterName, authority), 99.999, 0, append(defaultTracingTags(), fakeEnvTag)),
			wantReqIDExtCtx: &requestidextension.UUIDRequestIDExtensionContext{UseRequestIDForTraceSampling: false},
		},
		{
			name:            "invalid provider",
			inSpec:          fakeTracingSpec(fakePrometheus(), 99.999, false, true, true),
			opts:            fakeOptsMeshAndTelemetryAPI(true /* enable tracing */),
			want:            nil,
			wantReqIDExtCtx: nil,
		},
		{
			name:   "basic config (with opentelemetry provider via grpc with initial metadata)",
			inSpec: fakeTracingSpec(fakeOpenTelemetryGrpcWithInitialMetadata(), 99.999, false, true, true),
			opts:   fakeOptsOnlyOpenTelemetryGrpcWithInitialMetadataTelemetryAPI(28),
			want: fakeTracingConfig(fakeOpenTelemetryGrpcWithInitialMetadataProvider(clusterName, authority),
				99.999, 256, append(defaultTracingTags(), fakeEnvTag)),
			wantReqIDExtCtx: &defaultUUIDExtensionCtx,
		},
		{
			name:   "only telemetry api (with provider)",
			inSpec: fakeTracingSpec(fakeZipkinWithEndpoint(), 99.999, false, true, true),
			opts:   fakeOptsZipkinTelemetryWithEndpoint(),
			want: fakeTracingConfig(fakeZipkinProvider(clusterName, authority, "/custom/path/api/v2/spans", true),
				99.999, 256, append(defaultTracingTags(), fakeEnvTag)),
			wantReqIDExtCtx: &defaultUUIDExtensionCtx,
		},
		{
			name:   "zipkin with B3 only trace context option",
			inSpec: fakeTracingSpec(fakeZipkinWithB3Only(), 99.999, false, true, true),
			opts:   fakeOptsZipkinWithTraceContextOption(meshconfig.MeshConfig_ExtensionProvider_ZipkinTracingProvider_USE_B3),
			want: fakeTracingConfig(fakeZipkinProviderWithTraceContext(clusterName, authority, DefaultZipkinEndpoint, true, tracingcfg.ZipkinConfig_USE_B3),
				99.999, 256, append(defaultTracingTags(), fakeEnvTag)),
			wantReqIDExtCtx: &defaultUUIDExtensionCtx,
		},
		{
			name:   "zipkin with dual B3/W3C trace context option",
			inSpec: fakeTracingSpec(fakeZipkinWithDualHeaders(), 99.999, false, true, true),
			opts:   fakeOptsZipkinWithTraceContextOption(meshconfig.MeshConfig_ExtensionProvider_ZipkinTracingProvider_USE_B3_WITH_W3C_PROPAGATION),
			want: fakeTracingConfig(fakeZipkinProviderWithTraceContext(clusterName, authority, DefaultZipkinEndpoint, true,
				tracingcfg.ZipkinConfig_USE_B3_WITH_W3C_PROPAGATION),
				99.999, 256, append(defaultTracingTags(), fakeEnvTag)),
			wantReqIDExtCtx: &defaultUUIDExtensionCtx,
		},
		{
			name:   "with formatter tag",
			inSpec: fakeTracingSpec(fakeOpenTelemetryGrpcWithInitialMetadata(), 99.999, false, true, true),
			opts:   fakeOptsOnlyOpenTelemetryGrpcWithInitialMetadataTelemetryAPI(29),
			want: fakeTracingConfig(fakeOpenTelemetryGrpcWithInitialMetadataProvider(clusterName, authority),
				99.999, 256, append(defaultTracingTags(), fakeEnvTag, fakeFormatterTag)),
			wantReqIDExtCtx: &defaultUUIDExtensionCtx,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			hcm := &hcm.HttpConnectionManager{}
			gotReqIDExtCtx := configureTracingFromTelemetry(tc.inSpec, tc.opts.push, tc.opts.proxy, hcm, 0, nil)
			if diff := cmp.Diff(tc.want, hcm.Tracing, protocmp.Transform()); diff != "" {
				t.Fatalf("configureTracing returned unexpected diff (-want +got):\n%s", diff)
			}
			assert.Equal(t, tc.want, hcm.Tracing)
			assert.Equal(t, tc.wantReqIDExtCtx, gotReqIDExtCtx)
		})
	}
}

func TestConfigureDynatraceSampler(t *testing.T) {
	clusterName := "testcluster"
	authority := "testhost"
	dtTenant := "abc"
	var dtClusterID int32 = 123
	clusterLookupFn = func(push *model.PushContext, service string, port int) (hostname string, cluster string, err error) {
		return authority, clusterName, nil
	}
	defer func() {
		clusterLookupFn = model.LookupCluster
	}()

	testcases := []struct {
		name        string
		dtTenant    string
		dtClusterID int32
		spansPerMin uint32
		expectedURI string
	}{
		{
			name:        "re-use otlp http headers",
			dtTenant:    dtTenant,
			dtClusterID: dtClusterID,
			expectedURI: authority + "/api/v2/samplingConfiguration",
		},
		{
			name:        "custom root spans per minute fallback",
			dtTenant:    dtTenant,
			dtClusterID: dtClusterID,
			expectedURI: authority + "/api/v2/samplingConfiguration",
			spansPerMin: 9999,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			httpProvider := fakeOpenTelemetryHTTP()
			httpProvider.GetOpentelemetry().Sampling = &meshconfig.MeshConfig_ExtensionProvider_OpenTelemetryTracingProvider_DynatraceSampler_{
				DynatraceSampler: &meshconfig.MeshConfig_ExtensionProvider_OpenTelemetryTracingProvider_DynatraceSampler{
					Tenant:             tc.dtTenant,
					ClusterId:          tc.dtClusterID,
					RootSpansPerMinute: tc.spansPerMin,
				},
			}

			// Use a different value for RandomSamplingPercentage to ensure it is changed to 100%
			// when a custom sampler is used for the OTel tracing provider
			inSpec := &model.TracingConfig{
				ClientSpec: tracingSpec(httpProvider, 50, false, false, true),
				ServerSpec: tracingSpec(httpProvider, 50, false, false, true),
			}

			opts := fakeOptsOnlyOpenTelemetryHTTPTelemetryAPI()
			ep := opts.push.Mesh.ExtensionProviders[0]
			ep.GetOpentelemetry().Sampling = &meshconfig.MeshConfig_ExtensionProvider_OpenTelemetryTracingProvider_DynatraceSampler_{
				DynatraceSampler: &meshconfig.MeshConfig_ExtensionProvider_OpenTelemetryTracingProvider_DynatraceSampler{
					Tenant:             tc.dtTenant,
					ClusterId:          tc.dtClusterID,
					RootSpansPerMinute: tc.spansPerMin,
				},
			}

			// Envoy expected config
			fakeOTelHTTPProviderConfig := &tracingcfg.OpenTelemetryConfig{
				HttpService: &core.HttpService{
					HttpUri: &core.HttpUri{
						Uri: authority + "/v1/traces",
						HttpUpstreamType: &core.HttpUri_Cluster{
							Cluster: clusterName,
						},
						Timeout: &durationpb.Duration{Seconds: 3},
					},
					RequestHeadersToAdd: []*core.HeaderValueOption{
						{
							Header: &core.HeaderValue{
								Key:   "custom-header",
								Value: "custom-value",
							},
							AppendAction: core.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
						},
					},
				},
				Sampler: &core.TypedExtensionConfig{
					Name: "envoy.tracers.opentelemetry.samplers.dynatrace",
					TypedConfig: protoconv.MessageToAny(&otelsamplers.DynatraceSamplerConfig{
						Tenant:             tc.dtTenant,
						ClusterId:          tc.dtClusterID,
						RootSpansPerMinute: tc.spansPerMin,
						HttpService: &core.HttpService{
							HttpUri: &core.HttpUri{
								Uri: tc.expectedURI,
								HttpUpstreamType: &core.HttpUri_Cluster{
									Cluster: clusterName,
								},
								Timeout: &durationpb.Duration{Seconds: 3},
							},
							RequestHeadersToAdd: []*core.HeaderValueOption{
								{
									Header: &core.HeaderValue{
										Key:   "custom-header",
										Value: "custom-value",
									},
									AppendAction: core.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
								},
							},
						},
					}),
				},
			}

			fakeOtelHTTPAny := &tracingcfg.Tracing_Http{
				Name:       envoyOpenTelemetry,
				ConfigType: &tracingcfg.Tracing_Http_TypedConfig{TypedConfig: protoconv.MessageToAny(fakeOTelHTTPProviderConfig)},
			}
			want := fakeTracingConfig(fakeOtelHTTPAny, 100, 256, append(defaultTracingTags(), fakeEnvTag))

			hcm := &hcm.HttpConnectionManager{}
			configureTracingFromTelemetry(inSpec, opts.push, opts.proxy, hcm, 0, nil)

			if diff := cmp.Diff(want, hcm.Tracing, protocmp.Transform()); diff != "" {
				t.Fatalf("configureTracing returned unexpected diff (-want +got):\n%s", diff)
			}
			assert.Equal(t, want, hcm.Tracing)
		})
	}
}

func TestConfigureDynatraceSamplerWithCustomHttp(t *testing.T) {
	clusterName := "testcluster"
	authority := "testhost"

	dtClusterName := "dtcluster"
	dtAuthority := "dthost"
	expectedTenant := "abc"
	var expectedClusterID int32 = 123
	expectedHeader := "sampler-custom"
	expectedToken := "sampler-value"

	clusterLookupFn = func(push *model.PushContext, service string, port int) (hostname string, cluster string, err error) {
		if service == dtAuthority {
			return dtAuthority, dtClusterName, nil
		}
		return authority, clusterName, nil
	}
	defer func() {
		clusterLookupFn = model.LookupCluster
	}()

	httpProvider := fakeOpenTelemetryHTTP()
	httpProvider.GetOpentelemetry().Sampling = &meshconfig.MeshConfig_ExtensionProvider_OpenTelemetryTracingProvider_DynatraceSampler_{
		DynatraceSampler: &meshconfig.MeshConfig_ExtensionProvider_OpenTelemetryTracingProvider_DynatraceSampler{
			Tenant:             expectedTenant,
			ClusterId:          expectedClusterID,
			RootSpansPerMinute: 2000,
			HttpService: &meshconfig.MeshConfig_ExtensionProvider_OpenTelemetryTracingProvider_DynatraceSampler_DynatraceApi{
				Service: dtAuthority,
				Port:    123,
				Http: &meshconfig.MeshConfig_ExtensionProvider_HttpService{
					Path:    "api/v2/samplingConfiguration",
					Timeout: &durationpb.Duration{Seconds: 3},
					Headers: []*meshconfig.MeshConfig_ExtensionProvider_HttpHeader{
						{
							Name: expectedHeader,
							HeaderValue: &meshconfig.MeshConfig_ExtensionProvider_HttpHeader_Value{
								Value: expectedToken,
							},
						},
					},
				},
			},
		},
	}

	// Use a different value for RandomSamplingPercentage to ensure it is changed to 100%
	// when a custom sampler is used for the OTel tracing provider
	inSpec := &model.TracingConfig{
		ClientSpec: tracingSpec(httpProvider, 50, false, false, true),
		ServerSpec: tracingSpec(httpProvider, 50, false, false, true),
	}

	opts := fakeOptsOnlyOpenTelemetryHTTPTelemetryAPI()
	ep := opts.push.Mesh.ExtensionProviders[0]
	ep.GetOpentelemetry().Sampling = &meshconfig.MeshConfig_ExtensionProvider_OpenTelemetryTracingProvider_DynatraceSampler_{
		DynatraceSampler: &meshconfig.MeshConfig_ExtensionProvider_OpenTelemetryTracingProvider_DynatraceSampler{
			Tenant:             expectedTenant,
			ClusterId:          expectedClusterID,
			RootSpansPerMinute: 2000,
			HttpService: &meshconfig.MeshConfig_ExtensionProvider_OpenTelemetryTracingProvider_DynatraceSampler_DynatraceApi{
				Service: dtAuthority,
				Port:    123,
				Http: &meshconfig.MeshConfig_ExtensionProvider_HttpService{
					Path:    "api/v2/samplingConfiguration",
					Timeout: &durationpb.Duration{Seconds: 3},
					Headers: []*meshconfig.MeshConfig_ExtensionProvider_HttpHeader{
						{
							Name: expectedHeader,
							HeaderValue: &meshconfig.MeshConfig_ExtensionProvider_HttpHeader_Value{
								Value: expectedToken,
							},
						},
					},
				},
			},
		},
	}

	// Envoy expected config
	fakeOTelHTTPProviderConfig := &tracingcfg.OpenTelemetryConfig{
		HttpService: &core.HttpService{
			HttpUri: &core.HttpUri{
				Uri: authority + "/v1/traces",
				HttpUpstreamType: &core.HttpUri_Cluster{
					Cluster: clusterName,
				},
				Timeout: &durationpb.Duration{Seconds: 3},
			},
			RequestHeadersToAdd: []*core.HeaderValueOption{
				{
					Header: &core.HeaderValue{
						Key:   "custom-header",
						Value: "custom-value",
					},
					AppendAction: core.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
				},
			},
		},
		Sampler: &core.TypedExtensionConfig{
			Name: "envoy.tracers.opentelemetry.samplers.dynatrace",
			TypedConfig: protoconv.MessageToAny(&otelsamplers.DynatraceSamplerConfig{
				Tenant:             expectedTenant,
				ClusterId:          expectedClusterID,
				RootSpansPerMinute: 2000,
				HttpService: &core.HttpService{
					HttpUri: &core.HttpUri{
						Uri: dtAuthority + "/api/v2/samplingConfiguration",
						HttpUpstreamType: &core.HttpUri_Cluster{
							Cluster: dtClusterName,
						},
						Timeout: &durationpb.Duration{Seconds: 3},
					},
					RequestHeadersToAdd: []*core.HeaderValueOption{
						{
							Header: &core.HeaderValue{
								Key:   expectedHeader,
								Value: expectedToken,
							},
							AppendAction: core.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
						},
					},
				},
			}),
		},
	}

	fakeOtelHTTPAny := &tracingcfg.Tracing_Http{
		Name:       envoyOpenTelemetry,
		ConfigType: &tracingcfg.Tracing_Http_TypedConfig{TypedConfig: protoconv.MessageToAny(fakeOTelHTTPProviderConfig)},
	}
	want := fakeTracingConfig(fakeOtelHTTPAny, 100, 256, append(defaultTracingTags(), fakeEnvTag))

	hcm := &hcm.HttpConnectionManager{}
	configureTracingFromTelemetry(inSpec, opts.push, opts.proxy, hcm, 0, nil)

	if diff := cmp.Diff(want, hcm.Tracing, protocmp.Transform()); diff != "" {
		t.Fatalf("configureTracing returned unexpected diff (-want +got):\n%s", diff)
	}
	assert.Equal(t, want, hcm.Tracing)
}

func defaultTracingTags() []*tracing.CustomTag {
	return append(slices.Clone(optionalPolicyTags),
		&tracing.CustomTag{
			Tag: "istio.canonical_revision",
			Type: &tracing.CustomTag_Literal_{
				Literal: &tracing.CustomTag_Literal{
					Value: "latest",
				},
			},
		},
		&tracing.CustomTag{
			Tag: "istio.canonical_service",
			Type: &tracing.CustomTag_Literal_{
				Literal: &tracing.CustomTag_Literal{
					Value: "unknown",
				},
			},
		},
		&tracing.CustomTag{
			Tag: "istio.cluster_id",
			Type: &tracing.CustomTag_Literal_{
				Literal: &tracing.CustomTag_Literal{
					Value: "unknown",
				},
			},
		},
		&tracing.CustomTag{
			Tag: "istio.mesh_id",
			Type: &tracing.CustomTag_Literal_{
				Literal: &tracing.CustomTag_Literal{
					Value: "unknown",
				},
			},
		},
		&tracing.CustomTag{
			Tag: "istio.namespace",
			Type: &tracing.CustomTag_Literal_{
				Literal: &tracing.CustomTag_Literal{
					Value: "default",
				},
			},
		})
}

func fakeOptsWithDefaultProviders() gatewayListenerOpts {
	var opts gatewayListenerOpts
	opts.push = &model.PushContext{
		Mesh: &meshconfig.MeshConfig{
			EnableTracing: true,
			DefaultConfig: &meshconfig.ProxyConfig{
				Tracing: &meshconfig.Tracing{
					Sampling: 55.5,
				},
			},
			DefaultProviders: &meshconfig.MeshConfig_DefaultProviders{
				Tracing: []string{
					"foo",
				},
			},
			ExtensionProviders: []*meshconfig.MeshConfig_ExtensionProvider{
				{
					Name: "foo",
					Provider: &meshconfig.MeshConfig_ExtensionProvider_Zipkin{
						Zipkin: &meshconfig.MeshConfig_ExtensionProvider_ZipkinTracingProvider{
							Service:      "zipkin",
							Port:         9411,
							MaxTagLength: 256,
						},
					},
				},
			},
		},
	}
	opts.proxy = &model.Proxy{
		IstioVersion: &model.IstioVersion{Major: 1, Minor: 28, Patch: 0},
		Metadata:     &model.NodeMetadata{},
	}

	return opts
}

func fakeOptsNoTelemetryAPI() gatewayListenerOpts {
	var opts gatewayListenerOpts
	opts.push = &model.PushContext{
		Mesh: &meshconfig.MeshConfig{
			EnableTracing: true,
		},
	}
	opts.proxy = &model.Proxy{
		Metadata: &model.NodeMetadata{
			ProxyConfig: &model.NodeMetaProxyConfig{
				Tracing: &meshconfig.Tracing{
					Sampling:         55.55,
					MaxPathTagLength: 13,
					CustomTags: map[string]*meshconfig.Tracing_CustomTag{
						"test": {
							Type: &meshconfig.Tracing_CustomTag_Environment{
								Environment: &meshconfig.Tracing_Environment{
									Name: "FOO",
								},
							},
						},
					},
				},
			},
		},
		IstioVersion: &model.IstioVersion{Major: 1, Minor: 28, Patch: 0},
	}

	return opts
}

func fakeOptsNoTelemetryAPIWithNilCustomTag() gatewayListenerOpts {
	var opts gatewayListenerOpts
	opts.push = &model.PushContext{
		Mesh: &meshconfig.MeshConfig{
			EnableTracing: true,
		},
	}
	opts.proxy = &model.Proxy{
		IstioVersion: &model.IstioVersion{Major: 1, Minor: 28, Patch: 0},
		Metadata: &model.NodeMetadata{
			ProxyConfig: &model.NodeMetaProxyConfig{
				Tracing: &meshconfig.Tracing{
					Sampling:         55.55,
					MaxPathTagLength: 13,
					CustomTags: map[string]*meshconfig.Tracing_CustomTag{
						"test": nil,
					},
				},
			},
		},
	}

	return opts
}

func fakeOptsOnlyZipkinTelemetryAPI() gatewayListenerOpts {
	var opts gatewayListenerOpts
	opts.push = &model.PushContext{
		Mesh: &meshconfig.MeshConfig{
			ExtensionProviders: []*meshconfig.MeshConfig_ExtensionProvider{
				{
					Name: "foo",
					Provider: &meshconfig.MeshConfig_ExtensionProvider_Zipkin{
						Zipkin: &meshconfig.MeshConfig_ExtensionProvider_ZipkinTracingProvider{
							Service:      "zipkin",
							Port:         9411,
							MaxTagLength: 256,
						},
					},
				},
			},
		},
	}
	opts.proxy = &model.Proxy{
		Metadata: &model.NodeMetadata{
			ProxyConfig: &model.NodeMetaProxyConfig{},
		},
		IstioVersion: &model.IstioVersion{Major: 1, Minor: 28, Patch: 0},
	}

	return opts
}

func fakeOptsZipkinTelemetryWithEndpoint() gatewayListenerOpts {
	var opts gatewayListenerOpts
	opts.push = &model.PushContext{
		Mesh: &meshconfig.MeshConfig{
			ExtensionProviders: []*meshconfig.MeshConfig_ExtensionProvider{
				{
					Name: "foo",
					Provider: &meshconfig.MeshConfig_ExtensionProvider_Zipkin{
						Zipkin: &meshconfig.MeshConfig_ExtensionProvider_ZipkinTracingProvider{
							Service:      "zipkin",
							Port:         9411,
							MaxTagLength: 256,
							Path:         "/custom/path/api/v2/spans",
						},
					},
				},
			},
		},
	}
	opts.proxy = &model.Proxy{
		IstioVersion: &model.IstioVersion{Major: 1, Minor: 28, Patch: 0},
		Metadata: &model.NodeMetadata{
			ProxyConfig: &model.NodeMetaProxyConfig{},
		},
	}

	return opts
}

func fakeOptsZipkinWithTraceContextOption(
	traceContextOption meshconfig.MeshConfig_ExtensionProvider_ZipkinTracingProvider_TraceContextOption,
) gatewayListenerOpts {
	var opts gatewayListenerOpts
	opts.push = &model.PushContext{
		Mesh: &meshconfig.MeshConfig{
			ExtensionProviders: []*meshconfig.MeshConfig_ExtensionProvider{
				{
					Name: "foo",
					Provider: &meshconfig.MeshConfig_ExtensionProvider_Zipkin{
						Zipkin: &meshconfig.MeshConfig_ExtensionProvider_ZipkinTracingProvider{
							Service:            "zipkin",
							Port:               9411,
							MaxTagLength:       256,
							TraceContextOption: traceContextOption,
						},
					},
				},
			},
		},
	}
	opts.proxy = &model.Proxy{
		IstioVersion: &model.IstioVersion{Major: 1, Minor: 28, Patch: 0}, // Ensure proxy supports TraceContextOption
		Metadata: &model.NodeMetadata{
			ProxyConfig: &model.NodeMetaProxyConfig{},
		},
	}

	return opts
}

func fakeZipkin() *meshconfig.MeshConfig_ExtensionProvider {
	return &meshconfig.MeshConfig_ExtensionProvider{
		Name: "foo",
		Provider: &meshconfig.MeshConfig_ExtensionProvider_Zipkin{
			Zipkin: &meshconfig.MeshConfig_ExtensionProvider_ZipkinTracingProvider{
				Service:      "zipkin",
				Port:         9411,
				MaxTagLength: 256,
			},
		},
	}
}

func fakeZipkinWithEndpoint() *meshconfig.MeshConfig_ExtensionProvider {
	return &meshconfig.MeshConfig_ExtensionProvider{
		Name: "foo",
		Provider: &meshconfig.MeshConfig_ExtensionProvider_Zipkin{
			Zipkin: &meshconfig.MeshConfig_ExtensionProvider_ZipkinTracingProvider{
				Service:      "zipkin",
				Port:         9411,
				MaxTagLength: 256,
				Path:         "/custom/path/api/v2/spans",
			},
		},
	}
}

func fakePrometheus() *meshconfig.MeshConfig_ExtensionProvider {
	return &meshconfig.MeshConfig_ExtensionProvider{
		Name:     "foo",
		Provider: &meshconfig.MeshConfig_ExtensionProvider_Prometheus{},
	}
}

func fakeZipkinEnable64bitTraceID() *meshconfig.MeshConfig_ExtensionProvider {
	return &meshconfig.MeshConfig_ExtensionProvider{
		Name: "foo",
		Provider: &meshconfig.MeshConfig_ExtensionProvider_Zipkin{
			Zipkin: &meshconfig.MeshConfig_ExtensionProvider_ZipkinTracingProvider{
				Service:             "zipkin",
				Port:                9411,
				MaxTagLength:        256,
				Enable_64BitTraceId: true,
			},
		},
	}
}

func fakeZipkinWithTraceContextOption(
	traceContextOption meshconfig.MeshConfig_ExtensionProvider_ZipkinTracingProvider_TraceContextOption,
) *meshconfig.MeshConfig_ExtensionProvider {
	return &meshconfig.MeshConfig_ExtensionProvider{
		Name: "foo",
		Provider: &meshconfig.MeshConfig_ExtensionProvider_Zipkin{
			Zipkin: &meshconfig.MeshConfig_ExtensionProvider_ZipkinTracingProvider{
				Service:            "zipkin",
				Port:               9411,
				MaxTagLength:       256,
				TraceContextOption: traceContextOption,
			},
		},
	}
}

func fakeZipkinWithDualHeaders() *meshconfig.MeshConfig_ExtensionProvider {
	return fakeZipkinWithTraceContextOption(meshconfig.MeshConfig_ExtensionProvider_ZipkinTracingProvider_USE_B3_WITH_W3C_PROPAGATION)
}

func fakeZipkinWithB3Only() *meshconfig.MeshConfig_ExtensionProvider {
	return fakeZipkinWithTraceContextOption(meshconfig.MeshConfig_ExtensionProvider_ZipkinTracingProvider_USE_B3)
}

func fakeDatadog() *meshconfig.MeshConfig_ExtensionProvider {
	return &meshconfig.MeshConfig_ExtensionProvider{
		Name: "datadog",
		Provider: &meshconfig.MeshConfig_ExtensionProvider_Datadog{
			Datadog: &meshconfig.MeshConfig_ExtensionProvider_DatadogTracingProvider{
				Service:      "datadog",
				Port:         8126,
				MaxTagLength: 256,
			},
		},
	}
}

func fakeOptsOnlyDatadogTelemetryAPI() gatewayListenerOpts {
	var opts gatewayListenerOpts
	opts.push = &model.PushContext{
		Mesh: &meshconfig.MeshConfig{
			ExtensionProviders: []*meshconfig.MeshConfig_ExtensionProvider{
				{
					Name: "datadog",
					Provider: &meshconfig.MeshConfig_ExtensionProvider_Datadog{
						Datadog: &meshconfig.MeshConfig_ExtensionProvider_DatadogTracingProvider{
							Service:      "datadog",
							Port:         8126,
							MaxTagLength: 256,
						},
					},
				},
			},
		},
	}
	opts.proxy = &model.Proxy{
		Metadata: &model.NodeMetadata{
			ProxyConfig: &model.NodeMetaProxyConfig{},
		},
		XdsNode: &core.Node{
			Cluster: "fake-cluster",
		},
		IstioVersion: &model.IstioVersion{Major: 1, Minor: 28, Patch: 0},
	}

	return opts
}

func fakeOptsMeshAndTelemetryAPI(enableTracing bool) gatewayListenerOpts {
	var opts gatewayListenerOpts
	opts.push = &model.PushContext{
		Mesh: &meshconfig.MeshConfig{
			EnableTracing: enableTracing,
			ExtensionProviders: []*meshconfig.MeshConfig_ExtensionProvider{
				{
					Name: "foo",
					Provider: &meshconfig.MeshConfig_ExtensionProvider_Zipkin{
						Zipkin: &meshconfig.MeshConfig_ExtensionProvider_ZipkinTracingProvider{
							Service:      "zipkin",
							Port:         9411,
							MaxTagLength: 256,
						},
					},
				},
			},
		},
	}
	opts.proxy = &model.Proxy{
		Metadata: &model.NodeMetadata{
			ProxyConfig: &model.NodeMetaProxyConfig{
				Tracing: &meshconfig.Tracing{
					Sampling:         55.55,
					MaxPathTagLength: 13,
					CustomTags: map[string]*meshconfig.Tracing_CustomTag{
						"test": {
							Type: &meshconfig.Tracing_CustomTag_Environment{
								Environment: &meshconfig.Tracing_Environment{
									Name: "FOO",
								},
							},
						},
					},
				},
			},
		},
		IstioVersion: &model.IstioVersion{Major: 1, Minor: 28, Patch: 0},
	}

	return opts
}

func fakeSkywalking() *meshconfig.MeshConfig_ExtensionProvider {
	return &meshconfig.MeshConfig_ExtensionProvider{
		Name: "foo",
		Provider: &meshconfig.MeshConfig_ExtensionProvider_Skywalking{
			Skywalking: &meshconfig.MeshConfig_ExtensionProvider_SkyWalkingTracingProvider{
				Service: "skywalking-oap.istio-system.svc.cluster.local",
				Port:    11800,
			},
		},
	}
}

func fakeOptsOnlySkywalkingTelemetryAPI() gatewayListenerOpts {
	var opts gatewayListenerOpts
	opts.push = &model.PushContext{
		Mesh: &meshconfig.MeshConfig{
			ExtensionProviders: []*meshconfig.MeshConfig_ExtensionProvider{
				{
					Name: "foo",
					Provider: &meshconfig.MeshConfig_ExtensionProvider_Skywalking{
						Skywalking: &meshconfig.MeshConfig_ExtensionProvider_SkyWalkingTracingProvider{
							Service: "skywalking-oap.istio-system.svc.cluster.local",
							Port:    11800,
						},
					},
				},
			},
		},
	}
	opts.proxy = &model.Proxy{
		IstioVersion: &model.IstioVersion{Major: 1, Minor: 28, Patch: 0},
		Metadata: &model.NodeMetadata{
			ProxyConfig: &model.NodeMetaProxyConfig{},
		},
	}

	return opts
}

func fakeOpenTelemetryGrpc() *meshconfig.MeshConfig_ExtensionProvider {
	return &meshconfig.MeshConfig_ExtensionProvider{
		Name: "opentelemetry",
		Provider: &meshconfig.MeshConfig_ExtensionProvider_Opentelemetry{
			Opentelemetry: &meshconfig.MeshConfig_ExtensionProvider_OpenTelemetryTracingProvider{
				Service:      "otel-collector",
				Port:         4317,
				MaxTagLength: 256,
			},
		},
	}
}

func fakeOpenTelemetryGrpcWithInitialMetadata() *meshconfig.MeshConfig_ExtensionProvider {
	return &meshconfig.MeshConfig_ExtensionProvider{
		Name: "opentelemetry",
		Provider: &meshconfig.MeshConfig_ExtensionProvider_Opentelemetry{
			Opentelemetry: &meshconfig.MeshConfig_ExtensionProvider_OpenTelemetryTracingProvider{
				Service:      "tracing.example.com",
				Port:         8090,
				MaxTagLength: 256,
				Grpc: &meshconfig.MeshConfig_ExtensionProvider_GrpcService{
					InitialMetadata: []*meshconfig.MeshConfig_ExtensionProvider_HttpHeader{
						{
							Name: "Authentication",
							HeaderValue: &meshconfig.MeshConfig_ExtensionProvider_HttpHeader_Value{
								Value: "token-xxxxx",
							},
						},
					},
					Timeout: &durationpb.Duration{Seconds: 3},
				},
			},
		},
	}
}

func fakeOpenTelemetryHTTP() *meshconfig.MeshConfig_ExtensionProvider {
	return &meshconfig.MeshConfig_ExtensionProvider{
		Name: "opentelemetry",
		Provider: &meshconfig.MeshConfig_ExtensionProvider_Opentelemetry{
			Opentelemetry: &meshconfig.MeshConfig_ExtensionProvider_OpenTelemetryTracingProvider{
				Service:      "my-o11y-backend",
				Port:         443,
				MaxTagLength: 256,
				Http: &meshconfig.MeshConfig_ExtensionProvider_HttpService{
					Path:    "/v1/traces",
					Timeout: &durationpb.Duration{Seconds: 3},
					Headers: []*meshconfig.MeshConfig_ExtensionProvider_HttpHeader{
						{
							Name: "custom-header",
							HeaderValue: &meshconfig.MeshConfig_ExtensionProvider_HttpHeader_Value{
								Value: "custom-value",
							},
						},
					},
				},
			},
		},
	}
}

func fakeOpenTelemetryResourceDetectors() *meshconfig.MeshConfig_ExtensionProvider {
	ep := fakeOpenTelemetryHTTP()

	ep.GetOpentelemetry().ResourceDetectors = &meshconfig.MeshConfig_ExtensionProvider_ResourceDetectors{
		Environment: &meshconfig.MeshConfig_ExtensionProvider_ResourceDetectors_EnvironmentResourceDetector{},
		Dynatrace:   &meshconfig.MeshConfig_ExtensionProvider_ResourceDetectors_DynatraceResourceDetector{},
	}

	return ep
}

func fakeOptsOnlyOpenTelemetryGrpcTelemetryAPI() gatewayListenerOpts {
	var opts gatewayListenerOpts
	opts.push = &model.PushContext{
		Mesh: &meshconfig.MeshConfig{
			ExtensionProviders: []*meshconfig.MeshConfig_ExtensionProvider{
				{
					Name: "opentelemetry",
					Provider: &meshconfig.MeshConfig_ExtensionProvider_Opentelemetry{
						Opentelemetry: &meshconfig.MeshConfig_ExtensionProvider_OpenTelemetryTracingProvider{
							Service:      "otel-collector",
							Port:         4317,
							MaxTagLength: 256,
						},
					},
				},
			},
		},
	}
	opts.proxy = &model.Proxy{
		IstioVersion: &model.IstioVersion{Major: 1, Minor: 28, Patch: 0},
		Metadata: &model.NodeMetadata{
			ProxyConfig: &model.NodeMetaProxyConfig{},
		},
	}

	return opts
}

func fakeOptsOnlyOpenTelemetryGrpcWithInitialMetadataTelemetryAPI(minor int) gatewayListenerOpts {
	var opts gatewayListenerOpts
	opts.push = &model.PushContext{
		Mesh: &meshconfig.MeshConfig{
			ExtensionProviders: []*meshconfig.MeshConfig_ExtensionProvider{
				{
					Name: "opentelemetry",
					Provider: &meshconfig.MeshConfig_ExtensionProvider_Opentelemetry{
						Opentelemetry: &meshconfig.MeshConfig_ExtensionProvider_OpenTelemetryTracingProvider{
							Service:      "tracing.example.com",
							Port:         8090,
							MaxTagLength: 256,
							Grpc: &meshconfig.MeshConfig_ExtensionProvider_GrpcService{
								InitialMetadata: []*meshconfig.MeshConfig_ExtensionProvider_HttpHeader{
									{
										Name: "Authentication",
										HeaderValue: &meshconfig.MeshConfig_ExtensionProvider_HttpHeader_Value{
											Value: "token-xxxxx",
										},
									},
								},
								Timeout: &durationpb.Duration{Seconds: 3},
							},
						},
					},
				},
			},
		},
	}
	opts.proxy = &model.Proxy{
		IstioVersion: &model.IstioVersion{Major: 1, Minor: minor, Patch: 0},
		Metadata: &model.NodeMetadata{
			ProxyConfig: &model.NodeMetaProxyConfig{},
		},
	}

	return opts
}

func fakeOptsOnlyOpenTelemetryHTTPTelemetryAPI() gatewayListenerOpts {
	var opts gatewayListenerOpts
	opts.push = &model.PushContext{
		Mesh: &meshconfig.MeshConfig{
			ExtensionProviders: []*meshconfig.MeshConfig_ExtensionProvider{
				{
					Name: "opentelemetry",
					Provider: &meshconfig.MeshConfig_ExtensionProvider_Opentelemetry{
						Opentelemetry: &meshconfig.MeshConfig_ExtensionProvider_OpenTelemetryTracingProvider{
							Service:      "otel-collector",
							Port:         4317,
							MaxTagLength: 256,
							Http: &meshconfig.MeshConfig_ExtensionProvider_HttpService{
								Path:    "/v1/traces",
								Timeout: &durationpb.Duration{Seconds: 3},
								Headers: []*meshconfig.MeshConfig_ExtensionProvider_HttpHeader{
									{
										Name: "custom-header",
										HeaderValue: &meshconfig.MeshConfig_ExtensionProvider_HttpHeader_Value{
											Value: "custom-value",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	opts.proxy = &model.Proxy{
		IstioVersion: &model.IstioVersion{Major: 1, Minor: 28, Patch: 0},
		Metadata: &model.NodeMetadata{
			ProxyConfig: &model.NodeMetaProxyConfig{},
		},
	}

	return opts
}

func fakeOptsOnlyOpenTelemetryResourceDetectorsTelemetryAPI() gatewayListenerOpts {
	opts := fakeOptsOnlyOpenTelemetryHTTPTelemetryAPI()

	ep := opts.push.Mesh.ExtensionProviders[0]
	ep.GetOpentelemetry().ResourceDetectors = &meshconfig.MeshConfig_ExtensionProvider_ResourceDetectors{
		Environment: &meshconfig.MeshConfig_ExtensionProvider_ResourceDetectors_EnvironmentResourceDetector{},
		Dynatrace:   &meshconfig.MeshConfig_ExtensionProvider_ResourceDetectors_DynatraceResourceDetector{},
	}

	return opts
}

func fakeInboundOptsOnlySkywalkingTelemetryAPI() gatewayListenerOpts {
	opts := fakeOptsOnlySkywalkingTelemetryAPI()
	return opts
}

// nolint: unparam
func fakeTracingSpecNoProvider(sampling float64, disableReporting bool, useRequestIDForTraceSampling bool, enableIstiotags bool) *model.TracingConfig {
	return fakeTracingSpec(nil, sampling, disableReporting, useRequestIDForTraceSampling, enableIstiotags)
}

func fakeTracingSpecNoProviderWithNilCustomTag(sampling float64, disableReporting bool, useRequestIDForTraceSampling bool) *model.TracingConfig {
	return fakeTracingSpecWithNilCustomTag(nil, sampling, disableReporting, useRequestIDForTraceSampling)
}

func fakeTracingSpec(provider *meshconfig.MeshConfig_ExtensionProvider, sampling float64, disableReporting bool,
	useRequestIDForTraceSampling bool,
	enableIstioTags bool,
) *model.TracingConfig {
	t := &model.TracingConfig{
		ClientSpec: tracingSpec(provider, sampling, disableReporting, useRequestIDForTraceSampling, enableIstioTags),
		ServerSpec: tracingSpec(provider, sampling, disableReporting, useRequestIDForTraceSampling, enableIstioTags),
	}
	return t
}

func fakeClientOnlyTracingSpec(provider *meshconfig.MeshConfig_ExtensionProvider, sampling float64, disableReporting bool,
	useRequestIDForTraceSampling bool,
) *model.TracingConfig {
	t := &model.TracingConfig{
		ClientSpec: tracingSpec(provider, sampling, disableReporting, useRequestIDForTraceSampling, true),
		ServerSpec: model.TracingSpec{
			Disabled: true,
		},
	}
	return t
}

func fakeServerOnlyTracingSpec(provider *meshconfig.MeshConfig_ExtensionProvider, sampling float64, disableReporting bool,
	useRequestIDForTraceSampling bool,
) *model.TracingConfig {
	t := &model.TracingConfig{
		ClientSpec: model.TracingSpec{
			Disabled: true,
		},
		ServerSpec: tracingSpec(provider, sampling, disableReporting, useRequestIDForTraceSampling, true),
	}
	return t
}

func tracingSpec(provider *meshconfig.MeshConfig_ExtensionProvider, sampling float64, disableReporting bool,
	useRequestIDForTraceSampling bool,
	enableIstioTags bool,
) model.TracingSpec {
	return model.TracingSpec{
		Provider:                 provider,
		Disabled:                 disableReporting,
		RandomSamplingPercentage: ptr.Of(sampling),
		CustomTags: map[string]*tpb.Tracing_CustomTag{
			"test": {
				Type: &tpb.Tracing_CustomTag_Environment{
					Environment: &tpb.Tracing_Environment{
						Name: "FOO",
					},
				},
			},
			"test-formatter": {
				Type: &tpb.Tracing_CustomTag_Formatter{
					Formatter: &tpb.Tracing_Formatter{
						Value: "%REQ(fake-header)%",
					},
				},
			},
		},
		UseRequestIDForTraceSampling: useRequestIDForTraceSampling,
		EnableIstioTags:              enableIstioTags,
	}
}

func fakeTracingSpecWithNilCustomTag(provider *meshconfig.MeshConfig_ExtensionProvider, sampling float64, disableReporting bool,
	useRequestIDForTraceSampling bool,
) *model.TracingConfig {
	t := &model.TracingConfig{
		ClientSpec: model.TracingSpec{
			Provider:                 provider,
			Disabled:                 disableReporting,
			RandomSamplingPercentage: ptr.Of(sampling),
			CustomTags: map[string]*tpb.Tracing_CustomTag{
				"test": nil,
			},
			UseRequestIDForTraceSampling: useRequestIDForTraceSampling,
			EnableIstioTags:              true,
		},
		ServerSpec: model.TracingSpec{
			Provider:                 provider,
			Disabled:                 disableReporting,
			RandomSamplingPercentage: ptr.Of(sampling),
			CustomTags: map[string]*tpb.Tracing_CustomTag{
				"test": nil,
			},
			UseRequestIDForTraceSampling: useRequestIDForTraceSampling,
			EnableIstioTags:              true,
		},
	}
	return t
}

func fakeTracingConfigNoProvider(randomSampling float64, maxLen uint32, tags []*tracing.CustomTag) *hcm.HttpConnectionManager_Tracing {
	return fakeTracingConfig(nil, randomSampling, maxLen, tags)
}

func fakeTracingConfig(provider *tracingcfg.Tracing_Http, randomSampling float64, maxLen uint32, tags []*tracing.CustomTag) *hcm.HttpConnectionManager_Tracing {
	t := &hcm.HttpConnectionManager_Tracing{
		ClientSampling: &xdstype.Percent{
			Value: 100.0,
		},
		OverallSampling: &xdstype.Percent{
			Value: 100.0,
		},
		RandomSampling: &xdstype.Percent{
			Value: randomSampling,
		},
		CustomTags: tags,
	}
	if maxLen != 0 {
		t.MaxPathTagLength = wrapperspb.UInt32(maxLen)
	}
	if provider != nil {
		t.Provider = provider
	}
	return t
}

// nolint: lll
func fakeTracingConfigForSkywalking(provider *tracingcfg.Tracing_Http, randomSampling float64, maxLen uint32, tags []*tracing.CustomTag) *hcm.HttpConnectionManager_Tracing {
	cfg := fakeTracingConfig(provider, randomSampling, maxLen, tags)
	cfg.SpawnUpstreamSpan = wrapperspb.Bool(true)
	return cfg
}

var (
	fakeEnvTag = &tracing.CustomTag{
		Tag: "test",
		Type: &tracing.CustomTag_Environment_{
			Environment: &tracing.CustomTag_Environment{
				Name: "FOO",
			},
		},
	}
	fakeFormatterTag = &tracing.CustomTag{
		Tag: "test-formatter",
		Type: &tracing.CustomTag_Value{
			Value: "%REQ(fake-header)%",
		},
	}
)

func fakeZipkinProvider(expectClusterName, expectAuthority, expectEndpoint string, enableTraceID bool) *tracingcfg.Tracing_Http {
	return fakeZipkinProviderWithTraceContext(expectClusterName, expectAuthority, expectEndpoint, enableTraceID, tracingcfg.ZipkinConfig_USE_B3)
}

func fakeZipkinProviderWithTraceContext(
	expectClusterName, expectAuthority, expectEndpoint string, enableTraceID bool,
	traceContextOption tracingcfg.ZipkinConfig_TraceContextOption,
) *tracingcfg.Tracing_Http {
	fakeZipkinProviderConfig := &tracingcfg.ZipkinConfig{
		CollectorCluster:         expectClusterName,
		CollectorEndpoint:        expectEndpoint,
		CollectorEndpointVersion: tracingcfg.ZipkinConfig_HTTP_JSON,
		CollectorHostname:        expectAuthority,
		TraceId_128Bit:           enableTraceID,
		SharedSpanContext:        wrapperspb.Bool(false),
		TraceContextOption:       traceContextOption,
	}
	fakeZipkinAny := protoconv.MessageToAny(fakeZipkinProviderConfig)
	return &tracingcfg.Tracing_Http{
		Name:       envoyZipkin,
		ConfigType: &tracingcfg.Tracing_Http_TypedConfig{TypedConfig: fakeZipkinAny},
	}
}

func fakeSkywalkingProvider(expectClusterName, expectAuthority string) *tracingcfg.Tracing_Http {
	fakeSkywalkingProviderConfig := &tracingcfg.SkyWalkingConfig{
		GrpcService: &core.GrpcService{
			TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
				EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
					ClusterName: expectClusterName,
					Authority:   expectAuthority,
				},
			},
		},
	}
	fakeSkywalkingAny := protoconv.MessageToAny(fakeSkywalkingProviderConfig)
	return &tracingcfg.Tracing_Http{
		Name:       envoySkywalking,
		ConfigType: &tracingcfg.Tracing_Http_TypedConfig{TypedConfig: fakeSkywalkingAny},
	}
}

func fakeDatadogProvider(expectServcieName, expectHostName, expectClusterName string) *tracingcfg.Tracing_Http {
	fakeDatadogProviderConfig := &tracingcfg.DatadogConfig{
		CollectorCluster:  expectClusterName,
		ServiceName:       expectServcieName,
		CollectorHostname: expectHostName,
	}
	fakeAny := protoconv.MessageToAny(fakeDatadogProviderConfig)
	return &tracingcfg.Tracing_Http{
		Name:       envoyDatadog,
		ConfigType: &tracingcfg.Tracing_Http_TypedConfig{TypedConfig: fakeAny},
	}
}

func fakeOpenTelemetryGrpcProvider(expectClusterName, expectAuthority string) *tracingcfg.Tracing_Http {
	fakeOTelGrpcProviderConfig := &tracingcfg.OpenTelemetryConfig{
		GrpcService: &core.GrpcService{
			TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
				EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
					ClusterName: expectClusterName,
					Authority:   expectAuthority,
				},
			},
		},
	}
	fakeOtelGrpcAny := protoconv.MessageToAny(fakeOTelGrpcProviderConfig)
	return &tracingcfg.Tracing_Http{
		Name:       envoyOpenTelemetry,
		ConfigType: &tracingcfg.Tracing_Http_TypedConfig{TypedConfig: fakeOtelGrpcAny},
	}
}

func fakeOpenTelemetryGrpcWithInitialMetadataProvider(expectClusterName, expectAuthority string) *tracingcfg.Tracing_Http {
	fakeOTelGrpcProviderConfig := &tracingcfg.OpenTelemetryConfig{
		GrpcService: &core.GrpcService{
			InitialMetadata: []*core.HeaderValue{
				{
					Key:   "Authentication",
					Value: "token-xxxxx",
				},
			},
			Timeout: &durationpb.Duration{Seconds: 3},
			TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
				EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
					ClusterName: expectClusterName,
					Authority:   expectAuthority,
				},
			},
		},
	}
	fakeOtelGrpcAny := protoconv.MessageToAny(fakeOTelGrpcProviderConfig)
	return &tracingcfg.Tracing_Http{
		Name:       envoyOpenTelemetry,
		ConfigType: &tracingcfg.Tracing_Http_TypedConfig{TypedConfig: fakeOtelGrpcAny},
	}
}

func fakeOpenTelemetryHTTPProvider(expectClusterName, expectAuthority string) *tracingcfg.Tracing_Http {
	fakeOTelHTTPProviderConfig := &tracingcfg.OpenTelemetryConfig{
		HttpService: &core.HttpService{
			HttpUri: &core.HttpUri{
				Uri: expectAuthority + "/v1/traces",
				HttpUpstreamType: &core.HttpUri_Cluster{
					Cluster: expectClusterName,
				},
				Timeout: &durationpb.Duration{Seconds: 3},
			},
			RequestHeadersToAdd: []*core.HeaderValueOption{
				{
					Header: &core.HeaderValue{
						Key:   "custom-header",
						Value: "custom-value",
					},
					AppendAction: core.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
				},
			},
		},
	}
	fakeOtelHTTPAny := protoconv.MessageToAny(fakeOTelHTTPProviderConfig)
	return &tracingcfg.Tracing_Http{
		Name:       envoyOpenTelemetry,
		ConfigType: &tracingcfg.Tracing_Http_TypedConfig{TypedConfig: fakeOtelHTTPAny},
	}
}

func fakeOpenTelemetryResourceDetectorsProvider(expectClusterName, expectAuthority string) *tracingcfg.Tracing_Http {
	fakeOTelHTTPProviderConfig := &tracingcfg.OpenTelemetryConfig{
		HttpService: &core.HttpService{
			HttpUri: &core.HttpUri{
				Uri: expectAuthority + "/v1/traces",
				HttpUpstreamType: &core.HttpUri_Cluster{
					Cluster: expectClusterName,
				},
				Timeout: &durationpb.Duration{Seconds: 3},
			},
			RequestHeadersToAdd: []*core.HeaderValueOption{
				{
					Header: &core.HeaderValue{
						Key:   "custom-header",
						Value: "custom-value",
					},
					AppendAction: core.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
				},
			},
		},
		ResourceDetectors: []*core.TypedExtensionConfig{
			{
				Name:        "envoy.tracers.opentelemetry.resource_detectors.environment",
				TypedConfig: protoconv.MessageToAny(&resourcedetectors.EnvironmentResourceDetectorConfig{}),
			},
			{
				Name:        "envoy.tracers.opentelemetry.resource_detectors.dynatrace",
				TypedConfig: protoconv.MessageToAny(&resourcedetectors.DynatraceResourceDetectorConfig{}),
			},
		},
	}
	fakeOtelHTTPAny := protoconv.MessageToAny(fakeOTelHTTPProviderConfig)
	return &tracingcfg.Tracing_Http{
		Name:       envoyOpenTelemetry,
		ConfigType: &tracingcfg.Tracing_Http_TypedConfig{TypedConfig: fakeOtelHTTPAny},
	}
}

func TestConvertTraceContextOption(t *testing.T) {
	testcases := []struct {
		name     string
		input    meshconfig.MeshConfig_ExtensionProvider_ZipkinTracingProvider_TraceContextOption
		expected tracingcfg.ZipkinConfig_TraceContextOption
	}{
		{
			name:     "USE_B3 option",
			input:    meshconfig.MeshConfig_ExtensionProvider_ZipkinTracingProvider_USE_B3,
			expected: tracingcfg.ZipkinConfig_USE_B3,
		},
		{
			name:     "USE_B3_WITH_W3C_PROPAGATION option",
			input:    meshconfig.MeshConfig_ExtensionProvider_ZipkinTracingProvider_USE_B3_WITH_W3C_PROPAGATION,
			expected: tracingcfg.ZipkinConfig_USE_B3_WITH_W3C_PROPAGATION,
		},
		{
			name:     "default fallback for unknown value",
			input:    meshconfig.MeshConfig_ExtensionProvider_ZipkinTracingProvider_TraceContextOption(999), // Invalid enum value
			expected: tracingcfg.ZipkinConfig_USE_B3,                                                        // Should fallback to default
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			result := convertTraceContextOption(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestZipkinConfig(t *testing.T) {
	testcases := []struct {
		name                string
		hostname            string
		cluster             string
		endpoint            string
		enable128BitTraceID bool
		traceContextOption  tracingcfg.ZipkinConfig_TraceContextOption
		expectedConfig      *tracingcfg.ZipkinConfig
	}{
		{
			name:                "basic zipkin config with USE_B3",
			hostname:            "zipkin.istio-system.svc.cluster.local",
			cluster:             "zipkin-cluster",
			endpoint:            "/api/v2/spans",
			enable128BitTraceID: true,
			traceContextOption:  tracingcfg.ZipkinConfig_USE_B3,
			expectedConfig: &tracingcfg.ZipkinConfig{
				CollectorCluster:         "zipkin-cluster",
				CollectorEndpoint:        "/api/v2/spans",
				CollectorEndpointVersion: tracingcfg.ZipkinConfig_HTTP_JSON,
				CollectorHostname:        "zipkin.istio-system.svc.cluster.local",
				TraceId_128Bit:           true,
				SharedSpanContext:        wrapperspb.Bool(false),
				TraceContextOption:       tracingcfg.ZipkinConfig_USE_B3,
			},
		},
		{
			name:                "zipkin config with dual B3/W3C headers",
			hostname:            "zipkin.istio-system.svc.cluster.local",
			cluster:             "zipkin-cluster",
			endpoint:            "/api/v2/spans",
			enable128BitTraceID: false,
			traceContextOption:  tracingcfg.ZipkinConfig_USE_B3_WITH_W3C_PROPAGATION,
			expectedConfig: &tracingcfg.ZipkinConfig{
				CollectorCluster:         "zipkin-cluster",
				CollectorEndpoint:        "/api/v2/spans",
				CollectorEndpointVersion: tracingcfg.ZipkinConfig_HTTP_JSON,
				CollectorHostname:        "zipkin.istio-system.svc.cluster.local",
				TraceId_128Bit:           false,
				SharedSpanContext:        wrapperspb.Bool(false),
				TraceContextOption:       tracingcfg.ZipkinConfig_USE_B3_WITH_W3C_PROPAGATION,
			},
		},
		{
			name:                "zipkin config with empty endpoint defaults to /api/v2/spans",
			hostname:            "zipkin.istio-system.svc.cluster.local",
			cluster:             "zipkin-cluster",
			endpoint:            "",
			enable128BitTraceID: true,
			traceContextOption:  tracingcfg.ZipkinConfig_USE_B3,
			expectedConfig: &tracingcfg.ZipkinConfig{
				CollectorCluster:         "zipkin-cluster",
				CollectorEndpoint:        "/api/v2/spans",
				CollectorEndpointVersion: tracingcfg.ZipkinConfig_HTTP_JSON,
				CollectorHostname:        "zipkin.istio-system.svc.cluster.local",
				TraceId_128Bit:           true,
				SharedSpanContext:        wrapperspb.Bool(false),
				TraceContextOption:       tracingcfg.ZipkinConfig_USE_B3,
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a mock proxy with version that supports TraceContextOption
			proxy := &model.Proxy{
				IstioVersion: &model.IstioVersion{Major: 1, Minor: 28, Patch: 0},
			}
			result, err := zipkinConfig(tc.hostname, tc.cluster, tc.endpoint, tc.enable128BitTraceID, tc.traceContextOption, nil, nil, proxy)
			assert.NoError(t, err)

			// Unmarshal the Any proto to compare the actual ZipkinConfig
			var actualConfig tracingcfg.ZipkinConfig
			err = result.UnmarshalTo(&actualConfig)
			assert.NoError(t, err)

			// Compare the configurations
			if diff := cmp.Diff(tc.expectedConfig, &actualConfig, protocmp.Transform()); diff != "" {
				t.Fatalf("zipkinConfig returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

// TestZipkinConfigVersionGating tests that TraceContextOption is only set for proxies that support it
func TestZipkinConfigVersionGating(t *testing.T) {
	testcases := []struct {
		name                  string
		proxyVersion          *model.IstioVersion
		traceContextOption    tracingcfg.ZipkinConfig_TraceContextOption
		expectTraceContextSet bool
	}{
		{
			name:                  "New proxy (1.28+) should get TraceContextOption",
			proxyVersion:          &model.IstioVersion{Major: 1, Minor: 28, Patch: 0},
			traceContextOption:    tracingcfg.ZipkinConfig_USE_B3_WITH_W3C_PROPAGATION,
			expectTraceContextSet: true,
		},
		{
			name:                  "Old proxy (1.27) should NOT get TraceContextOption",
			proxyVersion:          &model.IstioVersion{Major: 1, Minor: 27, Patch: 0},
			traceContextOption:    tracingcfg.ZipkinConfig_USE_B3_WITH_W3C_PROPAGATION,
			expectTraceContextSet: false,
		},
		{
			name:                  "Old proxy (1.22) should NOT get TraceContextOption",
			proxyVersion:          &model.IstioVersion{Major: 1, Minor: 22, Patch: 0},
			traceContextOption:    tracingcfg.ZipkinConfig_USE_B3_WITH_W3C_PROPAGATION,
			expectTraceContextSet: false,
		},
		{
			name:                  "Old proxy (1.20) should NOT get TraceContextOption",
			proxyVersion:          &model.IstioVersion{Major: 1, Minor: 20, Patch: 0},
			traceContextOption:    tracingcfg.ZipkinConfig_USE_B3_WITH_W3C_PROPAGATION,
			expectTraceContextSet: false,
		},
		{
			name:                  "Very old proxy (1.19) should NOT get TraceContextOption",
			proxyVersion:          &model.IstioVersion{Major: 1, Minor: 19, Patch: 0},
			traceContextOption:    tracingcfg.ZipkinConfig_USE_B3_WITH_W3C_PROPAGATION,
			expectTraceContextSet: false,
		},
		{
			name:                  "Proxy with nil version should NOT get TraceContextOption",
			proxyVersion:          nil,
			traceContextOption:    tracingcfg.ZipkinConfig_USE_B3_WITH_W3C_PROPAGATION,
			expectTraceContextSet: false,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			proxy := &model.Proxy{
				IstioVersion: tc.proxyVersion,
			}

			result, err := zipkinConfig(
				"zipkin.istio-system.svc.cluster.local",
				"outbound|9411||zipkin.istio-system.svc.cluster.local",
				"/api/v2/spans",
				true,
				tc.traceContextOption,
				nil,
				nil,
				proxy,
			)
			assert.NoError(t, err)

			// Unmarshal the Any proto to check the actual ZipkinConfig
			var actualConfig tracingcfg.ZipkinConfig
			err = result.UnmarshalTo(&actualConfig)
			assert.NoError(t, err)

			versionStr := "nil"
			if tc.proxyVersion != nil {
				versionStr = tc.proxyVersion.String()
			}

			if tc.expectTraceContextSet {
				// For new proxies, TraceContextOption should be set
				assert.Equal(t, tc.traceContextOption, actualConfig.TraceContextOption,
					"TraceContextOption should be set for proxy version %s", versionStr)
			} else {
				// For old proxies, TraceContextOption should be the default (USE_B3)
				assert.Equal(t, tracingcfg.ZipkinConfig_USE_B3, actualConfig.TraceContextOption,
					"TraceContextOption should default to USE_B3 for proxy version %s", versionStr)
			}
		})
	}
}

// TestZipkinConfigWithTimeoutAndHeaders tests Zipkin configuration with timeout and custom headers
func TestZipkinConfigWithTimeoutAndHeaders(t *testing.T) {
	testcases := []struct {
		name           string
		hostname       string
		cluster        string
		endpoint       string
		timeout        *durationpb.Duration
		headers        []*meshconfig.MeshConfig_ExtensionProvider_HttpHeader
		expectedConfig *tracingcfg.ZipkinConfig
	}{
		{
			name:     "zipkin with timeout uses HttpService",
			hostname: "zipkin.istio-system.svc.cluster.local",
			cluster:  "zipkin-cluster",
			endpoint: "/api/v2/spans",
			timeout:  durationpb.New(10 * time.Second), // 10s
			headers:  nil,
			expectedConfig: &tracingcfg.ZipkinConfig{
				CollectorEndpointVersion: tracingcfg.ZipkinConfig_HTTP_JSON,
				TraceId_128Bit:           true,
				SharedSpanContext:        wrapperspb.Bool(false),
				TraceContextOption:       tracingcfg.ZipkinConfig_USE_B3,
				CollectorService: &core.HttpService{
					HttpUri: &core.HttpUri{
						Uri: "http://zipkin.istio-system.svc.cluster.local/api/v2/spans",
						HttpUpstreamType: &core.HttpUri_Cluster{
							Cluster: "zipkin-cluster",
						},
						Timeout: durationpb.New(10 * time.Second),
					},
				},
			},
		},
		{
			name:     "zipkin with custom headers uses HttpService",
			hostname: "zipkin.istio-system.svc.cluster.local",
			cluster:  "zipkin-cluster",
			endpoint: "/api/v2/spans",
			timeout:  nil,
			headers: []*meshconfig.MeshConfig_ExtensionProvider_HttpHeader{
				{
					Name: "Authorization",
					HeaderValue: &meshconfig.MeshConfig_ExtensionProvider_HttpHeader_Value{
						Value: "Bearer token123",
					},
				},
			},
			expectedConfig: &tracingcfg.ZipkinConfig{
				CollectorEndpointVersion: tracingcfg.ZipkinConfig_HTTP_JSON,
				TraceId_128Bit:           true,
				SharedSpanContext:        wrapperspb.Bool(false),
				TraceContextOption:       tracingcfg.ZipkinConfig_USE_B3,
				CollectorService: &core.HttpService{
					HttpUri: &core.HttpUri{
						Uri:     "http://zipkin.istio-system.svc.cluster.local/api/v2/spans",
						Timeout: durationpb.New(5 * time.Second),
						HttpUpstreamType: &core.HttpUri_Cluster{
							Cluster: "zipkin-cluster",
						},
					},
					RequestHeadersToAdd: []*core.HeaderValueOption{
						{
							AppendAction: core.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
							Header: &core.HeaderValue{
								Key:   "Authorization",
								Value: "Bearer token123",
							},
						},
					},
				},
			},
		},
		{
			name:     "zipkin with both timeout and headers",
			hostname: "zipkin.istio-system.svc.cluster.local",
			cluster:  "zipkin-cluster",
			endpoint: "/api/v2/spans",
			timeout:  durationpb.New(5000000000), // 5s
			headers: []*meshconfig.MeshConfig_ExtensionProvider_HttpHeader{
				{
					Name: "X-Custom-Header",
					HeaderValue: &meshconfig.MeshConfig_ExtensionProvider_HttpHeader_Value{
						Value: "custom-value",
					},
				},
				{
					Name: "X-Tenant-ID",
					HeaderValue: &meshconfig.MeshConfig_ExtensionProvider_HttpHeader_Value{
						Value: "tenant-123",
					},
				},
			},
			expectedConfig: &tracingcfg.ZipkinConfig{
				CollectorEndpointVersion: tracingcfg.ZipkinConfig_HTTP_JSON,
				TraceId_128Bit:           true,
				SharedSpanContext:        wrapperspb.Bool(false),
				TraceContextOption:       tracingcfg.ZipkinConfig_USE_B3,
				CollectorService: &core.HttpService{
					HttpUri: &core.HttpUri{
						Uri: "http://zipkin.istio-system.svc.cluster.local/api/v2/spans",
						HttpUpstreamType: &core.HttpUri_Cluster{
							Cluster: "zipkin-cluster",
						},
						Timeout: durationpb.New(5000000000),
					},
					RequestHeadersToAdd: []*core.HeaderValueOption{
						{
							AppendAction: core.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
							Header: &core.HeaderValue{
								Key:   "X-Custom-Header",
								Value: "custom-value",
							},
						},
						{
							AppendAction: core.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
							Header: &core.HeaderValue{
								Key:   "X-Tenant-ID",
								Value: "tenant-123",
							},
						},
					},
				},
			},
		},
		{
			name:     "zipkin without timeout and headers uses HttpService (for 1.29+)",
			hostname: "zipkin.istio-system.svc.cluster.local",
			cluster:  "zipkin-cluster",
			endpoint: "/api/v2/spans",
			timeout:  nil,
			headers:  nil,
			expectedConfig: &tracingcfg.ZipkinConfig{
				CollectorEndpointVersion: tracingcfg.ZipkinConfig_HTTP_JSON,
				TraceId_128Bit:           true,
				SharedSpanContext:        wrapperspb.Bool(false),
				TraceContextOption:       tracingcfg.ZipkinConfig_USE_B3,
				CollectorService: &core.HttpService{
					HttpUri: &core.HttpUri{
						Uri:     "http://zipkin.istio-system.svc.cluster.local/api/v2/spans",
						Timeout: durationpb.New(5 * time.Second),
						HttpUpstreamType: &core.HttpUri_Cluster{
							Cluster: "zipkin-cluster",
						},
					},
				},
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			// Use 1.29+ proxy to test timeout/headers support
			proxy := &model.Proxy{
				IstioVersion: &model.IstioVersion{Major: 1, Minor: 29, Patch: 0},
			}
			result, err := zipkinConfig(tc.hostname, tc.cluster, tc.endpoint, true, tracingcfg.ZipkinConfig_USE_B3, tc.timeout, tc.headers, proxy)
			assert.NoError(t, err)

			var actualConfig tracingcfg.ZipkinConfig
			err = result.UnmarshalTo(&actualConfig)
			assert.NoError(t, err)

			if diff := cmp.Diff(tc.expectedConfig, &actualConfig, protocmp.Transform()); diff != "" {
				t.Fatalf("zipkinConfig returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

// TestZipkinConfigTimeoutHeadersVersionGating tests that timeout and headers are only used for proxies that support it
func TestZipkinConfigTimeoutHeadersVersionGating(t *testing.T) {
	testcases := []struct {
		name              string
		proxyVersion      *model.IstioVersion
		timeout           *durationpb.Duration
		headers           []*meshconfig.MeshConfig_ExtensionProvider_HttpHeader
		expectHTTPService bool
	}{
		{
			name:              "New proxy (1.29+) with timeout should use HttpService",
			proxyVersion:      &model.IstioVersion{Major: 1, Minor: 29, Patch: 0},
			timeout:           durationpb.New(10 * time.Second),
			headers:           nil,
			expectHTTPService: true,
		},
		{
			name:         "New proxy (1.29+) with headers should use HttpService",
			proxyVersion: &model.IstioVersion{Major: 1, Minor: 29, Patch: 0},
			timeout:      nil,
			headers: []*meshconfig.MeshConfig_ExtensionProvider_HttpHeader{
				{
					Name: "Authorization",
					HeaderValue: &meshconfig.MeshConfig_ExtensionProvider_HttpHeader_Value{
						Value: "Bearer token",
					},
				},
			},
			expectHTTPService: true,
		},
		{
			name:              "Old proxy (1.28) with timeout should NOT use HttpService",
			proxyVersion:      &model.IstioVersion{Major: 1, Minor: 28, Patch: 0},
			timeout:           durationpb.New(10 * time.Second),
			headers:           nil,
			expectHTTPService: false,
		},
		{
			name:         "Old proxy (1.28) with headers should NOT use HttpService",
			proxyVersion: &model.IstioVersion{Major: 1, Minor: 28, Patch: 0},
			timeout:      nil,
			headers: []*meshconfig.MeshConfig_ExtensionProvider_HttpHeader{
				{
					Name: "Authorization",
					HeaderValue: &meshconfig.MeshConfig_ExtensionProvider_HttpHeader_Value{
						Value: "Bearer token",
					},
				},
			},
			expectHTTPService: false,
		},
		{
			name:              "New proxy (1.29+) without timeout/headers should use HttpService",
			proxyVersion:      &model.IstioVersion{Major: 1, Minor: 29, Patch: 0},
			timeout:           nil,
			headers:           nil,
			expectHTTPService: true,
		},
		{
			name:              "Old proxy (1.28) without timeout/headers should NOT use HttpService",
			proxyVersion:      &model.IstioVersion{Major: 1, Minor: 28, Patch: 0},
			timeout:           nil,
			headers:           nil,
			expectHTTPService: false,
		},
		{
			name:              "Proxy with nil version should NOT use HttpService",
			proxyVersion:      nil,
			timeout:           durationpb.New(10000000000),
			headers:           nil,
			expectHTTPService: false,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			proxy := &model.Proxy{
				IstioVersion: tc.proxyVersion,
			}

			result, err := zipkinConfig(
				"zipkin.istio-system.svc.cluster.local",
				"outbound|9411||zipkin.istio-system.svc.cluster.local",
				"/api/v2/spans",
				true,
				tracingcfg.ZipkinConfig_USE_B3,
				tc.timeout,
				tc.headers,
				proxy,
			)
			assert.NoError(t, err)

			var actualConfig tracingcfg.ZipkinConfig
			err = result.UnmarshalTo(&actualConfig)
			assert.NoError(t, err)

			versionStr := "nil"
			if tc.proxyVersion != nil {
				versionStr = tc.proxyVersion.String()
			}

			if tc.expectHTTPService {
				// For new proxies, HttpService should be set
				if actualConfig.CollectorService == nil {
					t.Fatalf("CollectorService should be set for proxy version %s", versionStr)
				}
				if actualConfig.CollectorCluster != "" {
					t.Fatalf("CollectorCluster should be empty when using HttpService for proxy version %s, got: %s", versionStr, actualConfig.CollectorCluster)
				}
			} else {
				// For old proxies, legacy fields should be set
				if actualConfig.CollectorService != nil {
					t.Fatalf("CollectorService should NOT be set for proxy version %s", versionStr)
				}
				if actualConfig.CollectorCluster == "" {
					t.Fatalf("CollectorCluster should be set when using legacy config for proxy version %s", versionStr)
				}
			}
		})
	}
}

func TestGetHeaderValue(t *testing.T) {
	t.Setenv("CUSTOM_ENV_NAME", "custom-env-value")
	cases := []struct {
		name     string
		input    *meshconfig.MeshConfig_ExtensionProvider_HttpHeader
		expected string
	}{
		{
			name: "custom-value",
			input: &meshconfig.MeshConfig_ExtensionProvider_HttpHeader{
				HeaderValue: &meshconfig.MeshConfig_ExtensionProvider_HttpHeader_Value{
					Value: "custom-value",
				},
			},
			expected: "custom-value",
		},
		{
			name: "read-from-env",
			input: &meshconfig.MeshConfig_ExtensionProvider_HttpHeader{
				HeaderValue: &meshconfig.MeshConfig_ExtensionProvider_HttpHeader_EnvName{
					EnvName: "CUSTOM_ENV_NAME",
				},
			},
			expected: "custom-env-value",
		},
		{
			name: "read-from-env-not-exists",
			input: &meshconfig.MeshConfig_ExtensionProvider_HttpHeader{
				HeaderValue: &meshconfig.MeshConfig_ExtensionProvider_HttpHeader_EnvName{
					EnvName: "CUSTOM_ENV_NAME1",
				},
			},
			expected: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := getHeaderValue(tc.input)
			assert.Equal(t, tc.expected, got)
		})
	}
}
