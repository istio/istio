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
	"io/ioutil"
	"strconv"
	"testing"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	i istio.Instance

	ingr ingress.Instance

	// Below are various preconfigured echo deployments. Whenever possible, tests should utilize these
	// to avoid excessive creation/tear down of deployments. In general, a test should only deploy echo if
	// its doing something unique to that specific test.
	apps EchoDeployments

	echoPorts = []echo.Port{
		{Name: "http", Protocol: protocol.HTTP, InstancePort: 18080},
		{Name: "grpc", Protocol: protocol.GRPC, InstancePort: 17070},
		{Name: "tcp", Protocol: protocol.TCP, InstancePort: 19090},
		{Name: "auto-tcp", Protocol: protocol.TCP, InstancePort: 19091},
		{Name: "auto-http", Protocol: protocol.HTTP, InstancePort: 18081},
		{Name: "auto-grpc", Protocol: protocol.GRPC, InstancePort: 17071},
	}
)

type EchoDeployments struct {
	// Namespace echo apps will be deployed
	namespace namespace.Instance
	// Namespace where external echo app will be deployed
	externalNamespace namespace.Instance

	// Standard echo app to be used by tests
	podA echo.Instance
	// Standard echo app to be used by tests
	podB echo.Instance
	// Standard echo app to be used by tests
	podC echo.Instance
	// Headless echo app to be used by tests
	headless echo.Instance
	// Echo app to be used by tests, with no sidecar injected
	naked echo.Instance
	// A virtual machine echo app
	vmA echo.Instance

	// Echo app to be used by tests, with no sidecar injected
	external echo.Instance
	// Fake hostname of external service
	externalHost string
}

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
		Setup(istio.Setup(&i, func(cfg *istio.Config) {
			cfg.Values["telemetry.v2.metadataExchange.wasmEnabled"] = "false"
			cfg.Values["telemetry.v2.prometheus.wasmEnabled"] = "false"
			cfg.ControlPlaneValues = `
values:
  global:
    meshExpansion:
      enabled: true
  pilot:
    env:
      PILOT_ENABLED_SERVICE_APIS: "true"`
		})).
		Setup(func(ctx resource.Context) error {
			var err error
			apps.namespace, err = namespace.New(ctx, namespace.Config{
				Prefix: "echo",
				Inject: true,
			})
			if err != nil {
				return err
			}
			apps.externalNamespace, err = namespace.New(ctx, namespace.Config{
				Prefix: "external",
				Inject: false,
			})
			if err != nil {
				return err
			}
			// Headless services don't work with targetPort, set to same port
			headlessPorts := make([]echo.Port, len(echoPorts))
			for i, p := range echoPorts {
				p.ServicePort = p.InstancePort
				headlessPorts[i] = p
			}
			if _, err := echoboot.NewBuilder(ctx).
				With(&apps.podA, echo.Config{
					Service:   "a",
					Namespace: apps.namespace,
					Ports:     echoPorts,
					Subsets:   []echo.SubsetConfig{{}},
					Locality:  "region.zone.subzone",
				}).
				With(&apps.podB, echo.Config{
					Service:   "b",
					Namespace: apps.namespace,
					Ports:     echoPorts,
					Subsets:   []echo.SubsetConfig{{}},
				}).
				With(&apps.podC, echo.Config{
					Service:   "c",
					Namespace: apps.namespace,
					Ports:     echoPorts,
					Subsets:   []echo.SubsetConfig{{}},
				}).
				With(&apps.headless, echo.Config{
					Service:   "headless",
					Headless:  true,
					Namespace: apps.namespace,
					Ports:     headlessPorts,
					Subsets:   []echo.SubsetConfig{{}},
				}).
				With(&apps.vmA, echo.Config{
					Service:    "vm-a",
					Namespace:  apps.namespace,
					Ports:      echoPorts,
					DeployAsVM: true,
					Subsets:    []echo.SubsetConfig{{}},
				}).
				With(&apps.naked, echo.Config{
					Service:   "naked",
					Namespace: apps.namespace,
					Ports:     echoPorts,
					Subsets: []echo.SubsetConfig{
						{
							Annotations: map[echo.Annotation]*echo.AnnotationValue{
								echo.SidecarInject: {
									Value: strconv.FormatBool(false)},
							},
						},
					},
				}).Build(); err != nil {
				return err
			}
			if _, err := echoboot.NewBuilder(ctx).
				With(&apps.external, echo.Config{
					Service:   "external",
					Namespace: apps.externalNamespace,
					Ports:     echoPorts,
					Subsets: []echo.SubsetConfig{
						{
							Annotations: map[echo.Annotation]*echo.AnnotationValue{
								echo.SidecarInject: {
									Value: strconv.FormatBool(false)},
							},
						},
					},
				}).
				Build(); err != nil {
				return err
			}
			return nil
		}).
		Setup(func(ctx resource.Context) (err error) {
			ingr = i.IngressFor(ctx.Clusters().Default())

			apps.externalHost = "fake.example.com"
			if err := ctx.Config().ApplyYAML(apps.namespace.Name(), fmt.Sprintf(`
apiVersion: networking.istio.io/v1alpha3
kind: Sidecar
metadata:
  name: restrict-to-namespace
spec:
  egress:
  - hosts:
    - "./*"
    - "istio-system/*"
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: external-service
spec:
  hosts:
  - %s
  location: MESH_EXTERNAL
  ports:
  - name: http
    number: 80
    protocol: HTTP
  - name: grpc
    number: 7070
    protocol: GRPC
  resolution: DNS
  endpoints:
  - address: external.%s
`, apps.externalHost, apps.externalNamespace.Name())); err != nil {
				return err
			}
			return nil
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
