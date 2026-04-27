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

package filebasedtlsorigination

import (
	"net/http"
	"testing"
	"time"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/util/retry"
	sdsutil "istio.io/istio/tests/integration/security/sds_ingress/util"
)

// TestDestinationRuleTls tests that MUTUAL tls mode is respected in DestinationRule.
// This sets up a client and server with appropriate cert config and ensures we can successfully send a message.
func TestDestinationRuleTls(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			ns := appNS

			// Setup our destination rule, enforcing TLS to "server". These certs will be created/mounted below.
			t.ConfigIstio().YAML(ns.Name(), `
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: db-mtls
spec:
  exportTo: ["."]
  host: server
  trafficPolicy:
    tls:
      mode: MUTUAL
      clientCertificate: /etc/certs/custom/cert-chain.pem
      privateKey: /etc/certs/custom/key.pem
      caCertificates: /etc/certs/custom/root-cert.pem
      sni: server
`).ApplyOrFail(t)

			for _, portName := range []string{"grpc", "http", "tcp"} {
				t.NewSubTest(portName).Run(func(t framework.TestContext) {
					opts := echo.CallOptions{
						To:    server,
						Count: 1,
						Port: echo.Port{
							Name: portName,
						},
						Check: check.OK(),
					}
					if portName == "tcp" {
						opts.Scheme = scheme.TCP
					}
					client[0].CallOrFail(t, opts)
				})
			}
		})
}

// TestDestinationRuleTlsSecretRotation verifies that pilot-agent correctly reloads
// file-mounted certificates referenced in a DestinationRule when the underlying
// Kubernetes secret is rotated. Two rotations are tested:
//   - rotate to incompatible cert set: traffic must fail (503)
//   - rotate back to original cert set: traffic must recover (200)
func TestDestinationRuleTlsSecretRotation(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			ns := appNS

			t.ConfigIstio().YAML(ns.Name(), `
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: db-mtls-secret-client
spec:
  exportTo: ["."]
  host: server
  trafficPolicy:
    connectionPool:
      http:
        maxRequestsPerConnection: 1
    tls:
      mode: MUTUAL
      clientCertificate: /etc/secret-client/client_certs/client.example.com/tls.crt
      privateKey: /etc/secret-client/client_certs/client.example.com/tls.key
      caCertificates: /etc/secret-client/ca_certs/example.com/cacert
      sni: server
`).ApplyOrFail(t, apply.CleanupConditionally)

			t.CleanupConditionally(func() {
				_ = updateSecretClientCertsFromInline(t, ns,
					mustReadCert("cert-chain.pem"),
					mustReadCert("key.pem"),
					mustReadCert("root-cert.pem"))
			})

			call := func(expected int) func() error {
				return func() error {
					_, err := secretClient[0].Call(echo.CallOptions{
						To:                      server,
						Count:                   1,
						NewConnectionPerRequest: true,
						Port: echo.Port{
							Name: "http",
						},
						Check: check.And(check.NoError(), check.Status(expected)),
					})
					return err
				}
			}

			// Verify baseline connectivity with original certs.
			retry.UntilSuccessOrFail(t, call(http.StatusOK), retry.Delay(time.Second), retry.Timeout(2*time.Minute))

			// Rotate to an incompatible cert set (different CA). pilot-agent should detect the
			// change via inotify and push updated certs to Envoy, causing TLS verification to fail (503).
			t.NewSubTest("rotate to wrong cert: traffic fails").Run(func(t framework.TestContext) {
				if err := updateSecretClientCertsFromInline(t, ns,
					sdsutil.TLSClientCertB,
					sdsutil.TLSClientKeyB,
					sdsutil.CaCertB); err != nil {
					t.Fatalf("failed rotating secret-client to set B: %v", err)
				}
				retry.UntilSuccessOrFail(t, call(http.StatusServiceUnavailable), retry.Delay(5*time.Second), retry.Timeout(2*time.Minute))
			})

			// Rotate back to the original certs. pilot-agent must detect the symlink swap and
			// push reloaded certs to Envoy so traffic recovers (200).
			t.NewSubTest("rotate back to original cert: traffic recovers").Run(func(t framework.TestContext) {
				if err := updateSecretClientCertsFromInline(t, ns,
					mustReadCert("cert-chain.pem"),
					mustReadCert("key.pem"),
					mustReadCert("root-cert.pem")); err != nil {
					t.Fatalf("failed rotating secret-client back to original: %v", err)
				}
				retry.UntilSuccessOrFail(t, call(http.StatusOK), retry.Delay(5*time.Second), retry.Timeout(2*time.Minute))
			})
		})
}
