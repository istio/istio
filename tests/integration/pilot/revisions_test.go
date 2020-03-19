// Copyright 2020 Istio Authors
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
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource/environment"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
)

// TestMultiRevision Sets up a simple client -> server call, where the client and server
// belong to different control planes.
func TestMultiRevision(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {

			if err := istio.Setup(&i, func(cfg *istio.Config) {
				cfg.ControlPlaneValues = `
profile: empty
revision: canary
components:
  pilot:
    enabled: true
`
				cfg.PartialCleanup = true
			})(ctx); err != nil {
				t.Fatal(err)
			}
			stable := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "stable",
				Inject: true,
			})
			canary := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix:   "canary",
				Inject:   true,
				Revision: "canary",
			})

			var client, server echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&client, echoConfig(stable, "client")).
				With(&server, echoConfig(canary, "server")).
				BuildOrFail(t)
			retry.UntilSuccessOrFail(t, func() error {
				resp, err := client.Call(echo.CallOptions{
					Target:   server,
					PortName: "http",
				})
				if err != nil {
					return err
				}
				return resp.CheckOK()
			}, retry.Delay(time.Millisecond*100))
		})
}
