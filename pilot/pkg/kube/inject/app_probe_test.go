// Copyright 2018 Istio Authors
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
package inject

import (
	"reflect"
	"testing"

	"github.com/gogo/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
)

func TestRewriteAppHTTPProbe(t *testing.T) {
	tests := []struct {
		name    string
		sidecar *SidecarInjectionSpec
		// PodSpec before injection.
		original *corev1.PodSpec
		// PodSpec after injection.
		want *corev1.PodSpec
	}{
		{
			name: "empty-spec",
		},
		{
			name: "one-container",
		},
		{
			name: "not-rewrite-istio-proxy",
		},
	}
	for _, tc := range tests {
		pod := proto.Clone(tc.original).(*corev1.PodSpec)
		rewriteAppHTTPProbe(tc.sidecar, pod)
		if !reflect.DeepEqual(pod, tc.want) {
			t.Errorf("[%v], want %v, got %v", tc.name, *tc.want, *pod)
		}
	}
}
