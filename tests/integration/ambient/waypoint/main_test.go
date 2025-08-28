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

package waypoint

import (
	"context"
	"fmt"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ilabel "istio.io/api/label"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/crd"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	deploy "istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	kubetest "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/retry"
)

var i istio.Instance

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
			if ctx.Settings().AmbientMultiNetwork {
				cfg.SkipDeployCrossClusterSecrets = true
			}
			cfg.EnableCNI = true
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
  ztunnel:
    terminationGracePeriodSeconds: 5
  gateways:
    istio-ingressgateway:
      enabled: false
    istio-egressgateway:
      enabled: false
      `
		})).
		Run()
}

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

func TestCrossNamespaceWaypoint(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			// Steps:
			// 1. create namespace for the waypoint
			// 2. deploy a gateway to the namespace
			// 3. label the app service for waypoint usage
			// 4. send a request from within the mesh to the app

			// pre-req setup instead of using the full echo suite
			crd.DeployGatewayAPIOrSkip(t)
			testNs, client, server := setupSmallTrafficTest(t)

			// step 1
			ns, err := t.Clusters().Default().Kube().CoreV1().Namespaces().Create(context.Background(), &v1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "waypoint",
				},
			}, metav1.CreateOptions{})
			if err != nil {
				t.Fatal(err)
			}

			// step 2
			t.ConfigIstio().YAML(ns.Name, fmt.Sprintf(`
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: crossns-waypoint
  namespace: %s
  annotations:
    networking.istio.io/service-type: ClusterIP
spec:
  gatewayClassName: istio-waypoint
  listeners:
  - allowedRoutes:
      namespaces:
        from: All
    name: mesh
    port: 15008
    protocol: HBONE
`, ns.Name)).ApplyOrFail(t)

			retry.UntilSuccessOrFail(t, func() error {
				return checkWaypointIsReady(t, ns.Name, "crossns-waypoint")
			}, retry.Timeout(2*time.Minute))

			// apply a route that adds a custom response header when
			// the request goes through the waypoint
			// this is used to prevent the test from prematurely succeeding
			// due to the configuration not being processed before
			// the request is sent
			t.ConfigIstio().YAML(testNs.Name(), fmt.Sprintf(`
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: httproute
  namespace: %s
spec:
  parentRefs:
    - name: server
      kind: Service
      group: ""
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /
      backendRefs:
        - name:  server
          port: 80
      filters:
        - type: ResponseHeaderModifier
          responseHeaderModifier:
            add:
              - name: X-Custom-Traversed-Waypoint
                value: crossns-waypoint
`, testNs.Name())).ApplyOrFail(t)

			// step 3
			svc, err := t.Clusters().Default().Kube().CoreV1().Services(server.NamespaceName()).Get(context.Background(), server.ServiceName(), metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}

			svc.Labels[ilabel.IoIstioUseWaypointNamespace.Name] = ns.Name
			svc.Labels[ilabel.IoIstioUseWaypoint.Name] = "crossns-waypoint"
			_, err = t.Clusters().Default().Kube().CoreV1().Services(server.NamespaceName()).Update(context.Background(), svc, metav1.UpdateOptions{})
			if err != nil {
				t.Fatal(err)
			}

			// wait for waypoint configuration to be successfully applied
			retry.UntilSuccessOrFail(t, func() error {
				s, err := t.Clusters().Default().Kube().CoreV1().Services(server.NamespaceName()).Get(context.Background(), server.ServiceName(), metav1.GetOptions{})
				if err != nil {
					return err
				}
				t.Log(s.Status.Conditions)

				for _, cond := range s.Status.Conditions {
					if cond.Type == string(model.WaypointBound) {
						if cond.Status == metav1.ConditionTrue {
							return nil
						}
						return fmt.Errorf("waypoint bound status does not equal true")
					}
				}

				return fmt.Errorf("waypoint condition not found on service")
			}, retry.Timeout(1*time.Minute))

			// step 4
			client.CallOrFail(t, echo.CallOptions{
				To:      server,
				Address: fmt.Sprintf("%s.%s.svc.cluster.local", server.ServiceName(), server.NamespaceName()),
				Port:    server.PortForName("http"),
				Scheme:  scheme.HTTP,
				Count:   5,
				Check: check.And(
					check.OK(),
					check.ResponseHeader("X-Custom-Traversed-Waypoint", "crossns-waypoint"),
				),
			})
		})
}

func checkWaypointIsReady(t framework.TestContext, ns, name string) error {
	fetch := kubetest.NewPodFetch(t.AllClusters()[0], ns, ilabel.IoK8sNetworkingGatewayGatewayName.Name+"="+name)
	_, err := kubetest.CheckPodsAreReady(fetch)
	return err
}
