package trustdomainalias

import (
	"fmt"
	"io/ioutil"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/tests/integration/security/util/connection"
	"os"
	"os/exec"
	"path"
	"testing"
)

const (
	httpMTLS = "http-mtls"
	policy   = `
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


// options to make the request:
// 1. directly use echo.Call
// 2. use Checker
//
// delay
// 1. add sleep up to 90 seconds
//
// subtests
// 1. by for loop
// 2. by envoking verify method
//
// clients
// 1. create separate clientB

// results:
// when only test bar, sometime it will get success, but it will change to fail after couple runs
// when test both bar and foo, sometime it till get success if run bar first, but will change to fail after couple runs
// after changing to fail, it seems not changing back without code changes

func TestTrustDomainAliasClient(t *testing.T) {
	framework.NewTest(t).
		Features("security.peer.trust-domain-validation-alias-client").
		Run(func(ctx framework.TestContext) {
			testNS := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "trust-domain-alias-client-validation",
				Inject: true,
			})

			generateCerts(t, testNS.Name())
			//defer cleanCerts(t)

			// Deploy 3 workloads:
			// client: echo app with istio-proxy sidecar injected, sends requests with default cluster.local trust domain
			// serverNakedFoo: echo app without istio-proxy sidecar, receives requests from client and holds custom trust domain trust-domain-foo
			// serverNakedBar: echo app without istio-proxy sidecar, receives requests from client and holds custom trust domain trust-domain-bar
			var client, clientB, serverNakedFoo, serverNakedBar echo.Instance
			echoboot.NewBuilder(ctx).
				With(&client, echo.Config{
					Namespace: testNS,
					Service:   "client",
				}).
				With(&clientB, echo.Config{
					Namespace: testNS,
					Service:   "client-b",
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
						ClientCert: readFile(t, "tmp/workload-foo-cert.pem"),
						Key:        readFile(t, "tmp/workload-foo-key.pem"),
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
						ClientCert: readFile(t, "tmp/workload-bar-cert.pem"),
						Key:        readFile(t, "tmp/workload-bar-key.pem"),
					},
				}).
				BuildOrFail(t)

			ctx.Config().ApplyYAMLOrFail(ctx, testNS.Name(), fmt.Sprintf(policy))
			//ctx.Config().ApplyYAMLOrFail(ctx, testNS.Name(), fmt.Sprintf(policy, testNS.Name(), testNS.Name(), testNS.Name()))

			// --- wait for config applied
			//retry.UntilSuccessOrFail(ctx, func() error {
			//	ctx.Logf("[%v] Apply istio config...", time.Now())
			//	// TODO(https://github.com/istio/istio/issues/20460) We shouldn't need a retry loop
			//	return ctx.Config().ApplyYAML(testNS.Name(), fmt.Sprintf(policy))
			//})
			//ik := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})
			//if err := ik.WaitForConfigs(testNS.Name(), policy); err != nil {
			//	// Continue anyways, so we can assess the effectiveness of using `istioctl wait`
			//	ctx.Logf("warning: failed to wait for config: %v", err)
			//	// Get proxy status for additional debugging
			//	s, _, _ := ik.Invoke([]string{"ps"})
			//	ctx.Logf("proxy status: %v", s)
			//}
			//ctx.Logf("Sleep for 90 secs....")
			//time.Sleep(time.Second * 90)


			// making subtests but getting the same results
			//t.Run("trust-domain-alias-client-validation", func(t *testing.T) {
			//	for _, dest := range []echo.Instance{serverNakedFoo, serverNakedBar} {
			//		src := client
			//		dest := dest
			//		opts := echo.CallOptions{
			//			Target:   dest,
			//			PortName: httpMTLS,
			//			Host:     dest.Config().Service,
			//			Scheme:   scheme.HTTPS,
			//		}
			//		ctx.NewSubTest("server:" + dest.Config().Service).
			//			Run(func(ctx framework.TestContext) {
			//				checker := connection.Checker{
			//					From:          src,
			//					Options:       opts,
			//					ExpectSuccess: false,
			//				}
			//				checker.CheckOrFail(ctx)
			//			})
			//	}
			//})

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
						PortName: httpMTLS,
						Host:     dest.Config().Service,
						Scheme:   s,
					}

					// Use checker to test
					//src := client
					checker := connection.Checker{
						From:          src,
						Options:       opt,
						ExpectSuccess: allow,
					}
					checker.CheckOrFail(ctx)

					// directly use echo.Call
					//retry.UntilSuccessOrFail(t, func() error {
					//	var resp client2.ParsedResponses
					//	var err error
					//	resp, err = src.Call(opt)
					//	//resp, err = src.Call(opt)
					//	if allow {
					//		if err != nil {
					//			return fmt.Errorf("want allow but got error: %v", err)
					//		} else if err := resp.CheckOK(); err != nil {
					//			return fmt.Errorf("want allow but got %v: %v", resp, err)
					//		}
					//	} else {
					//		if err == nil {
					//			return fmt.Errorf("want deny but got allow: %v", resp)
					//			//return nil
					//		}
					//		// Look up for the specific "tls: unknown certificate" error when trust domain validation failed.
					//		if tlsErr := "tls: unknown certificate"; !strings.Contains(err.Error(), tlsErr) {
					//			return fmt.Errorf("want error %q but got %v", tlsErr, err)
					//		}
					//	}
					//	return nil
					//}, retry.Delay(250*time.Millisecond), retry.Timeout(30*time.Second), retry.Converge(5))
				})
			}

			verify(t, client, serverNakedBar, scheme.HTTPS, false)
			//verify(t, clientB, serverNakedFoo, scheme.HTTPS, true)

		})
}

func readFile(t test.Failer, name string) string {
	data, err := ioutil.ReadFile(path.Join(env.IstioSrc, "samples/certs", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func generateCerts(t test.Failer, ns string) {
	workDir := path.Join(env.IstioSrc, "samples/certs")
	script := path.Join(workDir, "generate-temp-workload.sh")

	foo := exec.Cmd{
		Path:   script,
		Args:   []string{script, "foo", ns, "server-naked-foo", workDir},
		Stdout: os.Stdout,
		Stderr: os.Stdout,
	}

	bar := exec.Cmd{
		Path:   script,
		Args:   []string{script, "bar", ns, "server-naked-bar", workDir},
		Stdout: os.Stdout,
		Stderr: os.Stdout,
	}

	if err := foo.Run(); err != nil {
		fmt.Printf("error %s", err)
		//t.Fatal(err)
	}

	if err := bar.Run(); err != nil {
		fmt.Printf("error %s", err)
		//t.Fatal(err)
	}
}

func cleanCerts(t test.Failer) {
	err := os.RemoveAll(path.Join(env.IstioSrc, "samples/certs/tmp"))
	if err != nil {
		t.Fatal(err)
	}
}
