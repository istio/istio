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

package vm

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"istio.io/istio/pkg/test/echo/common/response"
	epb "istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/tmpl"

	"istio.io/istio/pkg/test/framework/components/namespace"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/util/retry"
)

type TrafficShiftConfig struct {
	Name      string
	Namespace string
	Host      string
	Subset0   string
	Subset1   string
	Weight0   int32
	Weight1   int32
}

// Test wrapper for the VM OS version test. This test will run in pre-submit
// to avoid building and testing all OS images
func TestVmOS(t *testing.T) {
	vmImages := []string{DefaultVMImage}
	VMTestBody(t, vmImages)
}

// Post-submit test wrapper to test against all OS images. These images will be build
// in post-submit to reduce the runtime of prow/lib.sh
func TestVmOSPost(t *testing.T) {
	vmImages := GetSupportedOSVersion()
	VMTestBody(t, vmImages, label.Postsubmit)
}

func VMTestBody(t *testing.T, vmImages []string, label ...label.Instance) {
	framework.
		NewTest(t).
		Features("traffic.reachability").
		Label(label...).
		Run(func(ctx framework.TestContext) {
			ns = namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "virtual-machine",
				Inject: true,
			})
			// Set up strict mTLS. This gives a bit more assurance the calls are actually going through envoy,
			// and certs are set up correctly.
			ctx.Config().ApplyYAMLOrFail(ctx, ns.Name(), `
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: default
spec:
  mtls:
    mode: STRICT
---
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: send-mtls
spec:
  host: "*.svc.cluster.local"
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
`)
			ports := []echo.Port{
				{
					Name:     "http",
					Protocol: protocol.HTTP,
					// Due to a bug in WorkloadEntry, service port must equal target port for now
					InstancePort: 8090,
					ServicePort:  8090,
				},
			}

			clusterServiceHostname := "cluster"
			headlessServiceHostname := "headless"
			var k8sClusterIPService echo.Instance
			var k8sHeadlessService echo.Instance
			// builder to build the instances iteratively
			echoboot.NewBuilderOrFail(t, ctx).
				With(&k8sClusterIPService, echo.Config{
					Service:   clusterServiceHostname,
					Namespace: ns,
					Ports:     ports,
					Pilot:     p,
				}).
				BuildOrFail(t)

			echoboot.NewBuilderOrFail(t, ctx).
				With(&k8sHeadlessService, echo.Config{
					Service:   headlessServiceHostname,
					Namespace: ns,
					Ports:     ports,
					Pilot:     p,
					Headless:  true,
				}).
				BuildOrFail(t)

			// build the VM instances in the array
			for i, vmImage := range vmImages {
				var vm echo.Instance
				echoboot.NewBuilderOrFail(t, ctx).
					With(&vm, echo.Config{
						Service:    fmt.Sprintf("vm-%v", i),
						Namespace:  ns,
						Ports:      ports,
						Pilot:      p,
						DeployAsVM: true,
						VMImage:    vmImage,
					}).
					BuildOrFail(t)

				testCases := []struct {
					name string
					from echo.Instance
					to   echo.Instance
					host string
				}{
					{
						name: "k8s to vm",
						from: k8sClusterIPService,
						to:   vm,
					},
					{
						name: "dns: VM to k8s cluster IP service fqdn host",
						from: vm,
						to:   k8sClusterIPService,
						host: k8sClusterIPService.Config().FQDN(),
					},
					{
						name: "dns: VM to k8s cluster IP service name.namespace host",
						from: vm,
						to:   k8sClusterIPService,
						host: clusterServiceHostname + "." + ns.Name(),
					},
					{
						name: "dns: VM to k8s cluster IP service short name host",
						from: vm,
						to:   k8sClusterIPService,
						host: clusterServiceHostname,
					},
					{
						name: "dns: VM to k8s headless service short name host",
						from: vm,
						to:   k8sHeadlessService,
						host: headlessServiceHostname,
					},
				}

				for _, tt := range testCases {
					ctx.NewSubTest(fmt.Sprintf("%s using image %v", tt.name, vmImages[i])).
						Run(func(ctx framework.TestContext) {
							retry.UntilSuccessOrFail(ctx, func() error {
								r, err := tt.from.Call(echo.CallOptions{
									Target:   tt.to,
									PortName: "http",
									Host:     tt.host,
								})
								if err != nil {
									return err
								}
								return r.CheckOK()
							}, retry.Delay(100*time.Millisecond))
						})
				}

				ctx.NewSubTest(fmt.Sprintf("VM proxy resolves unknown hosts using system resolver using %v",
					vmImages[i])).Run(func(ctx framework.TestContext) {
					w := vm.WorkloadsOrFail(ctx)[0]
					externalURL := "http://www.google.com"
					responses, err := w.ForwardEcho(context.TODO(), &epb.ForwardEchoRequest{
						Url:   externalURL,
						Count: 1,
					})
					if err != nil {
						ctx.Fatalf("failed to make request from VM echo instance to %s: %v", externalURL, err)
					}
					if len(responses) < 1 {
						ctx.Fatalf("received no responses from VM request to %s", externalURL)
					}
					resp := responses[0]

					if response.StatusCodeOK != resp.Code {
						ctx.Errorf("expected status %s but got %s", response.StatusCodeOK, resp.Code)
					}
				})

				// now launch another proper k8s pod for the same VM service, such that the VM service is made
				// of a pod and a VM. Set the subset for this pod to v2 so that we can check traffic shift
				echoboot.NewBuilderOrFail(t, ctx).
					With(&vm, echo.Config{
						Service:    fmt.Sprintf("vm-%v", i),
						Namespace:  ns,
						Ports:      ports,
						Pilot:      p,
						DeployAsVM: false,
						Version:    "v2", // WHY?
						Subsets: []echo.SubsetConfig{
							{
								Version: "v2",
							},
						},
					}).
					BuildOrFail(t)

				ctx.NewSubTest(fmt.Sprintf("traffic shifts for workload entries %v",
					vmImages[i])).Run(func(ctx framework.TestContext) {

					vsc := TrafficShiftConfig{
						"traffic-shifting-rule-wle",
						ns.Name(),
						fmt.Sprintf("vm-%v", i),
						"v1",
						"v2",
						50,
						50,
					}
					deployment := tmpl.EvaluateOrFail(t, file.AsStringOrFail(t, "testdata/traffic-shifting.yaml"), vsc)
					ctx.Config().ApplyYAMLOrFail(t, ns.Name(), deployment)
					sendTraffic(t, 100, k8sClusterIPService, vm, []string{"v1", "v2"}, []int32{50, 50}, 10.0)
				})
			}
		})
}

