//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package mixer

import (
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/components"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/ids"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/pkg/test/framework/runtime/components/bookinfo"
	"istio.io/istio/pkg/test/framework/runtime/components/environment/kube"
)

// This file contains Mixer tests that are ported from Mixer E2E tests

// Port of TestMetric
func TestIngessToPrometheus_ServiceMetric(t *testing.T) {
	ctx := framework.GetContext(t)
	ctx.RequireOrSkip(t, lifecycle.Test, &descriptors.KubernetesEnvironment, &ids.Mixer, &ids.Prometheus, &ids.BookInfo, &ids.Ingress)

	label := "source_workload"
	labelValue := "istio-ingressgateway"
	testMetric(t, ctx, label, labelValue)
}

// Port of TestMetric
func TestIngessToPrometheus_IngressMetric(t *testing.T) {
	ctx := framework.GetContext(t)
	ctx.RequireOrSkip(t, lifecycle.Test, &descriptors.KubernetesEnvironment, &ids.Mixer, &ids.Prometheus, &ids.BookInfo, &ids.Ingress)

	label := "destination_service"
	labelValue := "productpage.{{.TestNamespace}}.svc.cluster.local"
	testMetric(t, ctx, label, labelValue)
}

func testMetric(t *testing.T, ctx component.Repository, label string, labelValue string) {
	t.Helper()

	mxr := components.GetMixer(ctx, t)
	env, err := kube.GetEnvironment(ctx)
	if err != nil {
		t.Fatalf("Could not get test environment: %v", err)
	}
	destinationRuleAll := bookinfo.NetworkingDestinationRuleAll
	if env.IsMtlsEnabled() {
		destinationRuleAll = bookinfo.NetworkingDestinationRuleAllMtls
	}
	mxr.Configure(t,
		lifecycle.Test,
		test.JoinConfigs(
			bookinfo.NetworkingBookinfoGateway.LoadOrFail(t),
			destinationRuleAll.LoadOrFail(t),
			bookinfo.NetworkingVirtualServiceAllV1.LoadOrFail(t),
		))

	prometheus := components.GetPrometheus(ctx, t)
	ingress := components.GetIngress(ctx, t)

	// Warm up
	visitProductPage(ingress, 30*time.Second, 200, t)

	// Wait for some data to arrive.
	initial, err := prometheus.WaitForQuiesce(`istio_requests_total{%s=%q,response_code="200"}`, label, labelValue)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Baseline established: initial = %v", initial)

	visitProductPage(ingress, 30*time.Second, 200, t)

	final, err := prometheus.WaitForQuiesce(`istio_requests_total{%s=%q,response_code="200"}`, label, labelValue)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Quiesced to: final = %v", final)

	metricName := "istio_requests_total"
	i, err := prometheus.Sum(initial, nil)
	if err != nil {
		t.Logf("prometheus values for %s:\n%s", metricName, promDump(prometheus, metricName))
		t.Fatal(err)
	}

	f, err := prometheus.Sum(final, nil)
	if err != nil {
		t.Logf("prometheus values for %s:\n%s", metricName, promDump(prometheus, metricName))
		t.Fatal(err)
	}

	if (f - i) < float64(1) {
		t.Errorf("Bad metric value: got %f, want at least 1", f-i)
	}
}

// Port of TestTcpMetric
func TestTcpMetric(t *testing.T) {
	ctx := framework.GetContext(t)
	scope := lifecycle.Test
	ctx.RequireOrSkip(t, scope, &descriptors.KubernetesEnvironment, &ids.Mixer, &ids.Prometheus, &ids.BookInfo, &ids.Ingress)

	bookInfo := components.GetBookinfo(ctx, t)
	err := bookInfo.DeployRatingsV2(ctx, scope)
	if err != nil {
		t.Fatalf("Could not deploy ratings v2: %v", err)
	}
	err = bookInfo.DeployMongoDb(ctx, scope)
	if err != nil {
		t.Fatalf("Could not deploy mongodb: %v", err)
	}

	mxr := components.GetMixer(ctx, t)
	env, err := kube.GetEnvironment(ctx)
	if err != nil {
		t.Fatalf("Could not get test environment: %v", err)
	}
	destinationRuleAll := bookinfo.NetworkingDestinationRuleAll
	if env.IsMtlsEnabled() {
		destinationRuleAll = bookinfo.NetworkingDestinationRuleAllMtls
	}
	mxr.Configure(t,
		lifecycle.Test,
		test.JoinConfigs(
			bookinfo.NetworkingBookinfoGateway.LoadOrFail(t),
			destinationRuleAll.LoadOrFail(t),
			bookinfo.NetworkingVirtualServiceAllV1.LoadOrFail(t),
			bookinfo.NetworkingTCPDbRule.LoadOrFail(t),
		))

	prometheus := components.GetPrometheus(ctx, t)
	ingress := components.GetIngress(ctx, t)

	visitProductPage(ingress, 30*time.Second, 200, t)

	query := fmt.Sprintf("sum(istio_tcp_sent_bytes_total{destination_app=\"%s\"})", "mongodb")
	validateMetric(t, prometheus, query, "istio_tcp_sent_bytes_total")

	query = fmt.Sprintf("sum(istio_tcp_received_bytes_total{destination_app=\"%s\"})", "mongodb")
	validateMetric(t, prometheus, query, "istio_tcp_received_bytes_total")

	query = fmt.Sprintf("sum(istio_tcp_connections_opened_total{destination_app=\"%s\"})", "mongodb")
	validateMetric(t, prometheus, query, "istio_tcp_connections_opened_total")

	query = fmt.Sprintf("sum(istio_tcp_connections_closed_total{destination_app=\"%s\"})", "mongodb")
	validateMetric(t, prometheus, query, "istio_tcp_connections_closed_total")
}

func validateMetric(t *testing.T, prometheus components.Prometheus, query, metricName string) {
	t.Helper()
	want := float64(1)

	t.Logf("prometheus query: %s", query)
	value, err := prometheus.WaitForQuiesce(query)
	if err != nil {
		t.Fatalf("Could not get metrics from prometheus: %v", err)
	}

	got, err := prometheus.Sum(value, nil)
	if err != nil {
		t.Logf("value: %s", value.String())
		t.Logf("prometheus values for %s:\n%s", metricName, promDump(prometheus, metricName))
		t.Fatalf("Could not find metric value: %v", err)
	}
	t.Logf("%s: %f", metricName, got)
	if got < want {
		t.Logf("prometheus values for %s:\n%s", metricName, promDump(prometheus, metricName))
		t.Errorf("Bad metric value: got %f, want at least %f", got, want)
	}
}

func visitProductPage(ingress components.Ingress, timeout time.Duration, wantStatus int, t *testing.T) error {
	start := time.Now()
	for {
		response, err := ingress.Call("/productpage")
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
func promDump(prometheus components.Prometheus, metric string) string {
	if value, err := prometheus.WaitForQuiesce(fmt.Sprintf("%s{}", metric)); err == nil {
		return value.String()
	}
	return ""
}
