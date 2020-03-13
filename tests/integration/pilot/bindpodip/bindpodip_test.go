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

package bindpodip

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	"github.com/golang/protobuf/ptypes"

	"istio.io/istio/istioctl/pkg/util/configdump"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/response"
	epb "istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	i istio.Instance
	g galley.Instance
	p pilot.Instance
)

// TestMain defines the entrypoint for pilot tests using a standard Istio installation.
// If a test requires a custom install it should go into its own package, otherwise it should go
// here to reuse a single install across tests.
func TestMain(m *testing.M) {
	framework.
		NewSuite("bindpodip_test", m).
		Label(label.CustomSetup).
		RequireEnvironment(environment.Kube).
		SetupOnEnv(environment.Kube, istio.Setup(&i, nil)).
		Setup(func(ctx resource.Context) (err error) {
			if g, err = galley.New(ctx, galley.Config{}); err != nil {
				return err
			}
			return nil
		}).
		Setup(func(ctx resource.Context) (err error) {
			if p, err = pilot.New(ctx, pilot.Config{
				Galley: g,
			}); err != nil {
				return err
			}
			return nil
		}).
		Run()
}

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
			sidecar := serverWorkload.Sidecar()
			podIP := strings.Split(sidecar.NodeID(), "~")[1]
			expectedPortToEndpointMap := map[string]string{
				"5555": "127.0.0.1",
				"6666": podIP,
				"7777": podIP,
			}
			configDump, err := sidecar.Config()
			if err != nil {
				t.Fatal(err)
			}
			wrapper := configdump.Wrapper{ConfigDump: configDump}
			dcd, err := wrapper.GetClusterConfigDump()
			if err != nil {
				t.Fatal(err)
			}
			for _, cluster := range dcd.DynamicActiveClusters {
				clusterTyped := &xdsapi.Cluster{}
				err = ptypes.UnmarshalAny(cluster.Cluster, clusterTyped)
				if err != nil {
					t.Fatal(err)
				}
				for port, bind := range expectedPortToEndpointMap {
					if strings.HasPrefix(clusterTyped.Name, fmt.Sprintf("%s|%s", model.TrafficDirectionInbound, port)) {
						ep, ok := clusterTyped.LoadAssignment.Endpoints[0].LbEndpoints[0].HostIdentifier.(*endpoint.LbEndpoint_Endpoint)
						if !ok {
							t.Errorf("failed convert endpoint")
							break
						}
						sa, ok := ep.Endpoint.Address.Address.(*core.Address_SocketAddress)
						if !ok {
							t.Errorf("failed convert address")
						}
						addr := sa.SocketAddress.Address
						if addr != bind {
							t.Errorf("port %s is expected to be bind to %s but got %s", port, bind, addr)
						}
						break
					}
				}
			}

			for port := range expectedPortToEndpointMap {
				name := fmt.Sprintf("client->%s:%s", instance.Config().Service, port)
				host := fmt.Sprintf("%s:%s", serverWorkload.Address(), port)
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
