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

package cmd

import (
	"fmt"
	"io/ioutil"
	"testing"

	admit_v1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

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

func Test_getMatchingNamespacesWithDefaultAndRevInjector(t *testing.T) {
	cases := []struct {
		ns                    corev1.Namespace
		expectedInjectorTotal int
	}{
		{
			ns: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test1",
				},
			},
			expectedInjectorTotal: 2,
		},
		{
			ns: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test1",
					Labels: map[string]string{
						"istio-injection": "enabled",
					},
				},
			},
			expectedInjectorTotal: 1,
		},
		{
			ns: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test1",
					Labels: map[string]string{
						label.IoIstioRev.Name: "default",
					},
				},
			},
			expectedInjectorTotal: 1,
		},
		{
			ns: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test1",
					Labels: map[string]string{
						label.IoIstioRev.Name: "1-16",
					},
				},
			},
			expectedInjectorTotal: 1,
		},
	}
	defaultFile, err := ioutil.ReadFile("testdata/default-injector.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var defaultWh *admit_v1.MutatingWebhookConfiguration
	if err := yaml.Unmarshal(defaultFile, &defaultWh); err != nil {
		t.Fatal(err)
	}
	revFile, err := ioutil.ReadFile("testdata/rev-16-injector.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var revWh *admit_v1.MutatingWebhookConfiguration
	if err := yaml.Unmarshal(revFile, &revWh); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		assert.Equal(t, c.expectedInjectorTotal, len(getMatchingNamespaces(defaultWh, []corev1.Namespace{c.ns}))+
			len(getMatchingNamespaces(revWh, []corev1.Namespace{c.ns})))
	}
}
