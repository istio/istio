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
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

// RunTrafficTest deploys echo server/client and runs an Istio traffic test
func RunTrafficTest(t framework.TestContext, ctx resource.Context) {
	scopes.Framework.Infof("running sanity test")
	_, client, server := SetupTrafficTest(t, ctx, "")
	RunTrafficTestClientServer(t, client, server)
}

func SetupTrafficTest(t framework.TestContext, ctx resource.Context, revision string) (namespace.Instance, echo.Instance, echo.Instance) {
	var client, server echo.Instance
	testNs := namespace.NewOrFail(t, ctx, namespace.Config{
		Prefix:   "default",
		Revision: revision,
		Inject:   true,
	})
	echoboot.NewBuilder(ctx).
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
					InstancePort: 8090,
				},
			},
		}).
		BuildOrFail(t)

	return testNs, client, server
}

func RunTrafficTestClientServer(t framework.TestContext, client, server echo.Instance) {
	_ = client.CallWithRetryOrFail(t, echo.CallOptions{
		Target:    server,
		PortName:  "http",
		Validator: echo.ExpectOK(),
	})
}
