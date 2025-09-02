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
	"strings"
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	fileaccesslog "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/file/v3"
	httpwasm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/wasm/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	wasmfilter "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/wasm/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/envoy/extensions/stats"
	"istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	tpb "istio.io/api/telemetry/v1alpha1"
	"istio.io/api/type/v1beta1"
	networking "istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/protomarshal"
)

var (
	jsonTextProvider = &meshconfig.MeshConfig_ExtensionProvider{
		Name: "envoy-json",
		Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
			EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
				Path: "/dev/stdout",
				LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat{
					LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat_Labels{
						Labels: &structpb.Struct{},
					},
				},
			},
		},
	}

	textFormattersProvider = &meshconfig.MeshConfig_ExtensionProvider{
		Name: "envoy-text-formatters",
		Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
			EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
				Path: "/dev/stdout",
				LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat{
					LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat_Text{
						Text: "%REQ_WITHOUT_QUERY(key1:val1)% REQ_WITHOUT_QUERY(key2:val1)% %METADATA(UPSTREAM_HOST:istio)% %METADATA(CLUSTER:istio)%\n",
					},
				},
			},
		},
	}

	jsonFormattersProvider = &meshconfig.MeshConfig_ExtensionProvider{
		Name: "envoy-json-formatters",
		Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
			EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
				Path: "/dev/stdout",
				LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat{
					LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat_Labels{
						Labels: &structpb.Struct{
							Fields: map[string]*structpb.Value{
								"req1": {Kind: &structpb.Value_StringValue{StringValue: "%REQ_WITHOUT_QUERY(key1:val1)%"}},
								"req2": {Kind: &structpb.Value_StringValue{StringValue: "%REQ_WITHOUT_QUERY(key2:val1)%"}},
								"key1": {Kind: &structpb.Value_StringValue{StringValue: "%METADATA(CLUSTER:istio)%"}},
								"key2": {Kind: &structpb.Value_StringValue{StringValue: "%METADATA(UPSTREAM_HOST:istio)%"}},
							},
						},
					},
				},
			},
		},
	}

	defaultJSONLabelsOut = &fileaccesslog.FileAccessLog{
		Path: "/dev/stdout",
		AccessLogFormat: &fileaccesslog.FileAccessLog_LogFormat{
			LogFormat: &core.SubstitutionFormatString{
				Format: &core.SubstitutionFormatString_JsonFormat{
					JsonFormat: EnvoyJSONLogFormatIstio,
				},
				JsonFormatOptions: &core.JsonFormatOptions{SortProperties: false},
			},
		},
	}

	formattersJSONLabelsOut = &fileaccesslog.FileAccessLog{
		Path: "/dev/stdout",
		AccessLogFormat: &fileaccesslog.FileAccessLog_LogFormat{
			LogFormat: &core.SubstitutionFormatString{
				Formatters: []*core.TypedExtensionConfig{
					reqWithoutQueryFormatter,
					metadataFormatter,
				},
				Format: &core.SubstitutionFormatString_JsonFormat{
					JsonFormat: &structpb.Struct{
						Fields: map[string]*structpb.Value{
							"req1": {Kind: &structpb.Value_StringValue{StringValue: "%REQ_WITHOUT_QUERY(key1:val1)%"}},
							"req2": {Kind: &structpb.Value_StringValue{StringValue: "%REQ_WITHOUT_QUERY(key2:val1)%"}},
							"key1": {Kind: &structpb.Value_StringValue{StringValue: "%METADATA(CLUSTER:istio)%"}},
							"key2": {Kind: &structpb.Value_StringValue{StringValue: "%METADATA(UPSTREAM_HOST:istio)%"}},
						},
					},
				},
				JsonFormatOptions: &core.JsonFormatOptions{SortProperties: false},
			},
		},
	}

	formattersTextLabelsOut = &fileaccesslog.FileAccessLog{
		Path: "/dev/stdout",
		AccessLogFormat: &fileaccesslog.FileAccessLog_LogFormat{
			LogFormat: &core.SubstitutionFormatString{
				Formatters: []*core.TypedExtensionConfig{
					reqWithoutQueryFormatter,
					metadataFormatter,
				},
				Format: &core.SubstitutionFormatString_TextFormatSource{
					TextFormatSource: &core.DataSource{
						Specifier: &core.DataSource_InlineString{
							InlineString: "%REQ_WITHOUT_QUERY(key1:val1)% REQ_WITHOUT_QUERY(key2:val1)% %METADATA(UPSTREAM_HOST:istio)% %METADATA(CLUSTER:istio)%\n",
						},
					},
				},
			},
		},
	}
)

func createTestTelemetries(configs []config.Config, t *testing.T) (*Telemetries, *PushContext) {
	t.Helper()

	store := &telemetryStore{}
	for _, cfg := range configs {
		store.add(cfg)
	}
	m := mesh.DefaultMeshConfig()

	m.ExtensionProviders = append(m.ExtensionProviders, jsonTextProvider, textFormattersProvider, jsonFormattersProvider)

	environment := &Environment{
		ConfigStore: store,
		Watcher:     meshwatcher.NewTestWatcher(m),
	}
	telemetries := getTelemetries(environment)

	ctx := NewPushContext()
	ctx.Mesh = m
	return telemetries, ctx
}

func newTelemetry(ns string, spec config.Spec) config.Config {
	return config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.Telemetry,
			Name:             "default",
			Namespace:        ns,
		},
		Spec: spec,
	}
}

type telemetryStore struct {
	ConfigStore

	data []struct {
		typ config.GroupVersionKind
		ns  string
		cfg config.Config
	}
}

func (ts *telemetryStore) add(cfg config.Config) {
	ts.data = append(ts.data, struct {
		typ config.GroupVersionKind
		ns  string
		cfg config.Config
	}{
		typ: cfg.GroupVersionKind,
		ns:  cfg.Namespace,
		cfg: cfg,
	})
}

func (ts *telemetryStore) Schemas() collection.Schemas {
	return collection.SchemasFor()
}

func (ts *telemetryStore) Get(_ config.GroupVersionKind, _, _ string) *config.Config {
	return nil
}

