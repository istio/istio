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
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/crd"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
	testKube "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"
)

func TestGateway(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			crd.DeployGatewayAPIOrSkip(t)

			t.NewSubTest("unmanaged").Run(UnmanagedGatewayTest)
			t.NewSubTest("managed").Run(ManagedGatewayTest)
			t.NewSubTest("tagged").Run(TaggedGatewayTest)
			t.NewSubTest("managed-owner").Run(ManagedOwnerGatewayTest)
			t.NewSubTest("status").Run(StatusGatewayTest)
			t.NewSubTest("managed-short-name").Run(ManagedGatewayShortNameTest)
		})
}

func ManagedOwnerGatewayTest(t framework.TestContext) {
	image := fmt.Sprintf("%s/app:%s", t.Settings().Image.Hub, t.Settings().Image.Tag)
	t.ConfigIstio().YAML(apps.Namespace.Name(), fmt.Sprintf(`
apiVersion: v1
kind: Service
metadata:
  name: managed-owner-istio
spec:
  ports:
  - appProtocol: http
    name: default
    port: 80
  selector:
    gateway.networking.k8s.io/gateway-name: managed-owner
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: managed-owner-istio
spec:
  selector:
    matchLabels:
      gateway.networking.k8s.io/gateway-name: managed-owner
  replicas: 1
  template:
    metadata:
      labels:
        gateway.networking.k8s.io/gateway-name: managed-owner
    spec:
      containers:
      - name: fake
        image: %s
`, image)).ApplyOrFail(t)
	cls := t.Clusters().Default()
	fetchFn := testKube.NewSinglePodFetch(cls, apps.Namespace.Name(), label.IoK8sNetworkingGatewayGatewayName.Name+"=managed-owner")
	if _, err := testKube.WaitUntilPodsAreReady(fetchFn); err != nil {
		t.Fatal(err)
	}

	t.ConfigIstio().YAML(apps.Namespace.Name(), `
apiVersion: gateway.networking.k8s.io/v1beta1
kind: Gateway
metadata:
  name: managed-owner
spec:
  gatewayClassName: istio
  listeners:
  - name: default
    hostname: "*.example.com"
    port: 80
    protocol: HTTP
`).ApplyOrFail(t)

	// Make sure Gateway becomes programmed..
	client := t.Clusters().Default().GatewayAPI().GatewayV1beta1().Gateways(apps.Namespace.Name())
	check := func() error {
		gw, _ := client.Get(context.Background(), "managed-owner", metav1.GetOptions{})
		if gw == nil {
			return fmt.Errorf("failed to find gateway")
		}
		cond := kstatus.GetCondition(gw.Status.Conditions, string(k8sv1.GatewayConditionProgrammed))
		if cond == kstatus.EmptyCondition {
			return fmt.Errorf("failed to find programmed condition: %+v", cond)
		}
		if cond.Status != metav1.ConditionTrue {
			return fmt.Errorf("gateway not programmed: %+v", cond)
		}
		if cond.ObservedGeneration != gw.Generation {
			return fmt.Errorf("stale GWC generation: %+v", cond)
		}
		return nil
	}
	retry.UntilSuccessOrFail(t, check)

	// Make sure we did not overwrite our deployment or service
	dep, err := t.Clusters().Default().Kube().AppsV1().Deployments(apps.Namespace.Name()).
		Get(context.Background(), "managed-owner-istio", metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, dep.Labels[label.GatewayManaged.Name], "")
	assert.Equal(t, dep.Spec.Template.Spec.Containers[0].Image, image)

	svc, err := t.Clusters().Default().Kube().CoreV1().Services(apps.Namespace.Name()).
		Get(context.Background(), "managed-owner-istio", metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, svc.Labels[label.GatewayManaged.Name], "")
	assert.Equal(t, svc.Spec.Type, corev1.ServiceTypeClusterIP)
}

