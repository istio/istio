// Copyright 2020 Istio Authors
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

package pilot

import (
	"context"
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/response"
	epb "istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource/environment"
	"istio.io/istio/pkg/test/util/retry"
)

func TestBindPodIPPorts(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "bindpodip",
				Inject: true,
			})

			var client echo.Instance
			var instance echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&instance, echo.Config{
					Service:   "server",
					Namespace: ns,
					Subsets:   []echo.SubsetConfig{{Annotations: echo.NewAnnotations().Set(echo.BindPodIPPorts, "6666,7777")}},
					Pilot:     p,
					Galley:    g,
					Ports: []echo.Port{
						{
							Name:         "http5",
							Protocol:     protocol.HTTP,
							ServicePort:  5555,
							InstancePort: 5555,
						},
						{
							Name:         "http6",
							Protocol:     protocol.HTTP,
							ServicePort:  6666,
							InstancePort: 6666,
						},
						{
							Name:         "http7",
							Protocol:     protocol.HTTP,
							ServicePort:  7777,
							InstancePort: 7777,
						},
					},
					BindPodIPPorts: []int{6666, 7777},
				}).With(&client, echo.Config{
				Service:   "client",
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
				BuildOrFail(t)

			clientWorkload := getWorkload(client, t)
			serverWorkload := getWorkload(instance, t)
			expectPorts := []int{5555, 6666, 7777}

			for _, port := range expectPorts {
				name := fmt.Sprintf("client->%s:%d", instance.Config().Service, port)
				host := fmt.Sprintf("%s:%d", serverWorkload.Address(), port)
				request := &epb.ForwardEchoRequest{
					Url:   fmt.Sprintf("http://%s/", host),
					Count: 1,
					Headers: []*epb.Header{
						{
							Key:   "Host",
							Value: host,
						},
					},
				}
				t.Run(name, func(t *testing.T) {
					retry.UntilSuccessOrFail(t, func() error {
						responses, err := clientWorkload.ForwardEcho(context.TODO(), request)
						if err != nil {
							return fmt.Errorf("want allow but got error: %v", err)
						}
						if len(responses) < 1 {
							return fmt.Errorf("received no responses from request to %s", host)
						}
						if response.StatusCodeOK != responses[0].Code {
							return fmt.Errorf("want status %s but got %s", response.StatusCodeOK, responses[0].Code)
						}
						return nil
					}, retry.Delay(250*time.Millisecond), retry.Timeout(30*time.Second))
				})
			}
		})
}

func getWorkload(instance echo.Instance, t *testing.T) echo.Workload {
	workloads, err := instance.Workloads()
	if err != nil {
		t.Fatalf(fmt.Sprintf("failed to get Subsets: %v", err))
	}
	if len(workloads) < 1 {
		t.Fatalf("want at least 1 workload but found 0")
	}
	return workloads[0]
}
