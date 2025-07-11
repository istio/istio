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

package sanitycheck

import (
	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/label"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/ambient"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/k8sgateway"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/scopes"
)

// RunTrafficTest deploys echo server/client and runs an Istio traffic test
func RunTrafficTest(t framework.TestContext, ambient bool) {
	scopes.Framework.Infof("running sanity test")
	_, client, server, _ := setupTrafficTest(t, "", ambient)
	RunTrafficTestClientServer(t, client, server)
}

func SetupTrafficTestAmbient(t framework.TestContext, revision string) (namespace.Instance,
	echo.Instance, echo.Instance, map[types.NamespacedName]ambient.WaypointProxy,
) {
	return setupTrafficTest(t, revision, true)
}

func SetupTrafficTest(t framework.TestContext, revision string) (namespace.Instance,
	echo.Instance, echo.Instance,
) {
	ns, client, server, _ := setupTrafficTest(t, revision, false)
	return ns, client, server
}

func setupTrafficTest(t framework.TestContext, revision string, ambient bool) (namespace.Instance,
	echo.Instance, echo.Instance, map[types.NamespacedName]ambient.WaypointProxy,
) {
	var client, server echo.Instance
	nsConfig := namespace.Config{
		Prefix:   "default",
		Revision: revision,
		Inject:   true,
	}
	var subsetConfig []echo.SubsetConfig
	if ambient {
		nsConfig.Inject = false
		nsConfig.Labels = map[string]string{
			label.IoIstioDataplaneMode.Name: "ambient",
		}
		// not really needed from Istio POV, but the test will add the `istio-proxy` container if we don't tell it not to.
		subsetConfig = []echo.SubsetConfig{{
			Annotations: map[string]string{label.SidecarInject.Name: "false"},
		}}
	}
	testNs := namespace.NewOrFail(t, nsConfig)
	serverCfg := echo.Config{
		Service:   "server",
		Namespace: testNs,
		Ports: []echo.Port{
			{
				Name:         "http",
				Protocol:     protocol.HTTP,
				WorkloadPort: 8090,
			},
		},
		Subsets: subsetConfig,
	}
	if ambient {
		serverCfg.ServiceLabels = map[string]string{label.IoIstioUseWaypoint.Name: "waypoint"}
		serverCfg.ServiceAccount = true
		serverCfg.ServiceWaypointProxy = "waypoint"
	}
	deployment.New(t).
		With(&client, echo.Config{
			Service:        "client",
			Namespace:      testNs,
			Ports:          []echo.Port{},
			Subsets:        subsetConfig,
			ServiceAccount: true,
		}).
		With(&server, serverCfg).
		BuildOrFail(t)

	// TODO: this should really be a part of the echo deployment, but it will be a heavy lift.
	// for now, let's make sure sanity checks can include ready waypoints.
	waypoints := buildWaypointsOrFail(t, echo.Instances{client, server})

	for nsName := range waypoints {
		for _, cls := range t.AllClusters() {
			k8sgateway.VerifyGatewaysProgrammed(t, cls, []types.NamespacedName{nsName})
		}
	}

	return testNs, client, server, waypoints
}

func buildWaypointsOrFail(t framework.TestContext, echos echo.Instances) map[types.NamespacedName]ambient.WaypointProxy {
	waypoints := make(map[types.NamespacedName]ambient.WaypointProxy)
	// for _, echo := range echos {
	// 	svcwp := types.NamespacedName{
	// 		Name:      echo.Config().ServiceWaypointProxy,
	// 		Namespace: echo.NamespacedName().Namespace.Name(),
	// 	}
	// 	wlwp := types.NamespacedName{
	// 		Name:      echo.Config().WorkloadWaypointProxy,
	// 		Namespace: echo.NamespacedName().Namespace.Name(),
	// 	}
	// 	if svcwp.Name != "" {
	// 		if _, found := waypoints[svcwp]; !found {
	// 			var err error
	// 			waypoints[svcwp], err = ambient.NewWaypointProxy(t, echo.NamespacedName().Namespace, svcwp.Name)
	// 			if err != nil {
	// 				t.Fatal(err)
	// 			}
	// 		}
	// 	}
	// 	if wlwp.Name != "" {
	// 		if _, found := waypoints[wlwp]; !found {
	// 			var err error
	// 			waypoints[wlwp], err = ambient.NewWaypointProxy(t, echo.NamespacedName().Namespace, wlwp.Name)
	// 			if err != nil {
	// 				t.Fatal(err)
	// 			}
	// 		}
	// 	}
	// }
	return waypoints
}

func BlockTestWithPolicy(t framework.TestContext, client, server echo.Instance) {
	ns := server.Config().Namespace.Name()
	t.ConfigIstio().Eval(ns, map[string]any{"serviceAccount": client.SpiffeIdentity()}, `
apiVersion: security.istio.io/v1
kind: AuthorizationPolicy
metadata:
  name: block-sanity-test
spec:
  action: DENY
  rules:
  - from:
    - source:
        principals:
        - {{ .serviceAccount }}`).ApplyOrFail(t)
}

func RunTrafficTestClientServer(t framework.TestContext, client, server echo.Instance) {
	_ = client.CallOrFail(t, echo.CallOptions{
		To:    server,
		Count: 1,
		Port: echo.Port{
			Name: "http",
		},
		Check: check.OK(),
	})
}

func RunTrafficTestClientServerExpectFail(t framework.TestContext, client, server echo.Instance) {
	_ = client.CallOrFail(t, echo.CallOptions{
		To:    server,
		Count: 1,
		Port: echo.Port{
			Name: "http",
		},
		Check: check.Error(),
	})
}