func ManagedGatewayTest(t framework.TestContext) {
	t.ConfigIstio().YAML(apps.Namespace.Name(), `apiVersion: gateway.networking.k8s.io/v1beta1
kind: Gateway
metadata:
  name: gateway
spec:
  gatewayClassName: istio
  allowedListeners:
    namespaces:
      from: All
  listeners:
  - name: default
    hostname: "*.example.com"
    port: 80
    protocol: HTTP
---
apiVersion: gateway.networking.k8s.io/v1beta1
kind: HTTPRoute
metadata:
  name: http-1
spec:
  parentRefs:
  - name: gateway
  hostnames: ["bar.example.com"]
  rules:
  - backendRefs:
    - name: b
      port: 80
---
apiVersion: gateway.networking.k8s.io/v1beta1
kind: HTTPRoute
metadata:
  name: http-2
spec:
  parentRefs:
  - name: gateway
  hostnames: ["foo.example.com"]
  rules:
  - backendRefs:
    - name: d
      port: 80
`).ApplyOrFail(t)
	testCases := []struct {
		check echo.Checker
		from  echo.Instances
		host  string
	}{
		{
			check: check.OK(),
			from:  apps.B,
			host:  "bar.example.com",
		},
		{
			check: check.NotOK(),
			from:  apps.B,
			host:  "bar",
		},
	}
	// additional tests for dual-stack scenario
	if len(t.Settings().IPFamilies) > 1 {
		additionalTestCases := []struct {
			check echo.Checker
			from  echo.Instances
			host  string
		}{
			// apps.D and apps.E host single-stack services
			// apps.B hosts a dual-stack service
			{
				check: check.OK(),
				from:  apps.D,
				host:  "bar.example.com",
			},
			{
				check: check.OK(),
				from:  apps.E,
				host:  "bar.example.com",
			},
			{
				check: check.OK(),
				from:  apps.E,
				host:  "foo.example.com",
			},
			{
				check: check.OK(),
				from:  apps.D,
				host:  "foo.example.com",
			},
			{
				check: check.OK(),
				from:  apps.B,
				host:  "foo.example.com",
			},
		}
		testCases = append(testCases, additionalTestCases...)
	}
	for _, tc := range testCases {
		t.NewSubTest(fmt.Sprintf("gateway-connectivity-from-%s", tc.from[0].NamespacedName())).Run(func(t framework.TestContext) {
			tc.from[0].CallOrFail(t, echo.CallOptions{
				Port: echo.Port{
					Protocol:    protocol.HTTP,
					ServicePort: 80,
				},
				Scheme: scheme.HTTP,
				HTTP: echo.HTTP{
					Headers: headers.New().WithHost(tc.host).Build(),
				},
				Address: fmt.Sprintf("gateway-istio.%s.svc.cluster.local", apps.Namespace.Name()),
				Check:   tc.check,
			})
		})
	}

	t.NewSubTest("backend-tls").Run(func(t framework.TestContext) {
		ca := file.AsStringOrFail(t, filepath.Join(env.IstioSrc, "tests/testdata/certs/cert.crt"))
		t.ConfigIstio().Eval(apps.Namespace.Name(), ca, `
apiVersion: v1
kind: ConfigMap
data:
  ca.crt: |
{{. | indent 4}}
metadata:
  name: auth-cert
---
apiVersion: gateway.networking.k8s.io/v1beta1
kind: HTTPRoute
metadata:
  name: tls
spec:
  parentRefs:
  - name: gateway
  hostnames: ["tls.example.com"]
  rules:
  - backendRefs:
    - name: b
      port: 443
---
apiVersion: gateway.networking.k8s.io/v1
kind: BackendTLSPolicy
metadata:
  name: tls-upstream
spec:
  targetRefs:
  - group: ""
    kind: Service
    name: b
  validation:
    caCertificateRefs:
    - group: ""
      kind: ConfigMap
      name: auth-cert
    hostname: auth.example.com
`).ApplyOrFail(t)
		apps.A[0].CallOrFail(t, echo.CallOptions{
			Port: echo.Port{
				Protocol:    protocol.HTTP,
				ServicePort: 80,
			},
			Scheme: scheme.HTTP,
			HTTP: echo.HTTP{
				Headers: headers.New().WithHost("tls.example.com").Build(),
			},
			Address: fmt.Sprintf("gateway-istio.%s.svc.cluster.local", apps.Namespace.Name()),
			Check:   check.And(check.OK(), check.SNI("auth.example.com")),
		})
	})
	t.NewSubTest("listenerset").Run(func(t framework.TestContext) {
		ns := namespace.NewOrFail(t, namespace.Config{Prefix: "listenerset"})
		ingressutil.CreateIngressKubeSecretInNamespace(t, "tls", ingressutil.TLS, ingressutil.IngressCredentialA,
			false, ns.Name(), t.Clusters().Configs()...)
		t.ConfigIstio().Eval("", map[string]string{
			"GatewayNamespace":  apps.Namespace.Name(),
			"ListenerNamespace": ns.Name(),
		}, `apiVersion: gateway.networking.x-k8s.io/v1alpha1
kind: XListenerSet
metadata:
  name: listenerset
  namespace: {{.ListenerNamespace}}
spec:
  listeners:
  - name: tls
    port: 443
    protocol: HTTPS
    tls:
      certificateRefs:
      - group: ""
        kind: Secret
        name: tls
      mode: Terminate
  parentRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: gateway
    namespace: {{.GatewayNamespace}}
---
apiVersion: gateway.networking.k8s.io/v1beta1
kind: HTTPRoute
metadata:
  name: route-listenerset
  namespace: {{.ListenerNamespace}}
spec:
  parentRefs:
  - name: listenerset
    kind: XListenerSet
    group: gateway.networking.x-k8s.io
  hostnames: ["listenerset.example.com"]
  rules:
  - backendRefs:
    - name: b
      port: 80
      namespace: {{.GatewayNamespace}}
---
apiVersion: gateway.networking.k8s.io/v1beta1
kind: ReferenceGrant
metadata:
  name: allow-service
  namespace: {{.GatewayNamespace}}
spec:
  from:
  - group: gateway.networking.k8s.io
    kind: HTTPRoute
    namespace: {{.ListenerNamespace}}
  to:
  - group: ""
    kind: Service
    name: b
`).ApplyOrFail(t)

		apps.B[0].CallOrFail(t, echo.CallOptions{
			Port: echo.Port{
				Protocol:    protocol.HTTPS,
				ServicePort: 443,
			},
			Scheme: scheme.HTTPS,
			HTTP: echo.HTTP{
				Headers: headers.New().WithHost("listenerset.example.com").Build(),
			},
			TLS: echo.TLS{
				ServerName: "listenerset.example.com",
			},
			Address: fmt.Sprintf("gateway-istio.%s.svc.cluster.local", apps.Namespace.Name()),
			Check:   check.OK(),
		})
	})
}

