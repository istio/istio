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
	"istio.io/istio/pkg/test/istioio"
)

func TestRequestTimeouts(t *testing.T) {
	framework.
		NewTest(t).
		Run(istioio.NewBuilder("tasks__traffic_management__request_timeouts").
			Add(istioio.Script{
				Input: istioio.Path("../common/scripts/bookinfo.txt"),
			}).
			Add(istioio.Script{
				Input: istioio.Path("scripts/request_timeouts.txt"),
			}).
			Add(istioio.Script{
				Input: istioio.Path("scripts/request_timeouts_delay.txt"),
			}).
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
			Build())
}
