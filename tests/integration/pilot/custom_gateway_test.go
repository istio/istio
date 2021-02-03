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

package pilot

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/image"
	"istio.io/istio/pkg/test/shell"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/pilot/common"
	"istio.io/istio/tests/util"
)

const (
	customServiceGateway = "custom-ingressgateway"
	iopGWTemplate        = `
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
metadata:
  name: custom-ingressgateway-iop
  namespace: %s
spec:
  profile: empty
  components:
    ingressGateways:
    - name: custom-ingressgateway
      label:
        istio: custom-ingressgateway
      namespace: %s
      enabled: true
`
	gwTemplate = `
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: %s
spec:
  selector:
    istio: %s
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "*"
`
	vsTemplate = `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: %s
spec:
  hosts:
  - "*"
  gateways:
  - %s
  http:
  - match:
    - uri:
        prefix: /
    route:
    - destination:
        host: %s
`
)

var (
	customGWNamespace namespace.Instance
	cgwInst           istio.Instance
	ManifestPath      = filepath.Join(env.IstioSrc, "manifests")
)

// TestAccessAppViaCustomGateway tests access to an application using a custom gateway
func TestAccessAppViaCustomGateway(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.ingress.gateway").
		RequiresSingleCluster().
		Run(func(ctx framework.TestContext) {

			var err error

			// Setup namespace for custom gateway
			if customGWNamespace, err = namespace.New(ctx, namespace.Config{
				Prefix: "custom-gw",
				Inject: false,
			}); err != nil {
				t.Fatalf("failed to create custom gateway namespace: %v", err)
			}
			customGatewayNamespace := customGWNamespace.Name()

			// Create a custom gateway
			// Create the yaml
			var iopGWFile *os.File
			s, err := image.SettingsFromCommandLine()
			gatewayConfig := fmt.Sprintf(iopGWTemplate, customGatewayNamespace, customGatewayNamespace)
			if iopGWFile, err = ioutil.TempFile(ctx.WorkDir(), "modified_customgw.yaml"); err != nil {
				ctx.Fatal(err)
			}
			if _, err := iopGWFile.Write([]byte(gatewayConfig)); err != nil {
				t.Fatalf("failed to write iop cr file: %v", err)
			}
			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{Cluster: ctx.Environment().Clusters()[0]})
			createGateWayCmd := []string{
				"manifest", "generate",
				"--set", "hub=" + s.Hub,
				"--set", "tag=" + s.Tag,
				"--manifests=" + ManifestPath,
				"-f",
				iopGWFile.Name(),
			}
			gwCreateYaml, _ := istioCtl.InvokeOrFail(t, createGateWayCmd)

			// create the custom gateway
			ctx.Config().ApplyYAMLOrFail(ctx, customGatewayNamespace, gwCreateYaml)

			// Unable to find the ingress for the custom gateway install via the framework so retrieve URL and
			// use in the echo call.  TODO - Fix to use framework GetIngress methods (may need to fix method)
			gwIngressURL, err := getIngressURL(customGWNamespace.Name(), customServiceGateway)
			if err != nil {
				t.Fatalf("failed to get custom gateway URL: %v", err)
			}
			gwAddress := (strings.Split(gwIngressURL, ":"))[0]
			ingress := i.IngressFor(ctx.Clusters().Default())

			// Attempting to reach application A from custom gateway before creating
			// a gateway and virtual service should fail
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
			gwYaml := fmt.Sprintf(gwTemplate, common.PodASvc+"-gateway", customServiceGateway)
			ctx.Config().ApplyYAMLOrFail(ctx, apps.Namespace.Name(), gwYaml)
			vsYaml := fmt.Sprintf(vsTemplate, common.PodASvc, common.PodASvc+"-gateway", common.PodASvc)
			ctx.Config().ApplyYAMLOrFail(ctx, apps.Namespace.Name(), vsYaml)

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
