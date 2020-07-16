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
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/test/echo/common/response"
	epb "istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/util/file"
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
		Run(func(ctx framework.TestContext) {
			istioCfg := istio.DefaultConfigOrFail(t, ctx)

			namespace.ClaimOrFail(t, ctx, istioCfg.SystemNamespace)
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "sds-egress-gateway-workload",
				Inject: true,
			})
			applySetupConfig(ctx, ns)

			testCases := map[string]struct {
				configPath string
				response   string
			}{
				"ISTIO_MUTUAL TLS mode requests are routed through egress succeed": {
					configPath: istioMutualTLSGatewayConfig,
					response:   response.StatusCodeOK,
				},
				"SIMPLE TLS mode requests are routed through gateway but fail with 503": {
					configPath: simpleTLSGatewayConfig,
					response:   response.StatusCodeUnavailable,
				},
			}

			for name, tc := range testCases {
				ctx.NewSubTest(name).
					Run(func(ctx framework.TestContext) {
						doIstioMutualTest(ctx, ns, tc.configPath, tc.response)
					})
			}
		})
}

func doIstioMutualTest(
	ctx framework.TestContext, ns namespace.Instance, configPath, expectedResp string) {
	var client echo.Instance
	echoboot.NewBuilderOrFail(ctx, ctx).
		With(&client, util.EchoConfig("client", ns, false, nil)).
		BuildOrFail(ctx)
	ctx.Config().ApplyYAMLOrFail(ctx, ns.Name(), file.AsStringOrFail(ctx, configPath))
	defer ctx.Config().DeleteYAMLOrFail(ctx, ns.Name(), file.AsStringOrFail(ctx, configPath))

	// give the configuration a moment to kick in
	time.Sleep(time.Second * 20)
	pretestReqCount := getEgressRequestCountOrFail(ctx, ns, prom)

	for i := 0; i < externalReqCount; i++ {
		w := client.WorkloadsOrFail(ctx)[0]
		responses, err := w.ForwardEcho(context.TODO(), &epb.ForwardEchoRequest{
			Url:   externalURL,
			Count: 1,
		})
		if err != nil {
			ctx.Fatalf("failed to make request from echo instance to %s: %v", externalURL, err)
		}
		if len(responses) < 1 {
			ctx.Fatalf("received no responses from request to %s", externalURL)
		}
		resp := responses[0]

		if expectedResp != resp.Code {
			ctx.Errorf("expected status %s but got %s", expectedResp, resp.Code)
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
		if err := ctx.Config().ApplyYAML(ns.Name(), file.AsStringOrFail(ctx, c)); err != nil {
			ctx.Fatalf("failed to apply configuration file %s; err: %v", c, err)
		}
	}
}

func getMetric(ctx framework.TestContext, prometheus prometheus.Instance, query string) (float64, error) {
	ctx.Helper()

	value, err := prometheus.WaitForQuiesce(query)
	if err != nil {
		return 0, fmt.Errorf("failed to retrieve metric from prom with err: %v", err)
	}

	metric, err := prometheus.Sum(value, nil)
	if err != nil {
		ctx.Logf("value: %s", value.String())
		return 0, fmt.Errorf("could not find metric value: %v", err)
	}

	return metric, nil
}

func getEgressRequestCountOrFail(ctx framework.TestContext, ns namespace.Instance, prom prometheus.Instance) int {
	query := fmt.Sprintf("istio_requests_total{destination_app=\"%s\",source_workload_namespace=\"%s\"}",
		egressName, ns.Name())
	ctx.Helper()

	reqCount, err := getMetric(ctx, prom, query)
	if err != nil {
		// assume that if the request failed, it was because there was no metric ingested
		// if this is not the case, the test will fail down the road regardless
		// checking for error based on string match could lead to future failure
		reqCount = 0
	}

	return int(reqCount)
}
