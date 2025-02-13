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
			opts:   fakeOptsOnlyOpenTelemetryGrpcWithInitialMetadataTelemetryAPI(),
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
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			hcm := &hcm.HttpConnectionManager{}
			gotReqIDExtCtx := configureTracingFromTelemetry(tc.inSpec, tc.opts.push, tc.opts.proxy, hcm, 0)
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
			configureTracingFromTelemetry(inSpec, opts.push, opts.proxy, hcm, 0)

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
							Name:  expectedHeader,
							Value: expectedToken,
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
							Name:  expectedHeader,
							Value: expectedToken,
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
	configureTracingFromTelemetry(inSpec, opts.push, opts.proxy, hcm, 0)

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
		Metadata: &model.NodeMetadata{},
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
							Name:  "Authentication",
							Value: "token-xxxxx",
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
							Name:  "custom-header",
							Value: "custom-value",
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
		Metadata: &model.NodeMetadata{
			ProxyConfig: &model.NodeMetaProxyConfig{},
		},
	}

	return opts
}

func fakeOptsOnlyOpenTelemetryGrpcWithInitialMetadataTelemetryAPI() gatewayListenerOpts {
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
										Name:  "Authentication",
										Value: "token-xxxxx",
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
										Name:  "custom-header",
										Value: "custom-value",
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

var fakeEnvTag = &tracing.CustomTag{
	Tag: "test",
	Type: &tracing.CustomTag_Environment_{
		Environment: &tracing.CustomTag_Environment{
			Name: "FOO",
		},
	},
}

func fakeZipkinProvider(expectClusterName, expectAuthority, expectEndpoint string, enableTraceID bool) *tracingcfg.Tracing_Http {
	fakeZipkinProviderConfig := &tracingcfg.ZipkinConfig{
		CollectorCluster:         expectClusterName,
		CollectorEndpoint:        expectEndpoint,
		CollectorEndpointVersion: tracingcfg.ZipkinConfig_HTTP_JSON,
		CollectorHostname:        expectAuthority,
		TraceId_128Bit:           enableTraceID,
		SharedSpanContext:        wrapperspb.Bool(false),
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
