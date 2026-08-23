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

package agentgateway

import (
	"testing"

	ilabel "istio.io/api/label"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	deploy "istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	kubetest "istio.io/istio/pkg/test/kube"
	"istio.io/istio/tests/integration/security/util/cert"
)

var i istio.Instance

// TestMain installs an ambient control plane with the agentgateway controller enabled so that
// agentgateway can be deployed as a waypoint. It mirrors the ambient waypoint suite, adding the
// PILOT_ENABLE_AGENTGATEWAY flag which (together with the ambient defaults) registers the
// istio-agentgateway-waypoint GatewayClass.
func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(label.CustomSetup).
		Setup(func(t resource.Context) error {
			t.Settings().Ambient = true
			t.Settings().SkipTProxy = true
			return nil
		}).
		Setup(istio.Setup(&i, func(ctx resource.Context, cfg *istio.Config) {
			ctx.Settings().SkipVMs()
			ctx.Settings().SkipTProxy = true
			cfg.EnableCNI = false
			cfg.DeployEastWestGW = false
			cfg.DeployGatewayAPI = true
			cfg.ControlPlaneValues = `
profile: ambient
meshConfig:
  accessLogFile: "/dev/stdout"
values:
  pilot:
    env:
      PILOT_SKIP_VALIDATE_TRUST_DOMAIN: "true"
      PILOT_ENABLE_AGENTGATEWAY: "true"
  ztunnel:
    terminationGracePeriodSeconds: 5
  gateways:
    istio-ingressgateway:
      enabled: false
    istio-egressgateway:
      enabled: false
      `
			if ctx.Settings().NativeNftables {
				cfg.ControlPlaneValues += `
  global:
    nativeNftables: true
`
			}
		}, cert.CreateCASecretAlt)).
		Run()
}

// setupSmallTrafficTest deploys a minimal client/server pair into an ambient namespace, rather than
// using the full echo suite, so the agentgateway waypoint tests stay self-contained and fast.
func setupSmallTrafficTest(t framework.TestContext) (namespace.Instance, echo.Instance, echo.Instance) {
	var client, server echo.Instance
	testNs := namespace.NewOrFail(t, namespace.Config{
		Prefix: "default",
		Inject: false,
		Labels: map[string]string{
			ilabel.IoIstioDataplaneMode.Name: "ambient",
			"istio-injection":                "disabled",
		},
	})
	deploy.New(t).
		With(&client, echo.Config{
			Service:   "client",
			Namespace: testNs,
			Ports:     []echo.Port{},
		}).
		With(&server, echo.Config{
			Service:   "server",
			Namespace: testNs,
			Ports: []echo.Port{
				{
					Name:         "http",
					Protocol:     protocol.HTTP,
					WorkloadPort: 8090,
				},
			},
		}).
		BuildOrFail(t)

	return testNs, client, server
}

// checkWaypointIsReady returns nil once the waypoint's pod is ready.
func checkWaypointIsReady(t framework.TestContext, ns, name string) error {
	fetch := kubetest.NewPodFetch(t.AllClusters()[0], ns, ilabel.IoK8sNetworkingGatewayGatewayName.Name+"="+name)
	_, err := kubetest.CheckPodsAreReady(fetch)
	return err
}
