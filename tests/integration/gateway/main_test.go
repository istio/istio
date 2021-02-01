// +build integ
//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package gateway

import (
	"testing"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
)

const (
	ASvc                 = "a" // Used by the default gateway
	BSvc                 = "b" // Used by the custom ingress gateway
	CustomServiceGateway = "custom-ingressgateway"
)

type EchoDeployments struct {
	appANamespace, appBNamespace namespace.Instance
	A, B                         echo.Instances
}

var (
	Inst              istio.Instance
	CgwInst           istio.Instance
	Apps              = &EchoDeployments{}
	CustomGWNamespace namespace.Instance
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		RequireSingleCluster().
		Setup(istio.Setup(&Inst, nil)).
		Setup(func(ctx resource.Context) error {
			return setupCustomGatewayNamespace(ctx, &CustomGWNamespace)
		}).
		Setup(istio.Setup(&CgwInst, setupCustomGatewayConfig)).
		Setup(func(ctx resource.Context) error {
			return setupApps(ctx, Apps)
		}).
		Run()
}

// setupCustomGatewayNamespace will create a namespace for a custom gateway.
func setupCustomGatewayNamespace(ctx resource.Context, gwNamespace *namespace.Instance) error {
	var err error

	// Setup namespace for custom gateway
	if *gwNamespace, err = namespace.New(ctx, namespace.Config{
		Prefix: "custom-gw",
		Inject: false,
	}); err != nil {
		return err
	}

	return nil
}

// setupCustomGatewayConfig returns a config to create a custom gateway in the
// CustomGWNamespace.
func setupCustomGatewayConfig(ctx resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}

	cfg.ControlPlaneValues = `
components:
  ingressGateways:
  - name: ` + CustomServiceGateway + `
    label:
      istio: ` + CustomServiceGateway + `
    namespace: ` + CustomGWNamespace.Name() + `
    enabled:
      true
`
}

// setupApps creates two namespaces and starts an echo app in each namespace.
// Tests will be able to connect the apps to gateways and verify traffic.
func setupApps(ctx resource.Context, apps *EchoDeployments) error {
	var err error
	var echos echo.Instances

	// Setup namespace for app a
	if apps.appANamespace, err = namespace.New(ctx, namespace.Config{
		Prefix: "app-a",
		Inject: true,
	}); err != nil {
		return err
	}

	// Setup namespace for app b
	if apps.appBNamespace, err = namespace.New(ctx, namespace.Config{
		Prefix: "app-b",
		Inject: true,
	}); err != nil {
		return err
	}

	// Setup the two apps, one per namespace
	builder := echoboot.NewBuilder(ctx)
	builder.
		WithClusters(ctx.Clusters()...).
		WithConfig(echoConfig(ASvc, apps.appANamespace)).
		WithConfig(echoConfig(BSvc, apps.appBNamespace))

	if echos, err = builder.Build(); err != nil {
		return err
	}
	apps.A = echos.Match(echo.Service(ASvc))
	apps.B = echos.Match(echo.Service(BSvc))
	return nil
}

func echoConfig(name string, ns namespace.Instance) echo.Config {
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
