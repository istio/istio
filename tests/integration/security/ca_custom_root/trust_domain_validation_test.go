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
	"context"
	"fmt"
	"io/ioutil"
	"path"
	"strings"
	"testing"
	"time"

	client2 "istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/echo/common/scheme"
	epb "istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	httpPlaintext = "http-plaintext"
	httpMTLS      = "http-mtls"
	tcpPlaintext  = "tcp-plaintext"
	tcpMTLS       = "tcp-mtls"
	passThrough   = "tcp-mtls-pass-through"

	// policy to enable mTLS in client and server:
	// ports with plaintext: 8090 (http) and 8092 (tcp)
	// ports with mTLS: 8091 (http), 8093 (tcp) and 9000 (tcp passthrough).
	policy = `
apiVersion: "security.istio.io/v1beta1"
kind: "PeerAuthentication"
metadata:
  name: "mtls"
spec:
  selector:
    matchLabels:
      app: server
  mtls:
    mode: STRICT
  portLevelMtls:
    8090:
      mode: DISABLE
    8092:
      mode: DISABLE
---
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: server
spec:
  host: server.%s.svc.cluster.local
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
    portLevelSettings:
    - port:
        number: 8090
      tls:
        mode: DISABLE
    - port:
        number: 8092
      tls:
        mode: DISABLE
`
)

// TestTrustDomainValidation tests the trust domain validation when mTLS is enabled.
// The trust domain validation should reject a request if it's not from the trust domains configured in the mesh config.
// The test uses naked client (no sidecar) with custom certificates of different trust domains and covers the following:
// - plaintext requests are not affected
// - same trust domain (cluster.local) and aliases (trust-domain-foo and trust-domain-bar)
// - works for both HTTP and TCP protocol
// - works for pass through filter chains
func TestTrustDomainValidation(t *testing.T) {
	framework.NewTest(t).Features("security.peer.trust-domain-validation").Run(
		func(ctx framework.TestContext) {
			// TODO: remove the skip when https://github.com/istio/istio/issues/28798 is fixed
			if ctx.Clusters().IsMulticluster() {
				ctx.Skip()
			}

			testNS := apps.Namespace

			ctx.Config().ApplyYAMLOrFail(ctx, testNS.Name(), fmt.Sprintf(policy, testNS.Name()))

			trustDomains := map[string]struct {
				cert string
				key  string
			}{
				"foo": {
					cert: readFile(ctx, "workload-foo-cert.pem"),
					key:  readFile(ctx, "workload-foo-key.pem"),
				},
				"bar": {
					cert: readFile(ctx, "workload-bar-cert.pem"),
					key:  readFile(ctx, "workload-bar-key.pem"),
				},
			}

			for _, cluster := range ctx.Clusters() {
				ctx.NewSubTest(fmt.Sprintf("From %s", cluster.StableName())).Run(func(ctx framework.TestContext) {
					// naked: only test app without sidecar, send requests from trust domain aliases
					// client: app with sidecar, send request from cluster.local
					// server: app with sidecar, verify requests from cluster.local or trust domain aliases
					client := apps.Client.GetOrFail(ctx, echo.InCluster(cluster))
					naked := apps.Naked.GetOrFail(ctx, echo.InCluster(cluster))
					server := apps.Server.GetOrFail(ctx, echo.InCluster(cluster))
					verify := func(ctx framework.TestContext, from echo.Instance, td, port string, s scheme.Instance, allow bool) {
						ctx.Helper()
						want := "allow"
						if !allow {
							want = "deny"
						}
						name := fmt.Sprintf("%s[%s]->server:%s[%s]", from.Config().Service, td, port, want)
						ctx.NewSubTest(name).Run(func(ctx framework.TestContext) {
							ctx.Helper()
							opt := echo.CallOptions{
								Target:   server,
								PortName: port,
								Address:  "server",
								Scheme:   s,
								Cert:     trustDomains[td].cert,
								Key:      trustDomains[td].key,
							}
							retry.UntilSuccessOrFail(ctx, func() error {
								var resp client2.ParsedResponses
								var err error
								if port == passThrough {
									// Manually make the request for pass through port.
									resp, err = workload(ctx, from).ForwardEcho(context.TODO(), &epb.ForwardEchoRequest{
										Url:   fmt.Sprintf("tcp://%s:9000", workload(ctx, server).Address()),
										Count: 1,
										Cert:  trustDomains[td].cert,
										Key:   trustDomains[td].key,
									})
								} else {
									resp, err = from.Call(opt)
								}
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

					// Request using plaintext should always allowed.
					verify(ctx, client, "plaintext", httpPlaintext, scheme.HTTP, true)
					verify(ctx, client, "plaintext", tcpPlaintext, scheme.TCP, true)
					verify(ctx, naked, "plaintext", httpPlaintext, scheme.HTTP, true)
					verify(ctx, naked, "plaintext", tcpPlaintext, scheme.TCP, true)

					// Request from local trust domain should always allowed.
					verify(ctx, client, "cluster.local", httpMTLS, scheme.HTTP, true)
					verify(ctx, client, "cluster.local", tcpMTLS, scheme.TCP, true)

					// Trust domain foo is added as trust domain alias.
					// Request from trust domain bar should be denied.
					// Request from trust domain foo should be allowed.
					verify(ctx, naked, "bar", httpMTLS, scheme.HTTPS, false)
					verify(ctx, naked, "bar", tcpMTLS, scheme.TCP, false)
					verify(ctx, naked, "bar", passThrough, scheme.TCP, false)
					verify(ctx, naked, "foo", httpMTLS, scheme.HTTPS, true)
					verify(ctx, naked, "foo", tcpMTLS, scheme.TCP, true)
					verify(ctx, naked, "foo", passThrough, scheme.TCP, true)
				})
			}
		})
}

func readFile(ctx framework.TestContext, name string) string {
	data, err := ioutil.ReadFile(path.Join(env.IstioSrc, "samples/certs", name))
	if err != nil {
		ctx.Fatal(err)
	}
	return string(data)
}

func workload(ctx framework.TestContext, from echo.Instance) echo.Workload {
	workloads, err := from.Workloads()
	if err != nil {
		ctx.Fatalf("failed to get worklaods: %v", err)
	}
	if len(workloads) < 1 {
		ctx.Fatalf("got 0 workloads")
	}
	return workloads[0]
}
