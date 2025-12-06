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

package ambient

import (
	"testing"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	testlabel "istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/security/crl/util"
)

// TestZtunnelCRL verifies that ztunnel respects CRL revocation
// The test verifies that:
//  1. Initial call succeeds before CRL update
//  2. After certificate revocation and CRL update, calls fail
func TestZtunnelCRL(t *testing.T) {
	framework.NewTest(t).
		Label(testlabel.CustomSetup).
		Run(func(t framework.TestContext) {
			// configure call options
			opts := echo.CallOptions{
				To: server,
				Port: echo.Port{
					Name: "http",
				},
				Scheme:                  scheme.HTTP,
				NewConnectionPerRequest: true,
				Count:                   1,
			}

			// initial call should succeed
			t.Logf("testing initial call before CRL update")
			client.CallOrFail(t, opts)
			t.Logf("initial call succeeded")

			// revoke the intermediate certificate
			util.RevokeIntermediate(t, certBundle)

			// wait for the CRL ConfigMap to be updated in ambient namespaces
			util.WaitForCRLUpdate(
				t,
				[]string{
					clientNS.Name(),
					serverNS.Name(),
				},
				certBundle,
				client,
				server,
			)

			// after CRL update, the call should fail
			opts.Check = check.Error()
			t.Logf("testing call after CRL update, expecting failure")
			retry.UntilSuccessOrFail(t, func() error {
				client.CallOrFail(t, opts)
				return nil
			})
			t.Logf("call correctly failed after CRL update")
		})
}
