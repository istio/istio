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

package wildcardegress

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	i    istio.Instance
	apps = deployment.SingleNamespaceView{}
)

func setupConfig(_ resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.ControlPlaneValues = `
values:
  pilot:
    env:
      # Gateway support for TLS wildcard DYNAMIC_DNS ServiceEntries is alpha and gated by this flag.
      ENABLE_WILDCARD_HOST_SERVICE_ENTRIES_FOR_TLS: "true"
`
}

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(label.CustomSetup).
		Setup(istio.Setup(&i, setupConfig)).
		Setup(deployment.SetupSingleNamespace(&apps, deployment.Config{
			// Two sidecar clients with their own identities. The default "external" echo is the wildcard target.
			Configs: func() []echo.Config {
				return []echo.Config{
					{Service: "client", ServiceAccount: true},
					{Service: "other-client", ServiceAccount: true, Ports: echo.Ports{ports.GRPC, ports.HTTPS}},
				}
			},
		})).
		Run()
}
