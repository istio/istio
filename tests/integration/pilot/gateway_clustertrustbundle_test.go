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

package pilot

import (
	"context"
	"fmt"
	"path/filepath"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource/config"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/pilot/gatewayconformance"
	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"
)

func skipUnlessClusterTrustBundleSupported(t framework.TestContext) {
	t.Helper()
	if ok, reason := gatewayconformance.ClusterTrustBundleSupported(t); !ok {
		t.Skip(reason)
	}
}

// ClusterTrustBundles are cluster-scoped, so callers pass names unique per test namespace. spec.signerName is
// left unset: it would need extra RBAC and constrain the name.
func clusterTrustBundleConfig(t framework.TestContext, name, bundle string) config.Plan {
	t.Helper()
	return t.ConfigIstio().Eval("", map[string]string{"name": name, "bundle": bundle}, `apiVersion: certificates.k8s.io/v1
kind: ClusterTrustBundle
metadata:
  name: {{.name}}
spec:
  trustBundle: |
{{.bundle | indent 4}}
`)
}

// check5xx accepts any server error response, which is what a fail-closed gateway returns when the backend CA cannot be used.
func check5xx() echo.Checker {
	return check.Or(check.Status(500), check.Status(502), check.Status(503), check.Status(504))
}

const ctbPolicyName = "ctb-tls-upstream"

type policyCondition struct {
	condType string
	status   metav1.ConditionStatus
	reason   string
}

// expectBackendTLSPolicyConditions waits until some ancestor of the BackendTLSPolicy reports all of the given conditions.
func expectBackendTLSPolicyConditions(t framework.TestContext, c cluster.Cluster, namespace string, want ...policyCondition) {
	t.Helper()
	const name = ctbPolicyName
	client := c.GatewayAPI().GatewayV1().BackendTLSPolicies(namespace)
	retry.UntilSuccessOrFail(t, func() error {
		pol, err := client.Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get BackendTLSPolicy %s/%s: %v", namespace, name, err)
		}
		var lastErr error
		for _, anc := range pol.Status.Ancestors {
			lastErr = nil
			for _, w := range want {
				got := kstatus.GetCondition(anc.Conditions, w.condType)
				if got.Status != w.status || got.Reason != w.reason {
					lastErr = fmt.Errorf("condition %s: want %s/%s, got %s/%s (%s)",
						w.condType, w.status, w.reason, got.Status, got.Reason, got.Message)
					break
				}
				if got.ObservedGeneration != pol.Generation {
					lastErr = fmt.Errorf("condition %s is stale: generation %d, observed %d", w.condType, pol.Generation, got.ObservedGeneration)
					break
				}
			}
			if lastErr == nil {
				return nil
			}
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("no ancestor status reported")
		}
		return fmt.Errorf("BackendTLSPolicy %s/%s: %v (ancestors: %+v)", namespace, name, lastErr, pol.Status.Ancestors)
	})
}

