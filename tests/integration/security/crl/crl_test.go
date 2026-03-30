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
	"testing"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/security/crl/util"
)

// TestPluggedInCACRL verifies that an mTLS call succeeds before CRL update and fails after
func TestPluggedInCACRL(t *testing.T) {
	framework.NewTest(t).
		Label(label.CustomSetup).
		Run(func(t framework.TestContext) {
			opts := echo.CallOptions{
				To: server,
				Port: echo.Port{
					Name:     "https",
					Protocol: protocol.HTTPS,
					TLS:      true,
				},
				Scheme:                  scheme.HTTPS,
				NewConnectionPerRequest: true,
				Count:                   1,
			}

			// initial call should succeed
			t.Logf("testing initial mTLS call before CRL update")
			client.CallOrFail(t, opts)
			t.Logf("initial mTLS call succeeded")

			// revoke the intermediate certificate
			util.RevokeIntermediate(t, certBundle)

			// wait for the CRL ConfigMap to be updated
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
			t.Logf("testing mTLS call after CRL update, expecting failure")
			retry.UntilSuccessOrFail(t, func() error {
				client.CallOrFail(t, opts)
				return nil
			})
		})
}
