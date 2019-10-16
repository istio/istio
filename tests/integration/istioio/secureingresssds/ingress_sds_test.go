// Copyright 2019 Istio Authors
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

package secureingresssds

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/istioio"
)

// https://preliminary.istio.io/docs/tasks/traffic-management/ingress/secure-ingress-sds/
func TestIngressSDS(t *testing.T) {
	framework.
		NewTest(t).
		Run(istioio.NewBuilder().
			// Configure a TLS ingress gateway for a single host.
			// https://preliminary.istio.io/docs/tasks/traffic-management/ingress/secure-ingress-sds/#configure-a-tls-ingress-gateway-for-a-single-host
			RunScript(istioio.File("scripts/httpbin-deployment.sh"), nil).
			RunScript(istioio.File("scripts/create-httpbin-tls-secret.sh"), nil).
			RunScript(istioio.File("scripts/create-httpbin-tls-gateway.sh"), nil).
			RunScript(istioio.File("scripts/check-envoy-sds-update-1.sh"), nil).
			RunScript(istioio.FileFunc(func(ctx istioio.Context) string {
				// Send an HTTPS request to access the httpbin service TLS gateway.
				if ctx.Env.Settings().Minikube {
					return "scripts/curl-httpbin-tls-gateway-minikube.sh"
				}
				return "scripts/curl-httpbin-tls-gateway-gke.sh"
			}), nil).
			// Rotate secret and send HTTPS request with new credentials.
			RunScript(istioio.File("scripts/rotate-httpbin-tls-secret.sh"), nil).
			RunScript(istioio.File("scripts/check-envoy-sds-update-2.sh"), nil).
			RunScript(istioio.FileFunc(func(ctx istioio.Context) string {
				// Send an HTTPS request to access the httpbin service TLS gateway.
				if ctx.Env.Settings().Minikube {
					return "scripts/curl-httpbin-tls-gateway-minikube-new-tls-secret.sh"
				}
				return "scripts/curl-httpbin-tls-gateway-gke-new-tls-secret.sh"
			}), nil).
			// Configure a TLS ingress gateway for multiple hosts.
			// https://preliminary.istio.io/docs/tasks/traffic-management/ingress/secure-ingress-sds/#configure-a-tls-ingress-gateway-for-multiple-hosts
			RunScript(istioio.File("scripts/restore-httpbin-tls-secret.sh"), nil).
			RunScript(istioio.File("scripts/helloworld-deployment.sh"), nil).
			RunScript(istioio.File("scripts/create-helloworld-tls-secret.sh"), nil).
			RunScript(istioio.File("scripts/create-helloworld-tls-gateway.sh"), nil).
			// Send an HTTPS request to access the helloworld service TLS gateway.
			RunScript(istioio.File("scripts/check-envoy-sds-update-4.sh"), nil).
			RunScript(istioio.FileFunc(func(ctx istioio.Context) string {
				// Send an HTTPS request to access the httpbin service TLS gateway.
				if ctx.Env.Settings().Minikube {
					return "scripts/curl-helloworld-tls-gateway-minikube.sh"
				}
				return "scripts/curl-helloworld-tls-gateway-minikube.sh"
			}), nil).
			// Send an HTTPS request to access the httpbin service TLS gateway.
			RunScript(istioio.FileFunc(func(ctx istioio.Context) string {
				// Send an HTTPS request to access the httpbin service TLS gateway.
				if ctx.Env.Settings().Minikube {
					return "scripts/curl-httpbin-tls-gateway-minikube.sh"
				}
				return "scripts/curl-httpbin-tls-gateway-gke.sh"
			}), nil).
			// Configure a mutual TLS ingress gateway.
			// https://preliminary.istio.io/docs/tasks/traffic-management/ingress/secure-ingress-sds/#configure-a-mutual-tls-ingress-gateway
			RunScript(istioio.File("scripts/rotate-httpbin-mtls-secret.sh"), nil).
			RunScript(istioio.File("scripts/create-httpbin-mtls-gateway.sh"), nil).
			RunScript(istioio.File("scripts/check-envoy-sds-update-5.sh"), nil).
			// Send an HTTPS request to access the httpbin service mTLS gateway.
			RunScript(istioio.FileFunc(func(ctx istioio.Context) string {
				// Send an HTTPS request to access the httpbin service TLS gateway.
				if ctx.Env.Settings().Minikube {
					return "scripts/curl-httpbin-mtls-gateway-minikube.sh"
				}
				return "scripts/curl-httpbin-mtls-gateway-gke.sh"
			}), nil).
			// Cleanup
			RunScript(istioio.File("scripts/cleanup.sh"), nil).
			Build())
}
