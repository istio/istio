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

package sidecarwaypoint

import (
	"testing"

	"istio.io/api/label"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/ambient"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	testlabel "istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/tests/integration/security/util/cert"
)

var i istio.Instance

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(testlabel.CustomSetup).
		RequireMinVersion(31).
		Setup(func(ctx resource.Context) error {
			ctx.Settings().Ambient = true
			ctx.Settings().SkipVMs()

			return nil
		}).
		Setup(istio.Setup(&i, func(_ resource.Context, cfg *istio.Config) {
			cfg.EnableCNI = true
			cfg.DeployEastWestGW = false
			cfg.DeployGatewayAPI = true
			cfg.ControlPlaneValues = `
meshConfig:
  accessLogFile: /dev/stdout
values:
  pilot:
    env:
      ENABLE_SIDECAR_WAYPOINT_ROUTING: "true"
  cni:
    repair:
      enabled: false
  ztunnel:
    terminationGracePeriodSeconds: 5
`
		}, cert.CreateCASecretAlt)).
		Run()
}

func TestL7PolicyEnforcedOnce(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		testNamespace := namespace.NewOrFail(t, namespace.Config{
			Prefix: "sidecar-waypoint",
			Inject: false,
			Labels: map[string]string{
				label.IoIstioDataplaneMode.Name: constants.DataplaneModeAmbient,
			},
		})

		var client, server echo.Instance
		deployment.New(t).
			With(&client, echo.Config{
				Service:        "client",
				Namespace:      testNamespace,
				ServiceAccount: true,
				Ports:          []echo.Port{},
				Subsets: []echo.SubsetConfig{
					{
						Labels: map[string]string{
							"sidecar.istio.io/inject":       "true",
							label.IoIstioDataplaneMode.Name: constants.DataplaneModeNone,
						},
					},
				},
			}).
			With(&server, echo.Config{
				Service:        "server",
				Namespace:      testNamespace,
				ServiceAccount: true,
				ServiceLabels: map[string]string{
					label.IoIstioUseWaypoint.Name: "waypoint",
				},
				Ports: []echo.Port{
					{
						Name:         "http",
						Protocol:     protocol.HTTP,
						ServicePort:  80,
						WorkloadPort: 8090,
					},
				},
			}).
			BuildOrFail(t)

		if _, err := ambient.NewWaypointProxyWithTrafficType(t, testNamespace, "waypoint", constants.ServiceTraffic); err != nil {
			t.Fatal(err)
		}

		t.ConfigIstio().Eval(testNamespace.Name(), map[string]string{
			"Service": server.Config().Service,
		}, `apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: add-policy-header
spec:
  parentRefs:
  - name: {{.Service}}
    kind: Service
    group: ""
    port: 80
  rules:
  - filters:
    - type: RequestHeaderModifier
      requestHeaderModifier:
        add:
        - name: x-policy-applied
          value: once
    backendRefs:
    - name: {{.Service}}
      port: 80
`).ApplyOrFail(t)

		client.CallOrFail(t, echo.CallOptions{
			To:   server,
			Port: echo.Port{Name: "http"},
			Check: check.And(
				check.OK(),
				check.RequestHeader("x-policy-applied", "once"),
			),
		})
	})
}
