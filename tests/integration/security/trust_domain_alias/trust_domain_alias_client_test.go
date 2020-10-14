package trustdomainalias

import (
	"fmt"
	"io/ioutil"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
	client2 "istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
	"path"
	"strings"
	"testing"
	"time"
)

const (
	httpMTLS = "http-mtls"
	policy   = `
apiVersion: "security.istio.io/v1beta1"
kind: "PeerAuthentication"
metadata:
  name: "mtls"
  namespace: %s
spec:
  mtls:
    mode: STRICT
---
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: server-naked-foo
spec:
  host: server-naked-foo.%s.svc.cluster.local
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
---
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: server-naked-bar
spec:
  host: server-naked-bar.%s.svc.cluster.local
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
`
)

func TestTrustDomainAliasClient(t *testing.T) {
	framework.NewTest(t).Features("security.peer.trust-domain-validation-alias-client").Run(
		func(ctx framework.TestContext) {
			testNS := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "trust-domain-alias-client-validation",
				Inject: true,
			})

			// Deploy 3 workloads:
			// client: echo app with istio-proxy sidecar injected, sends requests with default cluster.local trust domain
			// serverNakedFoo: echo app without istio-proxy sidecar, receives requests from client and holds custom trust domain trust-domain-foo
			// serverNakedBar: echo app without istio-proxy sidecar, receives requests from client and holds custom trust domain trust-domain-bar
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
							Name:         httpMTLS,
							Protocol:     protocol.HTTPS,
							ServicePort:  443,
							InstancePort: 8443,
							TLS:          true,
						},
					},
					TLSSettings: &common.TLSSettings{
						// Echo has these test certs baked into the docker image
						RootCert:   readFile(t, "root-cert.pem"),
						ClientCert: readFile(t, "workload-foo-cert.pem"),
						Key:        readFile(t, "workload-foo-key.pem"),
						// Override hostname to match the SAN in the cert we are using
						// Hostname: "server.default.svc",
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
							Name:         httpMTLS,
							Protocol:     protocol.HTTPS,
							ServicePort:  443,
							InstancePort: 8443,
							TLS:          true,
						},
					},
					TLSSettings: &common.TLSSettings{
						// Echo has these test certs baked into the docker image
						RootCert:   readFile(t, "root-cert.pem"),
						ClientCert: readFile(t, "workload-bar-cert.pem"),
						Key:        readFile(t, "workload-bar-key.pem"),
						// Override hostname to match the SAN in the cert we are using
						//Hostname: "server.default.svc",
					},
				}).
				BuildOrFail(t)

			ctx.Config().ApplyYAMLOrFail(ctx, testNS.Name(), fmt.Sprintf(policy, testNS.Name(), testNS.Name(), testNS.Name()))

			//trustDomains := map[string]struct {
			//	cert string
			//	key  string
			//}{
			//	"foo": {
			//		cert: readFile(ctx, "workload-foo-cert.pem"),
			//		key:  readFile(ctx, "workload-foo-key.pem"),
			//	},
			//	"bar": {
			//		cert: readFile(ctx, "workload-bar-cert.pem"),
			//		key:  readFile(ctx, "workload-bar-key.pem"),
			//	},
			//}

			verify := func(t *testing.T, server echo.Instance, s scheme.Instance, allow bool) {
				t.Helper()
				want := "allow"
				if !allow {
					want = "deny"
				}
				name := fmt.Sprintf("server:%s[%s]", server.Config().Service, want)
				t.Run(name, func(t *testing.T) {
					t.Helper()
					opt := echo.CallOptions{
						Target:   server,
						PortName: httpMTLS,
						Host:     server.Config().Service,
						Scheme:   s,
					}
					retry.UntilSuccessOrFail(t, func() error {
						var resp client2.ParsedResponses
						var err error
						resp, err = client.Call(opt)
						if allow {
							if err != nil {
								return fmt.Errorf("want allow but got error: %v", err)
							} else if err := resp.CheckOK(); err != nil {
								return fmt.Errorf("want allow but got %v: %v", resp, err)
							}
						} else {
							if err == nil {
								return fmt.Errorf("want deny but got allow: %v", resp)
							}
							// Look up for the specific "tls: unknown certificate" error when trust domain validation failed.
							if tlsErr := "tls: unknown certificate"; !strings.Contains(err.Error(), tlsErr) {
								return fmt.Errorf("want error %q but got %v", tlsErr, err)
							}
						}
						return nil
					}, retry.Delay(250*time.Millisecond), retry.Timeout(30*time.Second), retry.Converge(5))
				})
			}

			verify(t, serverNakedFoo, scheme.HTTPS, true)
			verify(t, serverNakedBar, scheme.HTTPS, false)

		})
}

func readFile(t test.Failer, name string) string {
	data, err := ioutil.ReadFile(path.Join(env.IstioSrc, "samples/certs", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
