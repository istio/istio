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

package securegatewaysds

import (
	"istio.io/istio/pkg/test/istio.io/examples"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	inst istio.Instance
	g    galley.Instance
	p    pilot.Instance
)

func TestMain(m *testing.M) {
	// Integration test for the ingress SDS Gateway flow.
	framework.
		NewSuite("sds_ingress_gateway", m).
		Label(label.CustomSetup).
		SetupOnEnv(environment.Kube, istio.Setup(&inst, setupConfig)).
		Setup(func(ctx resource.Context) (err error) {
			if g, err = galley.New(ctx, galley.Config{}); err != nil {
				return err
			}
			if p, err = pilot.New(ctx, pilot.Config{
				Galley: g,
			}); err != nil {
				return err
			}
			return nil
		}).
		Run()

}

func setupConfig(cfg *istio.Config) {
	if cfg == nil {
		return
	}
	// Enable SDS key/certificate provisioning for ingress gateway.
	cfg.Values["gateways.istio-ingressgateway.sds.enabled"] = "true"
	// TODO(https://github.com/istio/istio/issues/14084) remove this
	cfg.Values["pilot.env.PILOT_ENABLE_FALLTHROUGH_ROUTE"] = "0"
}

// https://preliminary.istio.io/docs/tasks/traffic-management/ingress/secure-ingress-sds/
func TestMTLS(t *testing.T) {
	ex := examples.New(t, "secure-gateway-sds")

	////The following line is just an example of how to use addfile.
	//ex.AddFile("istio-system", "samples/sleep/sleep.yaml")
	//ex.AddScript("", "create-ns-foo-bar.sh", examples.TextOutput)
	//
	//ex.AddScript("", "curl-foo-bar-legacy.sh", examples.TextOutput)
	//
	////verify that all requests returns 200 ok
	//
	//ex.AddScript("", "verify-initial-policies.sh", examples.TextOutput)
	//
	////verify that only the following exist:
	//// NAMESPACE      NAME                          AGE
	//// istio-system   grafana-ports-mtls-disabled   3m
	//
	//ex.AddScript("", "verify-initial-destinationrules.sh", examples.TextOutput)
	//
	////verify that only the following exists:
	////NAMESPACE      NAME              AGE
	////istio-system   istio-policy      25m
	////istio-system   istio-telemetry   25m
	//
	//ex.AddScript("", "configure-mtls-destinationrule.sh", examples.TextOutput)
	//ex.AddScript("", "curl-foo-bar-legacy.sh", examples.TextOutput)
	//
	////verify 200ok from all requests
	//
	//ex.AddScript("", "httpbin-foo-mtls-only.sh", examples.TextOutput)
	//ex.AddScript("", "curl-foo-bar-legacy.sh", examples.TextOutput)
	//
	////verify 200 from first 2 requests and 503 from 3rd request
	//
	//ex.AddScript("", "cleanup.sh", examples.TextOutput)

	ex.AddScript("", "mtls-go-example.sh", examples.TextOutput)
	ex.Run()
}