func TaggedGatewayTest(t framework.TestContext) {
	revision := t.Settings().Revisions.Default()
	if revision == "" {
		revision = "default"
	}

	i := istio.DefaultConfigOrFail(t, t)
	istioctlCfg := istioctl.Config{
		IstioNamespace: i.SystemNamespace,
	}
	istioctl.NewOrFail(t, istioctlCfg).InvokeOrFail(
		t, append(strings.Split("tag set tag --revision", " "), revision))

	testCases := []struct {
		check         echo.Checker
		revisionValue string
	}{
		{
			check:         check.OK(),
			revisionValue: "tag",
		},
		{
			check:         check.NotOK(),
			revisionValue: "badtag",
		},
	}
	for _, tc := range testCases {
		t.NewSubTest(fmt.Sprintf("gateway-connectivity-tagged-%s", tc.revisionValue)).Run(func(t framework.TestContext) {
			t.ConfigIstio().Eval(apps.Namespace.Name(),
				map[string]string{"revision": tc.revisionValue}, `apiVersion: gateway.networking.k8s.io/v1beta1
kind: Gateway
metadata:
  name: gateway
  labels:
    istio.io/rev: {{.revision}}
spec:
  gatewayClassName: istio
  listeners:
  - name: default
    hostname: "*.example.com"
    port: 80
    protocol: HTTP
---
apiVersion: gateway.networking.k8s.io/v1beta1
kind: HTTPRoute
metadata:
  name: http-1
spec:
  parentRefs:
  - name: gateway
  hostnames: ["bar.example.com"]
  rules:
  - backendRefs:
    - name: b
      port: 80
`).ApplyOrFail(t)
			apps.B[0].CallOrFail(t, echo.CallOptions{
				Port: echo.Port{
					Protocol:    protocol.HTTP,
					ServicePort: 80,
				},
				Scheme: scheme.HTTP,
				HTTP: echo.HTTP{
					Headers: headers.New().WithHost("bar.example.com").Build(),
				},
				Address: fmt.Sprintf("gateway-istio.%s.svc.cluster.local", apps.Namespace.Name()),
				Check:   tc.check,
			})
		})
	}
}

