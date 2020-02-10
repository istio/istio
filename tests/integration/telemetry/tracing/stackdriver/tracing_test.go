// Copyright 2019 Istio Authors. All Rights Reserved.
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

package server

import (
	"testing"
	"time"

	"fmt"

	"os"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/bookinfo"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	util "istio.io/istio/tests/integration/mixer"
)

var (
	ist            istio.Instance
	bookinfoNsInst namespace.Instance
	galInst        galley.Instance
	ingInst        ingress.Instance
)

// TestProxyTracing exercises the trace generation features of Istio, based on the Envoy Trace driver for stackdriver.
// The test verifies that all expected spans (a client span and a server span for each service call in the sample bookinfo app)
// are generated and that they are all a part of the same distributed trace with correct hierarchy and name.
//
// More information on distributed tracing can be found here: https://istio.io/docs/tasks/telemetry/distributed-tracing/stackdriver/
func TestProxyTracing(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			t.Logf("os STACKDRIVER_TRACING_MAX_NUMBER_OF_ATTRIBUTES %v", os.Getenv("STACKDRIVER_TRACING_MAX_NUMBER_OF_ATTRIBUTES"))
			retry.UntilSuccessOrFail(t, func() error {
				util.SendTraffic(ingInst, t, "Sending traffic", "", "", 1)

				return fmt.Errorf("cannot get traces from stackdriver %v", bookinfoNsInst)
			}, retry.Delay(3*time.Second), retry.Timeout(80*time.Second))
		})
}

func TestMain(m *testing.M) {
	framework.NewSuite("stackdriver_tracing_test", m).
		RequireEnvironment(environment.Kube).
		Label(label.CustomSetup).
		SetupOnEnv(environment.Kube, istio.Setup(&ist, setupConfig)).
		Setup(testSetup).
		Run()
}

func testSetup(ctx resource.Context) (err error) {
	galInst, err = galley.New(ctx, galley.Config{})
	if err != nil {
		return
	}
	bookinfoNsInst, err = namespace.New(ctx, namespace.Config{
		Prefix: "istio-bookinfo",
		Inject: true,
	})
	if err != nil {
		return
	}
	if _, err = bookinfo.Deploy(ctx, bookinfo.Config{Namespace: bookinfoNsInst, Cfg: bookinfo.BookInfo}); err != nil {
		return
	}
	ingInst, err = ingress.New(ctx, ingress.Config{Istio: ist})
	if err != nil {
		return
	}
	// deploy bookinfo app, also deploy a virtualservice which forces all traffic to go to review v1,
	// which does not get ratings, so that exactly six spans will be included in the wanted trace.
	bookingfoGatewayFile, err := bookinfo.NetworkingBookinfoGateway.LoadGatewayFileWithNamespace(bookinfoNsInst.Name())
	if err != nil {
		return
	}
	destinationRule, err := bookinfo.GetDestinationRuleConfigFile(ctx)
	if err != nil {
		return
	}
	destinationRuleFile, err := destinationRule.LoadWithNamespace(bookinfoNsInst.Name())
	if err != nil {
		return
	}
	virtualServiceFile, err := bookinfo.NetworkingVirtualServiceAllV1.LoadWithNamespace(bookinfoNsInst.Name())
	if err != nil {
		return
	}
	err = galInst.ApplyConfig(
		bookinfoNsInst,
		bookingfoGatewayFile,
		destinationRuleFile,
		virtualServiceFile,
	)
	if err != nil {
		return
	}
	return nil
}

func setupConfig(cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.Values["global.proxy.tracer"] = "stackdriver"
	//os.Setenv("STACKDRIVER_TRACING_MAX_NUMBER_OF_ATTRIBUTES","500")
	//cfg.Values["global.tracer.stackdriver.debug"] = "true"
	//cfg.Values["global.tracer.stackdriver.maxNumberOfAttributes"] = "500"
	cfg.Values["global.enableTracing"] = "true"
	cfg.Values["global.disablePolicyChecks"] = "true"
	cfg.Values["pilot.traceSampling"] = "100.0"

	// TODO not needed once https://github.com/istio/istio/issues/20137 is in
	cfg.ControlPlaneValues = `
addonComponents:
values:
  global:
    enableTracing: true
    disablePolicyChecks: true
    proxy:
      tracer: stackdriver
    tracer:
      stackdriver:
        debug: true
        maxNumberOfAttributes: "500"
  pilot:
    traceSampling: "100.0"
`
}
