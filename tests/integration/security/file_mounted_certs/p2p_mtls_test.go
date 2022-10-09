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

package filemountedcerts

import (
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	ServerSecretName = "test-server-cred"
	ServerCertsPath  = "tests/testdata/certs/mountedcerts-server"

	ClientSecretName = "test-client-cred"
	ClientCertsPath  = "tests/testdata/certs/mountedcerts-client"

	// nolint: lll
	ExpectedXfccHeader = `By=spiffe://cluster.local/ns/mounted-certs/sa/server;Hash=d05a05528f4cfab744394ae9153b10e2c8a9b491ba5368a296e92ad3ab2e94c9;Subject="CN=cluster.local";URI=spiffe://cluster.local/ns/mounted-certs/sa/client;DNS=client.mounted-certs.svc`
)

func TestClientToServiceTls(t *testing.T) {
	framework.NewTest(t).
		Features("security.peer.file-mounted-certs").
		Run(func(t framework.TestContext) {
			createObject(t, echo1NS.Name(), DestinationRuleConfigMutual)
			createObject(t, "istio-system", PeerAuthenticationConfig)

			opts := echo.CallOptions{
				To:    server,
				Count: 1,
				Port: echo.Port{
					Name: "http",
				},
				Check: check.And(
					check.OK(),
					check.RequestHeader("X-Forwarded-Client-Cert", ExpectedXfccHeader)),
				Retry: echo.Retry{
					Options: []retry.Option{retry.Delay(5 * time.Second), retry.Timeout(1 * time.Minute)},
				},
			}

			client[0].CallOrFail(t, opts)
		})
}

const (
	DestinationRuleConfigMutual = `
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: server
  namespace: {{.AppNamespace}}
spec:
  host: "server.{{.AppNamespace}}.svc.cluster.local"
  trafficPolicy:
    tls:
      mode: MUTUAL
      caCertificates: /client-certs/root-cert.pem
      clientCertificate: /client-certs/cert-chain.pem
      privateKey: /client-certs/key.pem
      subjectAltNames:
        - server.mounted-certs.svc

`

	PeerAuthenticationConfig = `
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: default
  namespace: "istio-system"
spec:
  mtls:
    mode: STRICT
`
)

func createObject(ctx framework.TestContext, serviceNamespace string, yamlManifest string) {
	args := map[string]string{"AppNamespace": serviceNamespace}
	ctx.ConfigIstio().Eval(serviceNamespace, args, yamlManifest).ApplyOrFail(ctx)
}
