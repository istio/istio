// Copyright 2019 Istio Authors
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
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/response"
	epb "istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
)

func getWorkload(instance echo.Instance, t *testing.T) echo.Workload {
	workloads, err := instance.Workloads()
	if err != nil {
		t.Fatalf(fmt.Sprintf("failed to get workloads: %v", err))
	}
	if len(workloads) < 1 {
		t.Fatalf("want at least 1 workload but found 0")
	}
	return workloads[0]
}

// TestV1beta1_PassThroughFilterChain tests the v1beta1 authorization policy works on the passthrough filter chain.
func TestV1beta1_PassThroughFilterChain(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "v1beta1-pass-through",
				Inject: true,
			})

			// Apply the authorization policy to allow access to server-1 and deny access to server-2.
			args := map[string]string{
				"Namespace": ns.Name(),
			}
			policies := tmpl.EvaluateAllOrFail(t, args,
				file.AsStringOrFail(t, "testdata/rbac/v1beta1-pass-through.yaml.tmpl"))
			g.ApplyConfigOrFail(t, ns, policies...)
			defer g.DeleteConfigOrFail(t, ns, policies...)

			// client is created without sidecar to allow us to send traffic to server at
			// ports not defined in the service.
			// server-1 and server-2 are created without port 8081 (HTTP) in its service.
			var client, server1, server2 echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&client, echo.Config{
					Service:     "client",
					Namespace:   ns,
					Galley:      g,
					Pilot:       p,
					Annotations: echo.NewAnnotations().Set(echo.SidecarInject, "false"),
				}).
				With(&server1, echo.Config{
					Service:   "server-1",
					Namespace: ns,
					Galley:    g,
					Pilot:     p,
					Ports: []echo.Port{
						{
							Name:     "grpc",
							Protocol: protocol.GRPC,
						},
					},
				}).
				With(&server2, echo.Config{
					Service:   "server-2",
					Namespace: ns,
					Galley:    g,
					Pilot:     p,
					Ports: []echo.Port{
						{
							Name:     "grpc",
							Protocol: protocol.GRPC,
						},
					},
				}).BuildOrFail(t)

			clientWorkload := getWorkload(client, t)
			server1Workload := getWorkload(server1, t)
			server2Workload := getWorkload(server2, t)
			cases := []struct {
				name string
				ip   string
				want bool
			}{
				{
					name: "server-1",
					ip:   server1Workload.Address(),
					want: true,
				},
				{
					name: "server-2",
					ip:   server2Workload.Address(),
					want: false,
				},
			}

			verify := func(ip string, want bool) error {
				// Send the requests at port 8081 (HTTP) which is not defined in the service of
				// server-1 and server-2. The request should be handled by the passthrough filter chain.
				host := fmt.Sprintf("%s:8081", ip)
				responses, err := clientWorkload.ForwardEcho(context.TODO(), &epb.ForwardEchoRequest{
					Url:   fmt.Sprintf("http://%s/", host),
					Count: 1,
					Headers: []*epb.Header{
						{
							Key:   "Host",
							Value: host,
						},
					},
				})

				if want {
					if err != nil {
						return fmt.Errorf("failed to make request to %s: %v", host, err)
					}
					if len(responses) < 1 {
						return fmt.Errorf("received no responses from request to %s", host)
					}
					if response.StatusCodeOK != responses[0].Code {
						return fmt.Errorf("want status %s but got %s", response.StatusCodeOK, responses[0].Code)
					}
				} else {
					if err == nil {
						return fmt.Errorf("want error but got nil")
					}
					if !strings.Contains(err.Error(), "EOF") {
						return fmt.Errorf("want error EOF but got: %v", err)
					}
				}
				return nil
			}
			for _, tc := range cases {
				name := ""
				if tc.want {
					name = fmt.Sprintf("%s[allow]", tc.name)
				} else {
					name = fmt.Sprintf("%s[deny]", tc.name)
				}
				t.Run(name, func(t *testing.T) {
					retry.UntilSuccessOrFail(t, func() error {
						return verify(tc.ip, tc.want)
					}, retry.Delay(250*time.Millisecond), retry.Timeout(30*time.Second))
				})
			}
		})
}
