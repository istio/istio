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

package pqc

import (
	"fmt"
	"net/http"
	"path"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/annotation"
	"istio.io/api/label"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/http/headers"
	echot "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/crd"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	testlabel "istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/util/file"
	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"
)

var (
	i          istio.Instance
	internalNs namespace.Instance
	externalNs namespace.Instance
	a          echo.Instance
	server     echo.Instance
	configs    []echo.Config
	apps       deployment.TwoNamespaceView
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(testlabel.CustomSetup).
		Setup(istio.Setup(&i, func(ctx resource.Context, cfg *istio.Config) {
			ctx.Settings().Ambient = true
			ctx.Settings().SkipVMs()
			cfg.EnableCNI = true
			cfg.DeployGatewayAPI = true
			cfg.ControlPlaneValues = `
values:
  pilot:
    env:
      COMPLIANCE_POLICY: "pqc"
  ztunnel:
    env:
      COMPLIANCE_POLICY: "pqc"
`
		}, nil)).
		Setup(crd.DeployGatewayAPI).
		SetupParallel(
			namespace.Setup(&internalNs, namespace.Config{
				Prefix: "internal",
				Labels: map[string]string{
					label.IoIstioDataplaneMode.Name: "ambient",
				},
			}),
			namespace.Setup(&externalNs, namespace.Config{
				Prefix: "external",
				Labels: map[string]string{
					"istio.io/test-exclude-namespace": "true",
				},
			}),
		).
		Setup(setupAppsConfig).
		Setup(deployment.SetupTwoNamespaces(&apps, deployment.Config{
			Namespaces: []namespace.Getter{
				namespace.Future(&internalNs),
				namespace.Future(&externalNs),
			},
			Configs:             echo.ConfigFuture(&configs),
			NoExternalNamespace: true,
		})).
		Setup(func(ctx resource.Context) error {
			for _, echoInstance := range apps.All.Instances() {
				switch echoInstance.Config().Service {
				case deployment.ASvc:
					a = echoInstance
				case "server":
					server = echoInstance
				}
			}
			if a == nil {
				return fmt.Errorf("failed to find echo instance 'a'")
			}
			if server == nil {
				return fmt.Errorf("failed to find echo instance 'server'")
			}
			return nil
		}).
		Setup(func(ctx resource.Context) error {
			peerAuthYaml := `
apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: default
spec:
  mtls:
    mode: STRICT`
			return ctx.ConfigIstio().YAML(i.Settings().SystemNamespace, peerAuthYaml).Apply(apply.Wait)
		}).
		Run()
}

func setupAppsConfig(_ resource.Context) error {
	configs = []echo.Config{
		{
			Service:   deployment.ASvc,
			Namespace: internalNs,
			Ports:     ports.All(),
		},
		{
			Service:   "server",
			Namespace: externalNs,
			Ports:     ports.All(),
			TLSSettings: &common.TLSSettings{
				RootCert:         file.MustAsString(path.Join(env.IstioSrc, "tests/testdata/certs/dns/root-cert.pem")),
				ClientCert:       file.MustAsString(path.Join(env.IstioSrc, "tests/testdata/certs/dns/cert-chain.pem")),
				Key:              file.MustAsString(path.Join(env.IstioSrc, "tests/testdata/certs/dns/key.pem")),
				Hostname:         "server.default.svc",
				MinVersion:       "1.3",
				CurvePreferences: []string{"X25519MLKEM768"},
			},
			Subsets: []echo.SubsetConfig{{
				Annotations: map[string]string{annotation.SidecarInject.Name: "false"},
			}},
		},
	}
	return nil
}

