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

package util

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"istio.io/api/annotation"
	"istio.io/api/label"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/test/util/assert"
)

var defaultAmbientSelector = compileDefaultSelectors()

func compileDefaultSelectors() *CompiledEnablementSelectors {
	compiled, err := NewCompiledEnablementSelectors([]EnablementSelector{
		{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					label.IoIstioDataplaneMode.Name: constants.DataplaneModeAmbient,
				},
			},
		},
		{
			NamespaceSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					label.IoIstioDataplaneMode.Name: constants.DataplaneModeAmbient,
				},
			},
			PodSelector: metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      label.IoIstioDataplaneMode.Name,
						Operator: metav1.LabelSelectorOpNotIn,
						Values:   []string{constants.DataplaneModeNone},
					},
				},
			},
		},
	})
	if err != nil {
		panic(err)
	}
	return compiled
}

func TestGetPodIPIfPodIPPresent(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: corev1.PodSpec{
			NodeName: "derp",
		},
		Status: corev1.PodStatus{
			PodIP: "11.1.1.12",
		},
	}

	podIPs := GetPodIPsIfPresent(pod)
	assert.Equal(t, len(podIPs), 1)
}

func TestGetPodIPsIfPodIPPresent(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: corev1.PodSpec{
			NodeName: "derp",
		},
		Status: corev1.PodStatus{
			PodIP:  "2.2.2.2",
			PodIPs: []corev1.PodIP{{IP: "2.2.2.2"}, {IP: "3.3.3.3"}},
		},
	}

	podIPs := GetPodIPsIfPresent(pod)
	assert.Equal(t, len(podIPs), 2)
}

func TestGetPodIPsIfNoPodIPPresent(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: corev1.PodSpec{
			NodeName: "derp",
		},
		Status: corev1.PodStatus{},
	}

	podIPs := GetPodIPsIfPresent(pod)
	assert.Equal(t, len(podIPs), 0)
}

func TestPodRedirectionEnabled(t *testing.T) {
	var (
		ambientEnabledLabel     = map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeAmbient}
		ambientDisabledLabel    = map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeNone}
		sidecarStatusAnnotation = map[string]string{annotation.SidecarStatus.Name: "test"}

		namespaceWithAmbientEnabledLabel = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "test",
				Labels: ambientEnabledLabel,
			},
		}

		unlabelledNamespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
		}

		podWithAmbientEnabledLabel = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "test",
				Labels:    ambientEnabledLabel,
			},
		}

		unlabelledPod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "test",
			},
		}

		podWithSidecar = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "test",
				Namespace:   "test",
				Annotations: sidecarStatusAnnotation,
			},
		}

		podWithAmbientDisabledLabel = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "test",
				Labels:    ambientDisabledLabel,
			},
		}

		podWithSidecarAndAmbientEnabledLabel = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "test",
				Namespace:   "test",
				Labels:      ambientEnabledLabel,
				Annotations: sidecarStatusAnnotation,
			},
		}
	)

	type args struct {
		namespace *corev1.Namespace
		pod       *corev1.Pod
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "ambient mode enabled for namespace",
			args: args{
				namespace: namespaceWithAmbientEnabledLabel,
				pod:       unlabelledPod,
			},
			want: true,
		},
		{
			name: "ambient mode enabled for pod",
			args: args{
				namespace: unlabelledNamespace,
				pod:       podWithAmbientEnabledLabel,
			},
			want: true,
		},
		{
			name: "ambient mode enabled for both namespace and pod",
			args: args{
				namespace: namespaceWithAmbientEnabledLabel,
				pod:       podWithAmbientEnabledLabel,
			},
			want: true,
		},
		{
			name: "ambient mode enabled for neither namespace nor pod",
			args: args{
				namespace: unlabelledNamespace,
				pod:       unlabelledPod,
			},
			want: false,
		},
		{
			name: "pod has sidecar and namespace has ambient enabled",
			args: args{
				namespace: namespaceWithAmbientEnabledLabel,
				pod:       podWithSidecar,
			},
			want: false,
		},
		{
			name: "pod has label to disable ambient redirection",
			args: args{
				namespace: namespaceWithAmbientEnabledLabel,
				pod:       podWithAmbientDisabledLabel,
			},
			want: false,
		},
		{
			name: "pod has sidecar, pod has ambient mode label",
			args: args{
				namespace: unlabelledNamespace,
				pod:       podWithSidecarAndAmbientEnabledLabel,
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := defaultAmbientSelector.Matches(tt.args.pod.Labels, tt.args.pod.Annotations, tt.args.namespace.Labels); got != tt.want {
				t.Errorf("PodRedirectionEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEnablementFromString(t *testing.T) {
	tests := []struct {
		name string
		args string
	}{
		{
			name: "empty",
			args: "",
		},
		{
			name: "default",
			args: "- podSelector:\n    matchLabels:\n      istio.io/dataplane-mode: ambient\n- namespaceSelector:\n    matchLabels:\n      istio.io/dataplane-mode: ambient\n  podSelector:\n    matchExpressions:\n    - key: istio.io/dataplane-mode\n      operator: NotIn\n      values:\n      - none",
		},
		{
			name: "namespace only",
			args: "- namespaceSelector:\n    matchLabels:\n      istio.io/dataplane-mode: ambient",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			selectors := []EnablementSelector{}
			if err := yaml.Unmarshal([]byte(tt.args), &selectors); err != nil {
				t.Fatalf("failed to parse ambient enablement selector: %v", err)
			}
			_, err := NewCompiledEnablementSelectors(selectors)
			if err != nil {
				t.Errorf("failed to instantiate ambient enablement selector: %v", err)
			}
			// if the selector compiles, the test passes
		})
	}
}
