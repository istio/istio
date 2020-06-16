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
	vmImages := []string{"app_sidecar_xenial", "app_sidecar_focal", "app_sidecar_bionic", "app_sidecar_debian9",
		"app_sidecar_debian10"}

	framework.
		NewTest(t).
		Features("extensibility.vm").
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
			for i, vmImage := range vmImages {
				ctx.NewSubTest(vmImage).
					Run(func(ctx framework.TestContext) {
						var pod, vm echo.Instance
						echoboot.NewBuilderOrFail(t, ctx).
							With(&vm, echo.Config{
								Service:    fmt.Sprintf("vm-legacy-%v", i),
								Namespace:  ns,
								Ports:      ports,
								Pilot:      p,
								DeployAsVM: true,
								VMImage:    vmImage,
							}).
							With(&pod, echo.Config{
								Service:   "pod",
								Namespace: ns,
								Ports:     ports,
								Pilot:     p,
							}).
							BuildOrFail(t)

						// Check pod -> VM
						retry.UntilSuccessOrFail(ctx, func() error {
							r, err := pod.Call(echo.CallOptions{Target: vm, PortName: "http"})
							if err != nil {
								return err
							}
							return r.CheckOK()
						}, retry.Delay(100*time.Millisecond))
						// Check VM -> pod
						retry.UntilSuccessOrFail(ctx, func() error {
							r, err := vm.Call(echo.CallOptions{Target: pod, PortName: "http"})
							if err != nil {
								return err
							}
							return r.CheckOK()
						}, retry.Delay(100*time.Millisecond))
					})
			}
		})
}
