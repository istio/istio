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

// TestTrustDomainAliasClient scope:
// The client side mTLS connection should validate the trust domain alias during secure naming validation.
// - When server side has trust domain which defined as trust domain alias, the mTLS connection should expect success
// - When server side has trust domain which not defined as trust domain alias, the mTLS connection should expect fail
func TestTrustDomainAliasClient(t *testing.T) {
	framework.NewTest(t).
		Features("security.peer.trust-domain-validation-alias-client").
		Run(func(ctx framework.TestContext) {
			testNS := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "trust-domain-alias-client-validation",
				Inject: true,
			})

			// Create testing certs using runtime namespace
			//generateCerts(ctx, testNS.Name())
			generateCerts(ctx, testNS.Name(), "foo", "server-naked-foo")
			generateCerts(ctx, testNS.Name(), "bar", "server-naked-bar")
			defer cleanCerts(ctx)

			// Deploy 3 workloads:
			// client: echo app with istio-proxy sidecar injected, holds default trust domain cluster.local
			// serverNakedFoo: echo app without istio-proxy sidecar, holds custom trust domain trust-domain-foo
			// serverNakedBar: echo app without istio-proxy sidecar, holds custom trust domain trust-domain-bar
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
						ClientCert: loadCert(t, "tmp/workload-foo-cert.pem"),
						Key:        loadCert(t, "tmp/workload-foo-key.pem"),
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
						ClientCert: loadCert(t, "tmp/workload-bar-cert.pem"),
						Key:        loadCert(t, "tmp/workload-bar-key.pem"),
					},
				}).
				BuildOrFail(t)

			ctx.Config().ApplyYAMLOrFail(ctx, testNS.Name(), fmt.Sprintf(POLICY))

			verify := func(t *testing.T, src echo.Instance, dest echo.Instance, s scheme.Instance, allow bool) {
				t.Helper()
				want := "allow"
				if !allow {
					want = "deny"
				}
				name := fmt.Sprintf("server:%s[%s]", dest.Config().Service, want)
				t.Run(name, func(t *testing.T) {
					t.Helper()
					opt := echo.CallOptions{
						Target:   dest,
						PortName: HTTPS,
						Host:     dest.Config().Service,
						Scheme:   s,
					}
					checker := connection.Checker{
						From:          src,
						Options:       opt,
						ExpectSuccess: allow,
					}
					checker.CheckOrFail(ctx)
				})
			}

			verify(t, client, serverNakedFoo, scheme.HTTP, true)
			verify(t, client, serverNakedBar, scheme.HTTP, false)
		})
}

func loadCert(t test.Failer, name string) string {
	data, err := cert.ReadSampleCertFromFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func generateCerts(ctx framework.TestContext, ns string, td string, sa string) {
	workDir := path.Join(env.IstioSrc, "samples/certs")
	script := path.Join(workDir, "generate-workload.sh")
	command := exec.Cmd{
		Path:   script,
		Args:   []string{script, td, ns, sa, workDir, "tmp"},
		Stdout: os.Stdout,
		Stderr: os.Stdout,
	}
	if err := command.Run(); err != nil {
		ctx.Logf("Failed to create testing certificates: %s", err)
	}
}

func cleanCerts(ctx framework.TestContext) {
	err := os.RemoveAll(path.Join(env.IstioSrc, "samples/certs/tmp"))
	if err != nil {
		ctx.Logf("Failed to clean testing certificates: %s", err)
	}
}
