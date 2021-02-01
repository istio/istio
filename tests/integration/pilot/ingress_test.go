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
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/util/retry"
	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGateway(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			if !supportsCRDv1(ctx) {
				t.Skip("Not supported; requires CRDv1 support.")
			}
			ctx.Config().ApplyYAMLOrFail(ctx, apps.Namespace.Name(), `
apiVersion: networking.x-k8s.io/v1alpha1
kind: GatewayClass
metadata:
  name: istio
spec:
  controller: istio.io/gateway-controller
---
apiVersion: networking.x-k8s.io/v1alpha1
kind: Gateway
metadata:
  name: gateway
spec:
  gatewayClassName: istio
  listeners:
  - hostname: "*.domain.example"
    port: 80
    protocol: HTTP
    routes:
      kind: HTTPRoute
  - port: 31400
    protocol: TCP
    routes:
      kind: TCPRoute
---
apiVersion: networking.x-k8s.io/v1alpha1
kind: HTTPRoute
metadata:
  name: http
spec:
 hostnames: ["my.domain.example"]
 rules:
 - matches:
   - path:
       type: Prefix
       value: /get
   forwardTo:
     - serviceName: b
       port: 80
---
apiVersion: networking.x-k8s.io/v1alpha1
kind: TCPRoute
metadata:
  name: tcp
spec:
  rules:
  - forwardTo:
     - serviceName: b
       port: 80
`)

			ctx.NewSubTest("http").Run(func(ctx framework.TestContext) {
				_ = apps.Ingress.CallEchoWithRetryOrFail(ctx, echo.CallOptions{
					Port: &echo.Port{
						Protocol: protocol.HTTP,
					},
					Path: "/get",
					Headers: map[string][]string{
						"Host": {"my.domain.example"},
					},
					Validator: echo.ExpectOK(),
				})
			})
			ctx.NewSubTest("tcp").Run(func(ctx framework.TestContext) {
				address := apps.Ingress.TCPAddress()
				_ = apps.Ingress.CallEchoWithRetryOrFail(ctx, echo.CallOptions{
					Port: &echo.Port{
						Protocol:    protocol.HTTP,
						ServicePort: address.Port,
					},
					Address: address.IP.String(),
					Path:    "/",
					Headers: map[string][]string{
						"Host": {"my.domain.example"},
					},
					Validator: echo.ExpectOK(),
				})
			})
		})
}

func skipIfIngressClassUnsupported(ctx framework.TestContext) {
	ver, err := ctx.Clusters().Default().GetKubernetesVersion()
	if err != nil {
		ctx.Fatalf("failed to get Kubernetes version: %v", err)
	}
	serverVersion := fmt.Sprintf("%s.%s", ver.Major, ver.Minor)
	if serverVersion < "1.18" {
		ctx.Skip("IngressClass not supported")
	}
}

// TestIngress tests that we can route using standard Kubernetes Ingress objects.
func TestIngress(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			skipIfIngressClassUnsupported(ctx)
			// Set up secret contain some TLS certs for *.example.com
			// we will define one for foo.example.com and one for bar.example.com, to ensure both can co-exist
			credName := "k8s-ingress-secret-foo"
			ingressutil.CreateIngressKubeSecret(ctx, []string{credName}, ingressutil.TLS, ingressutil.IngressCredentialA, false)
			ctx.WhenDone(func() error {
				ingressutil.DeleteKubeSecret(ctx, []string{credName})
				return nil
			})
			credName2 := "k8s-ingress-secret-bar"
			ingressutil.CreateIngressKubeSecret(ctx, []string{credName2}, ingressutil.TLS, ingressutil.IngressCredentialB, false)
			ctx.WhenDone(func() error {
				ingressutil.DeleteKubeSecret(ctx, []string{credName2})
				return nil
			})

			if err := ctx.Config().ApplyYAML(apps.Namespace.Name(), `
apiVersion: networking.k8s.io/v1beta1
kind: IngressClass
metadata:
  name: istio-test
spec:
  controller: istio.io/ingress-controller`, `
apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  name: ingress
spec:
  ingressClassName: istio-test
  tls:
  - hosts: ["foo.example.com"]
    secretName: k8s-ingress-secret-foo
  - hosts: ["bar.example.com"]
    secretName: k8s-ingress-secret-bar
  rules:
    - http:
        paths:
          - path: /test/namedport
            backend:
              serviceName: b
              servicePort: http
          - path: /test
            backend:
              serviceName: b
              servicePort: 80`,
			); err != nil {
				t.Fatal(err)
			}

			// TODO check all clusters were hit
			cases := []struct {
				name string
				call echo.CallOptions
			}{
				{
					// Basic HTTP call
					name: "http",
					call: echo.CallOptions{
						Port: &echo.Port{
							Protocol: protocol.HTTP,
						},
						Path: "/test",
						Headers: map[string][]string{
							"Host": {"server"},
						},
						Validator: echo.ExpectOK(),
					},
				},
				{
					// Basic HTTPS call for foo. CaCert matches the secret
					name: "https-foo",
					call: echo.CallOptions{
						Port: &echo.Port{
							Protocol: protocol.HTTPS,
						},
						Path: "/test",
						Headers: map[string][]string{
							"Host": {"foo.example.com"},
						},
						CaCert:    ingressutil.IngressCredentialA.CaCert,
						Validator: echo.ExpectOK(),
					},
				},
				{
					// Basic HTTPS call for bar. CaCert matches the secret
					name: "https-bar",
					call: echo.CallOptions{
						Port: &echo.Port{
							Protocol: protocol.HTTPS,
						},
						Path: "/test",
						Headers: map[string][]string{
							"Host": {"bar.example.com"},
						},
						CaCert:    ingressutil.IngressCredentialB.CaCert,
						Validator: echo.ExpectOK(),
					},
				},
				{
					// HTTPS call for bar with namedport route. CaCert matches the secret
					name: "https-namedport",
					call: echo.CallOptions{
						Port: &echo.Port{
							Protocol: protocol.HTTPS,
						},
						Path: "/test/namedport",
						Headers: map[string][]string{
							"Host": {"bar.example.com"},
						},
						CaCert:    ingressutil.IngressCredentialB.CaCert,
						Validator: echo.ExpectOK(),
					},
				},
			}
			for _, c := range cases {
				c := c
				ctx.NewSubTest(c.name).Run(func(ctx framework.TestContext) {
					apps.Ingress.CallEchoWithRetryOrFail(ctx, c.call, retry.Timeout(time.Minute*2))
				})
			}

			t.Run("status", func(t *testing.T) {
				if !ctx.Environment().(*kube.Environment).Settings().LoadBalancerSupported {
					t.Skip("ingress status not supported without load balancer")
				}

				ip := apps.Ingress.HTTPAddress().IP.String()
				retry.UntilSuccessOrFail(t, func() error {
					ing, err := ctx.Clusters().Default().NetworkingV1beta1().Ingresses(apps.Namespace.Name()).Get(context.Background(), "ingress", metav1.GetOptions{})
					if err != nil {
						return err
					}
					if len(ing.Status.LoadBalancer.Ingress) != 1 || ing.Status.LoadBalancer.Ingress[0].IP != ip {
						return fmt.Errorf("unexpected ingress status, got %+v want %v", ing.Status.LoadBalancer, ip)
					}
					return nil
				}, retry.Delay(time.Second*5), retry.Timeout(time.Second*90))

			})
		})
}
