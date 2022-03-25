//go:build integ
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

package eccsignaturealgorithm

import (
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	inst istio.Instance
	apps = &EchoDeployments{}
)

type EchoDeployments struct {
	// Namespace is used as the default namespace for reachability tests and other tests which can reuse the same config for echo instances
	Namespace      namespace.Instance
	Client, Server echo.Instance
}

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		// Needed as it requires an environmental variable
		Label(label.CustomSetup).
		Setup(istio.Setup(&inst, setupConfig)).
		Setup(func(ctx resource.Context) error {
			return SetupApps(ctx, apps)
		}).
		Run()
}

func setupConfig(_ resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.ControlPlaneValues = `
values:
  meshConfig:
    defaultConfig:
      proxyMetadata:
        ECC_SIGNATURE_ALGORITHM: "ECDSA"
`
}

func SetupApps(ctx resource.Context, apps *EchoDeployments) error {
	var err error
	apps.Namespace, err = namespace.New(ctx, namespace.Config{
		Prefix: "test-ns",
		Inject: true,
	})
	if err != nil {
		return err
	}

	echos, err := deployment.New(ctx).
		WithClusters(ctx.Clusters()...).
		WithConfig(echo.Config{
			Namespace: apps.Namespace,
			Service:   "client",
		}).
		WithConfig(echo.Config{
			Subsets:        []echo.SubsetConfig{{}},
			Namespace:      apps.Namespace,
			Service:        "server",
			ServiceAccount: true,
			Ports: []echo.Port{
				{
					Name:         "http",
					Protocol:     protocol.HTTP,
					ServicePort:  8091,
					WorkloadPort: 8091,
				},
			},
		}).Build()
	if err != nil {
		return err
	}
	apps.Client, err = match.ServiceName(model.NamespacedName{Name: "client", Namespace: apps.Namespace.Name()}).First(echos)
	if err != nil {
		return err
	}

	apps.Server, err = match.ServiceName(model.NamespacedName{Name: "server", Namespace: apps.Namespace.Name()}).First(echos)
	if err != nil {
		return err
	}

	return nil
}
