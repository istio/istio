//go:build integ

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

package pilot

import (
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
)

func TestDNSAutoAllocation(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			ns := namespace.NewOrFail(t, namespace.Config{
				Prefix: "dns-auto-allocation",
				Inject: true,
			})
			cfg := `apiVersion: networking.istio.io/v1beta1
kind: ProxyConfig
metadata:
  name: enable-dns-auto-allocation
spec:
  selector:
    matchLabels:
      app: a
  environmentVariables:
    ISTIO_META_DNS_CAPTURE: "true"
    ISTIO_META_DNS_AUTO_ALLOCATE: "true"
---
apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: fake-local
spec:
  hosts:
  - "fake.local"
  resolution: DNS
  ports:
  - number: 80
    name: http
    protocol: HTTP
---
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: route-to-b
spec:
  hosts:
  - fake.local
  http:
  - route:
    - destination:
        host: b.{{.echoNamespace}}.svc.cluster.local
`
			t.ConfigIstio().Eval(ns.Name(), map[string]string{"echoNamespace": apps.Namespace.Name()}, cfg).ApplyOrFail(t)
			instances := deployment.New(t, t.AllClusters().Configs()...).WithConfig(echo.Config{Namespace: ns, Service: "a"}).BuildOrFail(t)

			retry.UntilSuccessOrFail(t, func() error {
				_, err := instances[0].Call(echo.CallOptions{
					Address: "fake.local",
					Port: echo.Port{
						Name:        "http",
						ServicePort: 80,
						Protocol:    protocol.HTTP,
					},
					Check: check.OK(),
				})
				if err == nil {
					return nil
				}
				// trigger injection in case of delay of ProxyConfig propagation
				for _, i := range instances {
					if err := i.Restart(); err != nil {
						return fmt.Errorf("failed to restart echo instance: %v", err)
					}
				}
				return nil
			}, retry.Timeout(time.Second*30))
		})
}
