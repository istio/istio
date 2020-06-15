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
	"bufio"
	"fmt"
	"os"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework/image"

	"istio.io/istio/pkg/test/framework/components/namespace"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/util/retry"
)

type VMConfig struct {
	VMHub   string
	VMTag   string
	VMImage string
}

func TestVmTraffic(t *testing.T) {
	// read from a config list to construct the test matrix
	file, err := os.Open("testdata/OS_Support_List")
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(file)
	configList := make([]string, 0)
	for scanner.Scan() {
		imageName := scanner.Text()
		// ignore empty line
		if imageName == "" {
			continue
		}
		configList = append(configList, imageName)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	VMConfigs := make([]VMConfig, 0)

	// read Hub and Tag info from command line
	settings, err := image.SettingsFromCommandLine()
	if err != nil {
		t.Fatal(err)
	}
	for _, OSImage := range configList {
		VMConfigs = append(VMConfigs, VMConfig{
			VMHub:   settings.Hub,
			VMTag:   settings.Tag,
			VMImage: OSImage,
		})
	}

	framework.
		NewTest(t).
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
			for i, VMConfig := range VMConfigs {
				ctx.NewSubTest(VMConfig.VMImage).
					Run(func(ctx framework.TestContext) {
						var pod, vm echo.Instance
						echoboot.NewBuilderOrFail(t, ctx).
							With(&vm, echo.Config{
								Service:    fmt.Sprintf("vm-legacy-%v", i),
								Namespace:  ns,
								Ports:      ports,
								Pilot:      p,
								DeployAsVM: true,
								VMHub:      VMConfig.VMHub,
								VMTag:      VMConfig.VMTag,
								VMImage:    VMConfig.VMImage,
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
