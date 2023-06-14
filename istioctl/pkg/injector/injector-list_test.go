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

package injector

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/annotation"
	"istio.io/api/label"
	"istio.io/istio/pkg/test/util/assert"
)

func Test_extractRevisionFromPod(t *testing.T) {
	cases := []struct {
		name             string
		pod              *corev1.Pod
		expectedRevision string
	}{
		{
			name:             "no rev",
			pod:              &corev1.Pod{},
			expectedRevision: "",
		},
		{
			name: "has rev annotation",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.SidecarStatus.Name: `{"revision": "test-anno"}`,
					},
				},
			},
			expectedRevision: "test-anno",
		},
		{
			name: "has both rev label and annotation, use label",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						label.IoIstioRev.Name: "test-label", // don't care about the label
					},
					Annotations: map[string]string{
						annotation.SidecarStatus.Name: `{"revision":"test-anno"}`,
					},
				},
			},
			expectedRevision: "test-anno",
		},
	}
	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, c.name), func(t *testing.T) {
			assert.Equal(t, c.expectedRevision, extractRevisionFromPod(c.pod))
		})
	}
}
