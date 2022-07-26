//go:build integ
// +build integ

//  Copyright Istio Authors
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

package sdsegress

import (
	"context"
	"net/http"
	"testing"
	"time"

	epb "istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/security/util"
)

const (
	// should be templated into YAML, and made interchangeable with other sites
	externalURL      = "http://bing.com"
	externalReqCount = 2
	egressName       = "istio-egressgateway"
	// paths to test configs
	istioMutualTLSGatewayConfig = "testdata/istio-mutual-gateway-bing.yaml"
	simpleTLSGatewayConfig      = "testdata/simple-tls-gateway-bing.yaml"
)

// TestSdsEgressGatewayIstioMutual brings up an SDS enabled cluster and will ensure that the ISTIO_MUTUAL
// TLS mode allows secure communication between the egress and workloads. This test brings up an ISTIO_MUTUAL enabled
// gateway, and then, an incorrectly configured simple TLS gateway. The test will ensure that requests are routed
// securely through the egress gateway in the first case, and fail in the second case.
func TestSdsEgressGatewayIstioMutual(t *testing.T) {
	// Turn it back on once issue is fixed.
	t.Skip("https://github.com/istio/istio/issues/17933")
	framework.NewTest(t).
		Features("security.egress.mtls.sds").
		Run(func(t framework.TestContext) {
			istioCfg := istio.DefaultConfigOrFail(t, t)

			namespace.ClaimOrFail(t, t, istioCfg.SystemNamespace)
			ns := namespace.NewOrFail(t, t, namespace.Config{
				Prefix: "sds-egress-gateway-workload",
				Inject: true,
			})
			applySetupConfig(t, ns)

			testCases := map[string]struct {
				configPath string
				code       int
			}{
				"ISTIO_MUTUAL TLS mode requests are routed through egress succeed": {
					configPath: istioMutualTLSGatewayConfig,
					code:       http.StatusOK,
				},
				"SIMPLE TLS mode requests are routed through gateway but fail with 503": {
					configPath: simpleTLSGatewayConfig,
					code:       http.StatusServiceUnavailable,
				},
			}

			for name, tc := range testCases {
				t.NewSubTest(name).
					Run(func(t framework.TestContext) {
						doIstioMutualTest(t, ns, tc.configPath, tc.code)
					})
			}
		})
}

func doIstioMutualTest(
	ctx framework.TestContext, ns namespace.Instance, configPath string, expectedCode int,
) {
	var client echo.Instance
	deployment.New(ctx).
		With(&client, util.EchoConfig("client", ns, false, nil)).
		BuildOrFail(ctx)
	ctx.ConfigIstio().File(ns.Name(), configPath).ApplyOrFail(ctx)

	// give the configuration a moment to kick in
	time.Sleep(time.Second * 20)
	pretestReqCount := getEgressRequestCountOrFail(ctx, ns, prom)

	for i := 0; i < externalReqCount; i++ {
		w := client.WorkloadsOrFail(ctx)[0]
		responses, err := w.ForwardEcho(context.TODO(), &epb.ForwardEchoRequest{
			Url:   externalURL,
			Count: 1,
		})

		if err := check.And(
			check.NoError(),
			check.Status(expectedCode)).Check(echo.CallResult{
			From:      client,
			Opts:      echo.CallOptions{},
			Responses: responses,
		}, err); err != nil {
			ctx.Fatal(err)
		}
	}

	// give prometheus some time to ingest the metrics
	posttestReqCount := getEgressRequestCountOrFail(ctx, ns, prom)
	newGatewayReqs := posttestReqCount - pretestReqCount
	if newGatewayReqs != externalReqCount {
		ctx.Errorf("expected %d requests routed through egress, got %d",
			externalReqCount, newGatewayReqs)
	}
}

// sets up the destination rule to route through egress, virtual service, and service entry
func applySetupConfig(ctx framework.TestContext, ns namespace.Instance) {
	ctx.Helper()

	configFiles := []string{
		"testdata/destination-rule-bing.yaml",
		"testdata/rule-route-sidecar-to-egress-bing.yaml",
		"testdata/service-entry-bing.yaml",
	}

	for _, c := range configFiles {
		if err := ctx.ConfigIstio().File(ns.Name(), c).Apply(); err != nil {
			ctx.Fatalf("failed to apply configuration file %s; err: %v", c, err)
		}
	}
}

func getEgressRequestCountOrFail(t framework.TestContext, ns namespace.Instance, prom prometheus.Instance) int {
	t.Helper()

	var res int
	retry.UntilSuccessOrFail(t, func() error {
		r, err := prom.QuerySum(t.Clusters().Default(), prometheus.Query{Metric: "istio_requests_total", Labels: map[string]string{
			"destination_app":           egressName,
			"source_workload_namespace": ns.Name(),
		}})
		res = int(r)
		return err
	})
	return res
}
