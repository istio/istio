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

package v1alpha3

import (
	"testing"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	tracingcfg "github.com/envoyproxy/go-control-plane/envoy/config/trace/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
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
		name               string
		opts               gatewayListenerOpts
		inSpec             *model.TracingConfig
		want               *hcm.HttpConnectionManager_Tracing
		wantStartChildSpan bool
		wantReqIDExtCtx    *requestidextension.UUIDRequestIDExtensionContext
	}{
		{
			name:               "no telemetry api",
			opts:               fakeOptsNoTelemetryAPI(),
			want:               fakeTracingConfigNoProvider(55.55, 13, append(defaultTracingTags(), fakeEnvTag)),
			wantStartChildSpan: false,
			wantReqIDExtCtx:    nil,
		},
		{
			name: "default providers",
			inSpec: &model.TracingConfig{
				ClientSpec: model.TracingSpec{
					Disabled: true,
				},
				ServerSpec: model.TracingSpec{
					Provider: fakeZipkin(),
				},
			},
			opts:               fakeOptsWithDefaultProviders(),
			want:               fakeTracingConfig(fakeZipkinProvider(clusterName, authority, true), 55.5, 256, defaultTracingTags()),
			wantStartChildSpan: false,
			wantReqIDExtCtx:    &requestidextension.UUIDRequestIDExtensionContext{},
		},
		{
			name:               "no telemetry api and nil custom tag",
			opts:               fakeOptsNoTelemetryAPIWithNilCustomTag(),
			want:               fakeTracingConfigNoProvider(55.55, 13, defaultTracingTags()),
			wantStartChildSpan: false,
			wantReqIDExtCtx:    nil,
		},
		{
			name:               "only telemetry api (no provider)",
			inSpec:             fakeTracingSpecNoProvider(99.999, false, true),
			opts:               fakeOptsOnlyZipkinTelemetryAPI(),
			want:               fakeTracingConfigNoProvider(99.999, 0, append(defaultTracingTags(), fakeEnvTag)),
			wantStartChildSpan: false,
			wantReqIDExtCtx:    &defaultUUIDExtensionCtx,
		},
		{
			name:               "only telemetry api (no provider) with nil custom tag",
			inSpec:             fakeTracingSpecNoProviderWithNilCustomTag(99.999, false, true),
			opts:               fakeOptsOnlyZipkinTelemetryAPI(),
			want:               fakeTracingConfigNoProvider(99.999, 0, defaultTracingTags()),
			wantStartChildSpan: false,
			wantReqIDExtCtx:    &defaultUUIDExtensionCtx,
		},
		{
			name:               "only telemetry api (with provider)",
			inSpec:             fakeTracingSpec(fakeZipkin(), 99.999, false, true),
			opts:               fakeOptsOnlyZipkinTelemetryAPI(),
			want:               fakeTracingConfig(fakeZipkinProvider(clusterName, authority, true), 99.999, 256, append(defaultTracingTags(), fakeEnvTag)),
			wantStartChildSpan: false,
			wantReqIDExtCtx:    &defaultUUIDExtensionCtx,
		},
		{
			name:               "zipkin enable 64bit trace id",
			inSpec:             fakeTracingSpec(fakeZipkinEnable64bitTraceID(), 99.999, false, true),
			opts:               fakeOptsOnlyZipkinTelemetryAPI(),
			want:               fakeTracingConfig(fakeZipkinProvider(clusterName, authority, false), 99.999, 256, append(defaultTracingTags(), fakeEnvTag)),
			wantStartChildSpan: false,
			wantReqIDExtCtx:    &defaultUUIDExtensionCtx,
		},
		{
			name:               "both tracing enabled (no provider)",
			inSpec:             fakeTracingSpecNoProvider(99.999, false, true),
			opts:               fakeOptsMeshAndTelemetryAPI(true /* enable tracing */),
			want:               fakeTracingConfigNoProvider(99.999, 13, append(defaultTracingTags(), fakeEnvTag)),
			wantStartChildSpan: false,
			wantReqIDExtCtx:    &defaultUUIDExtensionCtx,
		},
		{
			name:               "both tracing disabled (no provider)",
			inSpec:             fakeTracingSpecNoProvider(99.999, false, true),
			opts:               fakeOptsMeshAndTelemetryAPI(false /* no enable tracing */),
			want:               fakeTracingConfigNoProvider(99.999, 13, append(defaultTracingTags(), fakeEnvTag)),
			wantStartChildSpan: false,
			wantReqIDExtCtx:    &defaultUUIDExtensionCtx,
		},
		{
			name:               "both tracing enabled (with provider)",
			inSpec:             fakeTracingSpec(fakeZipkin(), 99.999, false, true),
			opts:               fakeOptsMeshAndTelemetryAPI(true /* enable tracing */),
			want:               fakeTracingConfig(fakeZipkinProvider(clusterName, authority, true), 99.999, 256, append(defaultTracingTags(), fakeEnvTag)),
			wantStartChildSpan: false,
			wantReqIDExtCtx:    &defaultUUIDExtensionCtx,
		},
		{
			name:               "both tracing disabled (with provider)",
			inSpec:             fakeTracingSpec(fakeZipkin(), 99.999, false, true),
			opts:               fakeOptsMeshAndTelemetryAPI(false /* no enable tracing */),
			want:               fakeTracingConfig(fakeZipkinProvider(clusterName, authority, true), 99.999, 256, append(defaultTracingTags(), fakeEnvTag)),
			wantStartChildSpan: false,
			wantReqIDExtCtx:    &defaultUUIDExtensionCtx,
		},
		{
			name:               "basic config (with datadog provider)",
			inSpec:             fakeTracingSpec(fakeDatadog(), 99.999, false, true),
			opts:               fakeOptsOnlyDatadogTelemetryAPI(),
			want:               fakeTracingConfig(fakeDatadogProvider("fake-cluster", "testhost", clusterName), 99.999, 256, append(defaultTracingTags(), fakeEnvTag)),
			wantStartChildSpan: false,
			wantReqIDExtCtx:    &defaultUUIDExtensionCtx,
		},
		{
			name:               "basic config (with skywalking provider)",
			inSpec:             fakeTracingSpec(fakeSkywalking(), 99.999, false, false),
			opts:               fakeOptsOnlySkywalkingTelemetryAPI(),
			want:               fakeTracingConfig(fakeSkywalkingProvider(clusterName, authority), 99.999, 0, append(defaultTracingTags(), fakeEnvTag)),
			wantStartChildSpan: true,
			wantReqIDExtCtx:    &requestidextension.UUIDRequestIDExtensionContext{UseRequestIDForTraceSampling: false},
		},
		{
			name:               "basic config (with opentelemetry provider via grpc)",
			inSpec:             fakeTracingSpec(fakeOpenTelemetryGrpc(), 99.999, false, true),
			opts:               fakeOptsOnlyOpenTelemetryGrpcTelemetryAPI(),
			want:               fakeTracingConfig(fakeOpenTelemetryGrpcProvider(clusterName, authority), 99.999, 256, append(defaultTracingTags(), fakeEnvTag)),
			wantStartChildSpan: false,
			wantReqIDExtCtx:    &defaultUUIDExtensionCtx,
		},
		{
			name:               "basic config (with opentelemetry provider via http)",
			inSpec:             fakeTracingSpec(fakeOpenTelemetryHttp(), 99.999, false, true),
			opts:               fakeOptsOnlyOpenTelemetryHttpTelemetryAPI(),
			want:               fakeTracingConfig(fakeOpenTelemetryHttpProvider(clusterName, authority), 99.999, 256, append(defaultTracingTags(), fakeEnvTag)),
			wantStartChildSpan: false,
			wantReqIDExtCtx:    &defaultUUIDExtensionCtx,
		},
		{
			name:               "client-only config for server",
			inSpec:             fakeClientOnlyTracingSpec(fakeSkywalking(), 99.999, false, false),
			opts:               fakeInboundOptsOnlySkywalkingTelemetryAPI(),
			want:               nil,
			wantStartChildSpan: false,
			wantReqIDExtCtx:    nil,
		},
		{
			name:               "server-only config for server",
			inSpec:             fakeServerOnlyTracingSpec(fakeSkywalking(), 99.999, false, false),
			opts:               fakeInboundOptsOnlySkywalkingTelemetryAPI(),
			want:               fakeTracingConfig(fakeSkywalkingProvider(clusterName, authority), 99.999, 0, append(defaultTracingTags(), fakeEnvTag)),
			wantStartChildSpan: true,
			wantReqIDExtCtx:    &requestidextension.UUIDRequestIDExtensionContext{UseRequestIDForTraceSampling: false},
		},
		{
			name:               "invalid provider",
			inSpec:             fakeTracingSpec(fakePrometheus(), 99.999, false, true),
			opts:               fakeOptsMeshAndTelemetryAPI(true /* enable tracing */),
			want:               nil,
			wantStartChildSpan: false,
			wantReqIDExtCtx:    nil,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			hcm := &hcm.HttpConnectionManager{}
			startChildSpan, gotReqIDExtCtx := configureTracingFromTelemetry(tc.inSpec, tc.opts.push, tc.opts.proxy, hcm, 0)
			if diff := cmp.Diff(tc.want, hcm.Tracing, protocmp.Transform()); diff != "" {
				t.Fatalf("configureTracing returned unexpected diff (-want +got):\n%s", diff)
			}
			assert.Equal(t, tc.want, hcm.Tracing)
			assert.Equal(t, startChildSpan, tc.wantStartChildSpan)
			assert.Equal(t, tc.wantReqIDExtCtx, gotReqIDExtCtx)
		})
	}
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

