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
	"fmt"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/bookinfo"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/environment"
	//"istio.io/istio/pkg/test/util/tmpl"
	//util "istio.io/istio/tests/integration/mixer"
)

//
//const (
//	envoyconfig          = "testdata/envoyconfig.yaml"
//)

var (
	ist        istio.Instance
	bookinfoNs namespace.Instance
	ing        ingress.Instance
	prom       prometheus.Instance
)

func TestEx(t *testing.T) {
	fmt.Println("Success!")
}

func TestMain(m *testing.M) {
	framework.
		NewSuite("mixer_telemetry_metrics", m).
		RequireEnvironment(environment.Kube).
		RequireSingleCluster().
		Label(label.CustomSetup).
		SetupOnEnv(environment.Kube, istio.Setup(&ist, func(cfg *istio.Config) {
			cfg.ControlPlaneValues = `
values:
  meshConfig:
    disablePolicyChecks: false
  prometheus:
    enabled: true
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

func testsetup(ctx resource.Context) (err error) {
	bookinfoNs, err = namespace.New(ctx, namespace.Config{
		Prefix: "istio-bookinfo",
		Inject: true,
	})
	if err != nil {
		return
	}
	if _, err := bookinfo.Deploy(ctx, bookinfo.Config{Namespace: bookinfoNs, Cfg: bookinfo.BookInfo}); err != nil {
		return err
	}
	ing, err = ingress.New(ctx, ingress.Config{Istio: ist})
	if err != nil {
		return err
	}
	prom, err = prometheus.New(ctx, prometheus.Config{})
	if err != nil {
		return err
	}
	yamlText, err := bookinfo.NetworkingBookinfoGateway.LoadGatewayFileWithNamespace(bookinfoNs.Name())
	if err != nil {
		return err
	}
	err = ctx.ApplyConfig(bookinfoNs.Name(), yamlText)
	if err != nil {
		return err
	}
	err = ctx.ApplyConfigDir(bookinfoNs.Name(), "testdata")
	if err != nil {
		return err
	}

	return nil
}
