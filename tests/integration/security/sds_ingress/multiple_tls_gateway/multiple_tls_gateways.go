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

package multiple_tls_gateway

import (
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/bookinfo"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/mixer"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/tmpl"
	"testing"
	"time"
)

var (
	ist istio.Instance
)

func testMultiTlsGateways(t *testing.T, ctx framework.TestContext) { // nolint:interfacer
	t.Helper()

	g := galley.NewOrFail(t, ctx, galley.Config{})
	_ = mixer.NewOrFail(t, ctx, mixer.Config{Galley: g})

	bookinfoNs, err := namespace.New(ctx, "istio-bookinfo", true)
	if err != nil {
		t.Fatalf("Could not create istio-bookinfo Namespace; err:%v", err)
	}
	d := bookinfo.DeployOrFail(t, ctx, bookinfo.Config{Namespace: bookinfoNs, Cfg: bookinfo.BookInfo})

	// Apply the policy to the system namespace.
	bGatewayDeployment := file.AsStringOrFail(t, "testdata/bookinfo-multiple-gateways.yaml")
	g.ApplyConfigOrFail(t, bookinfoNs, bGatewayDeployment)
	defer g.DeleteConfigOrFail(t, bookinfoNs, bGatewayDeployment)

	bVirtualServiceDeployment := file.AsStringOrFail(t, "testdata/bookinfo-multiple-virtualservices.yaml")
	g.ApplyConfigOrFail(t, bookinfoNs, bVirtualServiceDeployment)
	defer g.DeleteConfigOrFail(t, bookinfoNs, bVirtualServiceDeployment)



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

func TestTlsGateways(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
			Run(func(ctx framework.TestContext) {
			testMultiTlsGateways(t, ctx)
			})
}