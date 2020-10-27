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

package cacustomroot

import (
	"fmt"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/tests/integration/security/util/cert"
	"istio.io/istio/tests/integration/security/util/connection"
	"os"
	"os/exec"
	"path"
	"testing"
)

const (
	TmpDir = "tmp"
	HTTPS  = "https"
	POLICY = `
apiVersion: "security.istio.io/v1beta1"
kind: "PeerAuthentication"
metadata:
  name: "mtls"
spec:
  mtls:
    mode: STRICT
---
apiVersion: "networking.istio.io/v1alpha3"
kind: "DestinationRule"
metadata:
  name: "server-naked"
spec:
  host: "*.local"
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
`
)

// TestTrustDomainAliasSecureNaming scope:
// The client side mTLS connection should validate the trust domain alias during secure naming validation.
//
// Setup:
// 1. Setup Istio with custom CA cert. This is because we need to use that root cert to sign customized certificate for server workloads to give them different trust domains.
// 2. One client workload with sidecar injected.
// 3. Two naked server workloads with custom certs whose URI SAN have different SPIFFE trust domains.
// 4. PeerAuthentication with strict mtls, to enforce the mtls connection.
// 5. DestinaitonRule with tls ISTIO_MUTUAL mode, because Istio auto mTLS will let client send plaintext to naked servers by default.
// 6. MeshConfig.TrustDomainAliases contains one of the trust domain "server-naked-foo".
//
// Expectation:
// When the "server-naked-foo" is in the list of MeshConfig.TrustDomainAliases, client requests to "server-naked-foo" succeeds, and requests to "server-naked-bar" fails.
func TestTrustDomainAliasSecureNaming(t *testing.T) {
	framework.NewTest(t).
		Features("security.peer.trust-domain-alias-secure-naming").
		Run(func(ctx framework.TestContext) {
			testNS := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "trust-domain-alias",
				Inject: true,
			})

			// Create testing certs using runtime namespace.
			cleanup := generateCerts(ctx, testNS.Name())
			defer cleanup()

			// Deploy 3 workloads:
			// client: echo app with istio-proxy sidecar injected, holds default trust domain cluster.local.
			// serverNakedFoo: echo app without istio-proxy sidecar, holds custom trust domain trust-domain-foo.
			// serverNakedBar: echo app without istio-proxy sidecar, holds custom trust domain trust-domain-bar.
			var client, serverNakedFoo, serverNakedBar echo.Instance
			echoboot.NewBuilder(ctx).
				With(&client, echo.Config{
					Namespace: testNS,
					Service:   "client",
				}).
				With(&serverNakedFoo, echo.Config{
					Namespace: testNS,
					Service:   "server-naked-foo",
					Subsets: []echo.SubsetConfig{
						{
							Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
						},
					},
					ServiceAccount: true,
					Ports: []echo.Port{
						{
							Name:         HTTPS,
							Protocol:     protocol.HTTPS,
							ServicePort:  443,
							InstancePort: 8443,
							TLS:          true,
						},
					},
					TLSSettings: &common.TLSSettings{
						RootCert:   loadCert(t, "root-cert.pem"),
						ClientCert: loadCert(t, TmpDir + "/workload-foo-cert.pem"),
						Key:        loadCert(t, TmpDir + "/workload-foo-key.pem"),
					},
				}).
				With(&serverNakedBar, echo.Config{
					Namespace: testNS,
					Service:   "server-naked-bar",
					Subsets: []echo.SubsetConfig{
						{
							Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
						},
					},
					ServiceAccount: true,
					Ports: []echo.Port{
						{
							Name:         HTTPS,
							Protocol:     protocol.HTTPS,
							ServicePort:  443,
							InstancePort: 8443,
							TLS:          true,
						},
					},
					TLSSettings: &common.TLSSettings{
						RootCert:   loadCert(t, "root-cert.pem"),
						ClientCert: loadCert(t, TmpDir + "/workload-bar-cert.pem"),
						Key:        loadCert(t, TmpDir + "/workload-bar-key.pem"),
					},
				}).
				BuildOrFail(t)

			ctx.Config().ApplyYAMLOrFail(ctx, testNS.Name(), fmt.Sprintf(POLICY))

			verify := func(t *testing.T, src echo.Instance, dest echo.Instance, s scheme.Instance, success bool) {
				t.Helper()
				want := "success"
				if !success {
					want = "fail"
				}
				name := fmt.Sprintf("server:%s[%s]", dest.Config().Service, want)
				t.Run(name, func(t *testing.T) {
					t.Helper()
					opt := echo.CallOptions{
						Target:   dest,
						PortName: HTTPS,
						Address:  dest.Config().Service,
						Scheme:   s,
					}
					checker := connection.Checker{
						From:          src,
						Options:       opt,
						ExpectSuccess: success,
					}
					checker.CheckOrFail(ctx)
				})
			}

			cases := []struct {
				src    echo.Instance
				dest   echo.Instance
				expect bool
			}{
				{
					src:    client,
					dest:   serverNakedFoo,
					expect: true,
				},
				{
					src:    client,
					dest:   serverNakedBar,
					expect: false,
				},
			}

			for _, tc := range cases {
				verify(t, tc.src, tc.dest, scheme.HTTP, tc.expect)
			}
		})
}

func loadCert(t test.Failer, name string) string {
	data, err := cert.ReadSampleCertFromFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func generateCerts(t test.Failer, ns string) func() {
	workDir := path.Join(env.IstioSrc, "samples/certs")
	script := path.Join(workDir, "generate-workload.sh")

	crts := []struct {
		td string
		sa string
	}{
		{
			td: "foo",
			sa: "server-naked-foo",
		},
		{
			td: "bar",
			sa: "server-naked-bar",
		},
	}

	for _, crt := range crts {
		command := exec.Cmd{
			Path:   script,
			Args:   []string{script, crt.td, ns, crt.sa, TmpDir},
			Stdout: os.Stdout,
			Stderr: os.Stdout,
		}
		if err := command.Run(); err != nil {
			t.Fatal("Failed to create testing certificates: %s", err)
		}
	}

	return func() {
		err := os.RemoveAll(path.Join(env.IstioSrc, "samples/certs/" + TmpDir))
		if err != nil {
			t.Fatal("Failed to clean up testing certificates: %s", err)
		}
	}
}
