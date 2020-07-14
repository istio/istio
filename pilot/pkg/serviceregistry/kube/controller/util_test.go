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

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHasProxyIP(t *testing.T) {
	var tests = []struct {
		name      string
		addresses []v1.EndpointAddress
		proxyIP   string
		expected  bool
	}{
		{
			"has proxy ip",
			[]v1.EndpointAddress{{IP: "172.17.0.1"}, {IP: "172.17.0.2"}},
			"172.17.0.1",
			true,
		},
		{
			"has no proxy ip",
			[]v1.EndpointAddress{{IP: "172.17.0.1"}, {IP: "172.17.0.2"}},
			"172.17.0.100",
			false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := hasProxyIP(test.addresses, test.proxyIP)
			if test.expected != got {
				t.Errorf("Expected %v, but got %v", test.expected, got)
			}
		})
	}
}

func TestGetLabelValue(t *testing.T) {
	var tests = []struct {
		name               string
		node               *v1.Node
		expectedLabelValue string
	}{
		{
			"Chooses beta label",
			&v1.Node{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{NodeRegionLabel: "beta-region", NodeRegionLabelGA: "ga-region"}}},
			"beta-region",
		},
		{
			"Fallback no beta label defined",
			&v1.Node{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{NodeRegionLabelGA: "ga-region"}}},
			"ga-region",
		},
		{
			"Only beta label specified",
			&v1.Node{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{NodeRegionLabel: "beta-region"}}},
			"beta-region",
		},
		{
			"No label defined at all",
			&v1.Node{},
			"",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := getLabelValue(test.node, NodeRegionLabel, NodeRegionLabelGA)
			if test.expectedLabelValue != got {
				t.Errorf("Expected %v, but got %v", test.expectedLabelValue, got)
			}
		})
	}
}