func (ts *telemetryStore) List(typ config.GroupVersionKind, namespace string) []config.Config {
	var configs []config.Config
	for _, data := range ts.data {
		if data.typ == typ {
			if namespace != "" && data.ns == namespace {
				continue
			}
			configs = append(configs, data.cfg)
		}
	}
	return configs
}

func newTracingConfig(providerName string, disabled bool) *TracingConfig {
	return &TracingConfig{
		ClientSpec: TracingSpec{
			Provider:                     &meshconfig.MeshConfig_ExtensionProvider{Name: providerName},
			Disabled:                     disabled,
			UseRequestIDForTraceSampling: true,
			EnableIstioTags:              true,
		},
		ServerSpec: TracingSpec{
			Provider:                     &meshconfig.MeshConfig_ExtensionProvider{Name: providerName},
			Disabled:                     disabled,
			UseRequestIDForTraceSampling: true,
			EnableIstioTags:              true,
		},
	}
}

const (
	reportingEnabled  = false
	reportingDisabled = !reportingEnabled
)

func TestTracing(t *testing.T) {
	sidecar := &Proxy{
		ConfigNamespace: "default",
		Labels:          map[string]string{"app": "test"},
		Metadata:        &NodeMetadata{Labels: map[string]string{"app": "test"}},
	}
	envoy := &tpb.Telemetry{
		Tracing: []*tpb.Tracing{
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy",
					},
				},
			},
		},
	}
	stackdriver := &tpb.Telemetry{
		Tracing: []*tpb.Tracing{
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
		Tracing: []*tpb.Tracing{{}},
	}
	disabled := &tpb.Telemetry{
		Tracing: []*tpb.Tracing{
			{
				DisableSpanReporting: &wrappers.BoolValue{Value: true},
			},
		},
	}
	overidesA := &tpb.Telemetry{
		Tracing: []*tpb.Tracing{
			{
				RandomSamplingPercentage: &wrappers.DoubleValue{Value: 50.0},
				CustomTags: map[string]*tpb.Tracing_CustomTag{
					"foo": {},
					"bar": {},
				},
				UseRequestIdForTraceSampling: &wrappers.BoolValue{Value: false},
				EnableIstioTags:              &wrappers.BoolValue{Value: false},
			},
		},
	}
	overidesB := &tpb.Telemetry{
		Tracing: []*tpb.Tracing{
			{
				RandomSamplingPercentage: &wrappers.DoubleValue{Value: 80.0},
				CustomTags: map[string]*tpb.Tracing_CustomTag{
					"foo": {},
					"baz": {},
				},
				UseRequestIdForTraceSampling: &wrappers.BoolValue{Value: true},
				EnableIstioTags:              &wrappers.BoolValue{Value: true},
			},
		},
	}
	overridesWithDefaultSampling := &tpb.Telemetry{
		Tracing: []*tpb.Tracing{
			{
				CustomTags: map[string]*tpb.Tracing_CustomTag{
					"foo": {},
					"baz": {},
				},
			},
		},
	}
	nonExistent := &tpb.Telemetry{
		Tracing: []*tpb.Tracing{
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "custom-provider",
					},
				},
			},
		},
	}
	clientSideSampling := &tpb.Telemetry{
		Tracing: []*tpb.Tracing{
			{
				Match: &tpb.Tracing_TracingSelector{
					Mode: tpb.WorkloadMode_CLIENT,
				},
				Providers: []*tpb.ProviderRef{
					{
						Name: "stackdriver",
					},
				},
				RandomSamplingPercentage: &wrappers.DoubleValue{Value: 99.9},
			},
		},
	}
	serverSideDisabled := &tpb.Telemetry{
		Tracing: []*tpb.Tracing{
			{
				Match: &tpb.Tracing_TracingSelector{
					Mode: tpb.WorkloadMode_SERVER,
				},
				DisableSpanReporting: &wrappers.BoolValue{Value: true},
			},
		},
	}

	tests := []struct {
		name             string
		cfgs             []config.Config
		proxy            *Proxy
		defaultProviders []string
		want             *TracingConfig
	}{
		{
			"empty",
			nil,
			sidecar,
			nil,
			nil,
		},
		{
			"default provider only",
			nil,
			sidecar,
			[]string{"envoy"},
			newTracingConfig("envoy", reportingEnabled),
		},
		{
			"provider only",
			[]config.Config{newTelemetry("istio-system", envoy)},
			sidecar,
			nil,
			newTracingConfig("envoy", reportingEnabled),
		},
		{
			"override default",
			[]config.Config{newTelemetry("istio-system", envoy)},
			sidecar,
			[]string{"stackdriver"},
			newTracingConfig("envoy", reportingEnabled),
		},
		{
			"override namespace",
			[]config.Config{newTelemetry("istio-system", envoy), newTelemetry("default", stackdriver)},
			sidecar,
			nil,
			newTracingConfig("stackdriver", reportingEnabled),
		},
		{
			"empty config inherits",
			[]config.Config{newTelemetry("istio-system", envoy), newTelemetry("default", empty)},
			sidecar,
			nil,
			newTracingConfig("envoy", reportingEnabled),
		},
		{
			"disable config",
			[]config.Config{newTelemetry("istio-system", envoy), newTelemetry("default", disabled)},
			sidecar,
			nil,
			newTracingConfig("envoy", reportingDisabled),
		},
		{
			"disable default",
			[]config.Config{newTelemetry("default", disabled)},
			sidecar,
			[]string{"envoy"},
			newTracingConfig("envoy", reportingDisabled),
		},
		{
			"non existing",
			[]config.Config{newTelemetry("default", nonExistent)},
			sidecar,
			[]string{"envoy"},
			&TracingConfig{
				ClientSpec: TracingSpec{Disabled: true, UseRequestIDForTraceSampling: true, EnableIstioTags: true},
				ServerSpec: TracingSpec{Disabled: true, UseRequestIDForTraceSampling: true, EnableIstioTags: true},
			},
		},
		{
			"overrides",
			[]config.Config{newTelemetry("istio-system", overidesA)},
			sidecar,
			[]string{"envoy"},
			&TracingConfig{
				ClientSpec: TracingSpec{
					Provider:                 &meshconfig.MeshConfig_ExtensionProvider{Name: "envoy"},
					RandomSamplingPercentage: ptr.Of(50.0),
					CustomTags: map[string]*tpb.Tracing_CustomTag{
						"foo": {},
						"bar": {},
					},
					UseRequestIDForTraceSampling: false,
				}, ServerSpec: TracingSpec{
					Provider:                 &meshconfig.MeshConfig_ExtensionProvider{Name: "envoy"},
					RandomSamplingPercentage: ptr.Of(50.0),
					CustomTags: map[string]*tpb.Tracing_CustomTag{
						"foo": {},
						"bar": {},
					},
					UseRequestIDForTraceSampling: false,
					EnableIstioTags:              false,
				},
			},
		},
		{
			"overrides with default sampling",
			[]config.Config{newTelemetry("istio-system", overridesWithDefaultSampling)},
			sidecar,
			[]string{"envoy"},
			&TracingConfig{
				ClientSpec: TracingSpec{
					Provider: &meshconfig.MeshConfig_ExtensionProvider{Name: "envoy"},
					CustomTags: map[string]*tpb.Tracing_CustomTag{
						"foo": {},
						"baz": {},
					},
					UseRequestIDForTraceSampling: true,
					EnableIstioTags:              true,
				}, ServerSpec: TracingSpec{
					Provider: &meshconfig.MeshConfig_ExtensionProvider{Name: "envoy"},
					CustomTags: map[string]*tpb.Tracing_CustomTag{
						"foo": {},
						"baz": {},
					},
					UseRequestIDForTraceSampling: true,
					EnableIstioTags:              true,
				},
			},
		},
		{
			"multi overrides",
			[]config.Config{
				newTelemetry("istio-system", overidesA),
				newTelemetry("default", overidesB),
			},
			sidecar,
			[]string{"envoy"},
			&TracingConfig{
				ClientSpec: TracingSpec{
					Provider:                 &meshconfig.MeshConfig_ExtensionProvider{Name: "envoy"},
					RandomSamplingPercentage: ptr.Of(80.0),
					CustomTags: map[string]*tpb.Tracing_CustomTag{
						"foo": {},
						"baz": {},
					},
					UseRequestIDForTraceSampling: true,
					EnableIstioTags:              true,
				},
				ServerSpec: TracingSpec{
					Provider:                 &meshconfig.MeshConfig_ExtensionProvider{Name: "envoy"},
					RandomSamplingPercentage: ptr.Of(80.0),
					CustomTags: map[string]*tpb.Tracing_CustomTag{
						"foo": {},
						"baz": {},
					},
					UseRequestIDForTraceSampling: true,
					EnableIstioTags:              true,
				},
			},
		},
		{
			"client-only override",
			[]config.Config{newTelemetry("istio-system", envoy), newTelemetry("default", clientSideSampling)},
			sidecar,
			[]string{"envoy"},
			&TracingConfig{
				ClientSpec: TracingSpec{
					Provider: &meshconfig.MeshConfig_ExtensionProvider{
						Name: "stackdriver",
						Provider: &meshconfig.MeshConfig_ExtensionProvider_Stackdriver{
							Stackdriver: &meshconfig.MeshConfig_ExtensionProvider_StackdriverProvider{},
						},
					},
					RandomSamplingPercentage:     ptr.Of(99.9),
					UseRequestIDForTraceSampling: true,
					EnableIstioTags:              true,
				},
				ServerSpec: TracingSpec{
					Provider:                     &meshconfig.MeshConfig_ExtensionProvider{Name: "envoy"},
					UseRequestIDForTraceSampling: true,
					EnableIstioTags:              true,
				},
			},
		},
		{
			"server-only override",
			[]config.Config{newTelemetry("istio-system", envoy), newTelemetry("default", serverSideDisabled)},
			sidecar,
			[]string{"envoy"},
			&TracingConfig{
				ClientSpec: TracingSpec{
					Provider:                     &meshconfig.MeshConfig_ExtensionProvider{Name: "envoy"},
					UseRequestIDForTraceSampling: true,
					EnableIstioTags:              true,
				},
				ServerSpec: TracingSpec{
					Provider:                     &meshconfig.MeshConfig_ExtensionProvider{Name: "envoy"},
					Disabled:                     true,
					UseRequestIDForTraceSampling: true,
					EnableIstioTags:              true,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			telemetry, _ := createTestTelemetries(tt.cfgs, t)
			telemetry.meshConfig.DefaultProviders.Tracing = tt.defaultProviders
			got := telemetry.Tracing(tt.proxy, nil)
			if got != nil && got.ServerSpec.Provider != nil {
				// We don't match on this, just the name for test simplicity
				got.ServerSpec.Provider.Provider = nil
			}
			assert.Equal(t, got, tt.want)
		})
	}
}

