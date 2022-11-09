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

	corev1 "k8s.io/api/core/v1"
)

func TestEndpointsEqual(t *testing.T) {
	addressA := corev1.EndpointAddress{IP: "1.2.3.4", Hostname: "a"}
	addressB := corev1.EndpointAddress{IP: "1.2.3.4", Hostname: "b"}
	portA := corev1.EndpointPort{Name: "a"}
	portB := corev1.EndpointPort{Name: "b"}
	appProtocolA := "http"
	appProtocolB := "tcp"
	appProtocolPortA := corev1.EndpointPort{Name: "a", AppProtocol: &appProtocolA}
	appProtocolPortB := corev1.EndpointPort{Name: "a", AppProtocol: &appProtocolB}
	cases := []struct {
		name string
		a    *corev1.Endpoints
		b    *corev1.Endpoints
		want bool
	}{
		{"both empty", &corev1.Endpoints{}, &corev1.Endpoints{}, true},
		{
			"just not ready endpoints",
			&corev1.Endpoints{Subsets: []corev1.EndpointSubset{
				{NotReadyAddresses: []corev1.EndpointAddress{addressA}},
			}},
			&corev1.Endpoints{},
			false,
		},
		{
			"not ready to ready",
			&corev1.Endpoints{Subsets: []corev1.EndpointSubset{
				{NotReadyAddresses: []corev1.EndpointAddress{addressA}},
			}},
			&corev1.Endpoints{Subsets: []corev1.EndpointSubset{
				{Addresses: []corev1.EndpointAddress{addressA}},
			}},
			false,
		},
		{
			"ready and not ready address",
			&corev1.Endpoints{Subsets: []corev1.EndpointSubset{
				{
					NotReadyAddresses: []corev1.EndpointAddress{addressB},
					Addresses:         []corev1.EndpointAddress{addressA},
				},
			}},
			&corev1.Endpoints{Subsets: []corev1.EndpointSubset{
				{Addresses: []corev1.EndpointAddress{addressA}},
			}},
			true,
		},
		{
			"different addresses",
			&corev1.Endpoints{Subsets: []corev1.EndpointSubset{
				{Addresses: []corev1.EndpointAddress{addressB}},
			}},
			&corev1.Endpoints{Subsets: []corev1.EndpointSubset{
				{Addresses: []corev1.EndpointAddress{addressA}},
			}},
			false,
		},
		{
			"different ports",
			&corev1.Endpoints{Subsets: []corev1.EndpointSubset{
				{Addresses: []corev1.EndpointAddress{addressA}, Ports: []corev1.EndpointPort{portA}},
			}},
			&corev1.Endpoints{Subsets: []corev1.EndpointSubset{
				{Addresses: []corev1.EndpointAddress{addressA}, Ports: []corev1.EndpointPort{portB}},
			}},
			false,
		},
		{
			"same app protocol",
			&corev1.Endpoints{Subsets: []corev1.EndpointSubset{
				{Addresses: []corev1.EndpointAddress{addressA}, Ports: []corev1.EndpointPort{appProtocolPortA}},
			}},
			&corev1.Endpoints{Subsets: []corev1.EndpointSubset{
				{Addresses: []corev1.EndpointAddress{addressA}, Ports: []corev1.EndpointPort{appProtocolPortA}},
			}},
			true,
		},
		{
			"different app protocol",
			&corev1.Endpoints{Subsets: []corev1.EndpointSubset{
				{Addresses: []corev1.EndpointAddress{addressA}, Ports: []corev1.EndpointPort{appProtocolPortA}},
			}},
			&corev1.Endpoints{Subsets: []corev1.EndpointSubset{
				{Addresses: []corev1.EndpointAddress{addressA}, Ports: []corev1.EndpointPort{appProtocolPortB}},
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
