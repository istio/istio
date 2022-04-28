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

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	httpwasm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/wasm/v3"
	httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	wasmfilter "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/wasm/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/types/known/structpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	tpb "istio.io/api/telemetry/v1alpha1"
	"istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/test/util/assert"
)

func createTestTelemetries(configs []config.Config, t *testing.T) *Telemetries {
	t.Helper()

	store := &telemetryStore{}
	for _, cfg := range configs {
		store.add(cfg)
	}
	m := mesh.DefaultMeshConfig()
	jsonTextProvider := &meshconfig.MeshConfig_ExtensionProvider{
		Name: "envoy-json",
		Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
			EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
				Path: "/dev/null",
				LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat{
					LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat_Labels{
						Labels: &structpb.Struct{},
					},
				},
			},
		},
	}
	m.ExtensionProviders = append(m.ExtensionProviders, jsonTextProvider)

	environment := &Environment{
		ConfigStore: MakeIstioStore(store),
		Watcher:     mesh.NewFixedWatcher(m),
	}
	telemetries, err := getTelemetries(environment)
	if err != nil {
		t.Fatalf("getTelemetries failed: %v", err)
	}
	return telemetries
}

func newTelemetry(ns string, spec config.Spec) config.Config {
	return config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioTelemetryV1Alpha1Telemetries.Resource().GroupVersionKind(),
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

func (ts *telemetryStore) List(typ config.GroupVersionKind, namespace string) ([]config.Config, error) {
	var configs []config.Config
	for _, data := range ts.data {
		if data.typ == typ {
			if namespace != "" && data.ns == namespace {
				continue
			}
			configs = append(configs, data.cfg)
		}
	}
	return configs, nil
}

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
			telemetry := createTestTelemetries(tt.cfgs, t)
			telemetry.meshConfig.DefaultProviders.AccessLogging = tt.defaultProviders
			al := telemetry.AccessLogging(tt.proxy, tt.class)
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
			telemetry := createTestTelemetries(tt.cfgs, t)
			telemetry.meshConfig.DefaultProviders.AccessLogging = tt.defaultProviders
			got := telemetry.AccessLogging(tt.proxy, networking.ListenerClassSidecarOutbound)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

func newTracingConfig(providerName string, disabled bool) *TracingConfig {
	return &TracingConfig{
		ClientSpec: TracingSpec{
			Provider:                     &meshconfig.MeshConfig_ExtensionProvider{Name: providerName},
			Disabled:                     disabled,
			UseRequestIDForTraceSampling: true,
		},
		ServerSpec: TracingSpec{
			Provider:                     &meshconfig.MeshConfig_ExtensionProvider{Name: providerName},
			Disabled:                     disabled,
			UseRequestIDForTraceSampling: true,
		},
	}
}

const (
	reportingEnabled  = false
	reportingDisabled = !reportingEnabled
)

func TestTracing(t *testing.T) {
	sidecar := &Proxy{ConfigNamespace: "default", Metadata: &NodeMetadata{Labels: map[string]string{"app": "test"}}}
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
	nonExistant := &tpb.Telemetry{
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
			[]config.Config{newTelemetry("default", nonExistant)},
			sidecar,
			[]string{"envoy"},
			&TracingConfig{
				ClientSpec: TracingSpec{Disabled: true, UseRequestIDForTraceSampling: true},
				ServerSpec: TracingSpec{Disabled: true, UseRequestIDForTraceSampling: true},
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
					RandomSamplingPercentage: 50.0,
					CustomTags: map[string]*tpb.Tracing_CustomTag{
						"foo": {},
						"bar": {},
					},
					UseRequestIDForTraceSampling: false,
				}, ServerSpec: TracingSpec{
					Provider:                 &meshconfig.MeshConfig_ExtensionProvider{Name: "envoy"},
					RandomSamplingPercentage: 50.0,
					CustomTags: map[string]*tpb.Tracing_CustomTag{
						"foo": {},
						"bar": {},
					},
					UseRequestIDForTraceSampling: false,
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
					Provider:                 &meshconfig.MeshConfig_ExtensionProvider{Name: "envoy"},
					RandomSamplingPercentage: 0.0,
					CustomTags: map[string]*tpb.Tracing_CustomTag{
						"foo": {},
						"baz": {},
					},
					UseRequestIDForTraceSampling: true,
				}, ServerSpec: TracingSpec{
					Provider:                 &meshconfig.MeshConfig_ExtensionProvider{Name: "envoy"},
					RandomSamplingPercentage: 0.0,
					CustomTags: map[string]*tpb.Tracing_CustomTag{
						"foo": {},
						"baz": {},
					},
					UseRequestIDForTraceSampling: true,
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
					RandomSamplingPercentage: 80,
					CustomTags: map[string]*tpb.Tracing_CustomTag{
						"foo": {},
						"baz": {},
					},
					UseRequestIDForTraceSampling: true,
				},
				ServerSpec: TracingSpec{
					Provider:                 &meshconfig.MeshConfig_ExtensionProvider{Name: "envoy"},
					RandomSamplingPercentage: 80,
					CustomTags: map[string]*tpb.Tracing_CustomTag{
						"foo": {},
						"baz": {},
					},
					UseRequestIDForTraceSampling: true,
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
					RandomSamplingPercentage:     99.9,
					UseRequestIDForTraceSampling: true,
				},
				ServerSpec: TracingSpec{
					Provider:                     &meshconfig.MeshConfig_ExtensionProvider{Name: "envoy"},
					UseRequestIDForTraceSampling: true,
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
				},
				ServerSpec: TracingSpec{
					Provider:                     &meshconfig.MeshConfig_ExtensionProvider{Name: "envoy"},
					Disabled:                     true,
					UseRequestIDForTraceSampling: true,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			telemetry := createTestTelemetries(tt.cfgs, t)
			telemetry.meshConfig.DefaultProviders.Tracing = tt.defaultProviders
			got := telemetry.Tracing(tt.proxy)
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
	sidecar := &Proxy{ConfigNamespace: "default", Metadata: &NodeMetadata{Labels: map[string]string{"app": "test"}}}
	emptyPrometheus := &tpb.Telemetry{
		Metrics: []*tpb.Metrics{
			{
				Providers: []*tpb.ProviderRef{{Name: "prometheus"}},
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
	emptyStackdriver := &tpb.Telemetry{
		Metrics: []*tpb.Metrics{
			{
				Providers: []*tpb.ProviderRef{{Name: "stackdriver"}},
			},
		},
	}
	overridesStackdriver := &tpb.Telemetry{
		Metrics: []*tpb.Metrics{
			{
				Providers: []*tpb.ProviderRef{{Name: "stackdriver"}},
				Overrides: overrides,
			},
		},
		AccessLogging: []*tpb.AccessLogging{
			{
				Providers: []*tpb.ProviderRef{{Name: "stackdriver"}},
				Filter: &tpb.AccessLogging_Filter{
					Expression: `response.code >= 500 && response.code <= 800`,
				},
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
	sdLogging := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{
				Providers: []*tpb.ProviderRef{{Name: "stackdriver"}},
			},
		},
	}
	emptyLogging := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{},
		},
	}
	disbaledAllMetrics := &tpb.Telemetry{
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

	tests := []struct {
		name             string
		cfgs             []config.Config
		proxy            *Proxy
		class            networking.ListenerClass
		protocol         networking.ListenerProtocol
		defaultProviders *meshconfig.MeshConfig_DefaultProviders
		want             map[string]string
	}{
		{
			"empty",
			nil,
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			nil,
			map[string]string{},
		},
		{
			"disabled-prometheus",
			[]config.Config{newTelemetry("istio-system", disbaledAllMetrics)},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			nil,
			map[string]string{},
		},
		{
			"disabled-then-empty",
			[]config.Config{
				newTelemetry("istio-system", disbaledAllMetrics),
				newTelemetry("default", emptyPrometheus),
			},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			nil,
			map[string]string{},
		},
		{
			"disabled-then-overrides",
			[]config.Config{
				newTelemetry("istio-system", disbaledAllMetrics),
				newTelemetry("default", overridesPrometheus),
			},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			nil,
			map[string]string{
				"istio.stats": `{"metrics":[{"dimensions":{"add":"bar"},"name":"requests_total","tags_to_remove":["remove"]}]}`,
			},
		},
		{
			"default prometheus",
			[]config.Config{newTelemetry("istio-system", emptyPrometheus)},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			nil,
			map[string]string{
				"istio.stats": "{}",
			},
		},
		{
			"default provider prometheus",
			[]config.Config{},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			&meshconfig.MeshConfig_DefaultProviders{Metrics: []string{"prometheus"}},
			map[string]string{
				"istio.stats": "{}",
			},
		},
		{
			"prometheus overrides",
			[]config.Config{newTelemetry("istio-system", overridesPrometheus)},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			nil,
			map[string]string{
				"istio.stats": `{"metrics":[{"dimensions":{"add":"bar"},"name":"requests_total","tags_to_remove":["remove"]}]}`,
			},
		},
		{
			"prometheus overrides TCP",
			[]config.Config{newTelemetry("istio-system", overridesPrometheus)},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolTCP,
			nil,
			map[string]string{
				"istio.stats": `{"metrics":[{"dimensions":{"add":"bar"},"name":"requests_total","tags_to_remove":["remove"]}]}`,
			},
		},
		{
			"empty stackdriver",
			[]config.Config{newTelemetry("istio-system", emptyStackdriver)},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			nil,
			map[string]string{
				"istio.stackdriver": `{"disable_server_access_logging":true,"metric_expiry_duration":"3600s"}`,
			},
		},
		{
			"overrides stackdriver",
			[]config.Config{newTelemetry("istio-system", overridesStackdriver)},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			nil,
			map[string]string{
				"istio.stackdriver": `{"access_logging_filter_expression":"response.code >= 500 && response.code <= 800",` +
					`"metric_expiry_duration":"3600s","metrics_overrides":{"client/request_count":{"tag_overrides":{"add":"bar"}}}}`,
			},
		},
		{
			"namespace empty merge",
			[]config.Config{
				newTelemetry("istio-system", emptyPrometheus),
				newTelemetry("default", emptyStackdriver),
			},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			nil,
			map[string]string{
				"istio.stackdriver": `{"disable_server_access_logging":true,"metric_expiry_duration":"3600s"}`,
			},
		},
		{
			"namespace overrides merge without provider",
			[]config.Config{
				newTelemetry("istio-system", emptyPrometheus),
				newTelemetry("default", overridesEmptyProvider),
			},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			nil,
			map[string]string{
				"istio.stats": `{"metrics":[{"dimensions":{"add":"bar"},"name":"requests_total","tags_to_remove":["remove"]}]}`,
			},
		},
		{
			"namespace overrides merge with default provider",
			[]config.Config{
				newTelemetry("default", overridesEmptyProvider),
			},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			&meshconfig.MeshConfig_DefaultProviders{Metrics: []string{"prometheus"}},
			map[string]string{
				"istio.stats": `{"metrics":[{"dimensions":{"add":"bar"},"name":"requests_total","tags_to_remove":["remove"]}]}`,
			},
		},
		{
			"namespace overrides default provider",
			[]config.Config{
				newTelemetry("default", emptyStackdriver),
			},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			&meshconfig.MeshConfig_DefaultProviders{Metrics: []string{"prometheus"}},
			map[string]string{
				"istio.stackdriver": `{"disable_server_access_logging":true,"metric_expiry_duration":"3600s"}`,
			},
		},
		{
			"stackdriver logging",
			[]config.Config{
				newTelemetry("default", sdLogging),
			},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			nil,
			map[string]string{
				"istio.stackdriver": `{"access_logging":"ERRORS_ONLY","metric_expiry_duration":"3600s"}`,
			},
		},
		{
			"stackdriver logging default provider",
			[]config.Config{
				newTelemetry("default", emptyLogging),
			},
			sidecar,
			networking.ListenerClassSidecarInbound,
			networking.ListenerProtocolHTTP,
			&meshconfig.MeshConfig_DefaultProviders{AccessLogging: []string{"stackdriver"}},
			map[string]string{
				"istio.stackdriver": `{"disable_host_header_fallback":true,"access_logging":"FULL","metric_expiry_duration":"3600s"}`,
			},
		},
		{
			"stackdriver default for all",
			[]config.Config{},
			sidecar,
			networking.ListenerClassSidecarInbound,
			networking.ListenerProtocolHTTP,
			&meshconfig.MeshConfig_DefaultProviders{
				Metrics:       []string{"stackdriver"},
				AccessLogging: []string{"stackdriver"},
			},
			map[string]string{
				"istio.stackdriver": `{"disable_host_header_fallback":true,"access_logging":"FULL","metric_expiry_duration":"3600s"}`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			telemetry := createTestTelemetries(tt.cfgs, t)
			telemetry.meshConfig.DefaultProviders = tt.defaultProviders
			got := telemetry.telemetryFilters(tt.proxy, tt.class, tt.protocol)
			res := map[string]string{}
			http, ok := got.([]*httppb.HttpFilter)
			if ok {
				for _, f := range http {
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
			tcp, ok := got.([]*listener.Filter)
			if ok {
				for _, f := range tcp {
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
			if diff := cmp.Diff(res, tt.want); diff != "" {
				t.Errorf("got diff: %v", diff)
			}
		})
	}
}
