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

package remotejwks

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/jwt"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	ist       istio.Instance
	apps      deployment.SingleNamespaceView
	jwtServer jwt.Server
	echoNS    namespace.Instance
	systemNs  namespace.Instance
)

// remote_jwks is to test fully delegating Envoy to fetch JWKs server (PILOT_JWT_ENABLE_REMOTE_JWKS: envoy).
func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(label.CustomSetup).
		Setup(istio.Setup(&ist, setupConfig)).
		Setup(func(ctx resource.Context) error {
			var err error
			systemNs, err = istio.ClaimSystemNamespace(ctx)
			return err
		}).
		Setup(namespace.Setup(&echoNS, namespace.Config{Prefix: "echo1", Inject: true})).
		SetupParallel(
			jwt.Setup(&jwtServer, namespace.Future(&systemNs)),
			deployment.SetupSingleNamespace(&apps, deployment.Config{
				Namespaces: []namespace.Getter{
					namespace.Future(&echoNS),
				},
			})).
		Run()
}

func setupConfig(ctx resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.ControlPlaneValues = `
values:
  pilot:
    env:
      PILOT_JWT_ENABLE_REMOTE_JWKS: envoy
meshConfig:
  accessLogFile: /dev/stdout`
}
