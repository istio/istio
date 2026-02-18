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
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/label"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/http/headers"
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
	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"
)

var (
	i          istio.Instance
	internalNs namespace.Instance
	a          echo.Instance
	configs    []echo.Config
	apps       deployment.SingleNamespaceView
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(testlabel.CustomSetup).
		Setup(func(ctx resource.Context) error {
			ctx.Settings().Ambient = true
			ctx.Settings().SkipVMs()
			return crd.DeployGatewayAPI(ctx)
		}).
		Setup(istio.Setup(&i, func(ctx resource.Context, cfg *istio.Config) {
			cfg.EnableCNI = true
			cfg.DeployEastWestGW = false
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
		Setup(namespace.Setup(&internalNs, namespace.Config{
			Prefix: "internal",
			Inject: false,
			Labels: map[string]string{
				label.IoIstioDataplaneMode.Name: "ambient",
			},
		})).
		Setup(func(ctx resource.Context) error {
			return setupAppsConfig(ctx, &configs)
		}).
		Setup(deployment.SetupSingleNamespace(&apps, deployment.Config{
			Namespaces: []namespace.Getter{
				namespace.Future(&internalNs),
			},
			Configs:             echo.ConfigFuture(&configs),
			NoExternalNamespace: true,
		})).
		Setup(func(ctx resource.Context) error {
			for _, echoInstance := range apps.All.Instances() {
				if echoInstance.Config().Service == deployment.ASvc {
					a = echoInstance
					break
				}
			}
			if a == nil {
				return fmt.Errorf("failed to find echo instance")
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

func setupAppsConfig(_ resource.Context, out *[]echo.Config) error {
	*out = []echo.Config{
		{
			Service:        deployment.ASvc,
			Namespace:      internalNs,
			ServiceAccount: true,
			Ports:          ports.All(),
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
					Check:   checkTLSHandshakeFailure,
				})
			})
		})
}

func checkTLSHandshakeFailure(_ echo.CallResult, err error) error {
	if err == nil {
		return fmt.Errorf("expected to get TLS handshake error but got none")
	}
	if !strings.Contains(err.Error(), "tls: handshake failure") {
		return fmt.Errorf("expected to get TLS handshake error but got: %s", err)
	}
	return nil
}