func TestIngress(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			credName := "ingress-pqc-credential"
			host := "ingress-pqc.example.com"
			gatewayName := "ingress-pqc-credential"

			ingressutil.CreateIngressKubeSecretInNamespace(t, credName, ingressutil.TLS,
				ingressutil.IngressCredentialA, false, internalNs.Name())

			gatewayConfig := fmt.Sprintf(`
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: %s
spec:
  gatewayClassName: istio
  listeners:
  - name: https
    hostname: "%s"
    port: 443
    protocol: HTTPS
    tls:
      mode: Terminate
      certificateRefs:
      - kind: Secret
        name: %s
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: ingress-pqc-route
spec:
  parentRefs:
  - name: %s
  hostnames:
  - "%s"
  rules:
  - backendRefs:
    - name: %s
      port: 80
`, gatewayName, host, credName, gatewayName, host, deployment.ASvc)

			t.ConfigIstio().YAML(internalNs.Name(), gatewayConfig).ApplyOrFail(t, apply.Wait)

			svcName := types.NamespacedName{
				Name:      gatewayName + "-istio",
				Namespace: internalNs.Name(),
			}
			ing := i.CustomIngressFor(t.Clusters().Default(), svcName, "gateway.networking.k8s.io/gateway-name="+gatewayName)

			t.NewSubTest("request with TLS 1.3 and X25519MLKEM768 succeeds").Run(func(t framework.TestContext) {
				ing.CallOrFail(t, echo.CallOptions{
					HTTP: echo.HTTP{
						Headers: headers.New().WithHost(host).Build(),
					},
					Port: echo.Port{
						Protocol: protocol.HTTPS,
					},
					TLS: echo.TLS{
						CaCert:           ingressutil.CaCertA,
						MinVersion:       "1.3",
						CurvePreferences: []string{"X25519MLKEM768"},
					},
					Check: check.Status(http.StatusOK),
				})
			})

			t.NewSubTest("request with non-PQC curve is rejected").Run(func(t framework.TestContext) {
				ing.CallOrFail(t, echo.CallOptions{
					HTTP: echo.HTTP{
						Headers: headers.New().WithHost(host).Build(),
					},
					Port: echo.Port{
						Protocol: protocol.HTTPS,
					},
					TLS: echo.TLS{
						CaCert:           ingressutil.CaCertA,
						MinVersion:       "1.3",
						CurvePreferences: []string{"P-256"},
					},
					Timeout: 5 * time.Second,
					Check:   check.TLSHandshakeFailure(),
				})
			})
		})
}

