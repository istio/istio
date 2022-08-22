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

package cmd

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	prometheus_model "github.com/prometheus/common/model"
	v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/kube"
)

// mockPromAPI lets us mock calls to Prometheus API
type mockPromAPI struct {
	cannedResponse map[string]prometheus_model.Value
}

func mockExecClientAuthNoPilot(_, _, _ string) (kube.CLIClient, error) {
	return &kube.MockClient{}, nil
}

func TestMetricsNoPrometheus(t *testing.T) {
	kubeClientWithRevision = mockExecClientAuthNoPilot

	cases := []testCase{
		{ // case 0
			args:           strings.Split("experimental metrics", " "),
			expectedRegexp: regexp.MustCompile("Error: metrics requires workload name\n"),
			wantException:  true,
		},
		{ // case 1
			args:           strings.Split("experimental metrics details", " "),
			expectedOutput: "Error: no Prometheus pods found\n",
			wantException:  true,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyOutput(t, c)
		})
	}
}

func TestMetrics(t *testing.T) {
	kubeClientWithRevision = mockPortForwardClientAuthPrometheus

	cases := []testCase{
		{ // case 0
			args:           strings.Split("experimental metrics details", " "),
			expectedRegexp: regexp.MustCompile("could not build metrics for workload"),
			wantException:  true,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyOutput(t, c)
		})
	}
}

func mockPortForwardClientAuthPrometheus(_, _, _ string) (kube.CLIClient, error) {
	return &kube.MockClient{
		DiscoverablePods: map[string]map[string]*v1.PodList{
			"istio-system": {
				"app=prometheus": {
					Items: []v1.Pod{
						{
							TypeMeta: meta_v1.TypeMeta{
								Kind: "MockPod",
							},
						},
					},
				},
			},
		},
	}, nil
}

func TestAPI(t *testing.T) {
	_, _ = prometheusAPI(fmt.Sprintf("http://localhost:%d", 1234))
}

var _ promv1.API = mockPromAPI{}

func TestPrintMetrics(t *testing.T) {
	mockProm := mockPromAPI{
		cannedResponse: map[string]prometheus_model.Value{
			"sum(rate(istio_requests_total{destination_workload=~\"details.*\", destination_workload_namespace=~\".*\",reporter=\"destination\"}[1m0s]))": prometheus_model.Vector{ // nolint: lll
				&prometheus_model.Sample{Value: 0.04},
			},
			"sum(rate(istio_requests_total{destination_workload=~\"details.*\", destination_workload_namespace=~\".*\",reporter=\"destination\",response_code=~\"[45][0-9]{2}\"}[1m0s]))": prometheus_model.Vector{}, // nolint: lll
			"histogram_quantile(0.500000, sum(rate(istio_request_duration_milliseconds_bucket{destination_workload=~\"details.*\", destination_workload_namespace=~\".*\",reporter=\"destination\"}[1m0s])) by (le))": prometheus_model.Vector{ // nolint: lll
				&prometheus_model.Sample{Value: 2.5},
			},
			"histogram_quantile(0.900000, sum(rate(istio_request_duration_milliseconds_bucket{destination_workload=~\"details.*\", destination_workload_namespace=~\".*\",reporter=\"destination\"}[1m0s])) by (le))": prometheus_model.Vector{ // nolint: lll
				&prometheus_model.Sample{Value: 4.5},
			},
			"histogram_quantile(0.990000, sum(rate(istio_request_duration_milliseconds_bucket{destination_workload=~\"details.*\", destination_workload_namespace=~\".*\",reporter=\"destination\"}[1m0s])) by (le))": prometheus_model.Vector{ // nolint: lll
				&prometheus_model.Sample{Value: 4.95},
			},
		},
	}
	workload := "details"

	sm, err := metrics(mockProm, workload, time.Minute)
	if err != nil {
		t.Fatalf("Unwanted exception %v", err)
	}

	var out bytes.Buffer
	printHeader(&out)
	printMetrics(&out, sm)
	output := out.String()

	expectedOutput := `                                  WORKLOAD    TOTAL RPS    ERROR RPS  P50 LATENCY  P90 LATENCY  P99 LATENCY
                                   details        0.040        0.000          2ms          4ms          4ms
`
	if output != expectedOutput {
		t.Fatalf("Unexpected output; got:\n %q\nwant:\n %q", output, expectedOutput)
	}
}

