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
	"net"
	"path/filepath"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8s "sigs.k8s.io/gateway-api/apis/v1alpha1"

	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	kubecluster "istio.io/istio/pkg/test/framework/components/cluster/kube"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/image"
	"istio.io/istio/pkg/test/helm"
	kubetest "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
	helmtest "istio.io/istio/tests/integration/helm"
	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"
)

func TestGateway(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			if !supportsCRDv1(t) {
				t.Skip("Not supported; requires CRDv1 support.")
			}
			crd, err := ioutil.ReadFile("testdata/service-apis-crd.yaml")
			if err != nil {
				t.Fatal(err)
			}
			if err := t.Config().ApplyYAMLNoCleanup("", string(crd)); err != nil {
				t.Fatal(err)
			}
			t.Config().ApplyYAMLOrFail(t, "", `
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
  namespace: istio-system
spec:
  gatewayClassName: istio
  listeners:
  - hostname: "*.domain.example"
    port: 80
    protocol: HTTP
    routes:
      namespaces:
        from: All
      kind: HTTPRoute
  - port: 31400
    protocol: TCP
    routes:
      namespaces:
        from: All
      kind: TCPRoute
---`)
			t.Config().ApplyYAMLOrFail(t, apps.Namespace.Name(), `
apiVersion: networking.x-k8s.io/v1alpha1
kind: HTTPRoute
metadata:
  name: http
spec:
  hostnames: ["my.domain.example"]
  gateways:
    allow: All
  rules:
  - matches:
    - path:
        type: Prefix
        value: /get/
    forwardTo:
    - serviceName: b
      port: 80
---
apiVersion: networking.x-k8s.io/v1alpha1
kind: TCPRoute
metadata:
  name: tcp
spec:
  gateways:
    allow: All
  rules:
  - forwardTo:
     - serviceName: b
       port: 80
---
apiVersion: networking.x-k8s.io/v1alpha1
kind: HTTPRoute
metadata:
  name: b
spec:
  gateways:
    allow: FromList
    gatewayRefs:
      - name: mesh
        namespace: istio-system
  hostnames: ["b"]
  rules:
  - matches:
    - path:
        type: Prefix
        value: /path
    filters:
    - type: RequestHeaderModifier
      requestHeaderModifier:
        add:
          my-added-header: added-value
    forwardTo:
    - serviceName: b
      port: 80
`)

			t.NewSubTest("http").Run(func(t framework.TestContext) {
				paths := []string{"/get", "/get/", "/get/prefix"}
				for _, path := range paths {
					_ = apps.Ingress.CallWithRetryOrFail(t, echo.CallOptions{
						Port: &echo.Port{
							Protocol: protocol.HTTP,
						},
						Path: path,
						Headers: map[string][]string{
							"Host": {"my.domain.example"},
						},
					})
				}
			})
			t.NewSubTest("tcp").Run(func(t framework.TestContext) {
				host, port := apps.Ingress.TCPAddress()
				_ = apps.Ingress.CallWithRetryOrFail(t, echo.CallOptions{
					Port: &echo.Port{
						Protocol:    protocol.HTTP,
						ServicePort: port,
					},
					Address: host,
					Path:    "/",
					Headers: map[string][]string{
						"Host": {"my.domain.example"},
					},
				})
			})
			t.NewSubTest("mesh").Run(func(t framework.TestContext) {
				_ = apps.PodA[0].CallWithRetryOrFail(t, echo.CallOptions{
					Target:    apps.PodB[0],
					PortName:  "http",
					Path:      "/path",
					Validator: echo.And(echo.ExpectOK(), echo.ExpectKey("My-Added-Header", "added-value")),
				})
			})
			t.NewSubTest("status").Run(func(t framework.TestContext) {
				retry.UntilSuccessOrFail(t, func() error {
					gwc, err := t.Clusters().Kube().Default().GatewayAPI().NetworkingV1alpha1().GatewayClasses().Get(context.Background(), "istio", metav1.GetOptions{})
					if err != nil {
						return err
					}
					if s := kstatus.GetCondition(gwc.Status.Conditions, string(k8s.GatewayClassConditionStatusAdmitted)).Status; s != metav1.ConditionTrue {
						return fmt.Errorf("expected status %q, got %q", metav1.ConditionTrue, s)
					}
					return nil
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
			credName := "k8s-ingress-secret-foo"
			ingressutil.CreateIngressKubeSecret(t, []string{credName}, ingressutil.TLS, ingressutil.IngressCredentialA, false, t.Clusters().Kube()...)
			t.ConditionalCleanup(func() {
				ingressutil.DeleteKubeSecret(t, []string{credName})
			})
			credName2 := "k8s-ingress-secret-bar"
			ingressutil.CreateIngressKubeSecret(t, []string{credName2}, ingressutil.TLS, ingressutil.IngressCredentialB, false, t.Clusters().Kube()...)
			t.ConditionalCleanup(func() {
				ingressutil.DeleteKubeSecret(t, []string{credName2})
			})

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

			successValidator := echo.And(echo.ExpectOK(), echo.ExpectReachedClusters(apps.PodB.Clusters()))
			failureValidator := echo.ExpectCode("404")
			count := 1
			if t.Clusters().IsMulticluster() {
				count = 2 * len(t.Clusters())
			}
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
						Port: &echo.Port{
							Protocol: protocol.HTTP,
						},
						Path: "/test",
						Headers: map[string][]string{
							"Host": {"server"},
						},
						Validator: successValidator,
						Count:     count,
					},
					path:       "/test",
					prefixPath: "/prefix",
				},
				{
					// Prefix /prefix/should MATCHES prefix/should/match
					name: "http-prefix-matches-subpath",
					call: echo.CallOptions{
						Port: &echo.Port{
							Protocol: protocol.HTTP,
						},
						Path: "/prefix/should/match",
						Headers: map[string][]string{
							"Host": {"server"},
						},
						Validator: successValidator,
						Count:     count,
					},
					path:       "/test",
					prefixPath: "/prefix/should",
				},
				{
					// Prefix /prefix/test/ should match path /prefix/test
					name: "http-prefix-matches-without-trailing-backslash",
					call: echo.CallOptions{
						Port: &echo.Port{
							Protocol: protocol.HTTP,
						},
						Path: "/prefix/test",
						Headers: map[string][]string{
							"Host": {"server"},
						},
						Validator: successValidator,
						Count:     count,
					},
					path:       "/test",
					prefixPath: "/prefix/test/",
				},
				{
					// Prefix /prefix/test should match /prefix/test/
					name: "http-prefix-matches-trailing-blackslash",
					call: echo.CallOptions{
						Port: &echo.Port{
							Protocol: protocol.HTTP,
						},
						Path: "/prefix/test/",
						Headers: map[string][]string{
							"Host": {"server"},
						},
						Validator: successValidator,
						Count:     count,
					},
					path:       "/test",
					prefixPath: "/prefix/test",
				},
				{
					// Prefix /prefix/test should NOT match /prefix/testrandom
					name: "http-prefix-should-not-match-path-continuation",
					call: echo.CallOptions{
						Port: &echo.Port{
							Protocol: protocol.HTTP,
						},
						Path: "/prefix/testrandom/",
						Headers: map[string][]string{
							"Host": {"server"},
						},
						Validator: failureValidator,
						Count:     count,
					},
					path:       "/test",
					prefixPath: "/prefix/test",
				},
				{
					// Prefix / should match any path
					name: "http-root-prefix-should-match-random-path",
					call: echo.CallOptions{
						Port: &echo.Port{
							Protocol: protocol.HTTP,
						},
						Path: "/testrandom",
						Headers: map[string][]string{
							"Host": {"server"},
						},
						Validator: successValidator,
						Count:     count,
					},
					path:       "/test",
					prefixPath: "/",
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
						Validator: successValidator,
						Count:     count,
					},
					path:       "/test",
					prefixPath: "/prefix",
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
						Validator: successValidator,
						Count:     count,
					},
					path:       "/test",
					prefixPath: "/prefix",
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
						Validator: successValidator,
						Count:     count,
					},
					path:       "/test",
					prefixPath: "/prefix",
				},
			}

			for _, ingr := range apps.Ingresses {
				ingr := ingr
				t.NewSubTestf("from %s", ingr.Cluster().StableName()).Run(func(t framework.TestContext) {
					for _, c := range cases {
						c := c
						t.NewSubTest(c.name).Run(func(t framework.TestContext) {
							if err := t.Config().ApplyYAML(apps.Namespace.Name(), ingressClassConfig,
								fmt.Sprintf(ingressConfigTemplate, "ingress", "istio-test", c.path, c.path, c.prefixPath)); err != nil {
								t.Fatal(err)
							}
							ingr.CallWithRetryOrFail(t, c.call, retry.Converge(3), retry.Delay(500*time.Millisecond), retry.Timeout(time.Minute*2))
						})
					}
				})
			}

			t.NewSubTest("status").Run(func(t framework.TestContext) {
				if !t.Environment().(*kube.Environment).Settings().LoadBalancerSupported {
					t.Skip("ingress status not supported without load balancer")
				}
				if err := t.Config().ApplyYAML(apps.Namespace.Name(), ingressClassConfig,
					fmt.Sprintf(ingressConfigTemplate, "ingress", "istio-test", "/test", "/test", "/test")); err != nil {
					t.Fatal(err)
				}

				host, _ := apps.Ingress.HTTPAddress()
				hostIsIP := net.ParseIP(host).String() != "<nil>"
				retry.UntilSuccessOrFail(t, func() error {
					if apiVersion == "v1beta1" {
						ing, err := t.Clusters().Default().NetworkingV1beta1().Ingresses(apps.Namespace.Name()).Get(context.Background(), "ingress", metav1.GetOptions{})
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
					ing, err := t.Clusters().Default().NetworkingV1().Ingresses(apps.Namespace.Name()).Get(context.Background(), "ingress", metav1.GetOptions{})
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
			if err := t.Config().ApplyYAML(apps.Namespace.Name(), ingressClassConfig,
				fmt.Sprintf(ingressConfigTemplate, updateIngressName, "istio-test", "/update-test", "/update-test", "/update-test")); err != nil {
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
						Port: &echo.Port{
							Protocol: protocol.HTTP,
						},
						Path: "/update-test",
						Headers: map[string][]string{
							"Host": {"server"},
						},
						Validator: echo.Or(echo.ExpectError(), echo.ExpectCode("404")),
					},
				},
				{
					name:         "update-class-istio",
					ingressClass: "istio-test",
					path:         "/update-test",
					call: echo.CallOptions{
						Port: &echo.Port{
							Protocol: protocol.HTTP,
						},
						Path: "/update-test",
						Headers: map[string][]string{
							"Host": {"server"},
						},
						Validator: echo.ExpectCode("200"),
					},
				},
				{
					name:         "update-path",
					ingressClass: "istio-test",
					path:         "/updated",
					call: echo.CallOptions{
						Port: &echo.Port{
							Protocol: protocol.HTTP,
						},
						Path: "/updated",
						Headers: map[string][]string{
							"Host": {"server"},
						},
						Validator: echo.ExpectCode("200"),
					},
				},
			}

			for _, c := range ingressUpdateCases {
				c := c
				updatedIngress := fmt.Sprintf(ingressConfigTemplate, updateIngressName, c.ingressClass, c.path, c.path, c.path)
				t.Config().ApplyYAMLOrFail(t, apps.Namespace.Name(), updatedIngress)
				t.NewSubTest(c.name).Run(func(t framework.TestContext) {
					apps.Ingress.CallWithRetryOrFail(t, c.call, retry.Timeout(time.Minute))
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
				"imagePullSecret": image.PullSecretNameOrFail(t),
				"injectLabel":     injectLabel,
				"host":            apps.PodA[0].Config().FQDN(),
			}

			t.NewSubTest("minimal").Run(func(t framework.TestContext) {
				gatewayNs := namespace.NewOrFail(t, t, namespace.Config{Prefix: "custom-gateway-minimal"})
				_ = t.Config().ApplyYAMLNoCleanup(gatewayNs.Name(), tmpl.MustEvaluate(`apiVersion: v1
kind: Service
metadata:
  name: custom-gateway
  labels:
    istio: custom
spec:
  ports:
  - port: 80
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
`, templateParams))
				cs := t.Clusters().Default().(*kubecluster.Cluster)
				retry.UntilSuccessOrFail(t, func() error {
					_, err := kubetest.CheckPodsAreReady(kubetest.NewPodFetch(cs, gatewayNs.Name(), "istio=custom"))
					return err
				}, retry.Timeout(time.Minute*2))
				apps.PodB[0].CallWithRetryOrFail(t, echo.CallOptions{
					Port:      &echo.Port{ServicePort: 80},
					Scheme:    scheme.HTTP,
					Address:   fmt.Sprintf("custom-gateway.%s.svc.cluster.local", gatewayNs.Name()),
					Validator: echo.ExpectOK(),
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
				ioutil.WriteFile(d, []byte(fmt.Sprintf(`
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
				}, retry.Timeout(time.Minute*2))
				_ = t.Config().ApplyYAMLNoCleanup(gatewayNs.Name(), fmt.Sprintf(`apiVersion: networking.istio.io/v1alpha3
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
`, apps.PodA[0].Config().FQDN()))
				apps.PodB[0].CallWithRetryOrFail(t, echo.CallOptions{
					Port:      &echo.Port{ServicePort: 80},
					Scheme:    scheme.HTTP,
					Address:   fmt.Sprintf("custom-gateway-helm.%s.svc.cluster.local", gatewayNs.Name()),
					Validator: echo.ExpectOK(),
				})
			})
		})
}
