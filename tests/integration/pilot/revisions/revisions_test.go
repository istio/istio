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

package revisions

import (
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
)

// TestMain defines the entrypoint for pilot tests using a standard Istio installation.
// If a test requires a custom install it should go into its own package, otherwise it should go
// here to reuse a single install across tests.
func TestMain(m *testing.M) {
	// nolint: staticcheck
	framework.
		NewSuite(m).
		RequireMultiPrimary().
		// Requires two CPs with specific names to be configured.
		Label(label.CustomSetup).
		Setup(istio.Setup(nil, func(_ resource.Context, cfg *istio.Config) {
			cfg.ControlPlaneValues = `
revision: stable
`
		})).
		Setup(istio.Setup(nil, func(_ resource.Context, cfg *istio.Config) {
			cfg.ControlPlaneValues = `
profile: empty
revision: canary
components:
  pilot:
    enabled: true
`
		})).
		Run()
}

// TestMultiRevision Sets up a simple client -> server call, where the client and server
// belong to different control planes.
func TestMultiRevision(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			stable := namespace.NewOrFail(t, t, namespace.Config{
				Prefix:   "stable",
				Inject:   true,
				Revision: "stable",
			})
			canary := namespace.NewOrFail(t, t, namespace.Config{
				Prefix:   "canary",
				Inject:   true,
				Revision: "canary",
			})

			echos := deployment.New(t).
				WithClusters(t.Clusters()...).
				WithConfig(echo.Config{
					Service:   "client",
					Namespace: stable,
					Ports:     []echo.Port{},
				}).
				WithConfig(echo.Config{
					Service:   "server",
					Namespace: canary,
					Ports: []echo.Port{
						{
							Name:         "http",
							Protocol:     protocol.HTTP,
							WorkloadPort: 8090,
						},
					},
				}).
				WithConfig(echo.Config{
					Service:    "vm",
					Namespace:  canary,
					DeployAsVM: true,
					Ports:      []echo.Port{},
				}).
				BuildOrFail(t)

			echotest.New(t, echos).
				ConditionallyTo(echotest.ReachableDestinations).
				ToMatch(match.ServiceName(echo.NamespacedName{Name: "server", Namespace: canary})).
				Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
					retry.UntilSuccessOrFail(t, func() error {
						result, err := from.Call(echo.CallOptions{
							To: to,
							Port: echo.Port{
								Name: "http",
							},
							Retry: echo.Retry{
								NoRetry: true,
							},
							Check: check.And(
								check.OK(),
								check.ReachedTargetClusters(t.AllClusters()),
							),
						})
						return check.And(
							check.NoError(),
							check.OK()).Check(result, err)
					}, retry.Delay(time.Millisecond*100))
				})
		})
}