func (client mockPromAPI) Alerts(ctx context.Context) (promv1.AlertsResult, error) {
	return promv1.AlertsResult{}, fmt.Errorf("TODO mockPromAPI doesn't mock Alerts")
}

func (client mockPromAPI) AlertManagers(ctx context.Context) (promv1.AlertManagersResult, error) {
	return promv1.AlertManagersResult{}, fmt.Errorf("TODO mockPromAPI doesn't mock AlertManagers")
}

func (client mockPromAPI) CleanTombstones(ctx context.Context) error {
	return nil
}

func (client mockPromAPI) Config(ctx context.Context) (promv1.ConfigResult, error) {
	return promv1.ConfigResult{}, nil
}

func (client mockPromAPI) DeleteSeries(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) error {
	return nil
}

func (client mockPromAPI) Flags(ctx context.Context) (promv1.FlagsResult, error) {
	return nil, nil
}

func (client mockPromAPI) Query(ctx context.Context, query string, ts time.Time, opts ...promv1.Option) (prometheus_model.Value, promv1.Warnings, error) {
	canned, ok := client.cannedResponse[query]
	if !ok {
		return prometheus_model.Vector{}, nil, nil
	}
	return canned, nil, nil
}

func (client mockPromAPI) TSDB(ctx context.Context) (promv1.TSDBResult, error) {
	return promv1.TSDBResult{}, nil
}

func (client mockPromAPI) QueryRange(
	ctx context.Context,
	query string,
	r promv1.Range,
	opts ...promv1.Option,
) (prometheus_model.Value, promv1.Warnings, error) {
	canned, ok := client.cannedResponse[query]
	if !ok {
		return prometheus_model.Vector{}, nil, nil
	}
	return canned, nil, nil
}

func (client mockPromAPI) WalReplay(ctx context.Context) (promv1.WalReplayStatus, error) {
	// TODO implement me
	panic("implement me")
}

func (client mockPromAPI) Series(ctx context.Context, matches []string,
	startTime time.Time, endTime time.Time,
) ([]prometheus_model.LabelSet, promv1.Warnings, error) {
	return nil, nil, nil
}

func (client mockPromAPI) Snapshot(ctx context.Context, skipHead bool) (promv1.SnapshotResult, error) {
	return promv1.SnapshotResult{}, nil
}

func (client mockPromAPI) Rules(ctx context.Context) (promv1.RulesResult, error) {
	return promv1.RulesResult{}, nil
}

func (client mockPromAPI) Targets(ctx context.Context) (promv1.TargetsResult, error) {
	return promv1.TargetsResult{}, nil
}

func (client mockPromAPI) TargetsMetadata(ctx context.Context, matchTarget string, metric string, limit string) ([]promv1.MetricMetadata, error) {
	return nil, nil
}

func (client mockPromAPI) Runtimeinfo(ctx context.Context) (promv1.RuntimeinfoResult, error) {
	return promv1.RuntimeinfoResult{}, nil
}

func (client mockPromAPI) Metadata(ctx context.Context, metric string, limit string) (map[string][]promv1.Metadata, error) {
	return nil, nil
}

func (client mockPromAPI) LabelNames(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]string, promv1.Warnings, error) {
	return nil, nil, nil
}

func (client mockPromAPI) LabelValues(context.Context, string, []string, time.Time, time.Time) (prometheus_model.LabelValues, promv1.Warnings, error) {
	return nil, nil, nil
}

func (client mockPromAPI) Buildinfo(ctx context.Context) (promv1.BuildinfoResult, error) {
	return promv1.BuildinfoResult{}, nil
}

func (client mockPromAPI) QueryExemplars(ctx context.Context, query string, startTime time.Time, endTime time.Time) ([]promv1.ExemplarQueryResult, error) {
	return nil, nil
}