// ManagedGatewayBackendTLSClusterTrustBundleTest uses the Gateway created by ManagedGatewayTest.
func ManagedGatewayBackendTLSClusterTrustBundleTest(t framework.TestContext, gatewayClassName string) {
	skipUnlessClusterTrustBundleSupported(t)

	// Bundles live as long as the whole test, so state carries from one subtest to the next.
	owner := t
	ns := apps.Namespace.Name()
	c := t.Clusters().Default()
	bundleName := fmt.Sprintf("istio-it-%s-backend", ns)
	goodCA := file.AsStringOrFail(t, filepath.Join(env.IstioSrc, "tests/testdata/certs/dns/root-cert.pem"))

	t.ConfigIstio().Eval(ns, bundleName, `
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: ctb-tls
spec:
  parentRefs:
  - name: gateway
  hostnames: ["ctb-tls.example.com"]
  rules:
  - backendRefs:
    - name: b
      port: 443
---
apiVersion: gateway.networking.k8s.io/v1
kind: BackendTLSPolicy
metadata:
  name: ctb-tls-upstream
spec:
  targetRefs:
  - group: ""
    kind: Service
    name: b
  validation:
    caCertificateRefs:
    - group: certificates.k8s.io
      kind: ClusterTrustBundle
      name: {{.}}
    hostname: server.default.svc
`).ApplyOrFail(t)

	call := func(t framework.TestContext, checker echo.Checker) {
		t.Helper()
		apps.A[0].CallOrFail(t, echo.CallOptions{
			Port: echo.Port{
				Protocol:    protocol.HTTP,
				ServicePort: 80,
			},
			Scheme: scheme.HTTP,
			HTTP: echo.HTTP{
				Headers: headers.New().WithHost("ctb-tls.example.com").Build(),
			},
			Address: fmt.Sprintf("gateway-%s.%s.svc.cluster.local", gatewayClassName, ns),
			Check:   checker,
		})
	}
	resolved := []policyCondition{
		{string(k8sv1.PolicyConditionAccepted), metav1.ConditionTrue, string(k8sv1.PolicyReasonAccepted)},
		{string(k8sv1.BackendTLSPolicyConditionResolvedRefs), metav1.ConditionTrue, string(k8sv1.BackendTLSPolicyReasonResolvedRefs)},
	}
	unresolved := []policyCondition{
		{string(k8sv1.PolicyConditionAccepted), metav1.ConditionFalse, string(k8sv1.BackendTLSPolicyReasonNoValidCACertificate)},
		{string(k8sv1.BackendTLSPolicyConditionResolvedRefs), metav1.ConditionFalse, string(k8sv1.BackendTLSPolicyReasonInvalidCACertificateRef)},
	}

	// Bundle content updates only apply to new upstream connections, which the gateway pools, so the frontend test
	// covers them.
	t.NewSubTest("missing-bundle").Run(func(t framework.TestContext) {
		expectBackendTLSPolicyConditions(t, c, ns, unresolved...)
		call(t, check5xx())
	})
	t.NewSubTest("bundle-created").Run(func(t framework.TestContext) {
		clusterTrustBundleConfig(owner, bundleName, goodCA).ApplyOrFail(t)
		expectBackendTLSPolicyConditions(t, c, ns, resolved...)
		call(t, check.And(check.OK(), check.SNI("server.default.svc")))
	})
	t.NewSubTest("bundle-deleted").Run(func(t framework.TestContext) {
		clusterTrustBundleConfig(owner, bundleName, goodCA).DeleteOrFail(t)
		expectBackendTLSPolicyConditions(t, c, ns, unresolved...)
		call(t, check5xx())
	})
	t.NewSubTest("bundle-recreated").Run(func(t framework.TestContext) {
		clusterTrustBundleConfig(owner, bundleName, goodCA).ApplyOrFail(t)
		expectBackendTLSPolicyConditions(t, c, ns, resolved...)
		call(t, check.And(check.OK(), check.SNI("server.default.svc")))
	})
}

