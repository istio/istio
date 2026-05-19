//go:build integ

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

package externalsdsprovider

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"
)

var (
	inst         istio.Instance
	apps         deployment.SingleNamespaceView
	echoNS       namespace.Instance
	customConfig []echo.Config
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(label.CustomSetup).
		Setup(istio.Setup(&inst, setupConfig)).
		Setup(namespace.Setup(&echoNS, namespace.Config{Prefix: "echo", Inject: true})).
		Setup(func(ctx resource.Context) error {
			return ingressutil.SetupTest(ctx, &customConfig, namespace.Future(&echoNS))
		}).
		Setup(deployment.SetupSingleNamespace(&apps, deployment.Config{
			Namespaces: []namespace.Getter{
				namespace.Future(&echoNS),
			},
			Configs: echo.ConfigFuture(&customConfig),
		})).
		Setup(func(ctx resource.Context) error {
			return ingressutil.SetInstances(apps.All)
		}).
		Run()
}

func setupConfig(_ resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.ControlPlaneValues = `
meshConfig:
  extensionProviders:
  - name: my-sds-provider
    sds:
      name: my-sds-provider
      service: sds-grpc-server.istio-system.svc.cluster.local
      port: 8443
`
}
