// Copyright 2019 Istio Authors
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

package ingress

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	i    istio.Instance
	g    galley.Instance
	p    pilot.Instance
	ingr ingress.Instance
)

// TestMain defines the entrypoint for pilot tests using a standard Istio installation.
// If a test requires a custom install it should go into its own package, otherwise it should go
// here to reuse a single install across tests.
func TestMain(m *testing.M) {
	framework.
		NewSuite("pilot_test", m).
		Label(label.CustomSetup).
		RequireEnvironment(environment.Kube).
		SetupOnEnv(environment.Kube, istio.Setup(&i, func(cfg *istio.Config) {
			cfg.Values["global.k8sIngress.enabled"] = "true"
		})).
		Setup(func(ctx resource.Context) (err error) {
			if g, err = galley.New(ctx, galley.Config{}); err != nil {
				return err
			}
			if p, err = pilot.New(ctx, pilot.Config{
				Galley: g,
			}); err != nil {
				return err
			}
			if ingr, err = ingress.New(ctx, ingress.Config{
				Istio: i,
			}); err != nil {
				return err
			}
			return nil
		}).
		Run()
}

func TestIngress(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "ingress",
				Inject: true,
			})
			var instance echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&instance, echo.Config{
					Service:   "server",
					Namespace: ns,
					Pilot:     p,
					Galley:    g,
					Ports: []echo.Port{
						{
							Name:     "http",
							Protocol: protocol.HTTP,
							// We use a port > 1024 to not require root
							InstancePort: 8090,
						},
					},
				}).
				BuildOrFail(t)
			instance.Address()

			if err := g.ApplyConfig(ns, `
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  annotations:
    kubernetes.io/ingress.class: istio
  name: ingress
spec:
  rules:
    - http:
        paths:
          - path: /
            backend:
              serviceName: server
              servicePort: 80`,
			); err != nil {
				t.Fatal(err)
			}

			if err := retry.UntilSuccess(func() error {
				resp, err := ingr.Call(ingress.CallOptions{
					Host:     "server",
					Path:     "/",
					CallType: ingress.PlainText,
					Address:  ingr.HTTPAddress(),
				})
				if err != nil {
					return err
				}
				if resp.Code != 200 {
					return fmt.Errorf("got invalid response code %v: %v", resp.Code, resp.Body)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
}
