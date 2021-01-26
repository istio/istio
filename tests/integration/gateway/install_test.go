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
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/image"
	"istio.io/istio/pkg/test/framework/resource"
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

var (
	// ManifestPath is path of local manifests which istioctl operator init refers to.
	ManifestPath = filepath.Join(env.IstioSrc, "manifests")
)

// TestInstallCustomGateway tests installing a custom gateway
func TestInstallCustomGateway(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.ingress.gateway").
		Run(func(ctx framework.TestContext) {
			workDir, err := ctx.CreateTmpDirectory("gateway-installer-test")
			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})
			cs := ctx.Environment().Clusters().Default()
			t.Cleanup(func() {
				cleanupIstioResources(t, cs, istioCtl)
			})

			ns, err := namespace.New(ctx, namespace.Config{
				Prefix: "custom-gateways",
				Inject: false,
			})
			if err != nil {
				t.Fatal(err)
			}

			nsGWApp, err := namespace.New(ctx, namespace.Config{
				Prefix: "cgwapp",
				Inject: true,
			})
			if err != nil {
				t.Fatal(err)
			}

			// nsIstioApp, err := namespace.New(ctx, namespace.Config{
			// 	Prefix: "istioapp",
			// 	Inject: false,
			// })
			// if err != nil {
			// 	t.Fatal(err)
			// }

			customGatewayNamespace := ns.Name()

			s, err := image.SettingsFromCommandLine()

			config := `
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
      enabled: true`

			gatewayConfig := fmt.Sprintf(config, customGatewayNamespace, customGatewayNamespace)

			iopGWFile := filepath.Join(workDir, "iop_gw.yaml")
			if err := ioutil.WriteFile(iopGWFile, []byte(gatewayConfig), os.ModePerm); err != nil {
				t.Fatalf("failed to write iop cr file: %v", err)
			}

			createGateWayCmd := []string{
				"manifest", "generate",
				"--set", "hub=" + s.Hub,
				"--set", "tag=" + s.Tag,
				"--manifests=" + ManifestPath,
				"-f",
				iopGWFile,
			}

			// install gateway
			gwCreateYaml, _ := istioCtl.InvokeOrFail(t, createGateWayCmd)

			// scopes.Framework.Infof("=== installing with IOP: ===\n%s\n", gatewayConfig)
			ctx.Config().ApplyYAMLOrFail(ctx, customGatewayNamespace, gwCreateYaml)

			var customGatewayInstance echo.Instance
			builder := echoboot.NewBuilder(ctx)
			builder.With(&customGatewayInstance, echo.Config{
				Service:   "svc-cgw",
				Namespace: nsGWApp,
				Ports: []echo.Port{
					{
						Name:         "http",
						Protocol:     protocol.HTTP,
						InstancePort: 8080,
					},
				},
			})
			builder.BuildOrFail(t)

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
			gwIngressURL, err := getIngressURL(customGatewayNamespace, "custom-ingressgateway")
			if err != nil {
				t.Fatalf("failed to get custom gateway URL: %v", err)
			}

			scopes.Framework.Infof("Custom Gateway ingress URL: " +
				gwIngressURL)

			gwYaml := fmt.Sprintf(gwTemplate, customGatewayNamespace, "custom-ingressgateway", "*") // TODO Change to numbered name
			ctx.Config().ApplyYAMLOrFail(ctx, nsGWApp.Name(), gwYaml)
			vsYaml := fmt.Sprintf(vsTemplate, nsGWApp.Name(), "*", customGatewayNamespace, "svc-cgw")
			ctx.Config().ApplyYAMLOrFail(ctx, nsGWApp.Name(), vsYaml)

			scopes.Framework.Infof("sleeping for 5 minutes, check namespace: %s", customGatewayNamespace)
			time.Sleep(time.Minute * 5)
			scopes.Framework.Infof("done sleeping")
		})
}

func cleanupIstioResources(t *testing.T, cs resource.Cluster, istioCtl istioctl.Instance) {
	scopes.Framework.Infof("cleaning up resources")

	// clean up gateway namespace
	// if err := cs.CoreV1().Namespaces().Delete(context.TODO(), customGatewayNamespace,
	// 	kube2.DeleteOptionsForeground()); err != nil {
	// 	t.Logf("failed to delete operator namespace: %v", err)
	// }
	// if err := kube2.WaitForNamespaceDeletion(cs, customGatewayNamespace, retry.Timeout(nsDeletionTimeout)); err != nil {
	// 	t.Logf("failed wating for operator namespace to be deleted: %v", err)
	// }
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
