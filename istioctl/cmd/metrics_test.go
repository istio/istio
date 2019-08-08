// Copyright 2019 Istio Authors
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
	"strings"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/istioctl/pkg/kubernetes"
	"istio.io/pkg/version"

	prometheus_v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	prometheus_model "github.com/prometheus/common/model"
)

// mockPortForwardConfig includes a partial implementation of mocking istioctl's Kube client
type mockPortForwardConfig struct {
	discoverablePods map[string]map[string]*v1.PodList
}

// mockPromAPI lets us mock calls to Prometheus API
type mockPromAPI struct {
	cannedResponse map[string]prometheus_model.Value
}

func TestMetricsNoPrometheus(t *testing.T) {
	clientExecFactory = mockExecClientAuthNoPilot

	cases := []testCase{
		{ // case 0
			args:           strings.Split("experimental metrics", " "),
			expectedOutput: "Error: requires at least 1 arg(s), only received 0\n",
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
	clientExecFactory = mockPortForwardClientAuthPrometheus

	cases := []testCase{
		{ // case 0
			args:          strings.Split("experimental metrics details", " "),
			wantException: true,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyOutput(t, c)
		})
	}
}

func mockPortForwardClientAuthPrometheus(_, _ string) (kubernetes.ExecClient, error) {
	return &mockPortForwardConfig{
		discoverablePods: map[string]map[string]*v1.PodList{
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

// nolint: unparam
func (client mockPortForwardConfig) AllPilotsDiscoveryDo(pilotNamespace, method, path string, body []byte) (map[string][]byte, error) {
	return nil, fmt.Errorf("mockPortForwardConfig doesn't mock Pilot discovery")
}

// nolint: unparam
func (client mockPortForwardConfig) EnvoyDo(podName, podNamespace, method, path string, body []byte) ([]byte, error) {
	return nil, fmt.Errorf("mockPortForwardConfig doesn't mock Envoy")
}

// nolint: unparam
func (client mockPortForwardConfig) PilotDiscoveryDo(pilotNamespace, method, path string, body []byte) ([]byte, error) {
	return nil, fmt.Errorf("mockPortForwardConfig doesn't mock Pilot discovery")
}

func (client mockPortForwardConfig) GetIstioVersions(namespace string) (*version.MeshInfo, error) {
	return nil, nil
}

func (client mockPortForwardConfig) PodsForSelector(namespace, labelSelector string) (*v1.PodList, error) {
	podsForNamespace, ok := client.discoverablePods[namespace]
	if !ok {
		return &v1.PodList{}, nil
	}
	podsForLabel, ok := podsForNamespace[labelSelector]
	if !ok {
		return &v1.PodList{}, nil
	}
	return podsForLabel, nil
}

func (client mockPortForwardConfig) BuildPortForwarder(podName string, ns string, localPort int, podPort int) (*kubernetes.PortForward, error) {
	// TODO make istioctl/pkg/kubernetes/client.go use pkg/test/kube/port_forwarder.go
	// so that the port forward can be mocked.
	return nil, fmt.Errorf("TODO mockPortForwardConfig doesn't mock port forward")
}

func TestAPI(t *testing.T) {
	_, _ = prometheusAPI(1234)
}

func TestPrintMetrics(t *testing.T) {
	mockProm := mockPromAPI{
		cannedResponse: map[string]prometheus_model.Value{
			"sum(rate(istio_requests_total{destination_workload=~\"details.*\", destination_workload_namespace=~\".*\",reporter=\"destination\"}[1m]))": prometheus_model.Vector{ // nolint: lll
				&prometheus_model.Sample{Value: 0.04},
			},
			"sum(rate(istio_requests_total{destination_workload=~\"details.*\", destination_workload_namespace=~\".*\",reporter=\"destination\",response_code!=\"200\"}[1m]))": prometheus_model.Vector{}, // nolint: lll
			"histogram_quantile(0.500000, sum(rate(istio_request_duration_seconds_bucket{destination_workload=~\"details.*\", destination_workload_namespace=~\".*\",reporter=\"destination\"}[1m])) by (le))": prometheus_model.Vector{ // nolint: lll
				&prometheus_model.Sample{Value: 0.0025},
			},
			"histogram_quantile(0.900000, sum(rate(istio_request_duration_seconds_bucket{destination_workload=~\"details.*\", destination_workload_namespace=~\".*\",reporter=\"destination\"}[1m])) by (le))": prometheus_model.Vector{ // nolint: lll
				&prometheus_model.Sample{Value: 0.0045},
			},
			"histogram_quantile(0.990000, sum(rate(istio_request_duration_seconds_bucket{destination_workload=~\"details.*\", destination_workload_namespace=~\".*\",reporter=\"destination\"}[1m])) by (le))": prometheus_model.Vector{ // nolint: lll
				&prometheus_model.Sample{Value: 0.00495},
			},
		},
	}
	workload := "details"

	sm, err := metrics(mockProm, workload)
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
		t.Fatalf("Unexpected output; got: %q\nwant: %q", output, expectedOutput)
	}
}

func (client mockPromAPI) AlertManagers(ctx context.Context) (prometheus_v1.AlertManagersResult, error) {
	return prometheus_v1.AlertManagersResult{}, fmt.Errorf("TODO mockPromAPI doesn't mock AlertManagers")
}

func (client mockPromAPI) CleanTombstones(ctx context.Context) error {
	return nil
}

func (client mockPromAPI) Config(ctx context.Context) (prometheus_v1.ConfigResult, error) {
	return prometheus_v1.ConfigResult{}, nil
}

func (client mockPromAPI) DeleteSeries(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) error {
	return nil
}

func (client mockPromAPI) Flags(ctx context.Context) (prometheus_v1.FlagsResult, error) {
	return nil, nil
}

func (client mockPromAPI) LabelValues(ctx context.Context, label string) (prometheus_model.LabelValues, error) {
	return nil, nil
}

func (client mockPromAPI) Query(ctx context.Context, query string, ts time.Time) (prometheus_model.Value, error) {
	canned, ok := client.cannedResponse[query]
	if !ok {
		return prometheus_model.Vector{}, nil
	}
	return canned, nil
}

func (client mockPromAPI) QueryRange(ctx context.Context, query string, r prometheus_v1.Range) (prometheus_model.Value, error) {
	return nil, nil
}

func (client mockPromAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]prometheus_model.LabelSet, error) {
	return nil, nil
}

func (client mockPromAPI) Snapshot(ctx context.Context, skipHead bool) (prometheus_v1.SnapshotResult, error) {
	return prometheus_v1.SnapshotResult{}, nil
}

func (client mockPromAPI) Rules(ctx context.Context) (prometheus_v1.RulesResult, error) {
	return prometheus_v1.RulesResult{}, nil
}

func (client mockPromAPI) Targets(ctx context.Context) (prometheus_v1.TargetsResult, error) {
	return prometheus_v1.TargetsResult{}, nil
}
