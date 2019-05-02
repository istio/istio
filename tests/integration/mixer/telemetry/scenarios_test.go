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

package telemetry

import (
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/bookinfo"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/mixer"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/util/tmpl"
	util "istio.io/istio/tests/integration/mixer"
)

var (
	ist istio.Instance
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

	bookinfoNs, err := namespace.New(ctx, "istio-bookinfo", true)
	if err != nil {
		t.Fatalf("Could not create istio-bookinfo Namespace; err:%v", err)
	}
	d := bookinfo.DeployOrFail(t, ctx, bookinfo.Config{Namespace: bookinfoNs, Cfg: bookinfo.BookInfo})

	g.ApplyConfigOrFail(
		t,
		d.Namespace(),
		bookinfo.NetworkingBookinfoGateway.LoadGatewayFileWithNamespaceOrFail(t, bookinfoNs.Name()))
	g.ApplyConfigOrFail(
		t,
		d.Namespace(),
		bookinfo.GetDestinationRuleConfigFile(t, ctx).LoadWithNamespaceOrFail(t, bookinfoNs.Name()),
		bookinfo.NetworkingVirtualServiceAllV1.LoadWithNamespaceOrFail(t, bookinfoNs.Name()),
	)

	prom := prometheus.NewOrFail(t, ctx)
	ing := ingress.NewOrFail(t, ctx, ingress.Config{Istio: ist})

	// Warm up
	err = util.VisitProductPage(ing, 30*time.Second, 200, t)
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

	err = util.VisitProductPage(ing, 30*time.Second, 200, t)
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
		t.Logf("prometheus values for %s:\n%s", metricName, util.PromDump(prom, metricName))
		t.Fatal(err)
	}

	f, err := prom.Sum(final, nil)
	if err != nil {
		t.Logf("prometheus values for %s:\n%s", metricName, util.PromDump(prom, metricName))
		t.Fatal(err)
	}

	if (f - i) < float64(1) {
		t.Errorf("Bad metric value: got %f, want at least 1", f-i)
	}
}

// Port of TestTcpMetric
func TestTcpMetric(t *testing.T) {
	framework.Run(t, func(ctx framework.TestContext) {
		ctx.RequireOrSkip(t, environment.Kube)

		bookinfoNs, err := namespace.New(ctx, "istio-bookinfo", true)
		if err != nil {
			t.Fatalf("Could not create istio-bookinfo Namespace; err:%v", err)
		}
		d := bookinfo.DeployOrFail(t, ctx, bookinfo.Config{Namespace: bookinfoNs, Cfg: bookinfo.BookInfo})
		_ = bookinfo.DeployOrFail(t, ctx, bookinfo.Config{Namespace: bookinfoNs, Cfg: bookinfo.BookinfoRatingsv2})
		_ = bookinfo.DeployOrFail(t, ctx, bookinfo.Config{Namespace: bookinfoNs, Cfg: bookinfo.BookinfoDb})

		g := galley.NewOrFail(t, ctx, galley.Config{})
		_ = mixer.NewOrFail(t, ctx, mixer.Config{Galley: g})

		g.ApplyConfigOrFail(
			t,
			d.Namespace(),
			bookinfo.NetworkingBookinfoGateway.LoadGatewayFileWithNamespaceOrFail(t, bookinfoNs.Name()))
		g.ApplyConfigOrFail(
			t,
			d.Namespace(),
			bookinfo.GetDestinationRuleConfigFile(t, ctx).LoadWithNamespaceOrFail(t, bookinfoNs.Name()),
			bookinfo.NetworkingTCPDbRule.LoadWithNamespaceOrFail(t, bookinfoNs.Name()),
		)

		util.AllowRuleSync(t)

		prom := prometheus.NewOrFail(t, ctx)
		ing := ingress.NewOrFail(t, ctx, ingress.Config{Istio: ist})

		err = util.VisitProductPage(ing, 30*time.Second, 200, t)
		if err != nil {
			t.Fatalf("unable to retrieve 200 from product page: %v", err)
		}

		query := fmt.Sprintf("sum(istio_tcp_sent_bytes_total{destination_app=\"%s\"})", "mongodb")
		util.ValidateMetric(t, prom, query, "istio_tcp_sent_bytes_total")

		query = fmt.Sprintf("sum(istio_tcp_received_bytes_total{destination_app=\"%s\"})", "mongodb")
		util.ValidateMetric(t, prom, query, "istio_tcp_received_bytes_total")

		query = fmt.Sprintf("sum(istio_tcp_connections_opened_total{destination_app=\"%s\"})", "mongodb")
		util.ValidateMetric(t, prom, query, "istio_tcp_connections_opened_total")

		query = fmt.Sprintf("sum(istio_tcp_connections_closed_total{destination_app=\"%s\"})", "mongodb")
		util.ValidateMetric(t, prom, query, "istio_tcp_connections_closed_total")
	})
}

func TestMain(m *testing.M) {
	framework.
		NewSuite("mixer_policy_ratelimit", m).
		SetupOnEnv(environment.Kube, istio.Setup(&ist, nil)).
		Run()
}
