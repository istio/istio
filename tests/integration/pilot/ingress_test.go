//go:build integ
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
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8s "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	kubecluster "istio.io/istio/pkg/test/framework/components/cluster/kube"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/helm"
	kubetest "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/retry"
	helmtest "istio.io/istio/tests/integration/helm"
	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"
)

func TestGateway(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			DeployGatewayAPICRD(t)

			ingressutil.CreateIngressKubeSecret(t, "test-gateway-cert-same", ingressutil.TLS, ingressutil.IngressCredentialA,
				false, t.Clusters().Configs()...)
			ingressutil.CreateIngressKubeSecret(t, "test-gateway-cert-cross", ingressutil.TLS, ingressutil.IngressCredentialB,
				false, t.Clusters().Configs()...)

			retry.UntilSuccessOrFail(t, func() error {
				err := t.ConfigIstio().YAML("", fmt.Sprintf(`
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: GatewayClass
metadata:
  name: istio
spec:
  controllerName: istio.io/gateway-controller
---
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: Gateway
metadata:
  name: gateway
  namespace: istio-system
spec:
  addresses:
  - value: istio-ingressgateway
    type: Hostname
  gatewayClassName: istio
  listeners:
  - name: http
    hostname: "*.domain.example"
    port: 80
    protocol: HTTP
    allowedRoutes:
      namespaces:
        from: All
  - name: tcp
    port: 31400
    protocol: TCP
    allowedRoutes:
      namespaces:
        from: All
  - name: tls-cross
    hostname: cross-namespace.domain.example
    port: 443
    protocol: HTTPS
    allowedRoutes:
      namespaces:
        from: All
    tls:
      mode: Terminate
      certificateRefs:
      - kind: Secret
        name: test-gateway-cert-cross
        namespace: "%s"
  - name: tls-same
    hostname: same-namespace.domain.example
    port: 443
    protocol: HTTPS
    allowedRoutes:
      namespaces:
        from: All
    tls:
      mode: Terminate
      certificateRefs:
      - kind: Secret
        name: test-gateway-cert-same
---`, apps.Namespace.Name())).Apply()
				return err
			}, retry.Delay(time.Second*10), retry.Timeout(time.Second*90))
			retry.UntilSuccessOrFail(t, func() error {
				err := t.ConfigIstio().YAML(apps.Namespace.Name(), `
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: HTTPRoute
metadata:
  name: http
spec:
  hostnames: ["my.domain.example"]
  parentRefs:
  - name: gateway
    namespace: istio-system
  rules:
  - matches:
    - path:
        type: PathPrefix
        value: /get/
    backendRefs:
    - name: b
      port: 80
---
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: TCPRoute
metadata:
  name: tcp
spec:
  parentRefs:
  - name: gateway
    namespace: istio-system
  rules:
  - backendRefs:
    - name: b
      port: 80
---
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: HTTPRoute
metadata:
  name: b
spec:
  parentRefs:
  - kind: Mesh
    name: istio
  - name: gateway
    namespace: istio-system
  hostnames: ["b"]
  rules:
  - matches:
    - path:
        type: PathPrefix
        value: /path
    filters:
    - type: RequestHeaderModifier
      requestHeaderModifier:
        add:
        - name: my-added-header
          value: added-value
    backendRefs:
    - name: b
      port: 80
`).Apply()
				return err
			}, retry.Delay(time.Second*10), retry.Timeout(time.Second*90))
			for _, ingr := range istio.IngressesOrFail(t, t) {
				t.NewSubTest(ingr.Cluster().StableName()).Run(func(t framework.TestContext) {
					t.NewSubTest("http").Run(func(t framework.TestContext) {
						paths := []string{"/get", "/get/", "/get/prefix"}
						for _, path := range paths {
							_ = ingr.CallOrFail(t, echo.CallOptions{
								Port: echo.Port{
									Protocol: protocol.HTTP,
								},
								HTTP: echo.HTTP{
									Path:    path,
									Headers: headers.New().WithHost("my.domain.example").Build(),
								},
							})
						}
					})
					t.NewSubTest("tcp").Run(func(t framework.TestContext) {
						_ = ingr.CallOrFail(t, echo.CallOptions{
							Port: echo.Port{
								Protocol:    protocol.HTTP,
								ServicePort: 31400,
							},
							HTTP: echo.HTTP{
								Path:    "/",
								Headers: headers.New().WithHost("my.domain.example").Build(),
							},
						})
					})
					t.NewSubTest("mesh").Run(func(t framework.TestContext) {
						_ = apps.A[0].CallOrFail(t, echo.CallOptions{
							To:    apps.B,
							Count: 1,
							Port: echo.Port{
								Name: "http",
							},
							HTTP: echo.HTTP{
								Path: "/path",
							},
							Check: check.And(
								check.OK(),
								check.RequestHeader("My-Added-Header", "added-value")),
						})
					})
					t.NewSubTest("status").Run(func(t framework.TestContext) {
						retry.UntilSuccessOrFail(t, func() error {
							gwc, err := t.Clusters().Kube().Default().GatewayAPI().GatewayV1alpha2().GatewayClasses().Get(context.Background(), "istio", metav1.GetOptions{})
							if err != nil {
								return err
							}
							if s := kstatus.GetCondition(gwc.Status.Conditions, string(k8s.GatewayClassConditionStatusAccepted)).Status; s != metav1.ConditionTrue {
								return fmt.Errorf("expected status %q, got %q", metav1.ConditionTrue, s)
							}
							return nil
						})
					})
				})
			}
			t.NewSubTest("managed").Run(func(t framework.TestContext) {
				t.ConfigIstio().YAML(apps.Namespace.Name(), `apiVersion: gateway.networking.k8s.io/v1alpha2
kind: Gateway
metadata:
  name: gateway
spec:
  gatewayClassName: istio
  listeners:
  - name: default
    hostname: "*.example.com"
    port: 80
    protocol: HTTP
---
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: HTTPRoute
metadata:
  name: http
spec:
  parentRefs:
  - name: gateway
  rules:
  - backendRefs:
    - name: b
      port: 80
`).ApplyOrFail(t)
				apps.B[0].CallOrFail(t, echo.CallOptions{
					Port:   echo.Port{ServicePort: 80},
					Scheme: scheme.HTTP,
					HTTP: echo.HTTP{
						Headers: headers.New().WithHost("bar.example.com").Build(),
					},
					Address: fmt.Sprintf("gateway.%s.svc.cluster.local", apps.Namespace.Name()),
					Check:   check.OK(),
					Retry: echo.Retry{
						Options: []retry.Option{retry.Timeout(time.Minute)},
					},
				})
			})
		})
}

