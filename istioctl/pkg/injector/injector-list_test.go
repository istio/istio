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
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"istio.io/api/annotation"
	"istio.io/api/label"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
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

func Test_getNamespaces(t *testing.T) {
	createNamespace := func(name string, labels map[string]string) *corev1.Namespace {
		return &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: labels,
			},
		}
	}
	nss := []runtime.Object{
		createNamespace("default", nil),
		createNamespace("kube-system", nil),
		createNamespace("istio-system", nil),
		createNamespace("ambient", map[string]string{
			constants.DataplaneMode: constants.DataplaneModeAmbient,
		}),
		createNamespace("no-ambient", map[string]string{
			constants.DataplaneMode: constants.DataplaneModeAmbient,
			"istio-injection":       "enabled",
		}),
	}

	client := kube.NewFakeClient(nss...)
	expected := sets.New[string]("default", "no-ambient")
	actual, err := getNamespaces(context.TODO(), client, "istio-system")
	assert.NoError(t, err)
	for _, ns := range actual {
		assert.Equal(t, true, expected.Contains(ns.Name))
	}
}
