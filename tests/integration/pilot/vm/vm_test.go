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
	"testing"
	"time"

	"istio.io/istio/pkg/test/echo/common/response"
	epb "istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/util/retry"
)

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
					Name:         "http",
					Protocol:     protocol.HTTP,
					ServicePort:  8090,
					InstancePort: 10090,
				},
			}

			clusterServiceHostname := "c"
			headlessServiceHostname := "h"
			var k8sClusterIPService echo.Instance
			var k8sHeadlessService echo.Instance
			// builder to build the instances iteratively
			echoboot.NewBuilderOrFail(t, ctx).
				With(&k8sClusterIPService, echo.Config{
					Service:   clusterServiceHostname,
					Namespace: ns,
					Ports:     ports,
				}).
				BuildOrFail(t)

				// TargetPort does not work for headless services because headless services are orig dst clusters
				// i.e. we simply forward traffic as is. So, when creating the echo instance for the headless
				// service, set the target port and instance port to be the same so that the server side listens on
				// the same port as the service port.
			echoboot.NewBuilderOrFail(t, ctx).
				With(&k8sHeadlessService, echo.Config{
					Service:   headlessServiceHostname,
					Namespace: ns,
					Ports: []echo.Port{
						{
							Name:         "http",
							Protocol:     protocol.HTTP,
							ServicePort:  8090,
							InstancePort: 8090,
						},
					},
					Headless: true,
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
						name: "dns: VM to k8s headless service",
						from: vm,
						to:   k8sHeadlessService,
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

				ctx.NewSubTest(fmt.Sprintf("dns: VM proxy resolves unknown hosts using system resolver using %v",
					vmImages[i])).Run(func(ctx framework.TestContext) {
					w := vm.WorkloadsOrFail(ctx)[0]
					externalURL := "http://www.bing.com"
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
			}
		})
}
