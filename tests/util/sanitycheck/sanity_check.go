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
	"istio.io/api/label"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/scopes"
)

// RunTrafficTest deploys echo server/client and runs an Istio traffic test
func RunTrafficTest(t framework.TestContext, ambient bool) {
	scopes.Framework.Infof("running sanity test")
	_, client, server := setupTrafficTest(t, "", ambient)
	RunTrafficTestClientServer(t, client, server)
}

func SetupTrafficTestAmbient(t framework.TestContext, revision string) (namespace.Instance, echo.Instance, echo.Instance) {
	return setupTrafficTest(t, revision, true)
}

func SetupTrafficTest(t framework.TestContext, revision string) (namespace.Instance, echo.Instance, echo.Instance) {
	return setupTrafficTest(t, revision, false)
}

func setupTrafficTest(t framework.TestContext, revision string, ambient bool) (namespace.Instance, echo.Instance, echo.Instance) {
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
	deployment.New(t).
		With(&client, echo.Config{
			Service:        "client",
			Namespace:      testNs,
			Ports:          []echo.Port{},
			Subsets:        subsetConfig,
			ServiceAccount: true,
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
			Subsets: subsetConfig,
		}).
		BuildOrFail(t)

	return testNs, client, server
}

func BlockTestWithPolicy(t framework.TestContext, client, server echo.Instance) {
	ns := server.Config().Namespace.Name()
	t.ConfigIstio().Eval(ns, map[string]any{"serviceAccount": client.ServiceAccountName()}, `
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
	// _, err := server.Config().Cluster.Istio().SecurityV1().AuthorizationPolicies(ns).Create(
	// 	t.Context(),
	// 	&v1.AuthorizationPolicy{
	// 		ObjectMeta: metav1.ObjectMeta{
	// 			Name: "block-sanity-test",
	// 		},
	// 		Spec: v1beta1.AuthorizationPolicy{
	// 			Action: v1beta1.AuthorizationPolicy_DENY,
	// 			Rules: []*v1beta1.Rule{
	// 				{
	// 					From: []*v1beta1.Rule_From{
	// 						{
	// 							Source: &v1beta1.Source{
	// 								Principals: []string{client.ServiceAccountName()},
	// 							},
	// 						},
	// 					},
	// 				},
	// 			},
	// 		},
	// 	},
	// 	metav1.CreateOptions{},
	// )
	// if err != nil {
	// 	t.Fatal(err)
	// }
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
