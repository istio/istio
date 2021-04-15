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
	"testing"

	"github.com/gogo/protobuf/types"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	meshconfig "istio.io/api/mesh/v1alpha1"
	tpb "istio.io/api/telemetry/v1alpha1"
	"istio.io/api/type/v1beta1"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

func TestTelemetries_EffectiveTelemetry(t *testing.T) {
	rootTrace := &tpb.Telemetry{
		Tracing: []*tpb.Tracing{
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "zipkin",
					},
				},
				RandomSamplingPercentage: &types.DoubleValue{Value: 10.10},
			},
		},
	}

	fooTrace := &tpb.Telemetry{
		Selector: &v1beta1.WorkloadSelector{
			MatchLabels: map[string]string{"service.istio.io/canonical-name": "foo"},
		},
		Tracing: []*tpb.Tracing{
			{
				RandomSamplingPercentage: &types.DoubleValue{Value: 0.0},
			},
		},
	}

	barTrace := &tpb.Telemetry{
		Tracing: []*tpb.Tracing{
			{
				DisableSpanReporting: &types.BoolValue{Value: true},
			},
		},
	}

	bazTrace := &tpb.Telemetry{
		Tracing: []*tpb.Tracing{
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "not-zipkin",
					},
				},
				CustomTags: map[string]*tpb.Tracing_CustomTag{
					"fun": {Type: &tpb.Tracing_CustomTag_Environment{
						Environment: &tpb.Tracing_Environment{
							Name:         "SOME_ENV_VAR",
							DefaultValue: "EMPTY",
						},
					}},
				},
			},
		},
	}

	cases := []struct {
		name           string
		ns             string
		configs        []config.Config
		workloadLabels map[string]string
		want           *tpb.Telemetry
	}{
		{
			name: "no configured telemetry",
			ns:   "missing",
		},
		{
			name:    "root namespace no workload labels",
			ns:      "istio-system",
			configs: []config.Config{newTelemetry("root", "istio-system", rootTrace)},
			want:    rootTrace,
		},
		{
			name: "workload-specific in namespace but no workload labels",
			ns:   "foo",
			configs: []config.Config{
				newTelemetry("root", "istio-system", rootTrace),
				newTelemetry("foo", "foo", fooTrace),
			},
			want: &tpb.Telemetry{
				Tracing: []*tpb.Tracing{
					{
						Providers: []*tpb.ProviderRef{
							{
								Name: "zipkin",
							},
						},
						RandomSamplingPercentage: &types.DoubleValue{Value: 10.10},
					},
				},
			},
		},
		{
			name: "workload-agnostic in namespace and no workload labels",
			ns:   "foo",
			configs: []config.Config{
				newTelemetry("root", "istio-system", rootTrace),
				newTelemetry("foo", "foo", barTrace),
			},
			want: &tpb.Telemetry{
				Tracing: []*tpb.Tracing{
					{
						Providers: []*tpb.ProviderRef{
							{
								Name: "zipkin",
							},
						},
						RandomSamplingPercentage: &types.DoubleValue{Value: 10.10},
						DisableSpanReporting:     &types.BoolValue{Value: true},
					},
				},
			},
		},
		{
			name:           "both workload-specific and workload-agnostic in namespace with workload labels",
			ns:             "foo",
			workloadLabels: map[string]string{"service.istio.io/canonical-name": "foo"},
			configs: []config.Config{
				newTelemetry("root", "istio-system", rootTrace),
				newTelemetry("foo", "foo", fooTrace),
				newTelemetry("bar", "foo", barTrace),
				newTelemetry("baz", "baz", bazTrace),
			},
			want: &tpb.Telemetry{
				Tracing: []*tpb.Tracing{
					{
						Providers: []*tpb.ProviderRef{
							{
								Name: "zipkin",
							},
						},
						DisableSpanReporting:     &types.BoolValue{Value: true},
						RandomSamplingPercentage: &types.DoubleValue{Value: 0.0},
					},
				},
			},
		},
		{
			name: "both workload-specific and workload-agnostic in namespace without workload labels",
			ns:   "foobar",
			configs: []config.Config{
				newTelemetry("root", "istio-system", rootTrace),
				newTelemetry("foo", "foobar", fooTrace),
				newTelemetry("bar", "foobar", barTrace),
				newTelemetry("baz", "baz", bazTrace),
			},
			want: &tpb.Telemetry{
				Tracing: []*tpb.Tracing{
					{
						Providers: []*tpb.ProviderRef{
							{
								Name: "zipkin",
							},
						},
						RandomSamplingPercentage: &types.DoubleValue{Value: 10.10},
						DisableSpanReporting:     &types.BoolValue{Value: true},
					},
				},
			},
		},
		{
			name: "provider and custom tags override",
			ns:   "baz",
			configs: []config.Config{
				newTelemetry("root", "istio-system", rootTrace),
				newTelemetry("foo", "foo", fooTrace),
				newTelemetry("baz", "baz", bazTrace),
			},
			want: &tpb.Telemetry{
				Tracing: []*tpb.Tracing{
					{
						Providers: []*tpb.ProviderRef{
							{
								Name: "not-zipkin",
							},
						},
						RandomSamplingPercentage: &types.DoubleValue{Value: 10.10},
						CustomTags: map[string]*tpb.Tracing_CustomTag{
							"fun": {Type: &tpb.Tracing_CustomTag_Environment{
								Environment: &tpb.Tracing_Environment{
									Name:         "SOME_ENV_VAR",
									DefaultValue: "EMPTY",
								},
							}},
						},
					},
				},
			},
		},
	}

	for _, v := range cases {
		t.Run(v.name, func(tt *testing.T) {
			telemetries := createTestTelemetries(v.configs, tt)
			got := telemetries.EffectiveTelemetry(v.ns, []labels.Instance{v.workloadLabels})
			if diff := cmp.Diff(v.want, got, protocmp.Transform()); diff != "" {
				tt.Errorf("EffectiveTelemetry(%s, %v) returned unexpected diff (-want +got):\n%s", v.ns, v.workloadLabels, diff)
			}
		})
	}
}

func createTestTelemetries(configs []config.Config, t *testing.T) *Telemetries {
	t.Helper()

	store := &telemetryStore{}
	for _, cfg := range configs {
		store.add(cfg)
	}
	environment := &Environment{
		IstioConfigStore: MakeIstioStore(store),
		Watcher:          mesh.NewFixedWatcher(&meshconfig.MeshConfig{RootNamespace: "istio-system"}),
	}
	telemetries, err := GetTelemetries(environment)
	if err != nil {
		t.Fatalf("GetTelemetries failed: %v", err)
	}
	return telemetries
}

func newTelemetry(name, ns string, spec config.Spec) config.Config {
	return config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioTelemetryV1Alpha1Telemetries.Resource().GroupVersionKind(),
			Name:             name,
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
