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

	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	tpb "istio.io/api/telemetry/v1alpha1"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/test/util/assert"
)

func TestTelemetryMetricsExhaustiveness(t *testing.T) {
	AssertProvidersHandled(telemetryFilterHandled)
}

func TestMergeMetrics(t *testing.T) {
	withoutMetricsProviders := mesh.DefaultMeshConfig()
	withMetricsProviders := mesh.DefaultMeshConfig()
	withMetricsProviders.DefaultProviders = &meshconfig.MeshConfig_DefaultProviders{
		Metrics: []string{"prometheus"},
	}

	enablePrometheus := &tpb.Metrics{
		Providers: []*tpb.ProviderRef{
			{Name: "prometheus"},
		},
	}
	disableAll := &tpb.Metrics{
		Providers: []*tpb.ProviderRef{
			{Name: "prometheus"},
		},
		Overrides: []*tpb.MetricsOverrides{
			{
				Match: &tpb.MetricSelector{
					MetricMatch: &tpb.MetricSelector_Metric{
						Metric: tpb.MetricSelector_ALL_METRICS,
					},
					Mode: tpb.WorkloadMode_CLIENT_AND_SERVER,
				},
				Disabled: &wrappers.BoolValue{Value: true},
			},
		},
	}
	disableAllWithoutMode := &tpb.Metrics{
		Providers: []*tpb.ProviderRef{
			{Name: "prometheus"},
		},
		Overrides: []*tpb.MetricsOverrides{
			{
				Match: &tpb.MetricSelector{
					MetricMatch: &tpb.MetricSelector_Metric{
						Metric: tpb.MetricSelector_ALL_METRICS,
					},
				},
				Disabled: &wrappers.BoolValue{Value: true},
			},
		},
	}
	disableServer := &tpb.Metrics{
		Providers: []*tpb.ProviderRef{
			{Name: "prometheus"},
		},
		Overrides: []*tpb.MetricsOverrides{
			{
				Match: &tpb.MetricSelector{
					MetricMatch: &tpb.MetricSelector_Metric{
						Metric: tpb.MetricSelector_ALL_METRICS,
					},
					Mode: tpb.WorkloadMode_SERVER,
				},
				Disabled: &wrappers.BoolValue{Value: true},
			},
		},
	}
	disableClient := &tpb.Metrics{
		Providers: []*tpb.ProviderRef{
			{Name: "prometheus"},
		},
		Overrides: []*tpb.MetricsOverrides{
			{
				Match: &tpb.MetricSelector{
					MetricMatch: &tpb.MetricSelector_Metric{
						Metric: tpb.MetricSelector_ALL_METRICS,
					},
					Mode: tpb.WorkloadMode_CLIENT,
				},
				Disabled: &wrappers.BoolValue{Value: true},
			},
		},
	}
	disableServerAndCustomClient := &tpb.Metrics{
		Providers: []*tpb.ProviderRef{
			{Name: "prometheus"},
		},
		Overrides: []*tpb.MetricsOverrides{
			{
				Match: &tpb.MetricSelector{
					MetricMatch: &tpb.MetricSelector_Metric{
						Metric: tpb.MetricSelector_ALL_METRICS,
					},
					Mode: tpb.WorkloadMode_SERVER,
				},
				Disabled: &wrappers.BoolValue{Value: true},
			},
			{
				Match: &tpb.MetricSelector{
					MetricMatch: &tpb.MetricSelector_Metric{
						Metric: tpb.MetricSelector_REQUEST_COUNT,
					},
					Mode: tpb.WorkloadMode_CLIENT,
				},
				TagOverrides: map[string]*tpb.MetricsOverrides_TagOverride{
					"destination_canonical_service": {
						Operation: tpb.MetricsOverrides_TagOverride_REMOVE,
					},
				},
			},
		},
	}

	cases := []struct {
		name     string
		metrics  []*tpb.Metrics
		mesh     *meshconfig.MeshConfig
		expected map[string]metricsConfig
	}{
		// without providers
		{
			name:     "no metrics",
			mesh:     withoutMetricsProviders,
			expected: nil,
		},
		{
			name:    "enable all metrics",
			metrics: []*tpb.Metrics{enablePrometheus},
			mesh:    withoutMetricsProviders,
			expected: map[string]metricsConfig{
				"prometheus": {},
			},
		},
		// with providers
		{
			name: "metrics with default providers",
			mesh: withMetricsProviders,
			expected: map[string]metricsConfig{
				"prometheus": {},
			},
		},
		{
			name:    "disable all metrics",
			metrics: []*tpb.Metrics{disableAll},
			mesh:    withMetricsProviders,
			expected: map[string]metricsConfig{
				"prometheus": {
					ClientMetrics: metricConfig{
						Disabled: true,
					},
					ServerMetrics: metricConfig{
						Disabled: true,
					},
				},
			},
		},
		{
			name:    "disable all metrics without mode",
			metrics: []*tpb.Metrics{disableAllWithoutMode},
			mesh:    withMetricsProviders,
			expected: map[string]metricsConfig{
				"prometheus": {
					ClientMetrics: metricConfig{
						Disabled: true,
					},
					ServerMetrics: metricConfig{
						Disabled: true,
					},
				},
			},
		},
		{
			name:    "disable server metrics",
			metrics: []*tpb.Metrics{disableServer},
			mesh:    withMetricsProviders,
			expected: map[string]metricsConfig{
				"prometheus": {
					ServerMetrics: metricConfig{
						Disabled: true,
					},
				},
			},
		},
		{
			name:    "disable client metrics",
			metrics: []*tpb.Metrics{disableClient},
			mesh:    withMetricsProviders,
			expected: map[string]metricsConfig{
				"prometheus": {
					ClientMetrics: metricConfig{
						Disabled: true,
					},
				},
			},
		},
		{
			name:    "disable server and custom client metrics",
			metrics: []*tpb.Metrics{disableServerAndCustomClient},
			mesh:    withMetricsProviders,
			expected: map[string]metricsConfig{
				"prometheus": {
					ClientMetrics: metricConfig{
						Disabled: false,
						Overrides: []metricsOverride{
							{
								Name: "REQUEST_COUNT",
								Tags: []tagOverride{
									{Name: "destination_canonical_service", Remove: true},
								},
							},
						},
					},
					ServerMetrics: metricConfig{
						Disabled: true,
					},
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeMetrics(tc.metrics, tc.mesh)
			assert.Equal(t, tc.expected, got)
		})
	}
}
