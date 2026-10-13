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

package yml

import (
	"slices"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
)

func TestApplyPullSecret(t *testing.T) {
	for _, existing := range []string{"", "existing-secret"} {
		t.Run("existing="+existing, func(t *testing.T) {
			deployment := appsv1.Deployment{}
			pod := &deployment.Spec.Template.Spec
			pod.Containers = []corev1.Container{{Name: "app", Image: "example.com/app:latest"}}
			if existing != "" {
				pod.ImagePullSecrets = []corev1.LocalObjectReference{{Name: existing}}
			}
			original, err := yaml.Marshal(deployment)
			if err != nil {
				t.Fatal(err)
			}
			rendered, err := ApplyPullSecret(string(original), "private-registry")
			if err != nil {
				t.Fatal(err)
			}
			if err := yaml.Unmarshal([]byte(rendered), &deployment); err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(pod.ImagePullSecrets, corev1.LocalObjectReference{Name: "private-registry"}) {
				t.Fatal("configured pull secret missing from deployment")
			}
			if existing != "" && !slices.Contains(pod.ImagePullSecrets, corev1.LocalObjectReference{Name: existing}) {
				t.Fatal("existing pull secret was removed")
			}
			if len(pod.Containers) != 1 || pod.Containers[0].Image != "example.com/app:latest" {
				t.Fatal("pull-secret patch changed the app container")
			}
		})
	}
}

func TestApplyNamespace(t *testing.T) {
	cases := []struct {
		name      string
		yml       string
		namespace string
		expected  string
	}{
		{
			"no namespace",
			`metadata:
  name: foo`,
			"default",
			`metadata:
  name: foo
  namespace: default`,
		},
		{
			"empty namespace",
			`metadata:
  name: foo
  namespace: ""`,
			"default",
			`metadata:
  name: foo
  namespace: default`,
		},
		{
			"existing namespace",
			`metadata:
  name: foo
  namespace: bar`,
			"default",
			`metadata:
  name: foo
  namespace: bar`,
		},
		{
			"multipart",
			`apiVersion: v1
metadata:
  name: foo
---
apiVersion: v1
metadata:
  name: bar
`,
			"default",
			`apiVersion: v1
metadata:
  name: foo
  namespace: default
---
apiVersion: v1
metadata:
  name: bar
  namespace: default`,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ApplyNamespace(tt.yml, tt.namespace)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.expected {
				t.Fatalf("expected '%s', got '%s'", tt.expected, got)
			}
		})
	}
}
