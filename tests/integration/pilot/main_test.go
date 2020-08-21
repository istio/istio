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
		{Name: "tcp-server", Protocol: protocol.TCP, InstancePort: 16060, ServerFirst: true},
		{Name: "auto-tcp", Protocol: protocol.TCP, InstancePort: 19091},
		{Name: "auto-tcp-server", Protocol: protocol.TCP, InstancePort: 16061, ServerFirst: true},
		{Name: "auto-http", Protocol: protocol.HTTP, InstancePort: 18081},
		{Name: "auto-grpc", Protocol: protocol.GRPC, InstancePort: 17071},
	}
)

const (
	podASvc     = "a"
	podBSvc     = "b"
	podCSvc     = "c"
	vmASvc      = "vm-a"
	headlessSvc = "headless"
	nakedSvc    = "naked"
	externalSvc = "external"
)

type EchoDeployments struct {
	// Namespace echo apps will be deployed
	namespace namespace.Instance
	// Namespace where external echo app will be deployed
	externalNamespace namespace.Instance

	// Standard echo app to be used by tests
	podA echo.Instances
	// Standard echo app to be used by tests
	podB echo.Instances
	// Standard echo app to be used by tests
	podC echo.Instances
	// Headless echo app to be used by tests
	headless echo.Instances
	// Echo app to be used by tests, with no sidecar injected
	naked echo.Instances
	// A virtual machine echo app (only deployed to one cluster)
	vmA echo.Instances

	// Echo app to be used by tests, with no sidecar injected
	external echo.Instances
	// Fake hostname of external service
	externalHost string

	all echo.Instances
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
			builder := echoboot.NewBuilder(ctx)
			for _, c := range ctx.Environment().Clusters() {
				builder.
					With(nil, echo.Config{
						Service:   podASvc,
						Namespace: apps.namespace,
						Ports:     echoPorts,
						Subsets:   []echo.SubsetConfig{{}},
						Locality:  "region.zone.subzone",
						Cluster:   c,
					}).
					With(nil, echo.Config{
						Service:   podBSvc,
						Namespace: apps.namespace,
						Ports:     echoPorts,
						Subsets:   []echo.SubsetConfig{{}},
						Cluster:   c,
					}).
					With(nil, echo.Config{
						Service:   podCSvc,
						Namespace: apps.namespace,
						Ports:     echoPorts,
						Subsets:   []echo.SubsetConfig{{}},
						Cluster:   c,
					}).
					With(nil, echo.Config{
						Service:   headlessSvc,
						Headless:  true,
						Namespace: apps.namespace,
						Ports:     headlessPorts,
						Subsets:   []echo.SubsetConfig{{}},
						Cluster:   c,
					}).
					With(nil, echo.Config{
						Service:   nakedSvc,
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
						Cluster: c,
					}).
					With(nil, echo.Config{
						Service:   externalSvc,
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
						Cluster: c,
					})

			}

			for _, c := range ctx.Clusters().ByNetwork() {
				builder.With(nil, echo.Config{
					Service:    vmASvc,
					Namespace:  apps.namespace,
					Ports:      echoPorts,
					DeployAsVM: true,
					Subsets:    []echo.SubsetConfig{{}},
					Cluster:    c[0],
				})
			}

			echos, err := builder.Build()
			if err != nil {
				return err
			}
			apps.all = echos
			apps.podA = echos.Match(echo.Service(podASvc))
			apps.podB = echos.Match(echo.Service(podBSvc))
			apps.podC = echos.Match(echo.Service(podCSvc))
			apps.headless = echos.Match(echo.Service(headlessSvc))
			apps.naked = echos.Match(echo.Service(nakedSvc))
			apps.external = echos.Match(echo.Service(externalSvc))
			apps.vmA = echos.Match(echo.Service(vmASvc))

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
