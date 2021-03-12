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

package security

import (
	"context"
	"fmt"
	"testing"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/common/response"
	epb "istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
)

// TestPassThroughFilterChain tests the authN and authZ policy on the pass through filter chain.
func TestPassThroughFilterChain(t *testing.T) {
	framework.
		NewTest(t).
		Features("security.filterchain").
		Run(func(ctx framework.TestContext) {
			ns := apps.Namespace1

			type expect struct {
				port      int
				schema    protocol.Instance
				plaintext bool
				mtls      bool
			}
			cases := []struct {
				name     string
				config   string
				expected []expect
			}{
				// There is no authN/authZ policy.
				// All requests should success, this is to verify the pass through filter chain and
				// the workload ports are working correctly.
				{
					name: "DISABLE",
					config: `apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: mtls
spec:
  mtls:
    mode: DISABLE`,
					expected: []expect{
						{
							port:      8085,
							schema:    protocol.HTTP,
							plaintext: true,
							mtls:      false,
						},
						{
							port:      8086,
							schema:    protocol.HTTP,
							plaintext: true,
							mtls:      false,
						},
						{
							port:      8087,
							schema:    protocol.TCP,
							plaintext: true,
							mtls:      false,
						},
						{
							port:      8088,
							schema:    protocol.TCP,
							plaintext: true,
							mtls:      false,
						},
						{
							port:      8089,
							schema:    protocol.HTTPS,
							plaintext: true,
							mtls:      false,
						},
					},
				},
				{
					// There is only authZ policy that allows access to port 8085, 8087, and 8089.
					// Only request to port 8085, 8087, 8089 should be allowed.
					name: "DISABLE with authz",
					config: `apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: mtls
spec:
  mtls:
    mode: DISABLE
---
apiVersion: "security.istio.io/v1beta1"
kind: AuthorizationPolicy
metadata:
  name: authz
spec:
  rules:
  - to:
    - operation:
        ports: ["8085", "8087", "8089"]`,
					expected: []expect{
						{
							port:      8085,
							schema:    protocol.HTTP,
							plaintext: true,
							mtls:      false,
						},
						{
							port:      8086,
							schema:    protocol.HTTP,
							plaintext: false,
							mtls:      false,
						},
						{
							port:      8087,
							schema:    protocol.TCP,
							plaintext: true,
							mtls:      false,
						},
						{
							port:      8088,
							schema:    protocol.TCP,
							plaintext: false,
							mtls:      false,
						},
						{
							port:      8089,
							schema:    protocol.HTTPS,
							plaintext: true,
							mtls:      false,
						},
						{
							port:      8084,
							schema:    protocol.HTTPS,
							plaintext: false,
							mtls:      false,
						},
					},
				},
				{
					// There is only authN policy that enables mTLS (Strict).
					// The request should be denied because the client is always using plain text.
					name: "STRICT",
					config: `apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: mtls
spec:
  mtls:
    mode: STRICT`,
					expected: []expect{
						{
							port:      8085,
							schema:    protocol.HTTP,
							plaintext: false,
							mtls:      true,
						},
						{
							port:      8086,
							schema:    protocol.HTTP,
							plaintext: false,
							mtls:      true,
						},
						{
							port:      8087,
							schema:    protocol.TCP,
							plaintext: false,
							mtls:      true,
						},
						{
							port:      8088,
							schema:    protocol.TCP,
							plaintext: false,
							mtls:      true,
						},
						{
							port:      8089,
							schema:    protocol.HTTPS,
							plaintext: false,
							mtls:      true,
						},
					},
				},
				{
					// There is only authN policy that enables mTLS (Permissive).
					// The request should be allowed because the client is always using plain text.
					name: "PERMISSIVE",
					config: `apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: mtls
spec:
  mtls:
    mode: PERMISSIVE`,
					expected: []expect{
						{
							port:      8085,
							schema:    protocol.HTTP,
							plaintext: true,
							mtls:      true,
						},
						{
							port:      8086,
							schema:    protocol.HTTP,
							plaintext: true,
							mtls:      true,
						},
						{
							port:      8087,
							schema:    protocol.TCP,
							plaintext: true,
							mtls:      true,
						},
						{
							port:      8088,
							schema:    protocol.TCP,
							plaintext: true,
							mtls:      true,
						},
						{
							port:      8089,
							schema:    protocol.HTTPS,
							plaintext: true,
							mtls:      true,
						},
					},
				},
				{
					// There is only authN policy that disables mTLS by default and enables mTLS strict on port 8086, 8088, 8084.
					// The request should be denied on port 8086, 8088, 8084.
					name: "DISABLE with STRICT",
					config: `apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: mtls
spec:
  selector:
    matchLabels:
      app: a
  mtls:
    mode: DISABLE
  portLevelMtls:
    8086:
      mode: STRICT
    8088:
      mode: STRICT
    8084:
      mode: STRICT`,
					expected: []expect{
						{
							port:      8085,
							schema:    protocol.HTTP,
							plaintext: true,
							mtls:      false,
						},
						{
							port:      8086,
							schema:    protocol.HTTP,
							plaintext: false,
							mtls:      true,
						},
						{
							port:      8087,
							schema:    protocol.TCP,
							plaintext: true,
							mtls:      false,
						},
						{
							port:      8088,
							schema:    protocol.TCP,
							plaintext: false,
							mtls:      true,
						},
						{
							port:      8089,
							schema:    protocol.HTTPS,
							plaintext: true,
							mtls:      false,
						},
						{
							port:      8084,
							schema:    protocol.HTTPS,
							plaintext: false,
							mtls:      true,
						},
					},
				},
				{
					// There is only authN policy that enables mTLS by default and disables mTLS strict on port 8086 and 8088.
					// The request should be denied on port 8085 and 8071.
					name: "STRICT with disable",
					config: `apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: mtls
spec:
  selector:
    matchLabels:
      app: a
  mtls:
    mode: STRICT
  portLevelMtls:
    8086:
      mode: DISABLE
    8088:
      mode: DISABLE
    8084:
      mode: DISABLE`,
					expected: []expect{
						{
							port:      8085,
							schema:    protocol.HTTP,
							plaintext: false,
							mtls:      true,
						},
						{
							port:      8086,
							schema:    protocol.HTTP,
							plaintext: true,
							mtls:      false,
						},
						{
							port:      8087,
							schema:    protocol.TCP,
							plaintext: false,
							mtls:      true,
						},
						{
							port:      8088,
							schema:    protocol.TCP,
							plaintext: true,
							mtls:      false,
						},
						{
							port:      8089,
							schema:    protocol.HTTPS,
							plaintext: false,
							mtls:      true,
						},
						{
							port:      8084,
							schema:    protocol.HTTPS,
							plaintext: true,
							mtls:      false,
						},
					},
				},
				{
					name: "PERMISSIVE with strict",
					config: `apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: mtls
spec:
  selector:
    matchLabels:
      app: a
  mtls:
    mode: PERMISSIVE
  portLevelMtls:
    8086:
      mode: STRICT
    8088:
      mode: STRICT
    8084:
      mode: STRICT`,
					expected: []expect{
						{
							port:      8085,
							schema:    protocol.HTTP,
							plaintext: true,
							mtls:      true,
						},
						{
							port:      8086,
							schema:    protocol.HTTP,
							plaintext: false,
							mtls:      true,
						},
						{
							port:      8087,
							schema:    protocol.TCP,
							plaintext: true,
							mtls:      true,
						},
						{
							port:      8088,
							schema:    protocol.TCP,
							plaintext: false,
							mtls:      true,
						},
						{
							port:      8089,
							schema:    protocol.HTTPS,
							plaintext: true,
							mtls:      true,
						},
						{
							port:      8084,
							schema:    protocol.HTTPS,
							plaintext: false,
							mtls:      true,
						},
					},
				},
				{
					name: "STRICT with PERMISSIVE",
					config: `apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: mtls
spec:
  selector:
    matchLabels:
      app: a
  mtls:
    mode: STRICT
  portLevelMtls:
    8086:
      mode: PERMISSIVE
    8088:
      mode: PERMISSIVE
    8084:
      mode: PERMISSIVE`,
					expected: []expect{
						{
							port:      8085,
							schema:    protocol.HTTP,
							plaintext: false,
							mtls:      true,
						},
						{
							port:      8086,
							schema:    protocol.HTTP,
							plaintext: true,
							mtls:      true,
						},
						{
							port:      8087,
							schema:    protocol.TCP,
							plaintext: false,
							mtls:      true,
						},
						{
							port:      8088,
							schema:    protocol.TCP,
							plaintext: true,
							mtls:      true,
						},
						{
							port:      8089,
							schema:    protocol.HTTPS,
							plaintext: false,
							mtls:      true,
						},
						{
							port:      8084,
							schema:    protocol.HTTPS,
							plaintext: true,
							mtls:      true,
						},
					},
				},
				{
					name: "PERMISSIVE with disable",
					config: `apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: mtls
spec:
  selector:
    matchLabels:
      app: a
  mtls:
    mode: PERMISSIVE
  portLevelMtls:
    8086:
      mode: DISABLE
    8088:
      mode: DISABLE
    8084:
      mode: DISABLE`,
					expected: []expect{
						{
							port:      8085,
							schema:    protocol.HTTP,
							plaintext: true,
							mtls:      true,
						},
						{
							port:      8086,
							schema:    protocol.HTTP,
							plaintext: true,
							mtls:      false,
						},
						{
							port:      8087,
							schema:    protocol.TCP,
							plaintext: true,
							mtls:      true,
						},
						{
							port:      8088,
							schema:    protocol.TCP,
							plaintext: true,
							mtls:      false,
						},
						{
							port:      8089,
							schema:    protocol.HTTPS,
							plaintext: true,
							mtls:      true,
						},
						{
							port:      8084,
							schema:    protocol.HTTPS,
							plaintext: true,
							mtls:      false,
						},
					},
				},
				{
					name: "DISABLE with PERMISSIVE",
					config: `apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: mtls
spec:
  selector:
    matchLabels:
      app: a
  mtls:
    mode: DISABLE
  portLevelMtls:
    8086:
      mode: PERMISSIVE
    8088:
      mode: PERMISSIVE
    8084:
      mode: PERMISSIVE`,
					expected: []expect{
						{
							port:      8085,
							schema:    protocol.HTTP,
							plaintext: true,
							mtls:      false,
						},
						{
							port:      8086,
							schema:    protocol.HTTP,
							plaintext: true,
							mtls:      true,
						},
						{
							port:      8087,
							schema:    protocol.TCP,
							plaintext: true,
							mtls:      false,
						},
						{
							port:      8088,
							schema:    protocol.TCP,
							plaintext: true,
							mtls:      true,
						},
						{
							port:      8089,
							schema:    protocol.HTTPS,
							plaintext: true,
							mtls:      false,
						},
						{
							port:      8084,
							schema:    protocol.HTTPS,
							plaintext: true,
							mtls:      true,
						},
					},
				},
			}

			for _, cluster := range ctx.Clusters() {
				destination := apps.A.Match(echo.Namespace(ns.Name())).GetOrFail(ctx, echo.InCluster(cluster))
				destWorkload := getWorkload(destination, ctx).Address()
				se := tmpl.EvaluateOrFail(ctx, `apiVersion: networking.istio.io/v1beta1
kind: ServiceEntry
metadata:
  name: dest-via-mtls
spec:
  hosts:
  - fake.destination
  addresses:
  - {{.IP}}
  ports:
  - number: 8084
    name: port-8084
    protocol: TCP
  - number: 8085
    name: port-8085
    protocol: TCP
  - number: 8086
    name: port-8086
    protocol: TCP
  - number: 8087
    name: port-8087
    protocol: TCP
  - number: 8088
    name: port-8088
    protocol: TCP
  - number: 8089
    name: port-8089
    protocol: TCP
  location: MESH_INTERNAL
  resolution: NONE`,
					map[string]interface{}{"IP": destWorkload})
				ctx.Config().ApplyYAMLOrFail(t, ns.Name(), `
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: default
spec:
  host: "fake.destination"
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
---
`+se)
				fmt.Println(`
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: default
spec:
  host: "*.cluster.local"
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
---
` + se)
				clientNaked := apps.Naked.Match(echo.InCluster(cluster)).GetOrFail(ctx, echo.Namespace(ns.Name()))
				clientTLS := apps.B.Match(echo.InCluster(cluster)).GetOrFail(ctx, echo.Namespace(ns.Name()))
				for i, client := range []echo.Instance{clientTLS, clientNaked} {
					mtls := i == 0
					nameSuffix := "plaintext"
					if mtls {
						nameSuffix = "mtls"
					}
					from := getWorkload(client, ctx)
					for _, tc := range cases {
						ctx.NewSubTest(fmt.Sprintf("In %s/%v/%v", cluster.StableName(), tc.name, nameSuffix)).Run(func(ctx framework.TestContext) {
							ctx.Config().ApplyYAMLOrFail(ctx, ns.Name(), tc.config)
							for _, expect := range tc.expected {
								want := expect.plaintext
								if mtls {
									want = expect.mtls
								}
								name := fmt.Sprintf("port %d[%t]", expect.port, want)

								// The request should be handled by the pass through filter chain.
								host := fmt.Sprintf("%s:%d", destWorkload, expect.port)
								request := &epb.ForwardEchoRequest{
									Url:     fmt.Sprintf("%s://%s", expect.schema, host),
									Message: "HelloWorld",
									Headers: []*epb.Header{
										{
											Key:   "Host",
											Value: host,
										},
									},
								}
								ctx.NewSubTest(name).Run(func(ctx framework.TestContext) {
									retry.UntilSuccessOrFail(ctx, func() error {
										responses, err := from.ForwardEcho(context.TODO(), request)
										if want {
											if err != nil {
												return fmt.Errorf("want allow but got error: %v", err)
											}
											if len(responses) < 1 {
												return fmt.Errorf("received no responses from request to %s", host)
											}
											if expect.schema == protocol.HTTP && response.StatusCodeOK != responses[0].Code {
												return fmt.Errorf("want status %s but got %s", response.StatusCodeOK, responses[0].Code)
											}
										} else {
											// Check HTTP forbidden response
											if len(responses) >= 1 && response.StatusCodeForbidden == responses[0].Code {
												return nil
											}

											if err == nil {
												return fmt.Errorf("want error but got none: %v", responses)
											}
										}
										return nil
									}, echo.DefaultCallRetryOptions()...)
								})
							}
						})
					}
				}
			}
		})
}

func getWorkload(instance echo.Instance, t test.Failer) echo.Workload {
	workloads, err := instance.Workloads()
	if err != nil {
		t.Fatalf(fmt.Sprintf("failed to get Subsets: %v", err))
	}
	if len(workloads) < 1 {
		t.Fatalf("want at least 1 workload but found 0")
	}
	return workloads[0]
}
