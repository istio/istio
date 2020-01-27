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

package trafficmanagement

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

func TestRequestTimeouts(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			istioio.NewBuilder("tasks__traffic_management__request_timeouts").
				Add(istioio.Script{Input: istioio.Path("../common/scripts/bookinfo.txt")}).
				Add(script(ctx, "request_timeouts.txt")).
				Add(script(ctx, "request_timeouts_delay.txt")).
				Defer(istioio.Script{
					Input: istioio.Inline{
						FileName: "cleanup.sh",
						Value: `
kubectl delete -n default -f samples/bookinfo/platform/kube/bookinfo.yaml || true
kubectl delete -f samples/bookinfo/networking/destination-rule-all.yaml || true
kubectl delete -f samples/bookinfo/networking/virtual-service-all-v1.yaml || true
kubectl delete -f samples/bookinfo/networking/bookinfo-gateway.yaml || true
kubectl delete -f samples/sleep/sleep.yaml || true`,
					},
				}).
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
