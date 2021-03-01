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

package util

import (
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

func EchoConfig(name string, ns namespace.Instance, headless bool, annos echo.Annotations) echo.Config {
	out := echo.Config{
		Service:        name,
		Namespace:      ns,
		ServiceAccount: true,
		Headless:       headless,
		Subsets: []echo.SubsetConfig{
			{
				Version:     "v1",
				Annotations: annos,
			},
		},
		Ports: []echo.Port{
			{
				Name:     "http",
				Protocol: protocol.HTTP,
				// We use a port > 1024 to not require root
				InstancePort: 8090,
			},
			{
				Name:     "tcp",
				Protocol: protocol.TCP,
			},
			{
				Name:     "grpc",
				Protocol: protocol.GRPC,
			},
		},
	}

	// for headless service with selector, the port and target port must be equal
	// Ref: https://kubernetes.io/docs/concepts/services-networking/service/#headless-services
	if headless {
		out.Ports[0].ServicePort = 8090
	}
	return out
}

func WaitForConfigWithSleep(ctx framework.TestContext, testName, configs string, namespace namespace.Instance) {
	ik := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})
	t0 := time.Now()
	if err := ik.WaitForConfigs(namespace.Name(), configs); err != nil {
		// Continue anyways, so we can assess the effectiveness of using `istioctl wait`
		ctx.Logf("warning: failed to wait for config: %v", err)
		// Get proxy status for additional debugging
		s, _, _ := ik.Invoke([]string{"ps"})
		ctx.Logf("proxy status: %v", s)
	}
	// TODO(https://github.com/istio/istio/issues/25945) introducing istioctl wait in favor of a 10s sleep lead to flakes
	// to work around this, we will temporarily make sure we are always sleeping at least 10s, even if istioctl wait is faster.
	// This allows us to debug istioctl wait, while still ensuring tests are stable
	sleep := time.Second*10 - time.Since(t0)
	ctx.Logf("[%s] [%v] Wait for additional %v config propagate to endpoints...", testName, time.Now(), sleep)
	time.Sleep(sleep)
	ctx.Logf("[%s] [%v] Finish waiting. Continue testing.", testName, time.Now())
}
