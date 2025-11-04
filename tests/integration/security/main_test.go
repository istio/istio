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

package security

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/authz"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/jwt"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"
)

var (
	// Namespaces
	echo1NS    namespace.Instance
	echo2NS    namespace.Instance
	externalNS namespace.Instance
	serverNS   namespace.Instance

	// Servers
	apps             deployment.TwoNamespaceView
	authzServer      authz.Server
	localAuthzServer authz.Server
	jwtServer        jwt.Server

	i istio.Instance
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Setup(istio.Setup(&i, func(c resource.Context, cfg *istio.Config) {
			cfg.ControlPlaneValues = `
values:
  global:
    logging:
      level: delta:debug
  pilot: 
    env: 
      PILOT_JWT_ENABLE_REMOTE_JWKS: true
meshConfig:
  defaultConfig:
    gatewayTopology:
      numTrustedProxies: 1 # Needed for X-Forwarded-For (See https://istio.io/latest/docs/ops/configuration/traffic-management/network-topologies/)
`
		})).
		// Create namespaces first. This way, echo can correctly configure egress to all namespaces.
		SetupParallel(
			namespace.Setup(&echo1NS, namespace.Config{Prefix: "echo1", Inject: true}),
			namespace.Setup(&echo2NS, namespace.Config{Prefix: "echo2", Inject: true}),
			namespace.Setup(&externalNS, namespace.Config{Prefix: "external", Inject: false}),
			namespace.Setup(&serverNS, namespace.Config{Prefix: "servers", Inject: true})).
		SetupParallel(
			jwt.Setup(&jwtServer, namespace.Future(&serverNS)),
			authz.Setup(&authzServer, namespace.Future(&serverNS)),
			authz.SetupLocal(&localAuthzServer, namespace.Future(&echo1NS)),
			deployment.SetupTwoNamespaces(&apps, deployment.Config{
				IncludeExtAuthz: true,
				Namespaces: []namespace.Getter{
					namespace.Future(&echo1NS),
					namespace.Future(&echo2NS),
				},
				ExternalNamespace: namespace.Future(&externalNS),
			})).
		Setup(func(ctx resource.Context) error {
			return ingressutil.SetInstances(apps.Ns1.All)
		}).
		Run()
}
