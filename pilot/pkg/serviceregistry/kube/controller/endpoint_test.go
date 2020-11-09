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

package controller

import (
	"testing"

	coreV1 "k8s.io/api/core/v1"
)

func TestEndpointsEqual(t *testing.T) {
	addressA := coreV1.EndpointAddress{IP: "1.2.3.4", Hostname: "a"}
	addressB := coreV1.EndpointAddress{IP: "1.2.3.4", Hostname: "b"}
	portA := coreV1.EndpointPort{Name: "a"}
	portB := coreV1.EndpointPort{Name: "b"}
	appProtocolA := "http"
	appProtocolB := "tcp"
	appProtocolPortA := coreV1.EndpointPort{Name: "a", AppProtocol: &appProtocolA}
	appProtocolPortB := coreV1.EndpointPort{Name: "a", AppProtocol: &appProtocolB}
	cases := []struct {
		name string
		a    *coreV1.Endpoints
		b    *coreV1.Endpoints
		want bool
	}{
		{"both empty", &coreV1.Endpoints{}, &coreV1.Endpoints{}, true},
		{
			"just not ready endpoints",
			&coreV1.Endpoints{Subsets: []coreV1.EndpointSubset{
				{NotReadyAddresses: []coreV1.EndpointAddress{addressA}},
			}},
			&coreV1.Endpoints{},
			false,
		},
		{
			"not ready to ready",
			&coreV1.Endpoints{Subsets: []coreV1.EndpointSubset{
				{NotReadyAddresses: []coreV1.EndpointAddress{addressA}},
			}},
			&coreV1.Endpoints{Subsets: []coreV1.EndpointSubset{
				{Addresses: []coreV1.EndpointAddress{addressA}},
			}},
			false,
		},
		{
			"ready and not ready address",
			&coreV1.Endpoints{Subsets: []coreV1.EndpointSubset{
				{
					NotReadyAddresses: []coreV1.EndpointAddress{addressB},
					Addresses:         []coreV1.EndpointAddress{addressA},
				},
			}},
			&coreV1.Endpoints{Subsets: []coreV1.EndpointSubset{
				{Addresses: []coreV1.EndpointAddress{addressA}},
			}},
			true,
		},
		{
			"different addresses",
			&coreV1.Endpoints{Subsets: []coreV1.EndpointSubset{
				{Addresses: []coreV1.EndpointAddress{addressB}},
			}},
			&coreV1.Endpoints{Subsets: []coreV1.EndpointSubset{
				{Addresses: []coreV1.EndpointAddress{addressA}},
			}},
			false,
		},
		{
			"different ports",
			&coreV1.Endpoints{Subsets: []coreV1.EndpointSubset{
				{Addresses: []coreV1.EndpointAddress{addressA}, Ports: []coreV1.EndpointPort{portA}},
			}},
			&coreV1.Endpoints{Subsets: []coreV1.EndpointSubset{
				{Addresses: []coreV1.EndpointAddress{addressA}, Ports: []coreV1.EndpointPort{portB}},
			}},
			false,
		},
		{
			"same app protocol",
			&coreV1.Endpoints{Subsets: []coreV1.EndpointSubset{
				{Addresses: []coreV1.EndpointAddress{addressA}, Ports: []coreV1.EndpointPort{appProtocolPortA}},
			}},
			&coreV1.Endpoints{Subsets: []coreV1.EndpointSubset{
				{Addresses: []coreV1.EndpointAddress{addressA}, Ports: []coreV1.EndpointPort{appProtocolPortA}},
			}},
			true,
		},
		{
			"different app protocol",
			&coreV1.Endpoints{Subsets: []coreV1.EndpointSubset{
				{Addresses: []coreV1.EndpointAddress{addressA}, Ports: []coreV1.EndpointPort{appProtocolPortA}},
			}},
			&coreV1.Endpoints{Subsets: []coreV1.EndpointSubset{
				{Addresses: []coreV1.EndpointAddress{addressA}, Ports: []coreV1.EndpointPort{appProtocolPortB}},
			}},
			false,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := endpointsEqual(tt.a, tt.b)
			inverse := endpointsEqual(tt.b, tt.a)
			if got != tt.want {
				t.Fatalf("Compare endpoints got %v, want %v", got, tt.want)
			}
			if got != inverse {
				t.Fatalf("Expected to be commutative, but was not")
			}
		})
	}
}