func skipIfIngressClassUnsupported(t framework.TestContext) {
	if !t.Clusters().Default().MinKubeVersion(18) {
		t.Skip("IngressClass not supported")
	}
}

// TestIngress tests that we can route using standard Kubernetes Ingress objects.
func TestIngress(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			skipIfIngressClassUnsupported(t)
			// Set up secret contain some TLS certs for *.example.com
			// we will define one for foo.example.com and one for bar.example.com, to ensure both can co-exist
			ingressutil.CreateIngressKubeSecret(t, "k8s-ingress-secret-foo", ingressutil.TLS, ingressutil.IngressCredentialA, false, t.Clusters().Kube()...)
			ingressutil.CreateIngressKubeSecret(t, "k8s-ingress-secret-bar", ingressutil.TLS, ingressutil.IngressCredentialB, false, t.Clusters().Kube()...)

			apiVersion := "v1beta1"
			if t.Clusters().Default().MinKubeVersion(19) {
				apiVersion = "v1"
			}

			ingressClassConfig := fmt.Sprintf(`
apiVersion: networking.k8s.io/%s
kind: IngressClass
metadata:
  name: istio-test
spec:
  controller: istio.io/ingress-controller`, apiVersion)

			ingressConfigTemplate := `
apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  name: %s
spec:
  ingressClassName: %s
  tls:
  - hosts: ["foo.example.com"]
    secretName: k8s-ingress-secret-foo
  - hosts: ["bar.example.com"]
    secretName: k8s-ingress-secret-bar
  rules:
    - http:
        paths:
          - path: %s/namedport
            backend:
              serviceName: b
              servicePort: http
          - path: %s
            backend:
              serviceName: b
              servicePort: 80
          - path: %s
            pathType: Prefix
            backend:
              serviceName: b
              servicePort: http`
			if apiVersion == "v1" {
				ingressConfigTemplate = `
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: %s
spec:
  ingressClassName: %s
  tls:
  - hosts: ["foo.example.com"]
    secretName: k8s-ingress-secret-foo
  - hosts: ["bar.example.com"]
    secretName: k8s-ingress-secret-bar
  rules:
  - http:
      paths:
      - backend:
          service:
            name: b
            port:
              name: http
        path: %s/namedport
        pathType: ImplementationSpecific
      - backend:
          service:
            name: b
            port:
              number: 80
        path: %s
        pathType: ImplementationSpecific
      - backend:
          service:
            name: b
            port:
              number: 80
        path: %s
        pathType: Prefix
`
			}

			successChecker := check.And(check.OK(), check.ReachedClusters(t.AllClusters(), apps.B.Clusters()))
			failureChecker := check.Status(http.StatusNotFound)
			count := 2 * t.Clusters().Len()

			// TODO check all clusters were hit
			cases := []struct {
				name       string
				path       string
				prefixPath string
				call       echo.CallOptions
			}{
				{
					// Basic HTTP call
					name: "http",
					call: echo.CallOptions{
						Port: echo.Port{
							Protocol: protocol.HTTP,
						},
						HTTP: echo.HTTP{
							Path:    "/test",
							Headers: headers.New().WithHost("server").Build(),
						},
						Check: successChecker,
						Count: count,
					},
					path:       "/test",
					prefixPath: "/prefix",
				},
				{
					// Prefix /prefix/should MATCHES prefix/should/match
					name: "http-prefix-matches-subpath",
					call: echo.CallOptions{
						Port: echo.Port{
							Protocol: protocol.HTTP,
						},
						HTTP: echo.HTTP{
							Path:    "/prefix/should/match",
							Headers: headers.New().WithHost("server").Build(),
						},
						Check: successChecker,
						Count: count,
					},
					path:       "/test",
					prefixPath: "/prefix/should",
				},
				{
					// Prefix /prefix/test/ should match path /prefix/test
					name: "http-prefix-matches-without-trailing-backslash",
					call: echo.CallOptions{
						Port: echo.Port{
							Protocol: protocol.HTTP,
						},
						HTTP: echo.HTTP{
							Path:    "/prefix/test",
							Headers: headers.New().WithHost("server").Build(),
						},
						Check: successChecker,
						Count: count,
					},
					path:       "/test",
					prefixPath: "/prefix/test/",
				},
				{
					// Prefix /prefix/test should match /prefix/test/
					name: "http-prefix-matches-trailing-blackslash",
					call: echo.CallOptions{
						Port: echo.Port{
							Protocol: protocol.HTTP,
						},
						HTTP: echo.HTTP{
							Path:    "/prefix/test/",
							Headers: headers.New().WithHost("server").Build(),
						},
						Check: successChecker,
						Count: count,
					},
					path:       "/test",
					prefixPath: "/prefix/test",
				},
				{
					// Prefix /prefix/test should NOT match /prefix/testrandom
					name: "http-prefix-should-not-match-path-continuation",
					call: echo.CallOptions{
						Port: echo.Port{
							Protocol: protocol.HTTP,
						},
						HTTP: echo.HTTP{
							Path:    "/prefix/testrandom/",
							Headers: headers.New().WithHost("server").Build(),
						},
						Check: failureChecker,
						Count: count,
					},
					path:       "/test",
					prefixPath: "/prefix/test",
				},
				{
					// Prefix / should match any path
					name: "http-root-prefix-should-match-random-path",
					call: echo.CallOptions{
						Port: echo.Port{
							Protocol: protocol.HTTP,
						},
						HTTP: echo.HTTP{
							Path:    "/testrandom",
							Headers: headers.New().WithHost("server").Build(),
						},
						Check: successChecker,
						Count: count,
					},
					path:       "/test",
					prefixPath: "/",
				},
				{
					// Basic HTTPS call for foo. CaCert matches the secret
					name: "https-foo",
					call: echo.CallOptions{
						Port: echo.Port{
							Protocol: protocol.HTTPS,
						},
						HTTP: echo.HTTP{
							Path:    "/test",
							Headers: headers.New().WithHost("foo.example.com").Build(),
						},
						TLS: echo.TLS{
							CaCert: ingressutil.IngressCredentialA.CaCert,
						},
						Check: successChecker,
						Count: count,
					},
					path:       "/test",
					prefixPath: "/prefix",
				},
				{
					// Basic HTTPS call for bar. CaCert matches the secret
					name: "https-bar",
					call: echo.CallOptions{
						Port: echo.Port{
							Protocol: protocol.HTTPS,
						},
						HTTP: echo.HTTP{
							Path:    "/test",
							Headers: headers.New().WithHost("bar.example.com").Build(),
						},
						TLS: echo.TLS{
							CaCert: ingressutil.IngressCredentialB.CaCert,
						},
						Check: successChecker,
						Count: count,
					},
					path:       "/test",
					prefixPath: "/prefix",
				},
				{
					// HTTPS call for bar with namedport route. CaCert matches the secret
					name: "https-namedport",
					call: echo.CallOptions{
						Port: echo.Port{
							Protocol: protocol.HTTPS,
						},
						HTTP: echo.HTTP{
							Path:    "/test/namedport",
							Headers: headers.New().WithHost("bar.example.com").Build(),
						},
						TLS: echo.TLS{
							CaCert: ingressutil.IngressCredentialB.CaCert,
						},
						Check: successChecker,
						Count: count,
					},
					path:       "/test",
					prefixPath: "/prefix",
				},
			}

			for _, ingr := range istio.IngressesOrFail(t, t) {
				ingr := ingr
				t.NewSubTestf("from %s", ingr.Cluster().StableName()).Run(func(t framework.TestContext) {
					for _, c := range cases {
						c := c
						t.NewSubTest(c.name).Run(func(t framework.TestContext) {
							if err := t.ConfigIstio().YAML(apps.Namespace.Name(), ingressClassConfig,
								fmt.Sprintf(ingressConfigTemplate, "ingress", "istio-test", c.path, c.path, c.prefixPath)).
								Apply(); err != nil {
								t.Fatal(err)
							}
							c.call.Retry.Options = []retry.Option{
								retry.Delay(500 * time.Millisecond),
								retry.Timeout(time.Minute * 2),
							}
							ingr.CallOrFail(t, c.call)
						})
					}
				})
			}

			defaultIngress := istio.DefaultIngressOrFail(t, t)
			t.NewSubTest("status").Run(func(t framework.TestContext) {
				if !t.Environment().(*kube.Environment).Settings().LoadBalancerSupported {
					t.Skip("ingress status not supported without load balancer")
				}
				if err := t.ConfigIstio().YAML(apps.Namespace.Name(), ingressClassConfig,
					fmt.Sprintf(ingressConfigTemplate, "ingress", "istio-test", "/test", "/test", "/test")).
					Apply(); err != nil {
					t.Fatal(err)
				}

				host, _ := defaultIngress.HTTPAddress()
				hostIsIP := net.ParseIP(host).String() != "<nil>"
				retry.UntilSuccessOrFail(t, func() error {
					if apiVersion == "v1beta1" {
						ing, err := t.Clusters().Default().Kube().NetworkingV1beta1().Ingresses(apps.Namespace.Name()).Get(context.Background(), "ingress", metav1.GetOptions{})
						if err != nil {
							return err
						}
						if len(ing.Status.LoadBalancer.Ingress) < 1 {
							return fmt.Errorf("unexpected ingress status, ingress is empty")
						}
						got := ing.Status.LoadBalancer.Ingress[0].Hostname
						if hostIsIP {
							got = ing.Status.LoadBalancer.Ingress[0].IP
						}
						if got != host {
							return fmt.Errorf("unexpected ingress status, got %+v want %v", got, host)
						}
						return nil
					}
					ing, err := t.Clusters().Default().Kube().NetworkingV1().Ingresses(apps.Namespace.Name()).Get(context.Background(), "ingress", metav1.GetOptions{})
					if err != nil {
						return err
					}
					if len(ing.Status.LoadBalancer.Ingress) < 1 {
						return fmt.Errorf("unexpected ingress status, ingress is empty")
					}
					got := ing.Status.LoadBalancer.Ingress[0].Hostname
					if hostIsIP {
						got = ing.Status.LoadBalancer.Ingress[0].IP
					}
					if got != host {
						return fmt.Errorf("unexpected ingress status, got %+v want %v", got, host)
					}
					return nil
				}, retry.Delay(time.Second*5), retry.Timeout(time.Second*90))
			})

			// setup another ingress pointing to a different route; the ingress will have an ingress class that should be targeted at first
			const updateIngressName = "update-test-ingress"
			if err := t.ConfigIstio().YAML(apps.Namespace.Name(), ingressClassConfig,
				fmt.Sprintf(ingressConfigTemplate, updateIngressName, "istio-test", "/update-test", "/update-test", "/update-test")).
				Apply(); err != nil {
				t.Fatal(err)
			}
			// these cases make sure that when new Ingress configs are applied our controller picks up on them
			// and updates the accessible ingress-gateway routes accordingly
			ingressUpdateCases := []struct {
				name         string
				ingressClass string
				path         string
				call         echo.CallOptions
			}{
				{
					name:         "update-class-not-istio",
					ingressClass: "not-istio",
					path:         "/update-test",
					call: echo.CallOptions{
						Port: echo.Port{
							Protocol: protocol.HTTP,
						},
						HTTP: echo.HTTP{
							Path:    "/update-test",
							Headers: headers.New().WithHost("server").Build(),
						},
						Check: func(result echo.CallResult, err error) error {
							if err != nil {
								return nil
							}

							return check.Status(http.StatusNotFound).Check(result, nil)
						},
					},
				},
				{
					name:         "update-class-istio",
					ingressClass: "istio-test",
					path:         "/update-test",
					call: echo.CallOptions{
						Port: echo.Port{
							Protocol: protocol.HTTP,
						},
						HTTP: echo.HTTP{
							Path:    "/update-test",
							Headers: headers.New().WithHost("server").Build(),
						},
						Check: check.OK(),
					},
				},
				{
					name:         "update-path",
					ingressClass: "istio-test",
					path:         "/updated",
					call: echo.CallOptions{
						Port: echo.Port{
							Protocol: protocol.HTTP,
						},
						HTTP: echo.HTTP{
							Path:    "/updated",
							Headers: headers.New().WithHost("server").Build(),
						},
						Check: check.OK(),
					},
				},
			}

			for _, c := range ingressUpdateCases {
				c := c
				updatedIngress := fmt.Sprintf(ingressConfigTemplate, updateIngressName, c.ingressClass, c.path, c.path, c.path)
				t.ConfigIstio().YAML(apps.Namespace.Name(), updatedIngress).ApplyOrFail(t)
				t.NewSubTest(c.name).Run(func(t framework.TestContext) {
					c.call.Retry.Options = []retry.Option{retry.Timeout(time.Minute)}
					defaultIngress.CallOrFail(t, c.call)
				})
			}
		})
}

