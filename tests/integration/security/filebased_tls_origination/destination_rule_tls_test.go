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
	"os"
	"path"
	"testing"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/check"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

func mustReadFile(t framework.TestContext, f string) string {
	b, err := os.ReadFile(path.Join(env.IstioSrc, "tests/testdata/certs/dns", f))
	if err != nil {
		t.Fatalf("failed to read %v: %v", f, err)
	}
	return string(b)
}

// TestDestinationRuleTls tests that MUTUAL tls mode is respected in DestinationRule.
// This sets up a client and server with appropriate cert config and ensures we can successfully send a message.
func TestDestinationRuleTls(t *testing.T) {
	framework.
		NewTest(t).
		Features("security.egress.tls.filebased").
		Run(func(t framework.TestContext) {
			ns := namespace.NewOrFail(t, t, namespace.Config{
				Prefix: "tls",
				Inject: true,
			})

			// Setup our destination rule, enforcing TLS to "server". These certs will be created/mounted below.
			t.ConfigIstio().YAML(`
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
`).ApplyOrFail(t, ns.Name())

			var client, server echo.Instance
			deployment.New(t).
				With(&client, echo.Config{
					Service:   "client",
					Namespace: ns,
					Ports:     []echo.Port{},
					Subsets: []echo.SubsetConfig{{
						Version: "v1",
						// Set up custom annotations to mount the certs. We will re-use the configmap created by "server"
						// so that we don't need to manage it ourselves.
						// The paths here match the destination rule above
						Annotations: echo.NewAnnotations().
							Set(echo.SidecarVolume, `{"custom-certs":{"configMap":{"name":"server-certs"}}}`).
							Set(echo.SidecarVolumeMount, `{"custom-certs":{"mountPath":"/etc/certs/custom"}}`),
					}},
					Cluster: t.Clusters().Default(),
				}).
				With(&server, echo.Config{
					Service:   "server",
					Namespace: ns,
					Ports: []echo.Port{
						{
							Name:         "grpc",
							Protocol:     protocol.GRPC,
							WorkloadPort: 8090,
							TLS:          true,
						},
						{
							Name:         "http",
							Protocol:     protocol.HTTP,
							WorkloadPort: 8091,
							TLS:          true,
						},
						{
							Name:         "tcp",
							Protocol:     protocol.TCP,
							WorkloadPort: 8092,
							TLS:          true,
						},
					},
					// Set up TLS certs on the server. This will make the server listen with these credentials.
					TLSSettings: &common.TLSSettings{
						RootCert:   mustReadFile(t, "root-cert.pem"),
						ClientCert: mustReadFile(t, "cert-chain.pem"),
						Key:        mustReadFile(t, "key.pem"),
						// Override hostname to match the SAN in the cert we are using
						Hostname: "server.default.svc",
					},
					// Do not inject, as we are testing non-Istio TLS here
					Subsets: []echo.SubsetConfig{{
						Version:     "v1",
						Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
					}},
					Cluster: t.Clusters().Default(),
				}).
				BuildOrFail(t)

			for _, portName := range []string{"grpc", "http", "tcp"} {
				portName := portName
				t.NewSubTest(portName).Run(func(t framework.TestContext) {
					opts := echo.CallOptions{
						To: server,
						Port: echo.Port{
							Name: portName,
						},
						Check: check.OK(),
					}
					if portName == "tcp" {
						opts.Scheme = scheme.TCP
					}
					client.CallOrFail(t, opts)
				})
			}
		})
}
