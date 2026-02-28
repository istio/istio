//go:build integ

//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package pqc

import (
	"fmt"
	"net/http"
	"path"
	"testing"
	"time"

	"istio.io/api/annotation"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/http/headers"
	echoClient "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/util/file"
	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"
)

var (
	i          istio.Instance
	externalNs namespace.Instance
	internalNs namespace.Instance
	a          echo.Instance
	server     echo.Instance
	configs    []echo.Config
	apps       deployment.TwoNamespaceView
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(label.CustomSetup).
		Setup(istio.Setup(&i, func(ctx resource.Context, cfg *istio.Config) {
			cfg.ControlPlaneValues = `
values:
  pilot:
    env:
      COMPLIANCE_POLICY: "pqc"`
		}, nil)).
		SetupParallel(
			namespace.Setup(&internalNs, namespace.Config{Prefix: "internal", Inject: true}),
			namespace.Setup(&externalNs, namespace.Config{Prefix: "external", Inject: false}),
		).
		Setup(setupAppsConfig).
		Setup(deployment.SetupTwoNamespaces(&apps, deployment.Config{
			Configs:             echo.ConfigFuture(&configs),
			Namespaces:          []namespace.Getter{namespace.Future(&internalNs), namespace.Future(&externalNs)},
			NoExternalNamespace: true,
		})).
		Setup(func(ctx resource.Context) error {
			for _, echoInstance := range apps.All.Instances() {
				switch echoInstance.Config().Service {
				case "server":
					server = echoInstance
				case deployment.ASvc:
					a = echoInstance
				}
			}
			if server == nil || a == nil {
				return fmt.Errorf("failed to find all expected echo instances")
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
		{
			Service:   deployment.ASvc,
			Namespace: internalNs,
			Ports:     ports.All(),
		},
	}
	return nil
}

func TestIngress(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			gatewayTmpl := `
apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: {{ .CredentialName }}
spec:
  selector:
    istio: {{ .GatewayIstioLabel | default "ingressgateway" }}
  servers:
  - port:
      number: 443
      name: https
      protocol: HTTPS
    tls:
      mode: SIMPLE
      credentialName: "{{ .CredentialName }}"
    hosts:
    - "{{ .Host }}"
---
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: {{ .CredentialName }}
spec:
  hosts:
  - "{{ .Host }}"
  gateways:
  - {{ .CredentialName }}
  http:
  - route:
    - destination:
        host: {{ .ServiceName }}
        port:
          number: 80
`
			credName := "ingress-pqc-credential"
			host := "ingress-pqc.example.com"
			ingressConfig := map[string]string{
				"CredentialName": credName,
				"Host":           host,
				"ServiceName":    fmt.Sprintf("%s.%s.svc.cluster.local", a.Config().Service, internalNs.Name()),
				"GatewayLabel":   i.Settings().IngressGatewayIstioLabel,
			}
			t.ConfigIstio().Eval(internalNs.Name(), ingressConfig, gatewayTmpl).ApplyOrFail(t, apply.Wait)
			ingressutil.CreateIngressKubeSecret(t, credName, ingressutil.TLS, ingressutil.IngressCredentialA, false)

			ing := i.IngressFor(t.Clusters().Default())
			if ing == nil {
				t.Fatalf("failed to find ingress gateway for cluster %s", t.Clusters().Default().Name())
			}

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
					Timeout: 1 * time.Second,
					Check:   check.TLSHandshakeFailure(),
				})
			})
		})
}

func TestEgress(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			egressTmpl := `
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: mesh-to-egress-gateway
spec:
  hosts:
  - {{ .ServerHost }}
  gateways:
  - mesh
  http:
  - match:
    - port: 80
    route:
    - destination:
        host: {{ .EgressService | default "istio-egressgateway" }}.{{ .EgressNamespace | default "istio-system" }}.svc.cluster.local
        port:
          number: 443
---
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: originate-mtls-to-egress-gateway
spec:
  host: {{ .EgressService | default "istio-egressgateway "}}.{{ .EgressNamespace | default "istio-system" }}.svc.cluster.local
  trafficPolicy:
    portLevelSettings:
    - port:
        number: 443
      tls:
        mode: ISTIO_MUTUAL
        sni: {{ .ServerHost }}
---
apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: egress-gateway
spec:
  selector:
    istio: {{ .EgressLabel | default "egressgateway" }}
  servers:
  - port:
      number: 443
      name: https
      protocol: HTTPS
    hosts:
    - {{ .ServerHost }}
    tls:
      mode: ISTIO_MUTUAL
---
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: egress-gateway-to-server
spec:
  hosts:
  - {{ .ServerHost }}
  gateways:
  - egress-gateway
  http:
  - match:
    - port: 443
    route:
    - destination:
        host: {{ .ServerHost }}
        port:
          number: 443
    headers:
      request:
        add:
          handled-by-egress-gateway: "true"
---
`
			drTmpl := `
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: originate-tls-to-server
spec:
  host: {{ .ServerHost }}
  trafficPolicy:
    portLevelSettings:
    - port:
        number: 443
      tls:
        mode: SIMPLE
        credentialName: {{ .CredentialName }}
        sni: server.default.svc
`

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

			t.NewSubTest("TLS connection originated by the egress gateway should succeed").Run(func(t framework.TestContext) {
				credName := "external-server-ca"
				ingressutil.CreateIngressKubeSecret(t, credName, ingressutil.TLS, ingressutil.IngressCredential{
					CaCert: file.AsStringOrFail(t, path.Join(env.IstioSrc, "tests/testdata/certs/dns/root-cert.pem")),
				}, false)

				egressConfig := map[string]string{
					"EgressLabel":     i.Settings().EgressGatewayIstioLabel,
					"EgressService":   i.Settings().EgressGatewayServiceName,
					"EgressNamespace": i.Settings().EgressGatewayServiceNamespace,
					"ServerHost":      server.Config().ClusterLocalFQDN(),
					"CredentialName":  credName,
				}
				t.ConfigIstio().Eval(internalNs.Name(), egressConfig, egressTmpl).ApplyOrFail(t, apply.Wait)
				t.ConfigIstio().Eval(i.Settings().SystemNamespace, egressConfig, drTmpl).ApplyOrFail(t, apply.Wait)

				a.CallOrFail(t, echo.CallOptions{
					To: server,
					Port: echo.Port{
						Protocol: protocol.HTTP,
					},
					Check: check.And(
						check.OK(),
						check.Each(func(r echoClient.Response) error {
							if _, f := r.RequestHeaders["Handled-By-Egress-Gateway"]; !f {
								return fmt.Errorf("expected request to be handled by egress gateway, response: %s", r)
							}
							return nil
						})),
				})
			})
		})
}