func ManagedGatewayShortNameTest(t framework.TestContext) {
	t.ConfigIstio().YAML(apps.Namespace.Name(), `apiVersion: gateway.networking.k8s.io/v1beta1
kind: Gateway
metadata:
  name: gateway
spec:
  gatewayClassName: istio
  listeners:
  - name: default
    hostname: "bar"
    port: 80
    protocol: HTTP
---
apiVersion: gateway.networking.k8s.io/v1beta1
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
			Headers: headers.New().WithHost("bar").Build(),
		},
		Address: fmt.Sprintf("gateway-istio.%s.svc.cluster.local", apps.Namespace.Name()),
		Check:   check.OK(),
		Retry: echo.Retry{
			Options: []retry.Option{retry.Timeout(2 * time.Minute)},
		},
	})
	apps.B[0].CallOrFail(t, echo.CallOptions{
		Port:   echo.Port{ServicePort: 80},
		Scheme: scheme.HTTP,
		HTTP: echo.HTTP{
			Headers: headers.New().WithHost("bar.example.com").Build(),
		},
		Address: fmt.Sprintf("gateway-istio.%s.svc.cluster.local", apps.Namespace.Name()),
		Check:   check.NotOK(),
		Retry: echo.Retry{
			Options: []retry.Option{retry.Timeout(2 * time.Minute)},
		},
	})
}

func UnmanagedGatewayTest(t framework.TestContext) {
	i := istio.DefaultConfigOrFail(t, t)
	ingressGatewayNs := i.IngressGatewayServiceNamespace
	ingressGatewaySvcName := i.IngressGatewayServiceName

	// Fair assumption that the ingress gateway is installed in the same system namespace
	if ingressGatewayNs == "" {
		ingressGatewayNs = i.SystemNamespace
	}
	if ingressGatewaySvcName == "" {
		ingressGatewaySvcName = "istio-ingressgateway"
	}
	ingressutil.CreateIngressKubeSecretInNamespace(t, "test-gateway-cert-same", ingressutil.TLS, ingressutil.IngressCredentialA,
		false, ingressGatewayNs, t.Clusters().Configs()...)
	ingressutil.CreateIngressKubeSecretInNamespace(t, "test-gateway-cert-cross", ingressutil.TLS, ingressutil.IngressCredentialB,
		false, apps.Namespace.Name(), t.Clusters().Configs()...)

	// TODO: If we run this test choosing an specific istio revision, the Gateway
	// will not be programmed unless we add istio.io/rev
	// See: https://github.com/istio/istio/issues/56767
	templateArgs := map[string]string{
		"ingressSvcName":          ingressGatewaySvcName,
		"ingressGatewayNamespace": ingressGatewayNs,
		"appNamespace":            apps.Namespace.Name(),
		"revision":                t.Settings().Revisions.Default(),
	}
	t.ConfigIstio().
		YAML("", `
apiVersion: gateway.networking.k8s.io/v1beta1
kind: GatewayClass
metadata:
  name: custom-istio
spec:
  controllerName: istio.io/gateway-controller
`).
		Eval(ingressGatewayNs, templateArgs, `
apiVersion: gateway.networking.k8s.io/v1beta1
kind: Gateway
metadata:
  name: gateway
  {{- if .revision }}
  labels:
    istio.io/rev: {{.revision}}
  {{- end }}
spec:
  addresses:
  - value: {{.ingressSvcName}}.{{.ingressGatewayNamespace}}.svc.cluster.local
    type: Hostname
  gatewayClassName: custom-istio
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
        namespace: "{{.appNamespace}}"
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
`).
		Eval(apps.Namespace.Name(), templateArgs, `
apiVersion: gateway.networking.k8s.io/v1beta1
kind: HTTPRoute
metadata:
  name: http
spec:
  hostnames: ["my.domain.example"]
  parentRefs:
  - name: gateway
    namespace: {{.ingressGatewayNamespace}}
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
    namespace: {{.ingressGatewayNamespace}}
  rules:
  - backendRefs:
    - name: b
      port: 80
---
apiVersion: gateway.networking.k8s.io/v1beta1
kind: HTTPRoute
metadata:
  name: b
spec:
  parentRefs:
  - group: ""
    kind: Service
    name: b
  - name: gateway
    namespace: {{.ingressGatewayNamespace}}
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
---
apiVersion: gateway.networking.k8s.io/v1
kind: GRPCRoute
metadata:
  name: grpc
spec:
  parentRefs:
  - group: ""
    kind: Service
    name: c
  - name: gateway
    namespace: {{.ingressGatewayNamespace}}
  rules:
  - matches:
    - method:
        method: Echo
    filters:
    - type: RequestHeaderModifier
      requestHeaderModifier:
        add:
        - name: my-added-header
          value: added-grpc-value
    backendRefs:
    - name: c
      port: 7070
---
apiVersion: gateway.networking.k8s.io/v1beta1
kind: HTTPRoute
metadata:
  name: tls-same
spec:
  parentRefs:
  - name: gateway
    sectionName: tls-same
    namespace: {{.ingressGatewayNamespace}}
  rules:
  - backendRefs:
    - name: b
      port: 80
---
apiVersion: gateway.networking.k8s.io/v1beta1
kind: HTTPRoute
metadata:
  name: tls-cross
spec:
  parentRefs:
  - name: gateway
    sectionName: tls-cross
    namespace: {{.ingressGatewayNamespace}}
  rules:
  - backendRefs:
    - name: b
      port: 80
`).Eval(apps.Namespace.Name(), templateArgs, `
apiVersion: gateway.networking.k8s.io/v1beta1
kind: ReferenceGrant
metadata:
  name: allow-gateways-to-ref-secrets
spec:
  from:
  - group: gateway.networking.k8s.io
    kind: Gateway
    namespace: "{{.ingressGatewayNamespace}}"
  to:
  - group: ""
    kind: Secret
`).
		ApplyOrFail(t)
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
						Check: check.OK(),
						Retry: echo.Retry{
							Options: []retry.Option{retry.Timeout(2 * time.Minute)},
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
					Check: check.OK(),
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
			t.NewSubTest("mesh-grpc").Run(func(t framework.TestContext) {
				_ = apps.A[0].CallOrFail(t, echo.CallOptions{
					To:    apps.C,
					Count: 1,
					Port: echo.Port{
						Name: "grpc",
					},
					Check: check.And(
						check.OK(),
						check.RequestHeader("My-Added-Header", "added-grpc-value")),
				})
			})
			t.NewSubTest("status").Run(func(t framework.TestContext) {
				retry.UntilSuccessOrFail(t, func() error {
					gwc, err := t.Clusters().Default().GatewayAPI().GatewayV1beta1().GatewayClasses().Get(context.Background(), "istio", metav1.GetOptions{})
					if err != nil {
						return err
					}
					if s := kstatus.GetCondition(gwc.Status.Conditions, string(k8sv1.GatewayClassConditionStatusAccepted)).Status; s != metav1.ConditionTrue {
						return fmt.Errorf("expected status %q, got %q", metav1.ConditionTrue, s)
					}
					return nil
				})
			})
			t.NewSubTest("tls-same").Run(func(t framework.TestContext) {
				_ = ingr.CallOrFail(t, echo.CallOptions{
					Port: echo.Port{
						Protocol:    protocol.HTTPS,
						ServicePort: 443,
					},
					HTTP: echo.HTTP{
						Path:    "/",
						Headers: headers.New().WithHost("same-namespace.domain.example").Build(),
					},
					Check: check.OK(),
				})
			})
			t.NewSubTest("tls-cross").Run(func(t framework.TestContext) {
				_ = ingr.CallOrFail(t, echo.CallOptions{
					Port: echo.Port{
						Protocol:    protocol.HTTPS,
						ServicePort: 443,
					},
					HTTP: echo.HTTP{
						Path:    "/",
						Headers: headers.New().WithHost("cross-namespace.domain.example").Build(),
					},
					Check: check.OK(),
				})
			})
		})
	}
}

func StatusGatewayTest(t framework.TestContext) {
	client := t.Clusters().Default().GatewayAPI().GatewayV1beta1().GatewayClasses()

	check := func() error {
		gwc, _ := client.Get(context.Background(), "istio", metav1.GetOptions{})
		if gwc == nil {
			return fmt.Errorf("failed to find GatewayClass istio")
		}
		cond := kstatus.GetCondition(gwc.Status.Conditions, string(k8sv1.GatewayClassConditionStatusAccepted))
		if cond.Status != metav1.ConditionTrue {
			return fmt.Errorf("failed to find accepted condition: %+v", cond)
		}
		if cond.ObservedGeneration != gwc.Generation {
			return fmt.Errorf("stale GWC generation: %+v", cond)
		}
		return nil
	}
	retry.UntilSuccessOrFail(t, check)

	// Wipe out the status
	gwc, _ := client.Get(context.Background(), "istio", metav1.GetOptions{})
	gwc.Status.Conditions = nil
	client.Update(context.Background(), gwc, metav1.UpdateOptions{})
	// It should be added back
	retry.UntilSuccessOrFail(t, check)
}

// Verify that the envoy readiness probes are reachable at
// https://GatewaySvcIP:15021/healthz/ready . This is being explicitly done
// to make sure, in dual-stack scenarios both v4 and v6 probes are reachable.
func TestGatewayReadinessProbes(t *testing.T) {
	// nolint: staticcheck
	framework.NewTest(t).
		RequiresSingleCluster().
		RequiresLocalControlPlane().
		Run(func(t framework.TestContext) {
			c := t.Clusters().Default()
			var svc *corev1.Service
			svc, _, err := testKube.WaitUntilServiceEndpointsAreReady(c.Kube(), i.IngressFor(c).Namespace(), "istio-ingressgateway")
			if err != nil {
				t.Fatalf("error getting ingress gateway svc ips: %v", err)
			}
			for _, ip := range svc.Spec.ClusterIPs {
				t.NewSubTest("gateway-readiness-probe-" + ip).Run(func(t framework.TestContext) {
					apps.External.All[0].CallOrFail(t, echo.CallOptions{
						Address: ip,
						Port:    echo.Port{ServicePort: 15021},
						Scheme:  scheme.HTTP,
						HTTP: echo.HTTP{
							Path: "/healthz/ready",
						},
						Check: check.And(
							check.Status(200),
						),
					})
				})
			}
		})
}

// Verify that the envoy metrics endpoints are reachable at
// https://GatewayPodIP:15090/stats/prometheus . This is being explicitly done
// to make sure, in dual-stack scenarios both v4 and v6 probes are reachable.
func TestGatewayMetricsEndpoints(t *testing.T) {
	// nolint: staticcheck
	framework.NewTest(t).
		RequiresSingleCluster().
		RequiresLocalControlPlane().
		Run(func(t framework.TestContext) {
			c := t.Clusters().Default()
			podIPs, err := i.PodIPsFor(c, i.IngressFor(c).Namespace(), "app=istio-ingressgateway")
			if err != nil {
				t.Fatalf("error getting ingress gateway pod ips: %v", err)
			}
			for _, ip := range podIPs {
				t.NewSubTest("gateway-metrics-endpoints-" + ip.IP).Run(func(t framework.TestContext) {
					apps.External.All[0].CallOrFail(t, echo.CallOptions{
						Address: ip.IP,
						Port:    echo.Port{ServicePort: 15090},
						Scheme:  scheme.HTTP,
						HTTP: echo.HTTP{
							Path: "/stats/prometheus",
						},
						Check: check.And(
							check.Status(200),
						),
					})
				})
			}
		})
}