// TestCustomGateway deploys a simple gateway deployment, that is fully injected, and verifies it can startup and send traffic
func TestCustomGateway(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.ingress.custom").
		Run(func(t framework.TestContext) {
			injectLabel := `sidecar.istio.io/inject: "true"`
			if t.Settings().Revisions.Default() != "" {
				injectLabel = fmt.Sprintf(`istio.io/rev: "%v"`, t.Settings().Revisions.Default())
			}

			templateParams := map[string]string{
				"imagePullSecret": t.Settings().Image.PullSecretNameOrFail(t),
				"injectLabel":     injectLabel,
				"host":            apps.A.Config().ClusterLocalFQDN(),
				"imagePullPolicy": t.Settings().Image.PullPolicy,
			}

			t.NewSubTest("minimal").Run(func(t framework.TestContext) {
				gatewayNs := namespace.NewOrFail(t, t, namespace.Config{Prefix: "custom-gateway-minimal"})
				_ = t.ConfigIstio().Eval(gatewayNs.Name(), templateParams, `apiVersion: v1
kind: Service
metadata:
  name: custom-gateway
  labels:
    istio: custom
spec:
  ports:
  - port: 80
    targetPort: 8080
    name: http
  selector:
    istio: custom
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: custom-gateway
spec:
  selector:
    matchLabels:
      istio: custom
  template:
    metadata:
      annotations:
        inject.istio.io/templates: gateway
      labels:
        istio: custom
        {{ .injectLabel }}
    spec:
      {{- if ne .imagePullSecret "" }}
      imagePullSecrets:
      - name: {{ .imagePullSecret }}
      {{- end }}
      containers:
      - name: istio-proxy
        image: auto
        imagePullPolicy: {{ .imagePullPolicy }}
---
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: app
spec:
  selector:
    istio: custom
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "*"
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: app
spec:
  hosts:
  - "*"
  gateways:
  - app
  http:
  - route:
    - destination:
        host: {{ .host }}
        port:
          number: 80
`).Apply(apply.NoCleanup)
				cs := t.Clusters().Default().(*kubecluster.Cluster)
				retry.UntilSuccessOrFail(t, func() error {
					_, err := kubetest.CheckPodsAreReady(kubetest.NewPodFetch(cs, gatewayNs.Name(), "istio=custom"))
					return err
				}, retry.Timeout(time.Minute*2))
				apps.B[0].CallOrFail(t, echo.CallOptions{
					Port:    echo.Port{ServicePort: 80},
					Scheme:  scheme.HTTP,
					Address: fmt.Sprintf("custom-gateway.%s.svc.cluster.local", gatewayNs.Name()),
					Check:   check.OK(),
				})
			})
			// TODO we could add istioctl as well, but the framework adds a bunch of stuff beyond just `istioctl install`
			// that mess with certs, multicluster, etc
			t.NewSubTest("helm").Run(func(t framework.TestContext) {
				gatewayNs := namespace.NewOrFail(t, t, namespace.Config{Prefix: "custom-gateway-helm"})
				d := filepath.Join(t.TempDir(), "gateway-values.yaml")
				rev := ""
				if t.Settings().Revisions.Default() != "" {
					rev = t.Settings().Revisions.Default()
				}
				os.WriteFile(d, []byte(fmt.Sprintf(`
revision: %v
gateways:
  istio-ingressgateway:
    name: custom-gateway-helm
    injectionTemplate: gateway
    type: ClusterIP # LoadBalancer is slow and not necessary for this tests
    autoscaleMax: 1
    resources:
      requests:
        cpu: 10m
        memory: 40Mi
    labels:
      istio: custom-gateway-helm
`, rev)), 0o644)
				cs := t.Clusters().Default().(*kubecluster.Cluster)
				h := helm.New(cs.Filename())
				// Install ingress gateway chart
				if err := h.InstallChart("ingress", filepath.Join(env.IstioSrc, "manifests/charts/gateways/istio-ingress"), gatewayNs.Name(),
					d, helmtest.Timeout); err != nil {
					t.Fatal(err)
				}
				retry.UntilSuccessOrFail(t, func() error {
					_, err := kubetest.CheckPodsAreReady(kubetest.NewPodFetch(cs, gatewayNs.Name(), "istio=custom-gateway-helm"))
					return err
				}, retry.Timeout(time.Minute*2), retry.Delay(time.Millisecond*500))
				_ = t.ConfigIstio().YAML(gatewayNs.Name(), fmt.Sprintf(`apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: app
spec:
  selector:
    istio: custom-gateway-helm
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "*"
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: app
spec:
  hosts:
  - "*"
  gateways:
  - app
  http:
  - route:
    - destination:
        host: %s
        port:
          number: 80
`, apps.A.Config().ClusterLocalFQDN())).Apply(apply.NoCleanup)
				apps.B[0].CallOrFail(t, echo.CallOptions{
					Port:    echo.Port{ServicePort: 80},
					Scheme:  scheme.HTTP,
					Address: fmt.Sprintf("custom-gateway-helm.%s.svc.cluster.local", gatewayNs.Name()),
					Check:   check.OK(),
				})
			})
			t.NewSubTest("helm-simple").Run(func(t framework.TestContext) {
				gatewayNs := namespace.NewOrFail(t, t, namespace.Config{Prefix: "custom-gateway-helm"})
				d := filepath.Join(t.TempDir(), "gateway-values.yaml")
				rev := ""
				if t.Settings().Revisions.Default() != "" {
					rev = t.Settings().Revisions.Default()
				}
				os.WriteFile(d, []byte(fmt.Sprintf(`
revision: %v
service:
  type: ClusterIP # LoadBalancer is slow and not necessary for this tests
autoscaling:
  enabled: false
resources:
  requests:
    cpu: 10m
    memory: 40Mi
`, rev)), 0o644)
				cs := t.Clusters().Default().(*kubecluster.Cluster)
				h := helm.New(cs.Filename())
				// Install ingress gateway chart
				if err := h.InstallChart("helm-simple", filepath.Join(env.IstioSrc, "manifests/charts/gateway"), gatewayNs.Name(),
					d, helmtest.Timeout); err != nil {
					t.Fatal(err)
				}
				retry.UntilSuccessOrFail(t, func() error {
					_, err := kubetest.CheckPodsAreReady(kubetest.NewPodFetch(cs, gatewayNs.Name(), "istio=helm-simple"))
					return err
				}, retry.Timeout(time.Minute*2), retry.Delay(time.Millisecond*500))
				_ = t.ConfigIstio().YAML(gatewayNs.Name(), fmt.Sprintf(`apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: app
spec:
  selector:
    istio: helm-simple
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "*"
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: app
spec:
  hosts:
  - "*"
  gateways:
  - app
  http:
  - route:
    - destination:
        host: %s
        port:
          number: 80
`, apps.A.Config().ClusterLocalFQDN())).Apply(apply.NoCleanup)
				apps.B[0].CallOrFail(t, echo.CallOptions{
					Port:    echo.Port{ServicePort: 80},
					Scheme:  scheme.HTTP,
					Address: fmt.Sprintf("helm-simple.%s.svc.cluster.local", gatewayNs.Name()),
					Check:   check.OK(),
				})
			})
		})
}