func TestWaypoint(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			serviceEntryYaml := `
apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: external
spec:
  hosts:
  - server.{{.ExternalNamespace}}.svc.cluster.local
  ports:
  - name: https
    number: 8443
    protocol: HTTPS
  location: MESH_EXTERNAL
  resolution: DNS
  endpoints:
  - address: server.{{.ExternalNamespace}}.svc.cluster.local`
			t.ConfigIstio().
				Eval(internalNs.Name(), map[string]string{
					"ExternalNamespace": externalNs.Name(),
				}, serviceEntryYaml).
				ApplyOrFail(t)

			t.NewSubTest("TLS connection with PQC-compliant settings should succeed").Run(func(t framework.TestContext) {
				a.CallOrFail(t, echo.CallOptions{
					To: server,
					Port: echo.Port{
						Protocol: protocol.HTTPS,
					},
					TLS: echo.TLS{
						ServerName:       "server.default.svc",
						CaCert:           file.MustAsString(path.Join(env.IstioSrc, "tests/testdata/certs/dns/root-cert.pem")),
						MinVersion:       "1.3",
						CurvePreferences: []string{"X25519MLKEM768"},
					},
					Timeout: 1 * time.Second,
					Check:   check.OK(),
				})
			})

			t.NewSubTest("TLS connection with wrong curve preference should fail").Run(func(t framework.TestContext) {
				a.CallOrFail(t, echo.CallOptions{
					To: server,
					Port: echo.Port{
						Protocol: protocol.HTTPS,
					},
					TLS: echo.TLS{
						CaCert:           file.MustAsString(path.Join(env.IstioSrc, "tests/testdata/certs/dns/root-cert.pem")),
						MinVersion:       "1.3",
						CurvePreferences: []string{"P-256"},
					},
					Timeout: 1 * time.Second,
					Check:   check.TLSHandshakeFailure(),
				})
			})

			t.NewSubTest("TLS connection originated by the waypoint should succeed").Run(func(t framework.TestContext) {
				egressNamespace, err := namespace.Claim(t, namespace.Config{Prefix: "egress"})
				if err != nil {
					t.Fatal(err)
				}

				waypointYaml := `
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: egress-gateway
spec:
  gatewayClassName: istio-waypoint
  listeners:
  - name: mesh
    port: 15008
    protocol: HBONE
    allowedRoutes:
      namespaces:
        from: Selector
        selector:
          matchLabels:
            kubernetes.io/metadata.name: "{{.}}"`
				t.ConfigIstio().
					Eval(egressNamespace.Name(), internalNs.Name(), waypointYaml).
					ApplyOrFail(t, apply.CleanupConditionally)

				serviceEntryWithWaypointYaml := `
apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: external
  labels:
    istio.io/use-waypoint: egress-gateway
    istio.io/use-waypoint-namespace: {{.EgressNamespace}}
spec:
  hosts:
  - server.{{.ExternalNamespace}}.svc.cluster.local
  ports:
  - name: http
    number: 80
    protocol: HTTP
    targetPort: 443
  location: MESH_EXTERNAL
  resolution: DNS
  endpoints:
  - address: server.{{.ExternalNamespace}}.svc.cluster.local`
				t.ConfigIstio().
					Eval(internalNs.Name(), map[string]string{
						"ExternalNamespace": externalNs.Name(),
						"EgressNamespace":   egressNamespace.Name(),
					}, serviceEntryWithWaypointYaml).
					ApplyOrFail(t)

				rootCert := file.AsStringOrFail(t, path.Join(env.IstioSrc, "tests/testdata/certs/dns/root-cert.pem"))
				caConfigMap := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: external-ca-cert
data:
  ca.crt: |
{{.RootCert | indent 4}}`
				t.ConfigIstio().
					Eval(internalNs.Name(), map[string]any{"RootCert": rootCert}, caConfigMap).
					ApplyOrFail(t, apply.CleanupConditionally)

				backendTLSPolicy := `
apiVersion: gateway.networking.k8s.io/v1
kind: BackendTLSPolicy
metadata:
  name: external-tls
spec:
  targetRefs:
  - group: networking.istio.io
    kind: ServiceEntry
    name: external
    sectionName: http
  validation:
    hostname: server.default.svc
    caCertificateRefs:
    - kind: ConfigMap
      name: external-ca-cert
      group: ""`
				t.ConfigIstio().
					YAML(internalNs.Name(), backendTLSPolicy).
					ApplyOrFail(t, apply.CleanupConditionally)

				httpRoute := fmt.Sprintf(`
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: egress-route
  namespace: %s
spec:
  parentRefs:
  - name: external
    kind: ServiceEntry
    group: networking.istio.io
  rules:
  - filters:
    - type: RequestHeaderModifier
      requestHeaderModifier:
        add:
        - name: handled-by-egress-gateway
          value: "true"
    backendRefs:
    - name: server.%s.svc.cluster.local
      kind: Hostname
      group: networking.istio.io
      port: 80
`, internalNs.Name(), externalNs.Name())
				t.ConfigIstio().
					YAML(internalNs.Name(), httpRoute).
					ApplyOrFail(t, apply.CleanupConditionally)

				a.CallOrFail(t, echo.CallOptions{
					Address: fmt.Sprintf("server.%s.svc.cluster.local", externalNs.Name()),
					Port:    echo.Port{ServicePort: 80},
					Scheme:  scheme.HTTP,
					Count:   1,
					Check: check.And(
						check.OK(),
						check.Each(func(r echot.Response) error {
							if _, f := r.RequestHeaders["Handled-By-Egress-Gateway"]; !f {
								return fmt.Errorf("expected request to be handled by egress gateway, response: %s", r)
							}
							return nil
						}),
					),
				})
			})
		})
}
