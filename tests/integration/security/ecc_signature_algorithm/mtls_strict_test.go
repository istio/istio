//go:build integ
// +build integ

//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package eccsignaturealgorithm

import (
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"

	"istio.io/istio/pkg/test/echo/check"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/tests/integration/security/util/cert"
)

const (
	DestinationRuleConfigIstioMutual = `
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: server
  namespace: {{.AppNamespace}}
spec:
  host: "server.{{.AppNamespace}}.svc.cluster.local"
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
`

	PeerAuthenticationConfig = `
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: default
  namespace: {{.AppNamespace}}
spec:
  mtls:
    mode: STRICT
`
)

func TestStrictMTLS(t *testing.T) {
	framework.
		NewTest(t).
		Features("security.peer.ecc-signature-algorithm").
		Run(func(t framework.TestContext) {
			ns := apps.Namespace.Name()
			args := map[string]string{"AppNamespace": ns}
			t.ConfigIstio().Eval(ns, args, PeerAuthenticationConfig).ApplyOrFail(t, resource.Wait)
			t.ConfigIstio().Eval(ns, args, DestinationRuleConfigIstioMutual).ApplyOrFail(t, resource.Wait)

			apps.Client.CallOrFail(t, echo.CallOptions{
				To: apps.Server,
				Port: echo.Port{
					Name: "http",
				},
				Count: 1,
				Check: check.OK(),
			})

			certPEMs := cert.DumpCertFromSidecar(t, apps.Client, apps.Server, "http")
			block, _ := pem.Decode([]byte(strings.Join(certPEMs, "\n")))
			if block == nil { // nolint: staticcheck
				t.Fatalf("failed to parse certificate PEM")
			}

			certificate, err := x509.ParseCertificate(block.Bytes) // nolint: staticcheck
			if err != nil {
				t.Fatalf("failed to parse certificate: %v", err)
			}

			if certificate.PublicKeyAlgorithm != x509.ECDSA {
				t.Fatalf("public key used in server cert is not ECDSA: %v", certificate.PublicKeyAlgorithm)
			}
		})
}
