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

package observability

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
-o jsonpath='{.spec.ports[?(@.name=="http2")].port}')`
	minikubeIngressHostCommand = `$(kubectl -n istio-system get pod -l istio=ingressgateway \
-o jsonpath='{.items[0].status.hostIP}')`
	minikubeIngressPortCommand = `$(kubectl -n istio-system get service istio-ingressgateway \
-o jsonpath='{.spec.ports[?(@.name=="http2")].nodePort}')`
)

// TestMeshVisualizationWithKiali runs the example in this link
// https://preliminary.istio.io/docs/tasks/observability/kiali/
func TestMeshVisualizationWithKiali(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			istioio.NewBuilder("tasks__observability__kiali").
				Add(script(ctx, "setup_bookinfo.txt")).
				Add(script(ctx, "perform_kiali_test.txt")).
				Defer(script(ctx, "cleanup.txt")).
				BuildAndRun(ctx)
		})
}

func script(ctx framework.TestContext, filename string) istioio.Script {
	e := ctx.Environment().(*kube.Environment)
	runtimeSecureIngressPortCommand := ingressPortCommand
	runtimeIngressHostCommand := ingressHostCommand
	if e.Settings().Minikube {
		runtimeSecureIngressPortCommand = ingressPortCommand
		runtimeIngressHostCommand = minikubeIngressHostCommand
	}

	return istioio.Script{
		Input: istioio.Evaluate(istioio.Path("scripts/"+filename), map[string]interface{}{
			"isSnippet":          false,
			"inputTerminalFlag":  "",
			"ingressPortCommand": runtimeSecureIngressPortCommand,
			"ingressHostCommand": runtimeIngressHostCommand,
			"redirectToStdout":   "2>&1",
		}),
		SnippetInput: istioio.Evaluate(istioio.Path("scripts/"+filename), map[string]interface{}{
			"isSnippet":          true,
			"inputTerminalFlag":  "-it",
			"ingressPortCommand": ingressPortCommand,
			"ingressHostCommand": ingressHostCommand,
			"redirectToStdout":   "",
		}),
	}
}
