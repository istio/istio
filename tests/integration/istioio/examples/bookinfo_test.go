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

package security

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/istioio"
)

const (
	ingressPortCommand = `$(kubectl -n istio-system \
get service istio-ingressgateway -o jsonpath='{.spec.ports[?(@.name=="http2")].port}')`
	ingressHostCommand = `$(kubectl -n istio-system \
get service istio-ingressgateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}')`
	minikubeIngressPortCommand = `$(kubectl -n istio-system \
get service istio-ingressgateway -o jsonpath='{.spec.ports[?(@.name=="http2")].nodePort}')`
	minikubeIngressHostCommand = `$(kubectl -n istio-system \
get pod -l istio=ingressgateway -o jsonpath='{.items[0].status.hostIP}')`
)

//https://istio.io/docs/examples/bookinfo/
//https://github.com/istio/istio.io/blob/master/content/en/docs/examples/bookinfo/index.md
func TestBookinfo(t *testing.T) {
	framework.
		NewTest(t).
		Run(istioio.NewBuilder("examples__bookinfo").
			Add(istioio.Script{
				Input: istioio.InputSelectorFunc(func(ctx istioio.Context) istioio.Input {
					e := ctx.Environment().(*kube.Environment)
					portCommand := ingressPortCommand
					hostCommand := ingressHostCommand
					if e.Settings().Minikube {
						portCommand = minikubeIngressPortCommand
						hostCommand = minikubeIngressHostCommand
					}
					return istioio.Evaluate(
						istioio.Path("scripts/bookinfo.txt"),
						map[string]interface{}{
							"ingressPortCommand": portCommand,
							"ingressHostCommand": hostCommand,
						},
					).SelectInput(ctx)
				}),
			}).
			Defer(istioio.Script{
				Input: istioio.Inline{
					FileName: "cleanup.sh",
					Value: `
kubectl delete -n default -f samples/bookinfo/platform/kube/bookinfo.yaml || true
kubectl delete -n default -f samples/bookinfo/networking/bookinfo-gateway.yaml || true`,
				},
			}).
			Build())
}
