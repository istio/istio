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

package metrics

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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/util/testutil"
)

// mockPromAPI lets us mock calls to Prometheus API
type mockPromAPI struct {
	promv1.API
	cannedResponse map[string]prometheus_model.Value
}

func TestMetricsNoPrometheus(t *testing.T) {
	cases := []testutil.TestCase{
		{ // case 0
			Args:           []string{},
			ExpectedRegexp: regexp.MustCompile("Error: metrics requires workload name\n"),
			WantException:  true,
		},
		{ // case 1
			Args:           strings.Split("details", " "),
			ExpectedOutput: "Error: no Prometheus pods found\n",
			WantException:  true,
		},
	}

	metricCmd := Cmd(cli.NewFakeContext(nil))
	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.Args, " ")), func(t *testing.T) {
			testutil.VerifyOutput(t, metricCmd, c)
		})
	}
}

func TestMetrics(t *testing.T) {
	cases := []testutil.TestCase{
		{ // case 0
			Args:           strings.Split("details", " "),
			ExpectedRegexp: regexp.MustCompile("could not build metrics for workload"),
			WantException:  true,
		},
	}

	ctx := cli.NewFakeContext(&cli.NewFakeContextOption{
		IstioNamespace: "istio-system",
	})
	client, err := ctx.CLIClient()
	if err != nil {
		t.Fatal(err)
	}
	client.Kube().CoreV1().Pods("istio-system").Create(context.TODO(), &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "prometheus",
			Namespace: "istio-system",
			Labels: map[string]string{
				"app.kubernetes.io/name": "prometheus",
			},
		},
	}, metav1.CreateOptions{})
	metricCmd := Cmd(ctx)
	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.Args, " ")), func(t *testing.T) {
			testutil.VerifyOutput(t, metricCmd, c)
		})
	}
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

func (client mockPromAPI) Query(ctx context.Context, query string, ts time.Time, opts ...promv1.Option,
) (prometheus_model.Value, promv1.Warnings, promv1.Infos, error) {
	canned, ok := client.cannedResponse[query]
	if !ok {
		return prometheus_model.Vector{}, nil, nil, nil
	}
	return canned, nil, nil, nil
}