// TODO: dedup from main traffic shift test
func sendTraffic(t *testing.T, batchSize int, from, to echo.Instance, versions []string, weight []int32, errorThreshold float64) {
	t.Helper()
	// Send `batchSize` requests and ensure they are distributed as expected.
	retry.UntilSuccessOrFail(t, func() error {
		resp, err := from.Call(echo.CallOptions{
			Target:   to,
			PortName: "http",
			Count:    batchSize,
		})
		if err != nil {
			return fmt.Errorf("error during call: %v", err)
		}
		var totalRequests int
		hitCount := map[string]int{}
		for _, r := range resp {
			for _, v := range versions {
				if r.Version == v {
					hitCount[v]++
					totalRequests++
					break
				}
			}
		}

		for i, v := range versions {
			percentOfTrafficToVersion := float64(hitCount[v]) * 100.0 / float64(totalRequests)
			deltaFromExpected := math.Abs(float64(weight[i]) - percentOfTrafficToVersion)
			if errorThreshold-deltaFromExpected < 0 {
				return fmt.Errorf("unexpected traffic weight for version %v. Expected %d%%, got %g%% (thresold: %g%%)",
					v, weight[i], percentOfTrafficToVersion, errorThreshold)
			}
			t.Logf("Got expected traffic weight for version %v. Expected %d%%, got %g%% (thresold: %g%%)",
				v, weight[i], percentOfTrafficToVersion, errorThreshold)
		}
		return nil
	}, retry.Delay(time.Second))
}
