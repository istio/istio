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
	"fmt"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/tests/integration/security/util"
)

var (
	ist  istio.Instance
	apps = &util.EchoDeployments{}
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Setup(istio.Setup(&ist, setupConfig)).
		Setup(func(ctx resource.Context) error {
			return util.SetupApps(ctx, ist, apps, true)
		}).
		Run()
}

func setupConfig(ctx resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}

	// Create the namespace instance ahead of time so that it can be used in the mesh config.
	extAuthzServiceNamespace, extAuthzServiceNamespaceErr = namespace.New(ctx, namespace.Config{
		Prefix: "test-ns-ext-authz-service",
		Inject: true,
	})
	var extAuthzNamespace string
	if extAuthzServiceNamespaceErr == nil {
		extAuthzNamespace = extAuthzServiceNamespace.Name()
	}
	service := fmt.Sprintf("ext-authz.%s.svc.cluster.local", extAuthzNamespace)
	serviceWithNamespace := fmt.Sprintf("%s/%s", extAuthzNamespace, service)

	cfg.ControlPlaneValues = fmt.Sprintf(`
values:
  pilot: 
    env: 
      PILOT_JWT_ENABLE_REMOTE_JWKS: true
meshConfig:
  accessLogEncoding: JSON
  accessLogFile: /dev/stdout
  defaultConfig:
    gatewayTopology:
      numTrustedProxies: 1
  extensionProviders:
  - name: "ext-authz-http"
    envoyExtAuthzHttp:
      service: %q
      port: 8000
      pathPrefix: "/check"
      includeHeadersInCheck: ["x-ext-authz"]
  - name: "ext-authz-grpc"
    envoyExtAuthzGrpc:
      service: %q
      port: 9000
  - name: "ext-authz-http-local"
    envoyExtAuthzHttp:
      service: ext-authz-http.local
      port: 8000
      pathPrefix: "/check"
      includeHeadersInCheck: ["x-ext-authz"]
  - name: "ext-authz-grpc-local"
    envoyExtAuthzGrpc:
      service: ext-authz-grpc.local
      port: 9000
`, service, serviceWithNamespace)
}
