// +build integ
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

package gateway

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/shell"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/util"
)

// TestAccessAppViaCustomGateway tests access to an application using a custom gateway
func TestAccessAppViaCustomGateway(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.ingress.gateway").
		Run(func(ctx framework.TestContext) {
			// Unable to find the ingress for the custom gateway install via the framework so retrieve URL and
			// use in the echo call.
			gwIngressURL, err := getIngressURL(customGWNamespace.Name(), customServiceGateway)
			if err != nil {
				t.Fatalf("failed to get custom gateway URL: %v", err)
			}
			gwAddress := (strings.Split(gwIngressURL, ":"))[0]
			ingress := cgwInst.IngressFor(ctx.Clusters().Default())

			// Attempting to reach application A before creating a gateway an service should fail
			ctx.NewSubTest("no gateway or service").Run(func(ctx framework.TestContext) {
				ingress.CallEchoWithRetryOrFail(ctx, echo.CallOptions{
					Port: &echo.Port{
						Protocol: protocol.HTTP,
					},
					Address:   gwAddress,
					Path:      "/",
					Validator: echo.ExpectError(),
				}, retry.Timeout(time.Minute))
			})

			// Apply a gateway to the custom-gateway and a virtual service for appplication A in its namespace.
			// Application A will then be exposed externally on the custom-gateway
			gwYaml := fmt.Sprintf(gwTemplate, aSvc+"-gateway", customServiceGateway)
			ctx.Config().ApplyYAMLOrFail(ctx, apps.appANamespace.Name(), gwYaml)
			vsYaml := fmt.Sprintf(vsTemplate, aSvc, aSvc+"-gateway", aSvc)
			ctx.Config().ApplyYAMLOrFail(ctx, apps.appANamespace.Name(), vsYaml)

			// Verify that one can access application A on the custom-gateway
			ctx.NewSubTest("gateway and service applied").Run(func(ctx framework.TestContext) {
				ingress.CallEchoWithRetryOrFail(ctx, echo.CallOptions{
					Port: &echo.Port{
						Protocol: protocol.HTTP,
					},
					Address:   gwAddress,
					Path:      "/",
					Validator: echo.ExpectOK(),
				}, retry.Timeout(time.Minute))
			})
		})
}

func getIngressURL(ns, service string) (string, error) {
	retry := util.Retrier{
		BaseDelay: 10 * time.Second,
		Retries:   3,
		MaxDelay:  30 * time.Second,
	}
	var url string

	retryFn := func(_ context.Context, i int) error {
		hostCmd := fmt.Sprintf(
			"kubectl get service %s -n %s -o jsonpath='{.status.loadBalancer.ingress[0].ip}'",
			service, ns)
		portCmd := fmt.Sprintf(
			"kubectl get service %s -n %s -o jsonpath='{.spec.ports[?(@.name==\"http2\")].port}'",
			service, ns)
		host, err := shell.Execute(false, hostCmd)
		if err != nil {
			return fmt.Errorf("error executing the cmd (%v): %v", hostCmd, err)
		}
		port, err := shell.Execute(false, portCmd)
		if err != nil {
			return fmt.Errorf("error executing the cmd (%v): %v", portCmd, err)
		}
		url = strings.Trim(host, "'") + ":" + strings.Trim(port, "'")
		return nil
	}

	if _, err := retry.Retry(context.Background(), retryFn); err != nil {
		return url, fmt.Errorf("getIngressURL retry failed with err: %v", err)
	}
	return url, nil
}
