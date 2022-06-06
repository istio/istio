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

package security

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/authz"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	apps             deployment.TwoNamespaceView
	authzServer      authz.Server
	localAuthzServer authz.Server
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Setup(istio.Setup(nil, func(_ resource.Context, cfg *istio.Config) {
			cfg.ControlPlaneValues = `
values:
  pilot: 
    env: 
      PILOT_JWT_ENABLE_REMOTE_JWKS: true
meshConfig:
  defaultConfig:
    gatewayTopology:
      numTrustedProxies: 1 # Needed for X-Forwarded-For (See https://istio.io/latest/docs/ops/configuration/traffic-management/network-topologies/)
`
		})).
		Setup(authz.Setup(&authzServer, nil)).
		Setup(deployment.SetupTwoNamespaces(&apps, deployment.Config{IncludeExtAuthz: true})).
		Setup(authz.SetupLocal(&localAuthzServer, func() namespace.Instance { return apps.Ns1.Namespace })).
		Run()
}