func TestTelemetryFilters(t *testing.T) {
	overrides := []*tpb.MetricsOverrides{{
		Match: &tpb.MetricSelector{
			MetricMatch: &tpb.MetricSelector_Metric{
				Metric: tpb.MetricSelector_REQUEST_COUNT,
			},
		},
		TagOverrides: map[string]*tpb.MetricsOverrides_TagOverride{
			"remove": {
				Operation: tpb.MetricsOverrides_TagOverride_REMOVE,
			},
			"add": {
				Operation: tpb.MetricsOverrides_TagOverride_UPSERT,
				Value:     "bar",
			},
		},
	}}
	sidecar := &Proxy{
		ConfigNamespace: "default",
		Labels:          map[string]string{"app": "test"},
		Metadata:        &NodeMetadata{Labels: map[string]string{"app": "test"}},
	}
	waypoint := &Proxy{
		ConfigNamespace: "default",
		Type:            Waypoint,
		Labels:          map[string]string{label.IoK8sNetworkingGatewayGatewayName.Name: "waypoint"},
		Metadata:        &NodeMetadata{Labels: map[string]string{label.IoK8sNetworkingGatewayGatewayName.Name: "waypoint"}},
	}
	emptyPrometheus := &tpb.Telemetry{
		Metrics: []*tpb.Metrics{
			{
				Providers: []*tpb.ProviderRef{{Name: "prometheus"}},
			},
		},
	}
	reenable := &tpb.Telemetry{
		Metrics: []*tpb.Metrics{
			{
				Providers: []*tpb.ProviderRef{{Name: "prometheus"}},
				// need this to override disabledAllMetrics.overrides
				Overrides: []*tpb.MetricsOverrides{{
					Match: &tpb.MetricSelector{
						MetricMatch: &tpb.MetricSelector_Metric{
							Metric: tpb.MetricSelector_ALL_METRICS,
						},
					},
				}},
			},
		},
	}
	overridesPrometheus := &tpb.Telemetry{
		Metrics: []*tpb.Metrics{
			{
				Providers: []*tpb.ProviderRef{{Name: "prometheus"}},
				Overrides: overrides,
			},
		},
	}
	reportingInterval := &tpb.Telemetry{
		Metrics: []*tpb.Metrics{
			{
				Providers:         []*tpb.ProviderRef{{Name: "prometheus"}},
				ReportingInterval: durationpb.New(15 * time.Second),
			},
		},
	}
	overridesInterval := &tpb.Telemetry{
		Metrics: []*tpb.Metrics{
			{
				Providers:         []*tpb.ProviderRef{{Name: "prometheus"}},
				ReportingInterval: durationpb.New(10 * time.Second),
			},
		},
	}
	overridesEmptyProvider := &tpb.Telemetry{
		Metrics: []*tpb.Metrics{
			{
				Overrides: overrides,
			},
		},
	}
	disabledAllMetrics := &tpb.Telemetry{
		Metrics: []*tpb.Metrics{
			{
				Overrides: []*tpb.MetricsOverrides{{
					Match: &tpb.MetricSelector{
						MetricMatch: &tpb.MetricSelector_Metric{
							Metric: tpb.MetricSelector_ALL_METRICS,
						},
					},
					Disabled: &wrappers.BoolValue{
						Value: true,
					},
				}},

				Providers: []*tpb.ProviderRef{{Name: "prometheus"}},
			},
		},
	}
	disabledAllMetricsImplicit := &tpb.Telemetry{
		Metrics: []*tpb.Metrics{
			{
				Overrides: []*tpb.MetricsOverrides{{
					Disabled: &wrappers.BoolValue{
						Value: true,
					},
				}},

				Providers: []*tpb.ProviderRef{{Name: "prometheus"}},
			},
		},
	}
	targetRefs := &tpb.Telemetry{
		TargetRefs: []*v1beta1.PolicyTargetReference{{
			Group: gvk.Service.Group,
			Kind:  gvk.Service.Kind,
			Name:  "sample-svc",
		}},
		Metrics: []*tpb.Metrics{
			{
				Overrides: overrides,
			},
		},
	}
	emptyWaypointMetrics := `{"disable_host_header_fallback":true,"reporter":"SERVER_GATEWAY"}`
	cfg := `{"metrics":[{"dimensions":{"add":"bar"},"name":"requests_total","tags_to_remove":["remove"]}]}`

	tests := []struct {
		name             string
		cfgs             []config.Config
		proxy            *Proxy
		class            networking.ListenerClass
		protocol         networking.ListenerProtocol
		defaultProviders *meshconfig.MeshConfig_DefaultProviders
		service          *Service
		want             map[string]string
	}{
		{
			name:     "empty",
			proxy:    sidecar,
			class:    networking.ListenerClassSidecarOutbound,
			protocol: networking.ListenerProtocolHTTP,
			want:     map[string]string{},
		},
		{
			name:     "disabled-prometheus",
			cfgs:     []config.Config{newTelemetry("istio-system", disabledAllMetrics)},
			proxy:    sidecar,
			class:    networking.ListenerClassSidecarOutbound,
			protocol: networking.ListenerProtocolHTTP,
			want:     map[string]string{},
		},
		{
			name:     "disabled-prometheus-implicit",
			cfgs:     []config.Config{newTelemetry("istio-system", disabledAllMetricsImplicit)},
			proxy:    sidecar,
			class:    networking.ListenerClassSidecarOutbound,
			protocol: networking.ListenerProtocolHTTP,
			want:     map[string]string{},
		},
		{
			name: "disabled-then-empty",
			cfgs: []config.Config{
				newTelemetry("istio-system", disabledAllMetrics),
				newTelemetry("default", emptyPrometheus),
			},
			proxy:    sidecar,
			class:    networking.ListenerClassSidecarOutbound,
			protocol: networking.ListenerProtocolHTTP,
			want:     map[string]string{},
		},
		{
			name: "disabled-then-reenable",
			cfgs: []config.Config{
				newTelemetry("istio-system", disabledAllMetrics),
				newTelemetry("default", reenable),
			},
			proxy:    sidecar,
			class:    networking.ListenerClassSidecarOutbound,
			protocol: networking.ListenerProtocolHTTP,
			want: map[string]string{
				"istio.stats": `{"metrics":[` +
					`{"name":"request_messages_total"},` +
					`{"name":"response_messages_total"},` +
					`{"name":"requests_total"},` +
					`{"name":"request_duration_milliseconds"},` +
					`{"name":"request_bytes"},` +
					`{"name":"response_bytes"},` +
					`{"name":"tcp_connections_closed_total"},` +
					`{"name":"tcp_connections_opened_total"},` +
					`{"name":"tcp_received_bytes_total"},` +
					`{"name":"tcp_sent_bytes_total"}` +
					`]}`,
			},
		},
		{
			name: "disabled-then-overrides",
			cfgs: []config.Config{
				newTelemetry("istio-system", disabledAllMetrics),
				newTelemetry("default", overridesPrometheus),
			},
			proxy:    sidecar,
			class:    networking.ListenerClassSidecarOutbound,
			protocol: networking.ListenerProtocolHTTP,
			want: map[string]string{
				"istio.stats": cfg,
			},
		},
		{
			name:     "default prometheus",
			cfgs:     []config.Config{newTelemetry("istio-system", emptyPrometheus)},
			proxy:    sidecar,
			class:    networking.ListenerClassSidecarOutbound,
			protocol: networking.ListenerProtocolHTTP,
			want: map[string]string{
				"istio.stats": "{}",
			},
		},
		{
			name:             "default provider prometheus",
			cfgs:             []config.Config{},
			proxy:            sidecar,
			class:            networking.ListenerClassSidecarOutbound,
			protocol:         networking.ListenerProtocolHTTP,
			defaultProviders: &meshconfig.MeshConfig_DefaultProviders{Metrics: []string{"prometheus"}},
			want: map[string]string{
				"istio.stats": "{}",
			},
		},
		{
			name:     "prometheus overrides",
			cfgs:     []config.Config{newTelemetry("istio-system", overridesPrometheus)},
			proxy:    sidecar,
			class:    networking.ListenerClassSidecarOutbound,
			protocol: networking.ListenerProtocolHTTP,
			want: map[string]string{
				"istio.stats": cfg,
			},
		},
		{
			name: "prometheus overrides all metrics",
			cfgs: []config.Config{newTelemetry("istio-system", &tpb.Telemetry{
				Metrics: []*tpb.Metrics{
					{
						Providers: []*tpb.ProviderRef{{Name: "prometheus"}},
						Overrides: []*tpb.MetricsOverrides{
							{
								TagOverrides: map[string]*tpb.MetricsOverrides_TagOverride{
									"remove": {
										Operation: tpb.MetricsOverrides_TagOverride_REMOVE,
									},
									"add": {
										Operation: tpb.MetricsOverrides_TagOverride_UPSERT,
										Value:     "bar",
									},
								},
							},
						},
					},
				},
			})},
			proxy:    sidecar,
			class:    networking.ListenerClassSidecarOutbound,
			protocol: networking.ListenerProtocolHTTP,
			// TODO: the following should be simple to `{"metrics":[{"dimensions":{"add":"bar"},"tags_to_remove":["remove"]}]}`
			want: map[string]string{
				"istio.stats": `{"metrics":[` +
					`{"dimensions":{"add":"bar"},"name":"request_messages_total","tags_to_remove":["remove"]},` +
					`{"dimensions":{"add":"bar"},"name":"response_messages_total","tags_to_remove":["remove"]},` +
					`{"dimensions":{"add":"bar"},"name":"requests_total","tags_to_remove":["remove"]},` +
					`{"dimensions":{"add":"bar"},"name":"request_duration_milliseconds","tags_to_remove":["remove"]},` +
					`{"dimensions":{"add":"bar"},"name":"request_bytes","tags_to_remove":["remove"]},` +
					`{"dimensions":{"add":"bar"},"name":"response_bytes","tags_to_remove":["remove"]},` +
					`{"dimensions":{"add":"bar"},"name":"tcp_connections_closed_total","tags_to_remove":["remove"]},` +
					`{"dimensions":{"add":"bar"},"name":"tcp_connections_opened_total","tags_to_remove":["remove"]},` +
					`{"dimensions":{"add":"bar"},"name":"tcp_received_bytes_total","tags_to_remove":["remove"]},` +
					`{"dimensions":{"add":"bar"},"name":"tcp_sent_bytes_total","tags_to_remove":["remove"]}` +
					`]}`,
			},
		},
		{
			name: "prometheus overrides all metrics first",
			cfgs: []config.Config{newTelemetry("istio-system", &tpb.Telemetry{
				Metrics: []*tpb.Metrics{
					{
						Providers: []*tpb.ProviderRef{{Name: "prometheus"}},
						Overrides: []*tpb.MetricsOverrides{
							{
								TagOverrides: map[string]*tpb.MetricsOverrides_TagOverride{
									"remove": {
										Operation: tpb.MetricsOverrides_TagOverride_REMOVE,
									},
									"add": {
										Operation: tpb.MetricsOverrides_TagOverride_UPSERT,
										Value:     "bar",
									},
								},
							},
							{
								Match: &tpb.MetricSelector{
									MetricMatch: &tpb.MetricSelector_Metric{
										Metric: tpb.MetricSelector_REQUEST_COUNT,
									},
								},
								TagOverrides: map[string]*tpb.MetricsOverrides_TagOverride{
									"add": {
										Value: "add-override",
									},
								},
							},
						},
					},
				},
			})},
			proxy:    sidecar,
			class:    networking.ListenerClassSidecarOutbound,
			protocol: networking.ListenerProtocolHTTP,
			want: map[string]string{
				"istio.stats": `{"metrics":[` +
					`{"dimensions":{"add":"bar"},"name":"request_messages_total","tags_to_remove":["remove"]},` +
					`{"dimensions":{"add":"bar"},"name":"response_messages_total","tags_to_remove":["remove"]},` +
					`{"dimensions":{"add":"add-override"},"name":"requests_total","tags_to_remove":["remove"]},` +
					`{"dimensions":{"add":"bar"},"name":"request_duration_milliseconds","tags_to_remove":["remove"]},` +
					`{"dimensions":{"add":"bar"},"name":"request_bytes","tags_to_remove":["remove"]},` +
					`{"dimensions":{"add":"bar"},"name":"response_bytes","tags_to_remove":["remove"]},` +
					`{"dimensions":{"add":"bar"},"name":"tcp_connections_closed_total","tags_to_remove":["remove"]},` +
					`{"dimensions":{"add":"bar"},"name":"tcp_connections_opened_total","tags_to_remove":["remove"]},` +
					`{"dimensions":{"add":"bar"},"name":"tcp_received_bytes_total","tags_to_remove":["remove"]},` +
					`{"dimensions":{"add":"bar"},"name":"tcp_sent_bytes_total","tags_to_remove":["remove"]}` +
					`]}`,
			},
		},
		{
			name: "prometheus overrides all metrics secondary",
			cfgs: []config.Config{newTelemetry("istio-system", &tpb.Telemetry{
				Metrics: []*tpb.Metrics{
					{
						Providers: []*tpb.ProviderRef{{Name: "prometheus"}},
						Overrides: []*tpb.MetricsOverrides{
							{
								Match: &tpb.MetricSelector{
									MetricMatch: &tpb.MetricSelector_Metric{
										Metric: tpb.MetricSelector_REQUEST_COUNT,
									},
								},
								TagOverrides: map[string]*tpb.MetricsOverrides_TagOverride{
									"add": {
										Value: "add-override",
									},
								},
							},
							{
								TagOverrides: map[string]*tpb.MetricsOverrides_TagOverride{
									"remove": {
										Operation: tpb.MetricsOverrides_TagOverride_REMOVE,
									},
									"add": {
										Operation: tpb.MetricsOverrides_TagOverride_UPSERT,
										Value:     "bar",
									},
								},
							},
						},
					},
				},
			})},
			proxy:    sidecar,
			class:    networking.ListenerClassSidecarOutbound,
			protocol: networking.ListenerProtocolHTTP,
			want: map[string]string{
				"istio.stats": `{"metrics":[` +
					`{"dimensions":{"add":"bar"},"name":"request_messages_total","tags_to_remove":["remove"]},` +
					`{"dimensions":{"add":"bar"},"name":"response_messages_total","tags_to_remove":["remove"]},` +
					`{"dimensions":{"add":"bar"},"name":"requests_total","tags_to_remove":["remove"]},` +
					`{"dimensions":{"add":"bar"},"name":"request_duration_milliseconds","tags_to_remove":["remove"]},` +
					`{"dimensions":{"add":"bar"},"name":"request_bytes","tags_to_remove":["remove"]},` +
					`{"dimensions":{"add":"bar"},"name":"response_bytes","tags_to_remove":["remove"]},` +
					`{"dimensions":{"add":"bar"},"name":"tcp_connections_closed_total","tags_to_remove":["remove"]},` +
					`{"dimensions":{"add":"bar"},"name":"tcp_connections_opened_total","tags_to_remove":["remove"]},` +
					`{"dimensions":{"add":"bar"},"name":"tcp_received_bytes_total","tags_to_remove":["remove"]},` +
					`{"dimensions":{"add":"bar"},"name":"tcp_sent_bytes_total","tags_to_remove":["remove"]}` +
					`]}`,
			},
		},
		{
			name:     "prometheus overrides TCP",
			cfgs:     []config.Config{newTelemetry("istio-system", overridesPrometheus)},
			proxy:    sidecar,
			class:    networking.ListenerClassSidecarOutbound,
			protocol: networking.ListenerProtocolTCP,
			want: map[string]string{
				"istio.stats": cfg,
			},
		},
		{
			name:     "reporting-interval",
			cfgs:     []config.Config{newTelemetry("istio-system", reportingInterval)},
			proxy:    sidecar,
			class:    networking.ListenerClassSidecarOutbound,
			protocol: networking.ListenerProtocolHTTP,
			want: map[string]string{
				"istio.stats": `{"tcp_reporting_duration":"15s"}`,
			},
		},
		{
			name: "override-interval",
			cfgs: []config.Config{
				newTelemetry("istio-system", reportingInterval),
				newTelemetry("default", overridesInterval),
			},
			proxy:    sidecar,
			class:    networking.ListenerClassSidecarOutbound,
			protocol: networking.ListenerProtocolHTTP,
			want: map[string]string{
				"istio.stats": `{"tcp_reporting_duration":"10s"}`,
			},
		},
		{
			name: "namespace overrides merge without provider",
			cfgs: []config.Config{
				newTelemetry("istio-system", emptyPrometheus),
				newTelemetry("default", overridesEmptyProvider),
			},
			proxy:    sidecar,
			class:    networking.ListenerClassSidecarOutbound,
			protocol: networking.ListenerProtocolHTTP,
			want: map[string]string{
				"istio.stats": cfg,
			},
		},
		{
			name: "namespace overrides merge with default provider",
			cfgs: []config.Config{
				newTelemetry("default", overridesEmptyProvider),
			},
			proxy:            sidecar,
			class:            networking.ListenerClassSidecarOutbound,
			protocol:         networking.ListenerProtocolHTTP,
			defaultProviders: &meshconfig.MeshConfig_DefaultProviders{Metrics: []string{"prometheus"}},
			want: map[string]string{
				"istio.stats": cfg,
			},
		},
		{
			name: "targetRef mismatch no service",
			cfgs: []config.Config{
				newTelemetry("default", targetRefs),
			},
			proxy:            waypoint,
			class:            networking.ListenerClassSidecarInbound,
			protocol:         networking.ListenerProtocolHTTP,
			defaultProviders: &meshconfig.MeshConfig_DefaultProviders{Metrics: []string{"prometheus"}},
			want: map[string]string{
				// Config is for a service, but we are not building a service-based filter, so ignore it. We just fallback to default provider
				"istio.stats": emptyWaypointMetrics,
			},
		},
		{
			name: "targetRef mismatch wrong service",
			cfgs: []config.Config{
				newTelemetry("default", targetRefs),
			},
			service: &Service{
				Attributes: ServiceAttributes{
					Name:            "not-sample-svc",
					Namespace:       "default",
					ServiceRegistry: provider.Kubernetes,
				},
			},
			proxy:            waypoint,
			class:            networking.ListenerClassSidecarInbound,
			protocol:         networking.ListenerProtocolHTTP,
			defaultProviders: &meshconfig.MeshConfig_DefaultProviders{Metrics: []string{"prometheus"}},
			want: map[string]string{
				// Config is not for the service, so ignore it. We just fallback to default provider
				"istio.stats": emptyWaypointMetrics,
			},
		},
		{
			name: "targetRef match",
			cfgs: []config.Config{
				newTelemetry("default", targetRefs),
			},
			service: &Service{
				Attributes: ServiceAttributes{
					Name:            "sample-svc",
					Namespace:       "default",
					ServiceRegistry: provider.Kubernetes,
				},
			},
			proxy:            waypoint,
			class:            networking.ListenerClassSidecarInbound,
			protocol:         networking.ListenerProtocolHTTP,
			defaultProviders: &meshconfig.MeshConfig_DefaultProviders{Metrics: []string{"prometheus"}},
			want: map[string]string{
				"istio.stats": `{"disable_host_header_fallback":true,"metrics":[{"dimensions":{"add":"bar"},"name":"requests_total"` +
					`,"tags_to_remove":["remove"]}],"reporter":"SERVER_GATEWAY"}`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			telemetry, _ := createTestTelemetries(tt.cfgs, t)
			telemetry.meshConfig.DefaultProviders = tt.defaultProviders
			got := telemetry.telemetryFilters(tt.proxy, tt.class, tt.protocol, tt.service)
			res := map[string]string{}
			http, ok := got.([]*hcm.HttpFilter)
			if ok {
				for _, f := range http {
					if strings.HasSuffix(f.GetTypedConfig().GetTypeUrl(), "/stats.PluginConfig") {
						w := &stats.PluginConfig{}
						if err := f.GetTypedConfig().UnmarshalTo(w); err != nil {
							t.Fatal(err)
						}
						cfgJSON, _ := protomarshal.MarshalProtoNames(w)
						res[f.GetName()] = string(cfgJSON)
					} else {
						w := &httpwasm.Wasm{}

						if err := f.GetTypedConfig().UnmarshalTo(w); err != nil {
							t.Fatal(err)
						}
						cfg := &wrappers.StringValue{}
						if err := w.GetConfig().GetConfiguration().UnmarshalTo(cfg); err != nil {
							t.Fatal(err)
						}
						if _, dupe := res[f.GetName()]; dupe {
							t.Fatalf("duplicate filter found: %v", f.GetName())
						}
						res[f.GetName()] = cfg.GetValue()
					}
				}
			}
			tcp, ok := got.([]*listener.Filter)
			if ok {
				for _, f := range tcp {
					if strings.HasSuffix(f.GetTypedConfig().GetTypeUrl(), "/stats.PluginConfig") {
						w := &stats.PluginConfig{}
						if err := f.GetTypedConfig().UnmarshalTo(w); err != nil {
							t.Fatal(err)
						}
						cfgJSON, _ := protomarshal.MarshalProtoNames(w)
						res[f.GetName()] = string(cfgJSON)
					} else {
						w := &wasmfilter.Wasm{}

						if err := f.GetTypedConfig().UnmarshalTo(w); err != nil {
							t.Fatal(err)
						}
						cfg := &wrappers.StringValue{}
						if err := w.GetConfig().GetConfiguration().UnmarshalTo(cfg); err != nil {
							t.Fatal(err)
						}
						if _, dupe := res[f.GetName()]; dupe {
							t.Fatalf("duplicate filter found: %v", f.GetName())
						}
						res[f.GetName()] = cfg.GetValue()
					}
				}
			}
			if diff := cmp.Diff(res, tt.want); diff != "" {
				t.Errorf("got diff: %v", diff)
			}
		})
	}
}

