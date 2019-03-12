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

package mixer

import (
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework2/components/ingress"
	"istio.io/istio/pkg/test/framework2/components/prometheus"

	"istio.io/istio/pkg/test/framework2"
	"istio.io/istio/pkg/test/framework2/components/environment"
	"istio.io/istio/pkg/test/framework2/components/galley"
	"istio.io/istio/pkg/test/framework2/components/mixer"
	"istio.io/istio/pkg/test/framework2/runtime"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework2/components/bookinfo"
)

// This file contains Mixer tests that are ported from Mixer E2E tests

// Port of TestMetric
func TestIngessToPrometheus_ServiceMetric(t *testing.T) {
	ctx := framework2.NewContext(t)
	defer ctx.Done(t)

	ctx.RequireOrSkip(t, environment.Kube)

	label := "source_workload"
	labelValue := "istio-ingressgateway"
	testMetric(t, ctx, label, labelValue)
}

// Port of TestMetric
func TestIngessToPrometheus_IngressMetric(t *testing.T) {
	ctx := framework2.NewContext(t)
	defer ctx.Done(t)

	ctx.RequireOrSkip(t, environment.Kube)

	label := "destination_service"
	labelValue := "productpage.{{.TestNamespace}}.svc.cluster.local"
	testMetric(t, ctx, label, labelValue)
}

func testMetric(t *testing.T, ctx *runtime.TestContext, label string, labelValue string) {
	t.Helper()

	g := galley.NewOrFail(t, ctx)
	_ = mixer.NewOrFail(t, ctx, &mixer.Config{Galley: g})
	g.ApplyConfigOrFail(t,
		test.JoinConfigs(
			bookinfo.NetworkingBookinfoGateway.LoadOrFail(t),
			bookinfo.GetDestinationRuleConfigFile(t, ctx).LoadOrFail(t),
			bookinfo.NetworkingVirtualServiceAllV1.LoadOrFail(t),
		))

	prom := prometheus.NewOrFail(t, ctx)
	ing := ingress.NewOrFail(t, ctx)

	// Warm up
	visitProductPage(ing, 30*time.Second, 200, t)

	// Wait for some data to arrive.
	initial, err := prom.WaitForQuiesce(`istio_requests_total{%s=%q,response_code="200"}`, label, labelValue)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Baseline established: initial = %v", initial)

	visitProductPage(ing, 30*time.Second, 200, t)

	final, err := prom.WaitForQuiesce(`istio_requests_total{%s=%q,response_code="200"}`, label, labelValue)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Quiesced to: final = %v", final)

	metricName := "istio_requests_total"
	i, err := prom.Sum(initial, nil)
	if err != nil {
		t.Logf("prometheus values for %s:\n%s", metricName, promDump(prom, metricName))
		t.Fatal(err)
	}

	f, err := prom.Sum(final, nil)
	if err != nil {
		t.Logf("prometheus values for %s:\n%s", metricName, promDump(prom, metricName))
		t.Fatal(err)
	}

	if (f - i) < float64(1) {
		t.Errorf("Bad metric value: got %f, want at least 1", f-i)
	}
}

// Port of TestTcpMetric
func TestTcpMetric(t *testing.T) {
	t.Skip("https://github.com/istio/istio/issues/11430")
	ctx := framework2.NewContext(t)
	defer ctx.Done(t)

	ctx.RequireOrSkip(t, environment.Kube)

	bi := bookinfo.NewOrFail(t, ctx)
	err := bi.DeployRatingsV2(ctx)
	if err != nil {
		t.Fatalf("Could not deploy ratings v2: %v", err)
	}
	err = bi.DeployMongoDb(ctx)
	if err != nil {
		t.Fatalf("Could not deploy mongodb: %v", err)
	}

	g := galley.NewOrFail(t, ctx)
	_ = mixer.NewOrFail(t, ctx, &mixer.Config{Galley: g})

	g.ApplyConfigOrFail(t,
		test.JoinConfigs(
			bookinfo.NetworkingBookinfoGateway.LoadOrFail(t),
			bookinfo.GetDestinationRuleConfigFile(t, ctx).LoadOrFail(t),
			bookinfo.NetworkingVirtualServiceAllV1.LoadOrFail(t),
			bookinfo.NetworkingTCPDbRule.LoadOrFail(t),
		))

	prom := prometheus.NewOrFail(t, ctx)
	ing := ingress.NewOrFail(t, ctx)

	visitProductPage(ing, 30*time.Second, 200, t)

	query := fmt.Sprintf("sum(istio_tcp_sent_bytes_total{destination_app=\"%s\"})", "mongodb")
	validateMetric(t, prom, query, "istio_tcp_sent_bytes_total")

	query = fmt.Sprintf("sum(istio_tcp_received_bytes_total{destination_app=\"%s\"})", "mongodb")
	validateMetric(t, prom, query, "istio_tcp_received_bytes_total")

	query = fmt.Sprintf("sum(istio_tcp_connections_opened_total{destination_app=\"%s\"})", "mongodb")
	validateMetric(t, prom, query, "istio_tcp_connections_opened_total")

	query = fmt.Sprintf("sum(istio_tcp_connections_closed_total{destination_app=\"%s\"})", "mongodb")
	validateMetric(t, prom, query, "istio_tcp_connections_closed_total")
}

func validateMetric(t *testing.T, prom prometheus.Instance, query, metricName string) {
	t.Helper()
	want := float64(1)

	t.Logf("prometheus query: %s", query)
	value, err := prom.WaitForQuiesce(query)
	if err != nil {
		t.Fatalf("Could not get metrics from prometheus: %v", err)
	}

	got, err := prom.Sum(value, nil)
	if err != nil {
		t.Logf("value: %s", value.String())
		t.Logf("prometheus values for %s:\n%s", metricName, promDump(prom, metricName))
		t.Fatalf("Could not find metric value: %v", err)
	}
	t.Logf("%s: %f", metricName, got)
	if got < want {
		t.Logf("prometheus values for %s:\n%s", metricName, promDump(prom, metricName))
		t.Errorf("Bad metric value: got %f, want at least %f", got, want)
	}
}

func visitProductPage(ing ingress.Instance, timeout time.Duration, wantStatus int, t *testing.T) error {
	start := time.Now()
	for {
		response, err := ing.Call("/productpage")
		if err != nil {
			t.Logf("Unable to connect to product page: %v", err)
		}

		status := response.Code
		if status == wantStatus {
			t.Logf("Got %d response from product page!", wantStatus)
			return nil
		}

		if time.Since(start) > timeout {
			return fmt.Errorf("could not retrieve product page in %v: Last status: %v", timeout, status)
		}

		time.Sleep(3 * time.Second)
	}
}

// promDump gets all of the recorded values for a metric by name and generates a report of the values.
// used for debugging of failures to provide a comprehensive view of traffic experienced.
func promDump(p prometheus.Instance, metric string) string {
	if value, err := p.WaitForQuiesce(fmt.Sprintf("%s{}", metric)); err == nil {
		return value.String()
	}
	return ""
}
