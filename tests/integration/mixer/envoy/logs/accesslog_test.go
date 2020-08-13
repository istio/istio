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

package logs

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/bookinfo"
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
	ingInst           *ingress.Instance
)

func TestIstioAccessLogEnvoy(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			// enabling ext-authz and grpc access log service
			errr := ctx.Config().ApplyYAMLDir("istio-system", "../testdata")
			if errr != nil {
				t.Fatalf("cannot apply access log test config")
			}
			defer ctx.Config().DeleteYAMLDir("istio-system", "../testdata")

			_, ing := setupComponentsOrFail(t)

			ns := namespace.ClaimOrFail(t, ctx, ist.Settings().SystemNamespace)
			ctx.Config().ApplyYAMLOrFail(
				t,
				ns.Name(),
				bookinfo.TelemetryLogEntry.LoadOrFail(t))
			defer ctx.Config().DeleteYAMLOrFail(
				t,
				ns.Name(),
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
		NewSuite(m).
		RequireSingleCluster().
		Label(label.CustomSetup).
		Setup(istio.Setup(&ist, func(cfg *istio.Config) {
			cfg.ControlPlaneValues = `
values:
  meshConfig:
    disableMixerHttpReports: true
    disablePolicyChecks: true
  telemetry:
    v1:
      enabled: false
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
	ing, err := ingress.New(ctx, ingress.Config{Istio: ist})
	if err != nil {
		return err
	}
	ingInst = &ing

	bookinfoGateWayConfig, err := bookinfo.NetworkingBookinfoGateway.LoadGatewayFileWithNamespace(bookinfoNs.Name())
	if err != nil {
		return err
	}
	err = ctx.Config().ApplyYAML(bookinfoNs.Name(), bookinfoGateWayConfig)
	if err != nil {
		return err
	}

	return nil
}

func setupComponentsOrFail(t *testing.T) (bookinfoNs namespace.Instance, ing ingress.Instance) {
	if bookinfoNamespace == nil {
		t.Fatalf("bookinfo namespace not allocated in setup")
	}
	bookinfoNs = *bookinfoNamespace
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
	for _, expected := range []string{"details", "reviews", "productpage"} {
		if !strings.Contains(content, fmt.Sprintf("\"destination\":\"%s\"", expected)) {
			return fmt.Errorf("accesslog doesn't contain %s destination. Log %v", expected, content)
		}
	}
	if !strings.Contains(content, "\"level\":\"warn\"") {
		return fmt.Errorf("accesslog does'nt contain warning level. Log %v", content)
	}
	if !strings.Contains(content, "\"responseCode\":") {
		return fmt.Errorf("accesslog doesn't contain response code. Log %v", content)
	}
	if !strings.Contains(content, "\"source\":\"productpage\"") {
		return fmt.Errorf("accesslog doesn't contain productpage source. Log %v", content)
	}
	if !strings.Contains(content, "\"source\":\"istio-ingressgateway\"") {
		return fmt.Errorf("accesslog doesn't contain istio-ingressgateway source. Log %v", content)
	}
	return nil
}
