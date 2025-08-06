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

package checkinject

import (
	"os"
	"testing"

	admitv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"istio.io/api/annotation"
	"istio.io/api/label"
	"istio.io/istio/pkg/test/util/assert"
)

func Test_analyzeRunningWebhooks(t *testing.T) {
	cases := []struct {
		name             string
		pod              *corev1.Pod
		ns               *corev1.Namespace
		expectedMessages []webhookAnalysis
	}{
		{
			name: "no inj because of no match labels",
			pod:  podTestObject("test1", "test1", "", ""),
			ns:   nsTestObject("test1", "", ""),
			expectedMessages: []webhookAnalysis{
				{
					Name:     "istio-sidecar-injector",
					Revision: "default",
					Reason: "No matching namespace labels (istio.io/rev=default, istio-injection=enabled) " +
						"or pod labels (istio.io/rev=default, sidecar.istio.io/inject=true)",
				},
				{
					Name:     "istio-sidecar-injector-1-16",
					Revision: "1-16",
					Reason:   "No matching namespace labels (istio.io/rev=1-16) or pod labels (istio.io/rev=1-16)",
				},
				{
					Name:     "istio-sidecar-injector-deactivated",
					Revision: "default",
					Reason:   "The injection webhook is deactivated, and will never match labels.",
				},
			},
		},
		{
			name: "default ns injection",
			pod:  podTestObject("test1", "test1", "", ""),
			ns:   nsTestObject("test1", "enabled", ""),
			expectedMessages: []webhookAnalysis{
				{
					Name:     "istio-sidecar-injector",
					Revision: "default",
					Injected: true,
					Reason:   "Namespace label istio-injection=enabled matches",
				},
				{
					Name:     "istio-sidecar-injector-1-16",
					Revision: "1-16",
					Reason:   "No matching namespace labels (istio.io/rev=1-16) or pod labels (istio.io/rev=1-16)",
				},
				{
					Name:     "istio-sidecar-injector-deactivated",
					Revision: "default",
					Reason:   "The injection webhook is deactivated, and will never match labels.",
				},
			},
		},
		{
			name: "rev ns injection",
			pod:  podTestObject("test1", "test1", "", ""),
			ns:   nsTestObject("test1", "", "1-16"),
			expectedMessages: []webhookAnalysis{
				{
					Name:     "istio-sidecar-injector",
					Revision: "default",
					Reason: "No matching namespace labels (istio.io/rev=default, istio-injection=enabled) " +
						"or pod labels (istio.io/rev=default, sidecar.istio.io/inject=true)",
				},
				{
					Name:     "istio-sidecar-injector-1-16",
					Revision: "1-16",
					Injected: true,
					Reason:   "Namespace label istio.io/rev=1-16 matches",
				},
				{
					Name:     "istio-sidecar-injector-deactivated",
					Revision: "default",
					Reason:   "The injection webhook is deactivated, and will never match labels.",
				},
			},
		},
		{
			name: "rev po label injection",
			pod:  podTestObject("test1", "test1", "", "1-16"),
			ns:   nsTestObject("test1", "", ""),
			expectedMessages: []webhookAnalysis{
				{
					Name:     "istio-sidecar-injector",
					Revision: "default",
					Reason:   "Pod has istio.io/rev=1-16 label, preventing injection",
				},
				{
					Name:     "istio-sidecar-injector-1-16",
					Revision: "1-16",
					Injected: true,
					Reason:   "Pod label istio.io/rev=1-16 matches",
				},
				{
					Name:     "istio-sidecar-injector-deactivated",
					Revision: "default",
					Reason:   "The injection webhook is deactivated, and will never match labels.",
				},
			},
		},
		{
			name: "default pod label injection",
			pod:  podTestObject("test1", "test1", "true", ""),
			ns:   nsTestObject("test1", "", ""),
			expectedMessages: []webhookAnalysis{
				{
					Name:     "istio-sidecar-injector",
					Revision: "default",
					Injected: true,
					Reason:   "Pod label sidecar.istio.io/inject=true matches",
				},
				{
					Name:     "istio-sidecar-injector-1-16",
					Revision: "1-16",
					Reason:   "No matching namespace labels (istio.io/rev=1-16) or pod labels (istio.io/rev=1-16)",
				},
				{
					Name:     "istio-sidecar-injector-deactivated",
					Revision: "default",
					Reason:   "The injection webhook is deactivated, and will never match labels.",
				},
			},
		},
		{
			name: "both default label injection",
			pod:  podTestObject("test1", "test1", "true", ""),
			ns:   nsTestObject("test1", "enabled", ""),
			expectedMessages: []webhookAnalysis{
				{
					Name:     "istio-sidecar-injector",
					Revision: "default",
					Injected: true,
					Reason:   "Namespace label istio-injection=enabled matches, and pod label sidecar.istio.io/inject=true matches",
				},
				{
					Name:     "istio-sidecar-injector-1-16",
					Revision: "1-16",
					Reason:   "No matching namespace labels (istio.io/rev=1-16) or pod labels (istio.io/rev=1-16)",
				},
				{
					Name:     "istio-sidecar-injector-deactivated",
					Revision: "default",
					Reason:   "The injection webhook is deactivated, and will never match labels.",
				},
			},
		},
		{
			name: "both rev label injection",
			pod:  podTestObject("test1", "test1", "", "1-16"),
			ns:   nsTestObject("test1", "", "1-16"),
			expectedMessages: []webhookAnalysis{
				{
					Name:     "istio-sidecar-injector",
					Revision: "default",
					Reason: "No matching namespace labels (istio.io/rev=default, istio-injection=enabled) " +
						"or pod labels (istio.io/rev=default, sidecar.istio.io/inject=true)",
				},
				{
					Name:     "istio-sidecar-injector-1-16",
					Revision: "1-16",
					Injected: true,
					Reason:   "Namespace label istio.io/rev=1-16 matches",
				},
				{
					Name:     "istio-sidecar-injector-deactivated",
					Revision: "default",
					Reason:   "The injection webhook is deactivated, and will never match labels.",
				},
			},
		},
		{
			name: "disable ns label and rev po label",
			pod:  podTestObject("test1", "test1", "", "1-16"),
			ns:   nsTestObject("test1", "disabled", ""),
			expectedMessages: []webhookAnalysis{
				{
					Name:     "istio-sidecar-injector",
					Revision: "default",
					Reason:   "Namespace has istio-injection=disabled label, preventing injection",
				},
				{
					Name:     "istio-sidecar-injector-1-16",
					Revision: "1-16",
					Injected: false,
					Reason:   "Namespace has istio-injection=disabled label, preventing injection",
				},
				{
					Name:     "istio-sidecar-injector-deactivated",
					Revision: "default",
					Reason:   "The injection webhook is deactivated, and will never match labels.",
				},
			},
		},
		{
			name: "ns and rev pod label",
			pod:  podTestObject("test1", "test1", "", "1-16"),
			ns:   nsTestObject("test1", "enabled", ""),
			expectedMessages: []webhookAnalysis{
				{
					Name:     "istio-sidecar-injector",
					Revision: "default",
					Injected: true,
					Reason:   "Namespace label istio-injection=enabled matches",
				},
				{
					Name:     "istio-sidecar-injector-1-16",
					Revision: "1-16",
					Injected: false,
					Reason:   "No matching namespace labels (istio.io/rev=1-16) or pod labels (istio.io/rev=1-16)",
				},
				{
					Name:     "istio-sidecar-injector-deactivated",
					Revision: "default",
					Reason:   "The injection webhook is deactivated, and will never match labels.",
				},
			},
		},
		{
			name: "pod label with not inject",
			pod:  podTestObject("test1", "test1", "false", ""),
			ns:   nsTestObject("test1", "enabled", ""),
			expectedMessages: []webhookAnalysis{
				{
					Name:     "istio-sidecar-injector",
					Revision: "default",
					Injected: false,
					Reason:   "Pod has sidecar.istio.io/inject=false label, preventing injection",
				},
				{
					Name:     "istio-sidecar-injector-1-16",
					Revision: "1-16",
					Injected: false,
					Reason:   "No matching namespace labels (istio.io/rev=1-16) or pod labels (istio.io/rev=1-16)",
				},
				{
					Name:     "istio-sidecar-injector-deactivated",
					Revision: "default",
					Reason:   "The injection webhook is deactivated, and will never match labels.",
				},
			},
		},
		{
			name: "pod not injectable for ns rev",
			pod:  podTestObject("test1", "test1", "false", ""),
			ns:   nsTestObject("test1", "", "1-16"),
			expectedMessages: []webhookAnalysis{
				{
					Name:     "istio-sidecar-injector",
					Revision: "default",
					Injected: false,
					Reason: "No matching namespace labels (istio.io/rev=default, istio-injection=enabled) " +
						"or pod labels (istio.io/rev=default, sidecar.istio.io/inject=true)",
				},
				{
					Name:     "istio-sidecar-injector-1-16",
					Revision: "1-16",
					Injected: false,
					Reason:   "Pod has sidecar.istio.io/inject=false label, preventing injection",
				},
				{
					Name:     "istio-sidecar-injector-deactivated",
					Revision: "default",
					Reason:   "The injection webhook is deactivated, and will never match labels.",
				},
			},
		},
	}
	whFiles := []string{
		"testdata/check-inject/default-injector.yaml",
		"testdata/check-inject/rev-16-injector.yaml",
		"testdata/check-inject/-injector.yaml",
	}
	var whs []admitv1.MutatingWebhookConfiguration
	for _, whName := range whFiles {
		file, err := os.ReadFile(whName)
		if err != nil {
			t.Fatal(err)
		}
		var wh *admitv1.MutatingWebhookConfiguration
		if err := yaml.Unmarshal(file, &wh); err != nil {
			t.Fatal(err)
		}
		whs = append(whs, *wh)
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			checkResults := analyzeRunningWebhooks(whs,
				c.pod.Labels, c.ns.Labels)
			assert.Equal(t, c.expectedMessages, checkResults)
		})
	}
}

var nsTestObject = func(namespace, injLabelValue, revLabelValue string) *corev1.Namespace {
	labels := map[string]string{}
	if injLabelValue != "" {
		labels["istio-injection"] = injLabelValue
	}
	if revLabelValue != "" {
		labels[label.IoIstioRev.Name] = revLabelValue
	}
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   namespace,
			Labels: labels,
		},
	}
}

var podTestObject = func(name, namespace, injLabelValue, revLabelValue string) *corev1.Pod {
	labels := map[string]string{}
	if injLabelValue != "" {
		labels[annotation.SidecarInject.Name] = injLabelValue
	}
	if revLabelValue != "" {
		labels[label.IoIstioRev.Name] = revLabelValue
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
	}
}
