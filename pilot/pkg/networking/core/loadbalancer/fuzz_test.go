// Copyright Istio Authors
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

package loadbalancer

import (
	"testing"

	fuzz "github.com/AdaLogics/go-fuzz-headers"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"

	"istio.io/api/networking/v1alpha3"
)

func FuzzApplyLocalityLBSetting(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		ff := fuzz.NewConsumer(data)
		proxyLabels := make(map[string]string)
		err := ff.FuzzMap(&proxyLabels)
		if err != nil {
			return
		}
		wrappedLocalityLbEndpoints := make([]*WrappedLocalityLbEndpoints, 0)
		err = ff.CreateSlice(&wrappedLocalityLbEndpoints)
		if err != nil {
			return
		}

		loadAssignment := &endpoint.ClusterLoadAssignment{}
		err = ff.GenerateStruct(loadAssignment)
		if err != nil {
			return
		}

		locality := &core.Locality{}
		err = ff.GenerateStruct(locality)
		if err != nil {
			return
		}

		localityLB := &v1alpha3.LocalityLoadBalancerSetting{}
		err = ff.GenerateStruct(localityLB)
		if err != nil {
			return
		}

		enableFailover, err := ff.GetBool()
		if err != nil {
			return
		}
		ApplyLocalityLoadBalancer(loadAssignment, wrappedLocalityLbEndpoints, locality, proxyLabels, localityLB, enableFailover)
	})
}
