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

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/bookinfo"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/mixer"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
)

// This file contains Mixer tests that are ported from Mixer E2E tests

// Port of TestMetric
func TestIngessToPrometheus_ServiceMetric(t *testing.T) {
	ctx := framework.NewContext(t)
	defer ctx.Done(t)

	ctx.RequireOrSkip(t, environment.Kube)

	label := "source_workload"
	labelValue := "istio-ingressgateway"
	testMetric(t, ctx, label, labelValue)
}

// Port of TestMetric
func TestIngessToPrometheus_IngressMetric(t *testing.T) {
	ctx := framework.NewContext(t)
	defer ctx.Done(t)

	ctx.RequireOrSkip(t, environment.Kube)

	label := "destination_service"
	labelValue := "productpage.{{.TestNamespace}}.svc.cluster.local"
	testMetric(t, ctx, label, labelValue)
}

func testMetric(t *testing.T, ctx framework.TestContext, label string, labelValue string) { // nolint:interfacer
	t.Helper()

	g := galley.NewOrFail(t, ctx, galley.Config{})
	_ = mixer.NewOrFail(t, ctx, mixer.Config{Galley: g})

	d := bookinfo.DeployOrFail(t, ctx, bookinfo.BookInfo)

	g.ApplyConfigOrFail(t, d.Namespace(),
		bookinfo.NetworkingBookinfoGateway.LoadOrFail(t),
		bookinfo.GetDestinationRuleConfigFile(t, ctx).LoadOrFail(t),
		bookinfo.NetworkingVirtualServiceAllV1.LoadOrFail(t),
	)

	prom := prometheus.NewOrFail(t, ctx)
	ing := ingress.NewOrFail(t, ctx, ingress.Config{Istio: ist})

	// Warm up
	err := visitProductPage(ing, 30*time.Second, 200, t)
	if err != nil {
		t.Fatalf("unable to retrieve 200 from product page: %v", err)
	}

	label = tmpl.EvaluateOrFail(t, label, map[string]string{"TestNamespace": d.Namespace().Name()})
	labelValue = tmpl.EvaluateOrFail(t, labelValue, map[string]string{"TestNamespace": d.Namespace().Name()})

	// Wait for some data to arrive.
	initial, err := prom.WaitForQuiesce(`istio_requests_total{%s=%q,response_code="200"}`, label, labelValue)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Baseline established: initial = %v", initial)

	err = visitProductPage(ing, 30*time.Second, 200, t)
	if err != nil {
		t.Fatalf("unable to retrieve 200 from product page: %v", err)
	}

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
	ctx := framework.NewContext(t)
	defer ctx.Done(t)

	ctx.RequireOrSkip(t, environment.Kube)

	d := bookinfo.DeployOrFail(t, ctx, bookinfo.BookInfo)
	_ = bookinfo.DeployOrFail(t, ctx, bookinfo.BookinfoRatingsv2)
	_ = bookinfo.DeployOrFail(t, ctx, bookinfo.BookinfoDb)

	g := galley.NewOrFail(t, ctx, galley.Config{})
	_ = mixer.NewOrFail(t, ctx, mixer.Config{Galley: g})

	g.ApplyConfigOrFail(
		t,
		d.Namespace(),
		bookinfo.NetworkingBookinfoGateway.LoadOrFail(t),
		bookinfo.GetDestinationRuleConfigFile(t, ctx).LoadOrFail(t),
		bookinfo.NetworkingVirtualServiceAllV1.LoadOrFail(t),
		bookinfo.NetworkingTCPDbRule.LoadOrFail(t))

	prom := prometheus.NewOrFail(t, ctx)
	ing := ingress.NewOrFail(t, ctx, ingress.Config{Istio: ist})

	err := visitProductPage(ing, 30*time.Second, 200, t)
	if err != nil {
		t.Fatalf("unable to retrieve 200 from product page: %v", err)
	}

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
	want := float64(1)

	retry.UntilSuccessOrFail(t, func() error {
		t.Logf("prometheus query: %s", query)
		value, err := prom.WaitForQuiesce(query)
		if err != nil {
			return fmt.Errorf("could not get metrics from prometheus: %v", err)
		}

		got, err := prom.Sum(value, nil)
		if err != nil {
			t.Logf("value: %s", value.String())
			t.Logf("prometheus values for %s:\n%s", metricName, promDump(prom, metricName))
			return fmt.Errorf("could not find metric value: %v", err)
		}

		t.Logf("%s: %f", metricName, got)
		if got < want {
			t.Logf("prometheus values for %s:\n%s", metricName, promDump(prom, metricName))
			return fmt.Errorf("bad metric value: got %f, want at least %f", got, want)
		}
		return nil
	})
}

func visitProductPage(ing ingress.Instance, timeout time.Duration, wantStatus int, t *testing.T) error {
	return retry.UntilSuccess(func() error {

		response, err := ing.Call("/productpage")
		if err != nil {
			return fmt.Errorf("unable to connect to product page: %v", err)
		}

		status := response.Code
		if status != wantStatus {
			return fmt.Errorf("did not get the expected response (%d) from the product page: %d", wantStatus, status)
		}
		t.Logf("Got %d response from product page!", wantStatus)
		return nil
	}, retry.Delay(3*time.Second), retry.Timeout(timeout))
}

// promDump gets all of the recorded values for a metric by name and generates a report of the values.
// used for debugging of failures to provide a comprehensive view of traffic experienced.
func promDump(p prometheus.Instance, metric string) string {
	if value, err := p.WaitForQuiesce(fmt.Sprintf("%s{}", metric)); err == nil {
		return value.String()
	}
	return ""
}
