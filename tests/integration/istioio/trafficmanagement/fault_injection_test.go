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

package trafficmanagement

import (
	"testing"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/istioio"
)

//https://istio.io/docs/tasks/traffic-management/fault-injection/
//https://github.com/istio/istio.io/blob/release-1.2/content/docs/tasks/traffic-management/fault-injection/index.md
func TestFaultInjection(t *testing.T) {

	framework.
		NewTest(t).
		Run(istioio.NewBuilder("tasks__trafic__management_fault_injection").
			Add(istioio.Script{
				WorkDir: env.IstioSrc,
				Input: istioio.Inline{
					FileName: "create_request_routing_virtual_services.sh",
					Value: `
set -e
set -u
set -o pipefail
# $snippet create_request_routing_virtual_services.sh syntax="bash"
$ kubectl apply -f samples/bookinfo/networking/virtual-service-all-v1.yaml
$ kubectl apply -f samples/bookinfo/networking/virtual-service-reviews-test-v2.yaml
# $endsnippet`,
				},
			}).
            //crete Vs for rating test delay
			Add(istioio.Script{
				WorkDir: env.IstioSrc,
				Input: istioio.Inline{
					FileName: "create_virtual_service_delay_test.sh",
					Value: `
set -e
set -u
set -o pipefail
# $snippet create_virtual_service_delay_test.sh syntax="bash"
$ kubectl apply -f @samples/bookinfo/networking/virtual-service-ratings-test-delay.yaml@
# $endsnippet`,
				},
			}).
			// check if VS delay was created`.
			Add(istioio.Script{
				Input:   istioio.Path("scripts/faultinjection/virtual_service_delay_creation.txt"),
				WorkDir: env.IstioSrc,
			}).
            //crete Vs for rating test abort
			Add(istioio.Script{
				WorkDir: env.IstioSrc,
				Input: istioio.Inline{
					FileName: "create_virtual_service_rating_test.sh",
					Value: `
set -e
set -u
set -o pipefail
# $snippet create_virtual_service_rating_test.sh syntax="bash"
$ kubectl apply -f @samples/bookinfo/networking/virtual-service-ratings-test-abort.yaml@
# $endsnippet`,
				},
			}).

			// check if VS abort was created`.
			Add(istioio.Script{
				Input:   istioio.Path("scripts/faultinjection/virtual_service_abort_creation.txt"),
				WorkDir: env.IstioSrc,
			}).
			// Cleanup.
			Defer(istioio.Script{
				Input: istioio.Inline{
					FileName: "cleanup.sh",
					Value: `
# $snippet cleanup.sh syntax="bash" outputis="text"
$ kubectl delete VirtualService ratings
# $endsnippet`,
				},
			}).
			Build())
}