func fakeOpenTelemetryHttp() *meshconfig.MeshConfig_ExtensionProvider {
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

func fakeOptsOnlyOpenTelemetryHttpTelemetryAPI() gatewayListenerOpts {
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

func fakeInboundOptsOnlySkywalkingTelemetryAPI() gatewayListenerOpts {
	opts := fakeOptsOnlySkywalkingTelemetryAPI()
	return opts
}

func fakeTracingSpecNoProvider(sampling float64, disableReporting bool, useRequestIDForTraceSampling bool) *model.TracingConfig {
	return fakeTracingSpec(nil, sampling, disableReporting, useRequestIDForTraceSampling)
}

func fakeTracingSpecNoProviderWithNilCustomTag(sampling float64, disableReporting bool, useRequestIDForTraceSampling bool) *model.TracingConfig {
	return fakeTracingSpecWithNilCustomTag(nil, sampling, disableReporting, useRequestIDForTraceSampling)
}

func fakeTracingSpec(provider *meshconfig.MeshConfig_ExtensionProvider, sampling float64, disableReporting bool,
	useRequestIDForTraceSampling bool,
) *model.TracingConfig {
	t := &model.TracingConfig{
		ClientSpec: tracingSpec(provider, sampling, disableReporting, useRequestIDForTraceSampling),
		ServerSpec: tracingSpec(provider, sampling, disableReporting, useRequestIDForTraceSampling),
	}
	return t
}

func fakeClientOnlyTracingSpec(provider *meshconfig.MeshConfig_ExtensionProvider, sampling float64, disableReporting bool,
	useRequestIDForTraceSampling bool,
) *model.TracingConfig {
	t := &model.TracingConfig{
		ClientSpec: tracingSpec(provider, sampling, disableReporting, useRequestIDForTraceSampling),
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
		ServerSpec: tracingSpec(provider, sampling, disableReporting, useRequestIDForTraceSampling),
	}
	return t
}

func tracingSpec(provider *meshconfig.MeshConfig_ExtensionProvider, sampling float64, disableReporting bool,
	useRequestIDForTraceSampling bool,
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
		},
		ServerSpec: model.TracingSpec{
			Provider:                 provider,
			Disabled:                 disableReporting,
			RandomSamplingPercentage: ptr.Of(sampling),
			CustomTags: map[string]*tpb.Tracing_CustomTag{
				"test": nil,
			},
			UseRequestIDForTraceSampling: useRequestIDForTraceSampling,
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

var fakeEnvTag = &tracing.CustomTag{
	Tag: "test",
	Type: &tracing.CustomTag_Environment_{
		Environment: &tracing.CustomTag_Environment{
			Name: "FOO",
		},
	},
}

func fakeZipkinProvider(expectClusterName, expectAuthority string, enableTraceID bool) *tracingcfg.Tracing_Http {
	fakeZipkinProviderConfig := &tracingcfg.ZipkinConfig{
		CollectorCluster:         expectClusterName,
		CollectorEndpoint:        "/api/v2/spans",
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

func fakeOpenTelemetryHttpProvider(expectClusterName, expectAuthority string) *tracingcfg.Tracing_Http {
	fakeOTelHttpProviderConfig := &tracingcfg.OpenTelemetryConfig{
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
	fakeOtelHttpAny := protoconv.MessageToAny(fakeOTelHttpProviderConfig)
	return &tracingcfg.Tracing_Http{
		Name:       envoyOpenTelemetry,
		ConfigType: &tracingcfg.Tracing_Http_TypedConfig{TypedConfig: fakeOtelHttpAny},
	}
}
