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
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework/components/namespace"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/util/retry"
)

func TestVmOS(t *testing.T) {
	// read from a config list to construct the test matrix
	vmImages := []string{"app_sidecar_ubuntu_xenial", "app_sidecar_ubuntu_focal", "app_sidecar_ubuntu_bionic",
		"app_sidecar_debian_9", "app_sidecar_debian_10"}

	framework.
		NewTest(t).
		Features("traffic.reachability").
		Run(func(ctx framework.TestContext) {
			ns = namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "virtual-machine",
				Inject: true,
			})
			// Set up strict mTLS. This gives a bit more assurance the calls are actually going through envoy,
			// and certs are set up correctly.
			ctx.ApplyConfigOrFail(ctx, ns.Name(), `
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

			var pod echo.Instance
			vmInstances := make([]echo.Instance, len(vmImages))

			// builder to build the instances iteratively
			echoBuilder := echoboot.NewBuilderOrFail(t, ctx).
				With(&pod, echo.Config{
					Service:   "pod",
					Namespace: ns,
					Ports:     ports,
					Pilot:     p,
				})

			// build the VM instances in the array
			for i := 0; i < len(vmImages); i++ {
				echoBuilder = echoBuilder.With(&(vmInstances[i]), echo.Config{
					Service:    fmt.Sprintf("vm-legacy-%v", i),
					Namespace:  ns,
					Ports:      ports,
					Pilot:      p,
					DeployAsVM: true,
					VMImage:    vmImages[i],
				})
			}
			echoBuilder.BuildOrFail(t)

			for i, vmInstance := range vmInstances {
				t.Logf("Testing %v", vmImages[i])
				// Check pod -> VM
				retry.UntilSuccessOrFail(ctx, func() error {
					r, err := pod.Call(echo.CallOptions{Target: vmInstance, PortName: "http"})
					if err != nil {
						return err
					}
					return r.CheckOK()
				}, retry.Delay(100*time.Millisecond))
				// Check VM -> pod
				retry.UntilSuccessOrFail(ctx, func() error {
					r, err := vmInstance.Call(echo.CallOptions{Target: pod, PortName: "http"})
					if err != nil {
						return err
					}
					return r.CheckOK()
				}, retry.Delay(100*time.Millisecond))
			}
		})
}
