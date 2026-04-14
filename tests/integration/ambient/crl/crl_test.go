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

package crl

import (
	"fmt"
	"testing"
	"time"

	"istio.io/api/label"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/security/crl/util"
)

// TestZtunnelCRL verifies that ztunnel respects CRL revocation
// The test verifies that:
//  1. Initial call succeeds before CRL update
//  2. After certificate revocation, deploy NEW apps that get certs from revoked CA
//  3. Calls between new apps fail due to revoked certificate
func TestZtunnelCRL(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			// test pre-revocation apps (should succeed)
			opts := echo.CallOptions{
				To: server,
				Port: echo.Port{
					Name: "http",
				},
				Scheme:                  scheme.HTTP,
				NewConnectionPerRequest: true,
				Count:                   1,
			}

			t.Logf("testing initial call before CRL update")
			client.CallOrFail(t, opts)
			t.Logf("initial call succeeded")

			// revoke the intermediate certificate
			util.RevokeIntermediate(t, certBundle)

			// deploy NEW apps and test (should fail)
			// create new namespaces for post-revocation apps
			revokedClientNS := namespace.NewOrFail(t, namespace.Config{
				Prefix: "ambient-client-revoked",
				Inject: false,
				Labels: map[string]string{
					label.IoIstioDataplaneMode.Name: "ambient",
				},
			})
			revokedServerNS := namespace.NewOrFail(t, namespace.Config{
				Prefix: "ambient-server-revoked",
				Inject: false,
				Labels: map[string]string{
					label.IoIstioDataplaneMode.Name: "ambient",
				},
			})

			revokedClient, revokedServer := deployRevokedApps(t, revokedClientNS, revokedServerNS)

			// test should fail due to revoked cert; each retry restarts the client
			// to force a new HBONE connection with a fresh TLS handshake
			revokedOpts := echo.CallOptions{
				To: revokedServer,
				Port: echo.Port{
					Name: "http",
				},
				Scheme:                  scheme.HTTP,
				NewConnectionPerRequest: true,
				Count:                   1,
				Retry:                   echo.Retry{NoRetry: true},
			}
			t.Logf("testing call with revoked apps, expecting failure")
			retry.UntilSuccessOrFail(t, func() error {
				t.Logf("restarting revoked client to force new HBONE connection")
				if err := revokedClient.Restart(); err != nil {
					return fmt.Errorf("failed to restart revoked client: %v", err)
				}
				_, err := revokedClient.Call(revokedOpts)
				if err == nil {
					return fmt.Errorf("expected error but call succeeded, CRL may not have propagated yet")
				}
				return nil
			}, retry.Timeout(5*time.Minute), retry.Delay(5*time.Second))
			t.Logf("call correctly failed with revoked apps")
		})
}

// deployRevokedApps deploys new echo apps in the given namespaces
// these apps will get certificates from the now-revoked CA chain
func deployRevokedApps(t framework.TestContext, clientNS, serverNS namespace.Instance) (echo.Instance, echo.Instance) {
	t.Helper()

	var revokedClient, revokedServer echo.Instance

	deployment.New(t).
		With(&revokedClient, echo.Config{
			Service:        "ambient-client-revoked",
			Namespace:      clientNS,
			ServiceAccount: true,
			Ports: []echo.Port{{
				Name:         "http",
				Protocol:     protocol.HTTP,
				WorkloadPort: 8080,
			}},
		}).
		With(&revokedServer, echo.Config{
			Service:        "ambient-server-revoked",
			Namespace:      serverNS,
			ServiceAccount: true,
			Ports: []echo.Port{{
				Name:         "http",
				Protocol:     protocol.HTTP,
				WorkloadPort: 8080,
			}},
		}).
		BuildOrFail(t)

	return revokedClient, revokedServer
}
