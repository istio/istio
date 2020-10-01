// +build integ
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
	"io/ioutil"
	"testing"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/tests/integration/pilot/common"
)

var (
	i istio.Instance

	// Below are various preconfigured echo deployments. Whenever possible, tests should utilize these
	// to avoid excessive creation/tear down of deployments. In general, a test should only deploy echo if
	// its doing something unique to that specific test.
	apps = &common.EchoDeployments{}
)

// TestMain defines the entrypoint for pilot tests using a standard Istio installation.
// If a test requires a custom install it should go into its own package, otherwise it should go
// here to reuse a single install across tests.
func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Setup(func(ctx resource.Context) (err error) {
			crd, err := ioutil.ReadFile("testdata/service-apis-crd.yaml")
			if err != nil {
				return err
			}
			return ctx.Config().ApplyYAML("", string(crd))
		}).
		Setup(istio.Setup(&i, func(ctx resource.Context, cfg *istio.Config) {
			cfg.Values["telemetry.v2.metadataExchange.wasmEnabled"] = "false"
			cfg.Values["telemetry.v2.prometheus.wasmEnabled"] = "false"
			cfg.ControlPlaneValues = `
# Add TCP port, not in the default install
components:
  ingressGateways:
    - name: istio-ingressgateway
      enabled: true
      k8s:
        service:
          ports:
            - port: 15021
              targetPort: 15021
              name: status-port
            - port: 80
              targetPort: 8080
              name: http2
            - port: 443
              targetPort: 8443
              name: https
            - port: 15443
              targetPort: 15443
              name: tls
            - port: 31400
              name: tcp
            - port: 15012
              targetPort: 15012
              name: tcp-istiod
values:
  pilot:
    env:
      PILOT_ENABLED_SERVICE_APIS: "true"`
		})).
		Setup(func(ctx resource.Context) error {
			return common.SetupApps(ctx, i, apps)
		}).
		Run()
}

func echoConfig(ns namespace.Instance, name string) echo.Config {
	return echo.Config{
		Service:   name,
		Namespace: ns,
		Ports: []echo.Port{
			{
				Name:     "http",
				Protocol: protocol.HTTP,
				// We use a port > 1024 to not require root
				InstancePort: 8090,
			},
		},
		Subsets: []echo.SubsetConfig{{}},
	}
}
