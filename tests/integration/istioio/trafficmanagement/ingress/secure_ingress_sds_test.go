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

package ingress

import (
	"testing"

	"github.com/Masterminds/semver"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/istioio"
	"istio.io/istio/pkg/test/util/curl"
)

const (
	secureIngressPortCommand = `$(kubectl -n istio-system \
get service istio-ingressgateway -o jsonpath='{.spec.ports[?(@.name=="https")].port}')`
	ingressHostCommand = `$(kubectl -n istio-system \
get service istio-ingressgateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}')`
	minikubeSecureIngressPortCommand = `$(kubectl -n istio-system \
get service istio-ingressgateway -o jsonpath='{.spec.ports[?(@.name=="https")].nodePort}')`
	minikubeIngressHostCommand = `$(kubectl -n istio-system \
get pod -l istio=ingressgateway -o jsonpath='{.items[0].status.hostIP}')`
)

// https://preliminary.istio.io/docs/tasks/traffic-management/ingress/secure-ingress-sds/
func TestSecureIngressSDS(t *testing.T) {
	// Check the version of curl. This test requires the --retry-connrefused arg.
	curl.RequireMinVersionOrFail(t, semver.MustParse("7.52.0"))

	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			istioio.NewBuilder("traffic_management__ingress__secure_gateways_sds").
				Add(script(ctx, "generate_certs_and_keys.txt")).
				Add(script(ctx, "configure_tls_ingress_single_host.txt")).
				Add(script(ctx, "configure_tls_ingress_multiple_hosts.txt")).
				Add(script(ctx, "configure_mtls_ingress.txt")).
				Add(script(ctx, "troubleshooting.txt")).
				Defer(script(ctx, "cleanup.txt")).
				BuildAndRun(ctx)
		})
}

func script(ctx framework.TestContext, filename string) istioio.Script {
	// Determine the commands to use for ingress host/port.
	e := ctx.Environment().(*kube.Environment)
	runtimeSecureIngressPortCommand := secureIngressPortCommand
	runtimeIngressHostCommand := ingressHostCommand
	if e.Settings().Minikube {
		runtimeSecureIngressPortCommand = minikubeSecureIngressPortCommand
		runtimeIngressHostCommand = minikubeIngressHostCommand
	}

	return istioio.Script{
		Input: istioio.Evaluate(istioio.Path("scripts/"+filename), map[string]interface{}{
			"isSnippet":                false,
			"password":                 "password",
			"curlOptions":              "--retry 10 --retry-connrefused --retry-delay 5 ",
			"secureIngressPortCommand": runtimeSecureIngressPortCommand,
			"ingressHostCommand":       runtimeIngressHostCommand,
		}),
		SnippetInput: istioio.Evaluate(istioio.Path("scripts/"+filename), map[string]interface{}{
			"isSnippet":                true,
			"password":                 "<password>",
			"curlOptions":              "",
			"secureIngressPortCommand": secureIngressPortCommand,
			"ingressHostCommand":       ingressHostCommand,
		}),
	}
}
