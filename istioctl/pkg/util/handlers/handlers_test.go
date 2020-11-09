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

package handlers

import (
	"strings"
	"testing"
)

func TestInferPodInfo(t *testing.T) {
	tests := []struct {
		proxyName     string
		namespace     string
		wantPodName   string
		wantNamespace string
	}{
		{
			proxyName:     "istio-ingressgateway-8d9697654-qdzgh.istio-system",
			namespace:     "kube-system",
			wantPodName:   "istio-ingressgateway-8d9697654-qdzgh",
			wantNamespace: "istio-system",
		},
		{
			proxyName:     "istio-ingressgateway-8d9697654-qdzgh.istio-system",
			namespace:     "",
			wantPodName:   "istio-ingressgateway-8d9697654-qdzgh",
			wantNamespace: "istio-system",
		},
		{
			proxyName:     "istio-ingressgateway-8d9697654-qdzgh",
			namespace:     "kube-system",
			wantPodName:   "istio-ingressgateway-8d9697654-qdzgh",
			wantNamespace: "kube-system",
		},
		{
			proxyName:     "istio-security-post-install-1.2.2-bm9w2.istio-system",
			namespace:     "istio-system",
			wantPodName:   "istio-security-post-install-1.2.2-bm9w2",
			wantNamespace: "istio-system",
		},
		{
			proxyName:     "istio-security-post-install-1.2.2-bm9w2.istio-system",
			namespace:     "",
			wantPodName:   "istio-security-post-install-1.2.2-bm9w2",
			wantNamespace: "istio-system",
		},
	}
	for _, tt := range tests {
		t.Run(strings.Split(tt.proxyName, ".")[0], func(t *testing.T) {
			gotPodName, gotNamespace := InferPodInfo(tt.proxyName, tt.namespace)
			if gotPodName != tt.wantPodName || gotNamespace != tt.wantNamespace {
				t.Errorf("unexpected podName and namespace: wanted %v %v got %v %v", tt.wantPodName, tt.wantNamespace, gotPodName, gotNamespace)
			}
		})
	}
}

func TestHandleNamespace(t *testing.T) {
	ns := HandleNamespace("test", "default")
	if ns != "test" {
		t.Fatalf("Get the incorrect namespace: %q back", ns)
	}

	tests := []struct {
		description      string
		namespace        string
		defaultNamespace string
		wantNamespace    string
	}{
		{
			description:      "return test namespace",
			namespace:        "test",
			defaultNamespace: "default",
			wantNamespace:    "test",
		},
		{
			description:      "return default namespace",
			namespace:        "",
			defaultNamespace: "default",
			wantNamespace:    "default",
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			gotNs := HandleNamespace(tt.namespace, tt.defaultNamespace)
			if gotNs != tt.wantNamespace {
				t.Fatalf("unexpected namespace: wanted %v got %v", tt.wantNamespace, gotNs)
			}
		})
	}
}
