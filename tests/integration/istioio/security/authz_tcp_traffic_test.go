// Copyright 2020 Istio Authors
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

package security

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/istioio"
)

const (
	ingressHostCommand = `$(kubectl -n istio-system get service istio-ingressgateway \
-o jsonpath='{.status.loadBalancer.ingress[0].ip}')`
	ingressPortCommand = `$(kubectl -n istio-system get service istio-ingressgateway \
-o jsonpath='{.spec.ports[?(@.name=="tcp")].port}')`
	minikubeIngressHostCommand = `$(kubectl -n istio-system get pod -l istio=ingressgateway \
-o jsonpath='{.items[0].status.hostIP}')`
	minikubeIngressPortCommand = `$(kubectl -n istio-system get service istio-ingressgateway \
-o jsonpath='{.spec.ports[?(@.name=="tcp")].nodePort}')`
)

// https://preliminary.istio.io/docs/tasks/security/authorization/authz-tcp/
func TestAuthzTCPTraffic(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			istioio.NewBuilder("tasks__security__authorization__authz_tcp").
				Add(istioio.Script{
					Input: istioio.Inline{
						FileName: "enable_mtls.sh",
						Value: `
istioctl manifest apply --set values.global.mtls.auto=true --set values.global.mtls.enabled=true -w`,
					},
				}).
				Add(istioio.Script{Input: istioio.Path("../common/scripts/bookinfo.txt")}).
				Add(script(ctx, "authz_tcp_traffic.txt")).
				Defer(
					istioio.Script{Input: istioio.Path("scripts/authz_tcp_traffic_cleanup.txt")},
					istioio.Script{Input: istioio.Path("../common/scripts/bookinfo-cleanup.txt")},
					istioio.Script{
						Input: istioio.Inline{
							FileName: "disable_mtls.sh",
							Value: `
istioctl manifest apply --set values.global.mtls.auto=true --set values.global.mtls.enabled=false -w`,
						},
					},
				).
				BuildAndRun(ctx)
		})
}

func script(ctx framework.TestContext, filename string) istioio.Script {
	e := ctx.Environment().(*kube.Environment)
	runtimeIngressHostCommand := ingressHostCommand
	runtimeIngressPortCommand := ingressPortCommand
	if e.Settings().Minikube {
		runtimeIngressHostCommand = minikubeIngressHostCommand
		runtimeIngressPortCommand = minikubeIngressPortCommand
	}
	return istioio.Script{
		Input: istioio.Evaluate(istioio.Path("scripts/"+filename), map[string]interface{}{
			"isSnippet":          false,
			"ingressHostCommand": runtimeIngressHostCommand,
			"ingressPortCommand": runtimeIngressPortCommand,
		}),
		SnippetInput: istioio.Evaluate(istioio.Path("scripts/"+filename), map[string]interface{}{
			"isSnippet":          true,
			"ingressHostCommand": ingressHostCommand,
			"ingressPortCommand": ingressPortCommand,
		}),
	}
}
