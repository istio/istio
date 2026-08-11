//go:build integ

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

package common

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/config/protocol"
	istiohttp "istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	kubecluster "istio.io/istio/pkg/test/framework/components/cluster/kube"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	cdeployment "istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	istiocomponent "istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/helm"
	kubetest "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/retry"
	helmtest "istio.io/istio/tests/integration/helm"
	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"
)

// RunIngressTests validates Kubernetes Ingress routing via Istio ingress controller,
// including TLS, path types, named ports, ingress class, status updates, and path rewrites.
func RunIngressTests(t framework.TestContext, apps cdeployment.SingleNamespaceView) {
	t.Helper()
	if !t.Clusters().Default().MinKubeVersion(18) {
		t.Skip("IngressClass not supported")
	}
	ingressutil.CreateIngressKubeSecret(t, "k8s-ingress-secret-foo", ingressutil.TLS, ingressutil.IngressCredentialA, false, t.Clusters()...)
	ingressutil.CreateIngressKubeSecret(t, "k8s-ingress-secret-bar", ingressutil.TLS, ingressutil.IngressCredentialB, false, t.Clusters()...)

	ingressClassConfig := `
apiVersion: networking.k8s.io/v1
kind: IngressClass
metadata:
  name: istio-test
spec:
  controller: istio.io/ingress-controller`

	ingressConfigTemplate := `
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

	successChecker := check.And(check.OK(), check.ReachedClusters(t.AllClusters(), apps.B.Clusters()))
	failureChecker := check.Status(http.StatusNotFound)
	count := 2 * t.Clusters().Len()

	cases := []struct {
		name       string
		path       string
		prefixPath string
		call       echo.CallOptions
	}{
		{
			name: "http",
			call: echo.CallOptions{
				Port: echo.Port{Protocol: protocol.HTTP},
				HTTP: echo.HTTP{
					Path:    "/test",
					Headers: istiohttp.New().WithHost("server").Build(),
				},
				Check: successChecker,
				Count: count,
			},
			path:       "/test",
			prefixPath: "/prefix",
		},
		{
			name: "http-prefix-matches-subpath",
			call: echo.CallOptions{
				Port: echo.Port{Protocol: protocol.HTTP},
				HTTP: echo.HTTP{
					Path:    "/prefix/should/match",
					Headers: istiohttp.New().WithHost("server").Build(),
				},
				Check: successChecker,
				Count: count,
			},
			path:       "/test",
			prefixPath: "/prefix/should",
		},
		{
			name: "http-prefix-matches-without-trailing-backslash",
			call: echo.CallOptions{
				Port: echo.Port{Protocol: protocol.HTTP},
				HTTP: echo.HTTP{
					Path:    "/prefix/test",
					Headers: istiohttp.New().WithHost("server").Build(),
				},
				Check: successChecker,
				Count: count,
			},
			path:       "/test",
			prefixPath: "/prefix/test/",
		},
		{
			name: "http-prefix-matches-trailing-blackslash",
			call: echo.CallOptions{
				Port: echo.Port{Protocol: protocol.HTTP},
				HTTP: echo.HTTP{
					Path:    "/prefix/test/",
					Headers: istiohttp.New().WithHost("server").Build(),
				},
				Check: successChecker,
				Count: count,
			},
			path:       "/test",
			prefixPath: "/prefix/test",
		},
		{
			name: "http-prefix-should-not-match-path-continuation",
			call: echo.CallOptions{
				Port: echo.Port{Protocol: protocol.HTTP},
				HTTP: echo.HTTP{
					Path:    "/prefix/testrandom/",
					Headers: istiohttp.New().WithHost("server").Build(),
				},
				Check: failureChecker,
				Count: count,
			},
			path:       "/test",
			prefixPath: "/prefix/test",
		},
		{
			name: "http-root-prefix-should-match-random-path",
			call: echo.CallOptions{
				Port: echo.Port{Protocol: protocol.HTTP},
				HTTP: echo.HTTP{
					Path:    "/testrandom",
					Headers: istiohttp.New().WithHost("server").Build(),
				},
				Check: successChecker,
				Count: count,
			},
			path:       "/test",
			prefixPath: "/",
		},
		{
			name: "https-foo",
			call: echo.CallOptions{
				Port: echo.Port{Protocol: protocol.HTTPS},
				HTTP: echo.HTTP{
					Path:    "/test",
					Headers: istiohttp.New().WithHost("foo.example.com").Build(),
				},
				TLS:   echo.TLS{CaCert: ingressutil.IngressCredentialA.CaCert},
				Check: successChecker,
				Count: count,
			},
			path:       "/test",
			prefixPath: "/prefix",
		},
		{
			name: "https-bar",
			call: echo.CallOptions{
				Port: echo.Port{Protocol: protocol.HTTPS},
				HTTP: echo.HTTP{
					Path:    "/test",
					Headers: istiohttp.New().WithHost("bar.example.com").Build(),
				},
				TLS:   echo.TLS{CaCert: ingressutil.IngressCredentialB.CaCert},
				Check: successChecker,
				Count: count,
			},
			path:       "/test",
			prefixPath: "/prefix",
		},
		{
			name: "https-namedport",
			call: echo.CallOptions{
				Port: echo.Port{Protocol: protocol.HTTPS},
				HTTP: echo.HTTP{
					Path:    "/test/namedport",
					Headers: istiohttp.New().WithHost("bar.example.com").Build(),
				},
				TLS:   echo.TLS{CaCert: ingressutil.IngressCredentialB.CaCert},
				Check: successChecker,
				Count: count,
			},
			path:       "/test",
			prefixPath: "/prefix",
		},
	}

	for _, ingr := range istiocomponent.IngressesOrFail(t, t) {
		t.NewSubTestf("from %s", ingr.Cluster().StableName()).Run(func(t framework.TestContext) {
			for _, c := range cases {
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

	defaultIngress := istiocomponent.DefaultIngressOrFail(t, t)
	t.NewSubTest("status").Run(func(t framework.TestContext) {
		if !t.Environment().(*kube.Environment).Settings().LoadBalancerSupported {
			t.Skip("ingress status not supported without load balancer")
		}
		if err := t.ConfigIstio().YAML(apps.Namespace.Name(), ingressClassConfig,
			fmt.Sprintf(ingressConfigTemplate, "ingress", "istio-test", "/test", "/test", "/test")).
			Apply(); err != nil {
			t.Fatal(err)
		}

		hosts, _ := defaultIngress.HTTPAddresses()
		for _, host := range hosts {
			hostIsIP := net.ParseIP(host).String() != "<nil>"
			ingressHostFound := false
			actualHosts := []string{}
			retry.UntilSuccessOrFail(t, func() error {
				ing, err := t.Clusters().Default().Kube().NetworkingV1().Ingresses(apps.Namespace.Name()).Get(context.Background(), "ingress", metav1.GetOptions{})
				if err != nil {
					return err
				}
				if len(ing.Status.LoadBalancer.Ingress) < 1 {
					return fmt.Errorf("unexpected ingress status, ingress is empty")
				}
				for _, ingress := range ing.Status.LoadBalancer.Ingress {
					got := ingress.Hostname
					if hostIsIP {
						got = ingress.IP
					}
					actualHosts = append(actualHosts, got)
					if got == host {
						ingressHostFound = true
						break
					}
				}
				if !ingressHostFound {
					return fmt.Errorf("unexpected ingress status, got %+v want %v", actualHosts, host)
				}
				return nil
			}, retry.Timeout(time.Second*90))
		}
	})

	const updateIngressName = "update-test-ingress"
	if err := t.ConfigIstio().YAML(apps.Namespace.Name(), ingressClassConfig,
		fmt.Sprintf(ingressConfigTemplate, updateIngressName, "istio-test", "/update-test", "/update-test", "/update-test")).
		Apply(); err != nil {
		t.Fatal(err)
	}
	ingressUpdateCases := []struct {
		name         string
		ingressClass string
		path         string
		call         echo.CallOptions
	}{
		{
			name:         "initial state",
			ingressClass: "istio-test",
			path:         "/update-test",
			call: echo.CallOptions{
				Port: echo.Port{Protocol: protocol.HTTP},
				HTTP: echo.HTTP{
					Path:    "/update-test",
					Headers: istiohttp.New().WithHost("server").Build(),
				},
				Check: check.OK(),
			},
		},
		{
			name:         "update-class-not-istio",
			ingressClass: "not-istio",
			path:         "/update-test",
			call: echo.CallOptions{
				Port: echo.Port{Protocol: protocol.HTTP},
				HTTP: echo.HTTP{
					Path:    "/update-test",
					Headers: istiohttp.New().WithHost("server").Build(),
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
				Port: echo.Port{Protocol: protocol.HTTP},
				HTTP: echo.HTTP{
					Path:    "/update-test",
					Headers: istiohttp.New().WithHost("server").Build(),
				},
				Check: check.OK(),
			},
		},
		{
			name:         "update-path",
			ingressClass: "istio-test",
			path:         "/updated",
			call: echo.CallOptions{
				Port: echo.Port{Protocol: protocol.HTTP},
				HTTP: echo.HTTP{
					Path:    "/updated",
					Headers: istiohttp.New().WithHost("server").Build(),
				},
				Check: check.OK(),
			},
		},
	}

	for _, c := range ingressUpdateCases {
		updatedIngress := fmt.Sprintf(ingressConfigTemplate, updateIngressName, c.ingressClass, c.path, c.path, c.path)
		t.ConfigIstio().YAML(apps.Namespace.Name(), updatedIngress).ApplyOrFail(t)
		t.NewSubTest(c.name).Run(func(t framework.TestContext) {
			c.call.Retry.Options = []retry.Option{retry.Timeout(time.Minute)}
			defaultIngress.CallOrFail(t, c.call)
		})
	}
}

// RunCustomGatewayTests deploys a fully-injected custom gateway and verifies it can
// start up and route HTTP traffic (minimal, delay-create, and helm variants).
func RunCustomGatewayTests(t framework.TestContext, apps cdeployment.SingleNamespaceView) {
	t.Helper()
	inject := false
	if t.Settings().Compatibility {
		inject = true
	}
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
		gatewayNs := namespace.NewOrFail(t, namespace.Config{Prefix: "custom-gateway-minimal", Inject: inject})
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
apiVersion: networking.istio.io/v1
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
apiVersion: networking.istio.io/v1
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

	t.NewSubTest("minimal-delay-create-gateway-svc").Run(func(t framework.TestContext) {
		gatewayNs := namespace.NewOrFail(t, namespace.Config{Prefix: "custom-gateway-minimal", Inject: inject})
		_ = t.ConfigIstio().Eval(gatewayNs.Name(), templateParams, `apiVersion: apps/v1
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
apiVersion: networking.istio.io/v1
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
apiVersion: networking.istio.io/v1
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
`).Apply(apply.NoCleanup)
		apps.B[0].CallOrFail(t, echo.CallOptions{
			Port:    echo.Port{ServicePort: 80},
			Scheme:  scheme.HTTP,
			Address: fmt.Sprintf("custom-gateway.%s.svc.cluster.local", gatewayNs.Name()),
			Check:   check.OK(),
		})
	})

	t.NewSubTest("helm-simple").Run(func(t framework.TestContext) {
		gatewayNs := namespace.NewOrFail(t, namespace.Config{Prefix: "custom-gateway-helm", Inject: inject})
		d := filepath.Join(t.TempDir(), "gateway-values.yaml")
		rev := ""
		if t.Settings().Revisions.Default() != "" {
			rev = t.Settings().Revisions.Default()
		}
		gatewayValues := fmt.Sprintf(`
revision: %q
service:
  type: ClusterIP
autoscaling:
  enabled: false
resources:
  requests:
    cpu: 10m
    memory: 40Mi
`, rev)
		if t.Settings().OpenShift {
			gatewayValues += "\nplatform: openshift"
		}
		os.WriteFile(d, []byte(gatewayValues), 0o644)
		cs := t.Clusters().Default().(*kubecluster.Cluster)
		h := helm.New(cs.Filename())
		if err := h.InstallChart("helm-simple", filepath.Join(env.IstioSrc, "manifests/charts/gateway"), gatewayNs.Name(),
			d, helmtest.Timeout); err != nil {
			t.Fatal(err)
		}
		retry.UntilSuccessOrFail(t, func() error {
			_, err := kubetest.CheckPodsAreReady(kubetest.NewPodFetch(cs, gatewayNs.Name(), "istio=helm-simple"))
			return err
		}, retry.Timeout(time.Minute*2), retry.Delay(time.Millisecond*500))
		_ = t.ConfigIstio().YAML(gatewayNs.Name(), fmt.Sprintf(`apiVersion: networking.istio.io/v1
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
apiVersion: networking.istio.io/v1
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
}
