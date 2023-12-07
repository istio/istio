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
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/labels"
)

func TestHasProxyIP(t *testing.T) {
	tests := []struct {
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
	tests := []struct {
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
			got := getLabelValue(test.node.ObjectMeta, NodeRegionLabel, NodeRegionLabelGA)
			if test.expectedLabelValue != got {
				t.Errorf("Expected %v, but got %v", test.expectedLabelValue, got)
			}
		})
	}
}

func TestPodKeyByProxy(t *testing.T) {
	testCases := []struct {
		name        string
		proxy       *model.Proxy
		expectedKey types.NamespacedName
	}{
		{
			name: "invalid id: bad format",
			proxy: &model.Proxy{
				ID: "invalid",
				Metadata: &model.NodeMetadata{
					Namespace: "default",
				},
			},
		},
		{
			name: "invalid id: namespace mismatch",
			proxy: &model.Proxy{
				ID: "pod1.ns1",
				Metadata: &model.NodeMetadata{
					Namespace: "default",
				},
			},
		},
		{
			name: "invalid id: namespace mismatch",
			proxy: &model.Proxy{
				ID: "pod1.ns1",
				Metadata: &model.NodeMetadata{
					Namespace: "ns1",
				},
			},
			expectedKey: types.NamespacedName{Namespace: "ns1", Name: "pod1"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			key := podKeyByProxy(tc.proxy)
			if key != tc.expectedKey {
				t.Errorf("expected key %s != %s", tc.expectedKey, key)
			}
		})
	}
}

func TestGetNodeSelectorsForService(t *testing.T) {
	testCases := []struct {
		name                  string
		svc                   *v1.Service
		expectedLabelSelector labels.Instance
	}{
		{
			name:                  "empty selector",
			svc:                   makeFakeSvc(""),
			expectedLabelSelector: nil,
		},
		{
			name:                  "invalid selector",
			svc:                   makeFakeSvc("invalid value"),
			expectedLabelSelector: nil,
		},
		{
			name:                  "wildcard match",
			svc:                   makeFakeSvc("{}"),
			expectedLabelSelector: labels.Instance{},
		},
		{
			name:                  "specific match",
			svc:                   makeFakeSvc(`{"kubernetes.io/hostname": "node1"}`),
			expectedLabelSelector: labels.Instance{"kubernetes.io/hostname": "node1"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			selector := getNodeSelectorsForService(tc.svc)
			if !reflect.DeepEqual(selector, tc.expectedLabelSelector) {
				t.Errorf("expected selector %v != %v", tc.expectedLabelSelector, selector)
			}
		})
	}
}

func makeFakeSvc(nodeSelector string) *v1.Service {
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "service",
			Namespace: "ns",
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{{
				Name: "http",
				Port: 80,
			}},
			Selector:  map[string]string{"app": "helloworld"},
			ClusterIP: "9.9.9.9",
		},
	}

	if nodeSelector != "" {
		svc.Annotations = map[string]string{
			"traffic.istio.io/nodeSelector": nodeSelector,
		}
	}
	return svc
}

func hasProxyIP(addresses []v1.EndpointAddress, proxyIP string) bool {
	for _, addr := range addresses {
		if addr.IP == proxyIP {
			return true
		}
	}
	return false
}
