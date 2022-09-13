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

package filebasedtlsorigination

import (
	"testing"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
)

// TestDestinationRuleTls tests that MUTUAL tls mode is respected in DestinationRule.
// This sets up a client and server with appropriate cert config and ensures we can successfully send a message.
func TestDestinationRuleTls(t *testing.T) {
	framework.
		NewTest(t).
		Features("security.egress.tls.filebased").
		Run(func(t framework.TestContext) {
			ns := appNS

			// Setup our destination rule, enforcing TLS to "server". These certs will be created/mounted below.
			t.ConfigIstio().YAML(ns.Name(), `
apiVersion: networking.istio.io/v1alpha3
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
`).ApplyOrFail(t)

			for _, portName := range []string{"grpc", "http", "tcp"} {
				portName := portName
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