func TestGetInterval(t *testing.T) {
	cases := []struct {
		name              string
		input, defaultVal time.Duration
		expected          *durationpb.Duration
	}{
		{
			name:       "return nil",
			input:      0,
			defaultVal: 0,
			expected:   nil,
		},
		{
			name:       "return input",
			input:      1 * time.Second,
			defaultVal: 0,
			expected:   durationpb.New(1 * time.Second),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actual := getInterval(tc.input, tc.defaultVal)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func Test_appendApplicableTelemetries(t *testing.T) {
	namespacedName := types.NamespacedName{
		Name:      "my-telemetry",
		Namespace: "my-namespace",
	}
	emptyStackDriverTracing := &tpb.Tracing{
		Match: &tpb.Tracing_TracingSelector{
			Mode: tpb.WorkloadMode_CLIENT,
		},
		Providers: []*tpb.ProviderRef{
			{
				Name: "stackdriver",
			},
		},
	}
	prometheusMetrics := &tpb.Metrics{
		Providers:         []*tpb.ProviderRef{{Name: "prometheus"}},
		ReportingInterval: durationpb.New(15 * time.Second),
	}
	emptyEnvoyLogging := &tpb.AccessLogging{
		Providers: []*tpb.ProviderRef{
			{
				Name: "envoy",
			},
		},
	}
	testComputeAccessLogging := &computedAccessLogging{
		telemetryKey: telemetryKey{
			Workload: namespacedName,
		},
		Logging: []*tpb.AccessLogging{
			emptyEnvoyLogging,
		},
	}
	validTelemetryConfigurationWithTargetRef := &tpb.Telemetry{
		TargetRef: &v1beta1.PolicyTargetReference{
			Group: gvk.KubernetesGateway.Group,
			Kind:  gvk.KubernetesGateway.Kind,
			Name:  "my-gateway",
		},
		Tracing: []*tpb.Tracing{
			emptyStackDriverTracing,
		},
		Metrics: []*tpb.Metrics{
			prometheusMetrics,
		},
		AccessLogging: []*tpb.AccessLogging{
			emptyEnvoyLogging,
		},
	}
	validTelemetryConfiguration := &tpb.Telemetry{
		Tracing: []*tpb.Tracing{
			emptyStackDriverTracing,
		},
		Metrics: []*tpb.Metrics{
			prometheusMetrics,
		},
		AccessLogging: []*tpb.AccessLogging{
			emptyEnvoyLogging,
		},
	}
	type args struct {
		ct   *computedTelemetries
		tel  Telemetry
		spec *tpb.Telemetry
	}

	tests := []struct {
		name string
		args args
		want *computedTelemetries
	}{
		{
			name: "empty telemetry configuration",
			args: args{
				ct:   &computedTelemetries{},
				tel:  Telemetry{},
				spec: &tpb.Telemetry{},
			},
			want: &computedTelemetries{},
		},
		{
			name: "targetRef is defined, telemetry configurations are added to empty computed telemetries",
			args: args{
				ct: &computedTelemetries{},
				tel: Telemetry{
					Name:      "my-telemetry",
					Namespace: "my-namespace",
					Spec:      validTelemetryConfigurationWithTargetRef,
				},
				spec: validTelemetryConfigurationWithTargetRef,
			},
			want: &computedTelemetries{
				telemetryKey: telemetryKey{Workload: namespacedName},
				Metrics:      []*tpb.Metrics{prometheusMetrics},
				Logging:      []*computedAccessLogging{testComputeAccessLogging},
				Tracing:      []*tpb.Tracing{emptyStackDriverTracing},
			},
		},
		{
			name: "targetRef is not defined, telemetry configurations are added to empty computed telemetries",
			args: args{
				ct: &computedTelemetries{},
				tel: Telemetry{
					Name:      "my-telemetry",
					Namespace: "my-namespace",
					Spec:      validTelemetryConfiguration,
				},
				spec: validTelemetryConfiguration,
			},
			want: &computedTelemetries{
				telemetryKey: telemetryKey{
					Workload: namespacedName,
				},
				Metrics: []*tpb.Metrics{prometheusMetrics},
				Logging: []*computedAccessLogging{testComputeAccessLogging},
				Tracing: []*tpb.Tracing{emptyStackDriverTracing},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := appendApplicableTelemetries(tt.args.ct, tt.args.tel, tt.args.spec); !cmp.Equal(got, tt.want) {
				t.Errorf("appendApplicableTelemetries() = want %v", cmp.Diff(got, tt.want))
			}
		})
	}
}

func Test_computedTelemetries_Equal(t *testing.T) {
	type args struct {
		other *computedTelemetries
	}

	tests := []struct {
		name                string
		computedTelemetries *computedTelemetries
		args                args
		want                bool
	}{
		{
			name:                "nil",
			computedTelemetries: nil,
			args: args{
				other: nil,
			},
			want: true,
		},
		{
			name:                "empty",
			computedTelemetries: &computedTelemetries{},
			args: args{
				other: &computedTelemetries{},
			},
			want: true,
		},
		{
			name:                "computedTelemetries is nil and other computedTelemetries is not",
			computedTelemetries: nil,
			args: args{
				other: &computedTelemetries{
					Metrics: []*tpb.Metrics{
						{
							Providers: []*tpb.ProviderRef{{Name: "prometheus"}},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "other computedTelemetries is nil and computedTelemetries is not",
			computedTelemetries: &computedTelemetries{
				Metrics: []*tpb.Metrics{
					{
						Providers: []*tpb.ProviderRef{{Name: "prometheus"}},
					},
				},
			},
			args: args{
				other: nil,
			},
			want: false,
		},
		{
			name: "different length in metrics slice comparison",
			computedTelemetries: &computedTelemetries{
				Metrics: []*tpb.Metrics{
					{
						Providers: []*tpb.ProviderRef{{Name: "prometheus"}},
					},
				},
			},
			args: args{
				other: &computedTelemetries{
					Metrics: []*tpb.Metrics{
						{
							Providers: []*tpb.ProviderRef{{Name: "prometheus"}},
						},
						{
							Providers: []*tpb.ProviderRef{{Name: "stackdriver"}},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "different length in tracing slice comparison",
			computedTelemetries: &computedTelemetries{
				Tracing: []*tpb.Tracing{
					{
						Providers: []*tpb.ProviderRef{{Name: "stackdriver"}},
					},
				},
			},
			args: args{
				other: &computedTelemetries{
					Tracing: []*tpb.Tracing{
						{
							Providers: []*tpb.ProviderRef{{Name: "stackdriver"}},
						},
						{
							Providers: []*tpb.ProviderRef{{Name: "prometheus"}},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "different length in logging slice comparison",
			computedTelemetries: &computedTelemetries{
				Logging: []*computedAccessLogging{
					{
						Logging: []*tpb.AccessLogging{
							{
								Providers: []*tpb.ProviderRef{{Name: "stackdriver"}},
							},
						},
					},
				},
			},
			args: args{
				other: &computedTelemetries{
					Logging: []*computedAccessLogging{
						{
							Logging: []*tpb.AccessLogging{
								{
									Providers: []*tpb.ProviderRef{{Name: "stackdriver"}},
								},
							},
						},
						{
							Logging: []*tpb.AccessLogging{
								{
									Providers: []*tpb.ProviderRef{{Name: "prometheus"}},
								},
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "different metrics",
			computedTelemetries: &computedTelemetries{
				Metrics: []*tpb.Metrics{
					{
						Providers: []*tpb.ProviderRef{{Name: "prometheus"}},
					},
				},
			},
			args: args{
				other: &computedTelemetries{
					Metrics: []*tpb.Metrics{
						{
							Providers: []*tpb.ProviderRef{{Name: "stackdriver"}},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "different metrics reporting interval",
			computedTelemetries: &computedTelemetries{
				Metrics: []*tpb.Metrics{
					{
						Providers: []*tpb.ProviderRef{{Name: "prometheus"}},
						ReportingInterval: &durationpb.Duration{
							Seconds: 10,
						},
					},
				},
			},
			args: args{
				other: &computedTelemetries{
					Metrics: []*tpb.Metrics{
						{
							Providers: []*tpb.ProviderRef{{Name: "prometheus"}},
							ReportingInterval: &durationpb.Duration{
								Seconds: 15,
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "different tracing providers",
			computedTelemetries: &computedTelemetries{
				Tracing: []*tpb.Tracing{
					{
						Providers: []*tpb.ProviderRef{{Name: "stackdriver"}},
					},
				},
			},
			args: args{
				other: &computedTelemetries{
					Tracing: []*tpb.Tracing{
						{
							Providers: []*tpb.ProviderRef{{Name: "prometheus"}},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "different tracing match",
			computedTelemetries: &computedTelemetries{
				Tracing: []*tpb.Tracing{
					{
						Providers: []*tpb.ProviderRef{{Name: "stackdriver"}},
						Match: &tpb.Tracing_TracingSelector{
							Mode: tpb.WorkloadMode_CLIENT,
						},
					},
				},
			},
			args: args{
				other: &computedTelemetries{
					Tracing: []*tpb.Tracing{
						{
							Providers: []*tpb.ProviderRef{{Name: "stackdriver"}},
							Match: &tpb.Tracing_TracingSelector{
								Mode: tpb.WorkloadMode_SERVER,
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "different logging providers",
			computedTelemetries: &computedTelemetries{
				Logging: []*computedAccessLogging{
					{
						Logging: []*tpb.AccessLogging{
							{
								Providers: []*tpb.ProviderRef{{Name: "stackdriver"}},
							},
						},
					},
				},
			},
			args: args{
				other: &computedTelemetries{
					Logging: []*computedAccessLogging{
						{
							Logging: []*tpb.AccessLogging{
								{
									Providers: []*tpb.ProviderRef{{Name: "prometheus"}},
								},
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "different logging telemetryKey",
			computedTelemetries: &computedTelemetries{
				Logging: []*computedAccessLogging{
					{
						telemetryKey: telemetryKey{
							Workload: types.NamespacedName{
								Name:      "my-telemetry",
								Namespace: "my-namespace",
							},
						},
					},
				},
			},
			args: args{
				other: &computedTelemetries{
					Logging: []*computedAccessLogging{
						{
							telemetryKey: telemetryKey{
								Workload: types.NamespacedName{
									Name:      "my-telemetry",
									Namespace: "my-namespace-2",
								},
							},
						},
					},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.computedTelemetries.Equal(tt.args.other); got != tt.want {
				t.Errorf("computedTelemetries.Equal() = %v, want %v", got, tt.want)
			}
		})
	}
}
