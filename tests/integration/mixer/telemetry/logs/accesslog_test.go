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

package logs

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/bookinfo"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	util "istio.io/istio/tests/integration/mixer"
)

var (
	ist               istio.Instance
	bookinfoNamespace *namespace.Instance
	galInst           *galley.Instance
	ingInst           *ingress.Instance
)

func TestIstioAccessLog(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			_, g, ing := setupComponentsOrFail(t)

			ns := namespace.ClaimOrFail(t, ctx, ist.Settings().SystemNamespace)
			g.ApplyConfigOrFail(
				t,
				ns,
				bookinfo.TelemetryLogEntry.LoadOrFail(t))
			defer g.DeleteConfigOrFail(
				t,
				ns,
				bookinfo.TelemetryLogEntry.LoadOrFail(t))

			util.AllowRuleSync(t)

			err := util.VisitProductPage(ing, 30*time.Second, 200, t)
			if err != nil {
				t.Fatalf("unable to retrieve 200 from product page: %v", err)
			}

			util.GetAndValidateAccessLog(ns, t, "istio-mixer-type=telemetry", "mixer",
				validateLog)
		})
}

func TestMain(m *testing.M) {
	framework.
		NewSuite("mixer_telemetry_logs", m).
		RequireEnvironment(environment.Kube).
		Label(label.CustomSetup).
		SetupOnEnv(environment.Kube, istio.Setup(&ist, func(cfg *istio.Config) {
			cfg.ControlPlaneValues = `
values:
  global:
    disablePolicyChecks: false
  telemetry:
    v1:
      enabled: true
    v2:
      enabled: false
components:
  policy:
    enabled: true
  telemetry:
    enabled: true`
		})).
		Setup(testsetup).
		Run()
}

func testsetup(ctx resource.Context) error {
	bookinfoNs, err := namespace.New(ctx, namespace.Config{
		Prefix: "istio-bookinfo",
		Inject: true,
	})
	if err != nil {
		return err
	}
	bookinfoNamespace = &bookinfoNs
	if _, err := bookinfo.Deploy(ctx, bookinfo.Config{Namespace: bookinfoNs, Cfg: bookinfo.BookInfo}); err != nil {
		return err
	}
	g, err := galley.New(ctx, galley.Config{})
	if err != nil {
		return err
	}
	galInst = &g
	ing, err := ingress.New(ctx, ingress.Config{Istio: ist})
	if err != nil {
		return err
	}
	ingInst = &ing

	bookinfoGateWayConfig, err := bookinfo.NetworkingBookinfoGateway.LoadGatewayFileWithNamespace(bookinfoNs.Name())
	if err != nil {
		return err
	}
	err = g.ApplyConfig(bookinfoNs, bookinfoGateWayConfig)
	if err != nil {
		return err
	}
	return nil
}

func setupComponentsOrFail(t *testing.T) (bookinfoNs namespace.Instance, g galley.Instance,
	ing ingress.Instance) {
	if bookinfoNamespace == nil {
		t.Fatalf("bookinfo namespace not allocated in setup")
	}
	bookinfoNs = *bookinfoNamespace
	if galInst == nil {
		t.Fatalf("galley not setup")
	}
	g = *galInst
	if ingInst == nil {
		t.Fatalf("ingress not setup")
	}
	ing = *ingInst
	return
}

func validateLog(content string) error {
	if !strings.Contains(content, "newlog") {
		return fmt.Errorf("accesslog doesn't contain newlog instance. Log %v", content)
	}
	if !strings.Contains(content, "\"level\":\"warn\"") {
		return fmt.Errorf("accesslog does'nt contain warning level. Log %v", content)
	}
	for _, expected := range []string{"details", "reviews", "productpage"} {
		if !strings.Contains(content, fmt.Sprintf("\"destination\":\"%s\"", expected)) {
			return fmt.Errorf("accesslog doesn't contain %s destination. Log %v", expected, content)
		}
	}
	if !strings.Contains(content, "\"responseCode\":") {
		return fmt.Errorf("accesslog doesn't contain response code. Log %v", content)
	}
	if !strings.Contains(content, "\"source\":\"productpage\"") || !strings.Contains(content,
		"\"source\":\"istio-ingressgateway\"") {
		return fmt.Errorf(
			"accesslog doesn't contain either productpage or istio-ingressgateway source. Log %v", content)
	}

	return nil
}
