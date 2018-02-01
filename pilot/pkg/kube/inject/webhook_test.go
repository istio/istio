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
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/ghodss/yaml"
	"k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/util"
)

func TestInjectRequired(t *testing.T) {
	podSpec := &corev1.PodSpec{}
	podSpecHostNetwork := &corev1.PodSpec{
		HostNetwork: true,
	}
	cases := []struct {
		policy  InjectionPolicy
		podSpec *corev1.PodSpec
		meta    *metav1.ObjectMeta
		want    bool
	}{
		{
			policy:  InjectionPolicyEnabled,
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:        "no-policy",
				Namespace:   "test-namespace",
				Annotations: map[string]string{},
			},
			want: true,
		},
		{
			policy:  InjectionPolicyEnabled,
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "default-policy",
				Namespace: "test-namespace",
			},
			want: true,
		},
		{
			policy:  InjectionPolicyEnabled,
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:        "force-on-policy",
				Namespace:   "test-namespace",
				Annotations: map[string]string{istioSidecarAnnotationPolicyKey: "true"},
			},
			want: true,
		},
		{
			policy:  InjectionPolicyEnabled,
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:        "force-off-policy",
				Namespace:   "test-namespace",
				Annotations: map[string]string{istioSidecarAnnotationPolicyKey: "false"},
			},
			want: false,
		},
		{
			policy:  InjectionPolicyDisabled,
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:        "no-policy",
				Namespace:   "test-namespace",
				Annotations: map[string]string{},
			},
			want: false,
		},
		{
			policy:  InjectionPolicyDisabled,
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "default-policy",
				Namespace: "test-namespace",
			},
			want: false,
		},
		{
			policy:  InjectionPolicyDisabled,
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:        "force-on-policy",
				Namespace:   "test-namespace",
				Annotations: map[string]string{istioSidecarAnnotationPolicyKey: "true"},
			},
			want: true,
		},
		{
			policy:  InjectionPolicyDisabled,
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:        "force-off-policy",
				Namespace:   "test-namespace",
				Annotations: map[string]string{istioSidecarAnnotationPolicyKey: "false"},
			},
			want: false,
		},
		{
			policy:  InjectionPolicyEnabled,
			podSpec: podSpecHostNetwork,
			meta: &metav1.ObjectMeta{
				Name:        "force-off-policy",
				Namespace:   "test-namespace",
				Annotations: map[string]string{},
			},
			want: false,
		},
	}

	for _, c := range cases {
		if got := injectRequired(ignoredNamespaces, c.policy, c.podSpec, c.meta); got != c.want {
			t.Errorf("injectRequired(%v, %v) got %v want %v", c.policy, c.meta, got, c.want)
		}
	}
}

func createTestWebhook() (*Webhook, error) {
	mesh := model.DefaultMeshConfig()

	raw, err := ioutil.ReadFile("testdata/TestWebhookInject_template.yaml")
	if err != nil {
		return nil, err
	}
	sidecarTemplate := string(raw)

	return &Webhook{
		sidecarConfig: &Config{
			Policy:   InjectionPolicyEnabled,
			Template: sidecarTemplate,
		},
		sidecarTemplateVersion: "unit-test-fake-version",
		meshConfig:             &mesh,
	}, nil
}

func TestWebhookInject(t *testing.T) {
	cases := []struct {
		inputFile string
		wantFile  string
	}{
		{
			inputFile: "TestWebhookInject.yaml",
			wantFile:  "TestWebhookInject.patch",
		},
		{
			inputFile: "TestWebhookInject_no_volumes.yaml",
			wantFile:  "TestWebhookInject_no_volumes.patch",
		},
		{
			inputFile: "TestWebhookInject_no_containers_volumes.yaml",
			wantFile:  "TestWebhookInject_no_containers_volumes.patch",
		},
		{
			inputFile: "TestWebhookInject_no_containers.yaml",
			wantFile:  "TestWebhookInject_no_containers.patch",
		},
		{
			inputFile: "TestWebhookInject_no_initContainers.yaml",
			wantFile:  "TestWebhookInject_no_initContainers.patch",
		},
		{
			inputFile: "TestWebhookInject_no_initContainers_volumes.yaml",
			wantFile:  "TestWebhookInject_no_initContainers_volumes.patch",
		},
		{
			inputFile: "TestWebhookInject_no_initContainers_containers.yaml",
			wantFile:  "TestWebhookInject_no_initContainers_containers.patch",
		},
		{
			inputFile: "TestWebhookInject_no_initContainers_containers_volumes.yaml",
			wantFile:  "TestWebhookInject_no_initcontainers_containers_volumes.patch",
		},
	}

	for i, c := range cases {
		input := filepath.Join("testdata", c.inputFile)
		want := filepath.Join("testdata", c.wantFile)
		t.Run(fmt.Sprintf("[%d] %s", i, input), func(t *testing.T) {
			wh, err := createTestWebhook()
			if err != nil {
				t.Fatalf(err.Error())
			}
			podYAML, err := ioutil.ReadFile(input)
			if err != nil {
				t.Fatalf(err.Error())
			}
			podJSON, err := yaml.YAMLToJSON(podYAML)
			if err != nil {
				t.Fatalf(err.Error())
			}
			got := wh.inject(&v1beta1.AdmissionReview{
				Request: &v1beta1.AdmissionRequest{
					Object: runtime.RawExtension{
						Raw: podJSON,
					},
				},
			})
			var prettyPatch bytes.Buffer
			if err := json.Indent(&prettyPatch, got.Patch, "", "  "); err != nil {
				t.Fatalf(err.Error())
			}
			util.CompareContent(prettyPatch.Bytes(), want, t)
		})
	}
}

func TestRun(t *testing.T) {
	// TODO verify run loop handles HTTP serving and config reload properly
}
