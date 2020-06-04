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
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/environment"
	"istio.io/istio/pkg/test/util/retry"
)

var p pilot.Instance
var ns namespace.Instance

// This tests VM mesh expansion. Rather than deal with the infra to get a real VM, we will use a pod
// with no Service, no DNS, no service account, etc to simulate a VM.
func TestMain(m *testing.M) {
	framework.
		NewSuite("vm_test", m).
		RequireSingleCluster().
		RequireEnvironment(environment.Kube).
		SetupOnEnv(environment.Kube, func(ctx resource.Context) error {
			var err error
			ns, err = namespace.New(ctx, namespace.Config{
				Prefix: "virtual-machine",
				Inject: true,
			})
			return err
		}).
		SetupOnEnv(environment.Kube, istio.Setup(nil, func(cfg *istio.Config) {
			cfg.ControlPlaneValues = `
values:
  global:
    meshExpansion:
      enabled: true`
		})).
		Setup(func(ctx resource.Context) (err error) {
			if p, err = pilot.New(ctx, pilot.Config{}); err != nil {
				return err
			}
			return nil
		}).
		Run()
}

func TestVmTraffic(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
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
			var pod, vm echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&vm, echo.Config{
					Service:    "vm",
					Namespace:  ns,
					Ports:      ports,
					Pilot:      p,
					DeployAsVM: true,
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
