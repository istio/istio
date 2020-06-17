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

package kube

import (
	"testing"

	v1 "k8s.io/api/core/v1"

	"istio.io/pkg/log"
)

// TestRemoveIPFromEndpoint if the provided IP to deregister has already
// been registered.
func TestRemoveIPFromEndpoint(t *testing.T) {
	var match bool
	var tests = []struct {
		input1   *v1.Endpoints
		input2   string
		expected bool
	}{
		{&v1.Endpoints{}, "10.128.0.17", false},
		{
			&v1.Endpoints{
				Subsets: []v1.EndpointSubset{
					{
						[]v1.EndpointAddress{{"10.128.0.17", "", nil, nil}},
						[]v1.EndpointAddress{{"", "", nil, nil}},
						[]v1.EndpointPort{{"3309", 3309, "TCP", nil}},
					},
				},
			},
			"10.128.0.17",
			true,
		},
		{
			&v1.Endpoints{
				Subsets: []v1.EndpointSubset{
					{
						[]v1.EndpointAddress{{"10.128.0.18", "", nil, nil}},
						[]v1.EndpointAddress{{"", "", nil, nil}},
						[]v1.EndpointPort{{"3309", 3309, "TCP", nil}},
					},
				},
			},
			"10.128.0.17",
			false,
		},
		{
			&v1.Endpoints{
				Subsets: []v1.EndpointSubset{
					{
						[]v1.EndpointAddress{{"10.128.0.17", "", nil, nil}, {"10.128.0.18", "", nil, nil}, {"10.128.0.19", "", nil, nil}},
						[]v1.EndpointAddress{{"", "", nil, nil}},
						[]v1.EndpointPort{{"3309", 3309, "TCP", nil}},
					},
				},
			},
			"10.128.0.17",
			true,
		},
		{
			&v1.Endpoints{
				Subsets: []v1.EndpointSubset{
					{
						[]v1.EndpointAddress{{"10.128.0.17", "", nil, nil}, {"10.128.0.18", "", nil, nil}, {"10.128.0.19", "", nil, nil}},
						[]v1.EndpointAddress{{"", "", nil, nil}},
						[]v1.EndpointPort{{"3309", 3309, "TCP", nil}},
					},
					{
						[]v1.EndpointAddress{{"10.128.0.20", "", nil, nil}, {"10.128.0.21", "", nil, nil}, {"10.128.0.22", "", nil, nil}},
						[]v1.EndpointAddress{{"", "", nil, nil}},
						[]v1.EndpointPort{{"3308", 3308, "TCP", nil}},
					},
				},
			},
			"10.128.0.22",
			true,
		},
		{
			&v1.Endpoints{
				Subsets: []v1.EndpointSubset{
					{
						[]v1.EndpointAddress{{"10.128.0.17", "", nil, nil}, {"10.128.0.18", "", nil, nil}, {"10.128.0.19", "", nil, nil}},
						[]v1.EndpointAddress{{"", "", nil, nil}},
						[]v1.EndpointPort{{"3309", 3309, "TCP", nil}},
					},
					{
						[]v1.EndpointAddress{{"10.128.0.20", "", nil, nil}, {"10.128.0.21", "", nil, nil}, {"10.128.0.22", "", nil, nil}},
						[]v1.EndpointAddress{{"", "", nil, nil}},
						[]v1.EndpointPort{{"3308", 3308, "TCP", nil}},
					},
				},
			},
			"10.128.0.23",
			false,
		},
	}
	for _, tst := range tests {
		match = removeIPFromEndpoint(tst.input1, tst.input2)
		if tst.expected != match {
			t.Errorf("Expected %t got %t", tst.expected, match)
		}
		if !match {
			log.Infof("Ip %s does not exist in Endpoint %v", tst.input2, tst.input1)
		} else {
			log.Infof("Ip %s exist in Endpoint %v", tst.input2, tst.input1)
		}
	}
}
