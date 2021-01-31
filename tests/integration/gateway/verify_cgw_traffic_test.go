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

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/shell"
	"istio.io/istio/tests/util"
)

const (
	vsTemplate = `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: %s
spec:
  hosts:
  - "%s"
  gateways:
  - %s
  http:
  - match:
    - uri:
        prefix: /
    route:
    - destination:
        host: %s
        port:
          number: 9080
`
	gwTemplate = `
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: %s
spec:
  selector:
    istio: %s # use istio default ingress gateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "%s"
`
)

// TestAccessAppViaCustomGateway tests installing a custom gateway
func TestAccessAppViaCustomGateway(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.ingress.gateway").
		Run(func(ctx framework.TestContext) {

			// var customGatewayInstance echo.Instance
			// builder := echoboot.NewBuilder(ctx)
			// builder.With(&customGatewayInstance, echo.Config{
			// 	Service:   "svc-cgw",
			// 	Namespace: CustomGWNamespace,
			// 	Ports: []echo.Port{
			// 		{
			// 			Name:         "http",
			// 			Protocol:     protocol.HTTP,
			// 			InstancePort: 8080,
			// 		},
			// 	},
			// })
			// builder.BuildOrFail(t)

			// var istioGatewayInstance echo.Instance
			// builder.With(&istioGatewayInstance, echo.Config{
			// 	Service:   "svc-igw",
			// 	Namespace: nsIstioApp,
			// 	Ports: []echo.Port{
			// 		{
			// 			Name:         "http",
			// 			Protocol:     protocol.HTTP,
			// 			InstancePort: 8080,
			// 		},
			// 	},
			// })
			//builder.BuildOrFail(t)

			// sendSimpleTrafficOrFail(t, revisionedInstance)
			// gwIngressURL, err := getIngressURL(customGatewayNamespace, "custom-ingressgateway")
			// if err != nil {
			// 	t.Fatalf("failed to get custom gateway URL: %v", err)
			// }

			// scopes.Framework.Infof("Custom Gateway ingress URL: " +
			// 	gwIngressURL)

			// gwYaml := fmt.Sprintf(gwTemplate, customGatewayNamespace, "custom-ingressgateway", "*") // TODO Change to numbered name
			// ctx.Config().ApplyYAMLOrFail(ctx, nsGWApp.Name(), gwYaml)
			// vsYaml := fmt.Sprintf(vsTemplate, nsGWApp.Name(), "*", customGatewayNamespace, "svc-cgw")
			// ctx.Config().ApplyYAMLOrFail(ctx, nsGWApp.Name(), vsYaml)

			scopes.Framework.Infof("sleeping for 2 minutes, check namespace: %s", CustomGWNamespace.Name())
			time.Sleep(time.Minute * 2)
			scopes.Framework.Infof("done sleeping")
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