func FrontendTLSClusterTrustBundleTest(t framework.TestContext) {
	skipUnlessClusterTrustBundleSupported(t)

	// Bundles live as long as the whole test, so state carries from one subtest to the next.
	owner := t
	i := istio.DefaultConfigOrFail(t, t)
	ns := apps.Namespace.Name()
	c := t.Clusters().Default()
	bundleName := fmt.Sprintf("istio-it-%s-frontend", ns)
	const (
		gatewayName = "ctb-frontend"
		host        = "server.example.com"
	)

	t.ConfigIstio().YAML(ns, ingressutil.IngressKubeSecretYAML("ctb-frontend-tls", ns, ingressutil.TLS,
		ingressutil.IngressCredentialServerKeyCertA)).ApplyOrFail(t)
	t.ConfigIstio().Eval(ns, map[string]string{
		"gatewayClass": i.GatewayClassName,
		"gatewayName":  gatewayName,
		"bundleName":   bundleName,
		"host":         host,
	}, `apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: {{.gatewayName}}
spec:
  gatewayClassName: {{.gatewayClass}}
  tls:
    frontend:
      default:
        validation:
          caCertificateRefs:
          - group: certificates.k8s.io
            kind: ClusterTrustBundle
            name: {{.bundleName}}
  listeners:
  - name: default
    hostname: {{.host}}
    port: 443
    protocol: HTTPS
    tls:
      mode: Terminate
      certificateRefs:
      - group: ""
        kind: Secret
        name: ctb-frontend-tls
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: ctb-frontend
spec:
  parentRefs:
  - name: {{.gatewayName}}
  hostnames: ["{{.host}}"]
  rules:
  - backendRefs:
    - name: b
      port: 80
`).ApplyOrFail(t)

	call := func(t framework.TestContext, certA bool, checker echo.Checker) {
		t.Helper()
		tls := echo.TLS{
			CaCert:     ingressutil.CaCertA,
			ServerName: host,
			Cert:       ingressutil.TLSClientCertA,
			Key:        ingressutil.TLSClientKeyA,
		}
		if !certA {
			tls.Cert = ingressutil.TLSClientCertB
			tls.Key = ingressutil.TLSClientKeyB
		}
		// Not through a sidecar, so that the gateway sees the TLS connection of the client itself.
		apps.Naked[0].CallOrFail(t, echo.CallOptions{
			Port: echo.Port{
				Protocol:    protocol.HTTPS,
				ServicePort: 443,
			},
			Scheme:  scheme.HTTPS,
			TLS:     tls,
			HTTP:    echo.HTTP{Headers: headers.New().WithHost(host).Build()},
			Address: fmt.Sprintf("%s-%s.%s.svc.cluster.local", gatewayName, i.GatewayClassName, ns),
			Check:   checker,
		})
	}
	rejected := check.NotOK()

	listenerResolvedRefs := func(t framework.TestContext, status metav1.ConditionStatus, reason string) {
		t.Helper()
		client := c.GatewayAPI().GatewayV1().Gateways(ns)
		retry.UntilSuccessOrFail(t, func() error {
			gw, err := client.Get(context.Background(), gatewayName, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("failed to get gateway %s/%s: %v", ns, gatewayName, err)
			}
			for _, l := range gw.Status.Listeners {
				if l.Name != "default" {
					continue
				}
				got := kstatus.GetCondition(l.Conditions, string(k8sv1.ListenerConditionResolvedRefs))
				if got.Status != status || got.Reason != reason {
					return fmt.Errorf("listener ResolvedRefs: want %s/%s, got %s/%s (%s)", status, reason, got.Status, got.Reason, got.Message)
				}
				return nil
			}
			return fmt.Errorf("listener status not found: %+v", gw.Status.Listeners)
		})
	}

	t.NewSubTest("missing-bundle").Run(func(t framework.TestContext) {
		listenerResolvedRefs(t, metav1.ConditionFalse, string(k8sv1.ListenerReasonInvalidCACertificateRef))
		call(t, true, rejected)
	})
	t.NewSubTest("bundle-created").Run(func(t framework.TestContext) {
		clusterTrustBundleConfig(owner, bundleName, ingressutil.CaCertA).ApplyOrFail(t)
		listenerResolvedRefs(t, metav1.ConditionTrue, string(k8sv1.ListenerReasonResolvedRefs))
		call(t, true, check.And(check.OK(), check.MTLSForHTTP()))
		call(t, false, rejected)
	})
	t.NewSubTest("bundle-updated").Run(func(t framework.TestContext) {
		call(t, true, check.OK())
		clusterTrustBundleConfig(owner, bundleName, ingressutil.CaCertB).ApplyOrFail(t)
		call(t, false, check.OK())
		call(t, true, rejected)
	})
	t.NewSubTest("bundle-restored").Run(func(t framework.TestContext) {
		clusterTrustBundleConfig(owner, bundleName, ingressutil.CaCertA).ApplyOrFail(t)
		call(t, true, check.OK())
		call(t, false, rejected)
	})
	t.NewSubTest("bundle-deleted").Run(func(t framework.TestContext) {
		clusterTrustBundleConfig(owner, bundleName, ingressutil.CaCertA).DeleteOrFail(t)
		listenerResolvedRefs(t, metav1.ConditionFalse, string(k8sv1.ListenerReasonInvalidCACertificateRef))
		call(t, true, rejected)
	})
	t.NewSubTest("bundle-recreated").Run(func(t framework.TestContext) {
		clusterTrustBundleConfig(owner, bundleName, ingressutil.CaCertA).ApplyOrFail(t)
		listenerResolvedRefs(t, metav1.ConditionTrue, string(k8sv1.ListenerReasonResolvedRefs))
		call(t, true, check.OK())
	})
}
