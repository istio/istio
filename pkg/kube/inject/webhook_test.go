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

package inject

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/types"
	"k8s.io/api/admission/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"

	"istio.io/api/annotation"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube"
)

const yamlSeparator = "\n---"

var minimalSidecarTemplate = &Config{
	Policy: InjectionPolicyEnabled,
	Template: `
initContainers:
- name: istio-init
containers:
- name: istio-proxy
volumes:
- name: istio-envoy
imagePullSecrets:
- name: istio-image-pull-secrets
`,
}

func parseToLabelSelector(t *testing.T, selector string) *metav1.LabelSelector {
	result, err := metav1.ParseToLabelSelector(selector)
	if err != nil {
		t.Errorf("Invalid selector %v: %v", selector, err)
	}

	return result
}

func TestInjectRequired(t *testing.T) {
	podSpec := &corev1.PodSpec{}
	podSpecHostNetwork := &corev1.PodSpec{
		HostNetwork: true,
	}
	cases := []struct {
		config  *Config
		podSpec *corev1.PodSpec
		meta    *metav1.ObjectMeta
		want    bool
	}{
		{
			config: &Config{
				Policy: InjectionPolicyEnabled,
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:        "no-policy",
				Namespace:   "test-namespace",
				Annotations: map[string]string{},
			},
			want: true,
		},
		{
			config: &Config{
				Policy: InjectionPolicyEnabled,
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "default-policy",
				Namespace: "test-namespace",
			},
			want: true,
		},
		{
			config: &Config{
				Policy: InjectionPolicyEnabled,
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:        "force-on-policy",
				Namespace:   "test-namespace",
				Annotations: map[string]string{annotation.SidecarInject.Name: "true"},
			},
			want: true,
		},
		{
			config: &Config{
				Policy: InjectionPolicyEnabled,
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:        "force-off-policy",
				Namespace:   "test-namespace",
				Annotations: map[string]string{annotation.SidecarInject.Name: "false"},
			},
			want: false,
		},
		{
			config: &Config{
				Policy: InjectionPolicyDisabled,
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:        "no-policy",
				Namespace:   "test-namespace",
				Annotations: map[string]string{},
			},
			want: false,
		},
		{
			config: &Config{
				Policy: InjectionPolicyDisabled,
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "default-policy",
				Namespace: "test-namespace",
			},
			want: false,
		},
		{
			config: &Config{
				Policy: InjectionPolicyDisabled,
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:        "force-on-policy",
				Namespace:   "test-namespace",
				Annotations: map[string]string{annotation.SidecarInject.Name: "true"},
			},
			want: true,
		},
		{
			config: &Config{
				Policy: InjectionPolicyDisabled,
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:        "force-off-policy",
				Namespace:   "test-namespace",
				Annotations: map[string]string{annotation.SidecarInject.Name: "false"},
			},
			want: false,
		},
		{
			config: &Config{
				Policy: InjectionPolicyEnabled,
			},
			podSpec: podSpecHostNetwork,
			meta: &metav1.ObjectMeta{
				Name:        "force-off-policy",
				Namespace:   "test-namespace",
				Annotations: map[string]string{},
			},
			want: false,
		},
		{
			config: &Config{
				Policy: "wrong_policy",
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:        "wrong-policy",
				Namespace:   "test-namespace",
				Annotations: map[string]string{},
			},
			want: false,
		},
		{
			config: &Config{
				Policy:               InjectionPolicyEnabled,
				AlwaysInjectSelector: []metav1.LabelSelector{{MatchLabels: map[string]string{"foo": "bar"}}},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-enabled-always-inject-no-labels",
				Namespace: "test-namespace",
			},
			want: true,
		},
		{
			config: &Config{
				Policy:               InjectionPolicyEnabled,
				AlwaysInjectSelector: []metav1.LabelSelector{{MatchLabels: map[string]string{"foo": "bar"}}},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-enabled-always-inject-with-labels",
				Namespace: "test-namespace",
				Labels:    map[string]string{"foo": "bar1"},
			},
			want: true,
		},
		{
			config: &Config{
				Policy:               InjectionPolicyDisabled,
				AlwaysInjectSelector: []metav1.LabelSelector{{MatchLabels: map[string]string{"foo": "bar"}}},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-disabled-always-inject-no-labels",
				Namespace: "test-namespace",
			},
			want: false,
		},
		{
			config: &Config{
				Policy:               InjectionPolicyDisabled,
				AlwaysInjectSelector: []metav1.LabelSelector{{MatchLabels: map[string]string{"foo": "bar"}}},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-disabled-always-inject-with-labels",
				Namespace: "test-namespace",
				Labels:    map[string]string{"foo": "bar"},
			},
			want: true,
		},
		{
			config: &Config{
				Policy:              InjectionPolicyEnabled,
				NeverInjectSelector: []metav1.LabelSelector{{MatchLabels: map[string]string{"foo": "bar"}}},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-enabled-never-inject-no-labels",
				Namespace: "test-namespace",
			},
			want: true,
		},
		{
			config: &Config{
				Policy:              InjectionPolicyEnabled,
				NeverInjectSelector: []metav1.LabelSelector{{MatchLabels: map[string]string{"foo": "bar"}}},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-enabled-never-inject-with-labels",
				Namespace: "test-namespace",
				Labels:    map[string]string{"foo": "bar"},
			},
			want: false,
		},
		{
			config: &Config{
				Policy:              InjectionPolicyDisabled,
				NeverInjectSelector: []metav1.LabelSelector{{MatchLabels: map[string]string{"foo": "bar"}}},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-disabled-never-inject-no-labels",
				Namespace: "test-namespace",
			},
			want: false,
		},
		{
			config: &Config{
				Policy:              InjectionPolicyDisabled,
				NeverInjectSelector: []metav1.LabelSelector{{MatchLabels: map[string]string{"foo": "bar"}}},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-disabled-never-inject-with-labels",
				Namespace: "test-namespace",
				Labels:    map[string]string{"foo": "bar"},
			},
			want: false,
		},
		{
			config: &Config{
				Policy:              InjectionPolicyEnabled,
				NeverInjectSelector: []metav1.LabelSelector{*parseToLabelSelector(t, "foo")},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-enabled-never-inject-with-empty-label",
				Namespace: "test-namespace",
				Labels:    map[string]string{"foo": "", "foo2": "bar2"},
			},
			want: false,
		},
		{
			config: &Config{
				Policy:               InjectionPolicyDisabled,
				AlwaysInjectSelector: []metav1.LabelSelector{*parseToLabelSelector(t, "foo")},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-disabled-always-inject-with-empty-label",
				Namespace: "test-namespace",
				Labels:    map[string]string{"foo": "", "foo2": "bar2"},
			},
			want: true,
		},
		{
			config: &Config{
				Policy:               InjectionPolicyDisabled,
				AlwaysInjectSelector: []metav1.LabelSelector{*parseToLabelSelector(t, "always")},
				NeverInjectSelector:  []metav1.LabelSelector{*parseToLabelSelector(t, "never")},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-disabled-always-never-inject-with-label-returns-true",
				Namespace: "test-namespace",
				Labels:    map[string]string{"always": "bar", "foo2": "bar2"},
			},
			want: true,
		},
		{
			config: &Config{
				Policy:               InjectionPolicyDisabled,
				AlwaysInjectSelector: []metav1.LabelSelector{*parseToLabelSelector(t, "always")},
				NeverInjectSelector:  []metav1.LabelSelector{*parseToLabelSelector(t, "never")},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-disabled-always-never-inject-with-label-returns-false",
				Namespace: "test-namespace",
				Labels:    map[string]string{"never": "bar", "foo2": "bar2"},
			},
			want: false,
		},
		{
			config: &Config{
				Policy:               InjectionPolicyDisabled,
				AlwaysInjectSelector: []metav1.LabelSelector{*parseToLabelSelector(t, "always")},
				NeverInjectSelector:  []metav1.LabelSelector{*parseToLabelSelector(t, "never")},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-disabled-always-never-inject-with-both-labels",
				Namespace: "test-namespace",
				Labels:    map[string]string{"always": "bar", "never": "bar2"},
			},
			want: false,
		},
		{
			config: &Config{
				Policy:              InjectionPolicyEnabled,
				NeverInjectSelector: []metav1.LabelSelector{*parseToLabelSelector(t, "foo")},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:        "policy-enabled-annotation-true-never-inject",
				Namespace:   "test-namespace",
				Annotations: map[string]string{annotation.SidecarInject.Name: "true"},
				Labels:      map[string]string{"foo": "", "foo2": "bar2"},
			},
			want: true,
		},
		{
			config: &Config{
				Policy:               InjectionPolicyEnabled,
				AlwaysInjectSelector: []metav1.LabelSelector{*parseToLabelSelector(t, "foo")},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:        "policy-enabled-annotation-false-always-inject",
				Namespace:   "test-namespace",
				Annotations: map[string]string{annotation.SidecarInject.Name: "false"},
				Labels:      map[string]string{"foo": "", "foo2": "bar2"},
			},
			want: false,
		},
		{
			config: &Config{
				Policy:               InjectionPolicyDisabled,
				AlwaysInjectSelector: []metav1.LabelSelector{*parseToLabelSelector(t, "foo")},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:        "policy-disabled-annotation-false-always-inject",
				Namespace:   "test-namespace",
				Annotations: map[string]string{annotation.SidecarInject.Name: "false"},
				Labels:      map[string]string{"foo": "", "foo2": "bar2"},
			},
			want: false,
		},
		{
			config: &Config{
				Policy:              InjectionPolicyEnabled,
				NeverInjectSelector: []metav1.LabelSelector{*parseToLabelSelector(t, "foo"), *parseToLabelSelector(t, "bar")},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-enabled-never-inject-multiple-labels",
				Namespace: "test-namespace",
				Labels:    map[string]string{"label1": "", "bar": "anything"},
			},
			want: false,
		},
		{
			config: &Config{
				Policy:               InjectionPolicyDisabled,
				AlwaysInjectSelector: []metav1.LabelSelector{*parseToLabelSelector(t, "foo"), *parseToLabelSelector(t, "bar")},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-enabled-always-inject-multiple-labels",
				Namespace: "test-namespace",
				Labels:    map[string]string{"label1": "", "bar": "anything"},
			},
			want: true,
		},
		{
			config: &Config{
				Policy:              InjectionPolicyDisabled,
				NeverInjectSelector: []metav1.LabelSelector{*parseToLabelSelector(t, "foo")},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:        "policy-disabled-annotation-true-never-inject",
				Namespace:   "test-namespace",
				Annotations: map[string]string{annotation.SidecarInject.Name: "true"},
				Labels:      map[string]string{"foo": "", "foo2": "bar2"},
			},
			want: true,
		},
	}

	for _, c := range cases {
		if got := injectRequired(ignoredNamespaces, c.config, c.podSpec, c.meta); got != c.want {
			t.Errorf("injectRequired(%v, %v) got %v want %v", c.config, c.meta, got, c.want)
		}
	}
}

func TestWebhookInject(t *testing.T) {
	cases := []struct {
		inputFile    string
		wantFile     string
		templateFile string
	}{
		{
			inputFile: "TestWebhookInject.yaml",
			wantFile:  "TestWebhookInject.patch",
		},
		{
			inputFile: "TestWebhookInject_no_initContainers.yaml",
			wantFile:  "TestWebhookInject_no_initContainers.patch",
		},
		{
			inputFile: "TestWebhookInject_no_containers.yaml",
			wantFile:  "TestWebhookInject_no_containers.patch",
		},
		{
			inputFile: "TestWebhookInject_no_volumes.yaml",
			wantFile:  "TestWebhookInject_no_volumes.patch",
		},
		{
			inputFile: "TestWebhookInject_no_imagePullSecrets.yaml",
			wantFile:  "TestWebhookInject_no_imagePullSecrets.patch",
		},
		{
			inputFile: "TestWebhookInject_no_volumes_imagePullSecrets.yaml",
			wantFile:  "TestWebhookInject_no_volumes_imagePullSecrets.patch",
		},
		{
			inputFile: "TestWebhookInject_no_containers_volumes.yaml",
			wantFile:  "TestWebhookInject_no_containers_volumes.patch",
		},
		{
			inputFile: "TestWebhookInject_no_containers_imagePullSecrets.yaml",
			wantFile:  "TestWebhookInject_no_containers_imagePullSecrets.patch",
		},
		{
			inputFile: "TestWebhookInject_no_initContainers_containers.yaml",
			wantFile:  "TestWebhookInject_no_initContainers_containers.patch",
		},
		{
			inputFile: "TestWebhookInject_no_initContainers_volumes.yaml",
			wantFile:  "TestWebhookInject_no_initContainers_volumes.patch",
		},
		{
			inputFile: "TestWebhookInject_no_initContainers_imagePullSecrets.yaml",
			wantFile:  "TestWebhookInject_no_initContainers_imagePullSecrets.patch",
		},
		{
			inputFile: "TestWebhookInject_no_containers_volumes_imagePullSecrets.yaml",
			wantFile:  "TestWebhookInject_no_containers_volumes_imagePullSecrets.patch",
		},
		{
			inputFile: "TestWebhookInject_no_initContainers_volumes_imagePullSecrets.yaml",
			wantFile:  "TestWebhookInject_no_initContainers_volumes_imagePullSecrets.patch",
		},
		{
			inputFile: "TestWebhookInject_no_initContainers_containers_volumes.yaml",
			wantFile:  "TestWebhookInject_no_initContainers_containers_volumes.patch",
		},
		{
			inputFile: "TestWebhookInject_no_initContainers_containers_imagePullSecrets.yaml",
			wantFile:  "TestWebhookInject_no_initContainers_containers_imagePullSecrets.patch",
		},
		{
			inputFile: "TestWebhookInject_no_initContainers_containers_volumes_imagePullSecrets.yaml",
			wantFile:  "TestWebhookInject_no_initcontainers_containers_volumes_imagePullSecrets.patch",
		},
		{
			inputFile: "TestWebhookInject_replace.yaml",
			wantFile:  "TestWebhookInject_replace.patch",
		},
		{
			inputFile: "TestWebhookInject_replace_backwards_compat.yaml",
			wantFile:  "TestWebhookInject_replace_backwards_compat.patch",
		},
		{
			inputFile:    "TestWebhookInject_http_probe_rewrite.yaml",
			wantFile:     "TestWebhookInject_http_probe_rewrite.patch",
			templateFile: "TestWebhookInject_http_probe_rewrite_template.yaml",
		},
		{
			inputFile:    "TestWebhookInject_http_probe_nosidecar_rewrite.yaml",
			wantFile:     "TestWebhookInject_http_probe_nosidecar_rewrite.patch",
			templateFile: "TestWebhookInject_http_probe_nosidecar_rewrite_template.yaml",
		},
		{
			inputFile:    "TestWebhookInject_https_probe_rewrite.yaml",
			wantFile:     "TestWebhookInject_https_probe_rewrite.patch",
			templateFile: "TestWebhookInject_https_probe_rewrite_template.yaml",
		},
		{
			inputFile:    "TestWebhookInject_http_probe_rewrite_enabled_via_annotation.yaml",
			wantFile:     "TestWebhookInject_http_probe_rewrite_enabled_via_annotation.patch",
			templateFile: "TestWebhookInject_http_probe_rewrite_enabled_via_annotation_template.yaml",
		},
		{
			inputFile:    "TestWebhookInject_http_probe_rewrite_disabled_via_annotation.yaml",
			wantFile:     "TestWebhookInject_http_probe_rewrite_disabled_via_annotation.patch",
			templateFile: "TestWebhookInject_http_probe_rewrite_disabled_via_annotation_template.yaml",
		},
		{
			inputFile:    "TestWebhookInject_injectorAnnotations.yaml",
			wantFile:     "TestWebhookInject_injectorAnnotations.patch",
			templateFile: "TestWebhookInject_injectorAnnotations_template.yaml",
		},
		{
			inputFile: "TestWebhookInject_mtls_not_ready.yaml",
			wantFile:  "TestWebhookInject_mtls_not_ready.patch",
		},
		{
			inputFile:    "TestWebhookInject_validationOrder.yaml",
			wantFile:     "TestWebhookInject_validationOrder.patch",
			templateFile: "TestWebhookInject_validationOrder_template.yaml",
		},
		{
			inputFile:    "TestWebhookInject_probe_rewrite_timeout_retention.yaml",
			wantFile:     "TestWebhookInject_probe_rewrite_timeout_retention.patch",
			templateFile: "TestWebhookInject_probe_rewrite_timeout_retention_template.yaml",
		},
		{
			inputFile:    "TestWebhookInject_cron_job.yaml",
			wantFile:     "TestWebhookInject_cron_job.patch",
			templateFile: "TestWebhookInject_cron_job_template.yaml",
		},
	}

	for i, c := range cases {
		input := filepath.Join("testdata/webhook", c.inputFile)
		want := filepath.Join("testdata/webhook", c.wantFile)
		templateFile := "TestWebhookInject_template.yaml"
		if c.templateFile != "" {
			templateFile = c.templateFile
		}
		c := c
		t.Run(fmt.Sprintf("[%d] %s", i, c.inputFile), func(t *testing.T) {
			t.Parallel()
			wh, cleanup := createTestWebhookFromFile(filepath.Join("testdata/webhook", templateFile), t)
			defer cleanup()
			podYAML := util.ReadFile(input, t)
			podJSON, err := yaml.YAMLToJSON(podYAML)
			if err != nil {
				t.Fatalf(err.Error())
			}
			got := wh.inject(&kube.AdmissionReview{
				Request: &kube.AdmissionRequest{
					Object: runtime.RawExtension{
						Raw: podJSON,
					},
				},
			}, "")
			var prettyPatch bytes.Buffer
			if err := json.Indent(&prettyPatch, got.Patch, "", "  "); err != nil {
				t.Fatalf(err.Error())
			}
			util.CompareContent(prettyPatch.Bytes(), want, t)
		})
	}
}

// TestHelmInject tests the webhook injector with the installation configmap.yaml. It runs through many of the
// same tests as TestIntoResourceFile in order to verify that the webhook performs the same way as the manual injector.
func TestHelmInject(t *testing.T) {
	// Create the webhook from the install configmap.
	webhook, cleanup := createTestWebhookFromHelmConfigMap(t)
	defer cleanup()
	// NOTE: this list is a subset of the list in TestIntoResourceFile. It contains all test cases that operate
	// on Deployments and do not require updates to the injection Params.
	cases := []struct {
		inputFile string
		wantFile  string
	}{
		{
			inputFile: "hello-probes.yaml",
			wantFile:  "hello-probes.yaml.injected",
		},
		{
			inputFile: "hello.yaml",
			wantFile:  "hello-config-map-name.yaml.injected",
		},
		{
			inputFile: "frontend.yaml",
			wantFile:  "frontend.yaml.injected",
		},
		{
			inputFile: "hello-multi.yaml",
			wantFile:  "hello-multi.yaml.injected",
		},
		{
			inputFile: "statefulset.yaml",
			wantFile:  "statefulset.yaml.injected",
		},
		{
			inputFile: "daemonset.yaml",
			wantFile:  "daemonset.yaml.injected",
		},
		{
			inputFile: "job.yaml",
			wantFile:  "job.yaml.injected",
		},
		{
			inputFile: "replicaset.yaml",
			wantFile:  "replicaset.yaml.injected",
		},
		{
			inputFile: "replicationcontroller.yaml",
			wantFile:  "replicationcontroller.yaml.injected",
		},
		{
			inputFile: "list.yaml",
			wantFile:  "list.yaml.injected",
		},
		{
			inputFile: "list-frontend.yaml",
			wantFile:  "list-frontend.yaml.injected",
		},
		{
			inputFile: "deploymentconfig.yaml",
			wantFile:  "deploymentconfig.yaml.injected",
		},
		{
			inputFile: "deploymentconfig-with-canonical-service-label.yaml",
			wantFile:  "deploymentconfig-with-canonical-service-label.yaml.injected",
		},
		{
			inputFile: "deploymentconfig-multi.yaml",
			wantFile:  "deploymentconfig-multi.yaml.injected",
		},
		{
			// Verifies that annotation values are applied properly. This also tests that annotation values
			// override params when specified.
			inputFile: "traffic-annotations.yaml",
			wantFile:  "traffic-annotations.yaml.injected",
		},
		{
			// Verifies that the wildcard character "*" behaves properly when used in annotations.
			inputFile: "traffic-annotations-wildcards.yaml",
			wantFile:  "traffic-annotations-wildcards.yaml.injected",
		},
		{
			// Verifies that the wildcard character "*" behaves properly when used in annotations.
			inputFile: "traffic-annotations-empty-includes.yaml",
			wantFile:  "traffic-annotations-empty-includes.yaml.injected",
		},
		{
			// Verifies that the status port annotation overrides the default.
			inputFile: "status_annotations.yaml",
			wantFile:  "status_annotations.yaml.injected",
		},
		{
			// Verifies that the resource annotation overrides the default.
			inputFile: "resource_annotations.yaml",
			wantFile:  "resource_annotations.yaml.injected",
		},
		{
			inputFile: "user-volume.yaml",
			wantFile:  "user-volume.yaml.injected",
		},
		{
			inputFile: "hello-mtls-not-ready.yaml",
			wantFile:  "hello-mtls-not-ready.yaml.injected",
		},
	}

	for ci, c := range cases {
		inputFile := filepath.Join("testdata/webhook", c.inputFile)
		wantFile := filepath.Join("testdata/webhook", c.wantFile)
		testName := fmt.Sprintf("[%02d] %s", ci, c.wantFile)
		t.Run(testName, func(t *testing.T) {
			// Split multi-part yaml documents. Input and output will have the same number of parts.
			inputYAMLs := splitYamlFile(inputFile, t)
			wantYAMLs := splitYamlFile(wantFile, t)
			goldenYAMLs := make([][]byte, len(inputYAMLs))

			for i := 0; i < len(inputYAMLs); i++ {
				t.Run(fmt.Sprintf("yamlPart[%d]", i), func(t *testing.T) {
					// Convert the input YAML to a deployment.
					inputYAML := inputYAMLs[i]
					inputJSON := yamlToJSON(inputYAML, t)
					inputDeployment := jsonToDeployment(inputJSON, t)

					// Convert the wanted YAML to a deployment.
					wantYAML := wantYAMLs[i]
					wantJSON := yamlToJSON(wantYAML, t)
					wantDeployment := jsonToDeployment(wantJSON, t)

					// Generate the patch.  At runtime, the webhook would actually generate the patch against the
					// pod configuration. But since our input files are deployments, rather than actual pod instances,
					// we have to apply the patch to the template portion of the deployment only.
					templateJSON := convertToJSON(inputDeployment.Spec.Template, t)
					got := webhook.inject(&kube.AdmissionReview{
						Request: &kube.AdmissionRequest{
							Object: runtime.RawExtension{
								Raw: templateJSON,
							},
						},
					}, "")

					// Apply the generated patch to the template.
					patch := prettyJSON(got.Patch, t)
					patchedTemplateJSON := applyJSONPatch(templateJSON, patch, t)
					// Create the patched deployment. It's just a copy of the original, but with a patched template
					// applied.
					patchedDeployment := inputDeployment.DeepCopy()
					patchedDeployment.Spec.Template.Reset()
					if err := json.Unmarshal(patchedTemplateJSON, &patchedDeployment.Spec.Template); err != nil {
						t.Fatal(err)
					}

					// normalize and compare the patched deployment with the one we expected.
					err := normalizeAndCompareDeployments(patchedDeployment, wantDeployment, t)

					if !util.Refresh() {
						if err != nil {
							t.Fatalf("Failed validating golden file %s:\n%v", c.wantFile, err)
						}
					} else {
						if err != nil {
							t.Logf("Updating %s", c.wantFile)
						}
						goldenYAMLs[i] = deploymentToYaml(patchedDeployment, t)
					}
				})
			}
			if util.Refresh() {
				writeYamlsToGoldenFile(goldenYAMLs, wantFile, t)
			}
		})
	}
}

func createTestWebhook(t testing.TB, config *Config, values string) (*Webhook, func()) {
	m := mesh.DefaultMeshConfig()
	dir, err := ioutil.TempDir("", "webhook_test")
	if err != nil {
		t.Fatalf("TempDir() failed: %v", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(dir)
	}
	return &Webhook{
		Config:                 config,
		sidecarTemplateVersion: "unit-test-fake-version",
		meshConfig:             &m,
		valuesConfig:           values,
	}, cleanup
}

func createTestWebhookFromFile(templateFile string, t *testing.T) (*Webhook, func()) {
	t.Helper()
	injectConfig := &Config{}
	if err := yaml.Unmarshal(util.ReadFile(templateFile, t), injectConfig); err != nil {
		t.Fatalf("failed to unmarshal injectionConfig: %v", err)
	}
	return createTestWebhook(t, injectConfig, "{}")
}

func createTestWebhookFromHelmConfigMap(t *testing.T) (*Webhook, func()) {
	t.Helper()
	// Load the config map with Helm. This simulates what will be done at runtime, by replacing function calls and
	// variables and generating a new configmap for use by the injection logic.
	sidecarTemplate, values, _ := loadInjectionSettings(t, nil, "")
	return createTestWebhook(t, sidecarTemplate, values)
}

// loadInjectionSettings will render the charts using the operator, with given yaml overrides.
// This allows us to fully simulate what will actually happen at run time.
func loadInjectionSettings(t testing.TB, setFlags []string, inFilePath string) (template *Config, values string, meshConfig *meshconfig.MeshConfig) {
	t.Helper()
	// add --set installPackagePath=<path to charts snapshot>
	setFlags = append(setFlags, "installPackagePath="+defaultInstallPackageDir())
	var inFilenames []string
	if inFilePath != "" {
		inFilenames = []string{"testdata/inject/" + inFilePath}
	}

	l := clog.NewConsoleLogger(os.Stdout, os.Stderr, nil)
	manifests, _, err := manifest.GenManifests(inFilenames, setFlags, false, nil, l)
	if err != nil {
		t.Fatalf("failed to generate manifests: %v", err)
	}
	for _, mlist := range manifests[name.PilotComponentName] {
		for _, object := range strings.Split(mlist, yamlSeparator) {

			r := bytes.NewReader([]byte(object))
			decoder := k8syaml.NewYAMLOrJSONDecoder(r, 1024)

			out := &unstructured.Unstructured{}
			err := decoder.Decode(out)
			if err != nil {
				t.Fatalf("error decoding object: %v", err)
			}
			if out.GetName() == "istio-sidecar-injector" && (out.GroupVersionKind() == schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}) {
				data, ok := out.Object["data"].(map[string]interface{})
				if !ok {
					t.Fatalf("failed to convert %v", out)
				}
				config, ok := data["config"].(string)
				if !ok {
					t.Fatalf("failed to config %v", data)
				}
				values, ok = data["values"].(string)
				if !ok {
					t.Fatalf("failed to config %v", data)
				}
				template = &Config{}
				if err := yaml.Unmarshal([]byte(config), template); err != nil {
					t.Fatalf("failed to unmarshal injectionConfig: %v", err)
				}
				if meshConfig != nil {
					return template, values, meshConfig
				}
			} else if out.GetName() == "istio" && (out.GroupVersionKind() == schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}) {
				data, ok := out.Object["data"].(map[string]interface{})
				if !ok {
					t.Fatalf("failed to convert %v", out)
				}
				meshdata, ok := data["mesh"].(string)
				if !ok {
					t.Fatalf("failed to get meshconfig %v", data)
				}
				meshConfig, err = mesh.ApplyMeshConfig(meshdata, mesh.DefaultMeshConfig())
				if err != nil {
					t.Fatalf("failed to unmarshal meshconfig: %v", err)
				}
				if template != nil {
					return template, values, meshConfig
				}
			}
		}
	}
	t.Fatal("could not find injection template")
	return nil, "", nil
}

func splitYamlFile(yamlFile string, t *testing.T) [][]byte {
	t.Helper()
	yamlBytes := util.ReadFile(yamlFile, t)
	return splitYamlBytes(yamlBytes, t)
}

func splitYamlBytes(yaml []byte, t *testing.T) [][]byte {
	t.Helper()
	stringParts := strings.Split(string(yaml), yamlSeparator)
	byteParts := make([][]byte, 0)
	for _, stringPart := range stringParts {
		byteParts = append(byteParts, getInjectableYamlDocs(stringPart, t)...)
	}
	if len(byteParts) == 0 {
		t.Fatalf("Found no injectable parts")
	}
	return byteParts
}

func writeYamlsToGoldenFile(yamls [][]byte, goldenFile string, t *testing.T) {
	content := make([]byte, 0)
	for _, part := range yamls {
		content = append(content, part...)
		content = append(content, []byte(yamlSeparator)...)
		content = append(content, '\n')
	}

	util.RefreshGoldenFile(content, goldenFile, t)
}
func getInjectableYamlDocs(yamlDoc string, t *testing.T) [][]byte {
	t.Helper()
	m := make(map[string]interface{})
	if err := yaml.Unmarshal([]byte(yamlDoc), &m); err != nil {
		t.Fatal(err)
	}
	switch m["kind"] {
	case "Deployment":
		return [][]byte{[]byte(yamlDoc)}
	case "DeploymentConfig":
		return [][]byte{[]byte(yamlDoc)}
	case "DaemonSet":
		return [][]byte{[]byte(yamlDoc)}
	case "StatefulSet":
		return [][]byte{[]byte(yamlDoc)}
	case "Job":
		return [][]byte{[]byte(yamlDoc)}
	case "ReplicaSet":
		return [][]byte{[]byte(yamlDoc)}
	case "ReplicationController":
		return [][]byte{[]byte(yamlDoc)}
	case "CronJob":
		return [][]byte{[]byte(yamlDoc)}
	case "List":
		// Split apart the list into separate yaml documents.
		out := make([][]byte, 0)
		list := metav1.List{}
		if err := yaml.Unmarshal([]byte(yamlDoc), &list); err != nil {
			t.Fatal(err)
		}
		for _, i := range list.Items {
			iout, err := yaml.Marshal(i)
			if err != nil {
				t.Fatal(err)
			}
			injectables := getInjectableYamlDocs(string(iout), t)
			out = append(out, injectables...)
		}
		return out
	default:
		// No injectable parts.
		return [][]byte{}
	}
}

func convertToJSON(i interface{}, t *testing.T) []byte {
	t.Helper()
	outputJSON, err := json.Marshal(i)
	if err != nil {
		t.Fatal(err)
	}
	return prettyJSON(outputJSON, t)
}

func yamlToJSON(inputYAML []byte, t *testing.T) []byte {
	t.Helper()
	// Convert to JSON
	out, err := yaml.YAMLToJSON(inputYAML)
	if err != nil {
		t.Fatalf(err.Error())
	}
	return prettyJSON(out, t)
}

func prettyJSON(inputJSON []byte, t *testing.T) []byte {
	t.Helper()
	// Pretty-print the JSON
	var prettyBuffer bytes.Buffer
	if err := json.Indent(&prettyBuffer, inputJSON, "", "  "); err != nil {
		t.Fatalf(err.Error())
	}
	return prettyBuffer.Bytes()
}

func applyJSONPatch(input, patch []byte, t *testing.T) []byte {
	t.Helper()
	p, err := jsonpatch.DecodePatch(patch)
	if err != nil {
		t.Fatal(err)
	}

	patchedJSON, err := p.Apply(input)
	if err != nil {
		t.Fatal(err)
	}
	return prettyJSON(patchedJSON, t)
}

func jsonToDeployment(deploymentJSON []byte, t *testing.T) *appsv1.Deployment {
	t.Helper()
	var deployment appsv1.Deployment
	if err := json.Unmarshal(deploymentJSON, &deployment); err != nil {
		t.Fatal(err)
	}
	return &deployment
}

func deploymentToYaml(deployment *appsv1.Deployment, t *testing.T) []byte {
	t.Helper()
	out, err := yaml.Marshal(deployment)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func normalizeAndCompareDeployments(got, want *appsv1.Deployment, t *testing.T) error {
	t.Helper()
	// Scrub unimportant fields that tend to differ.
	getAnnotations(got)[annotation.SidecarStatus.Name] = getAnnotations(want)[annotation.SidecarStatus.Name]
	gotIstioInit := istioInit(got, t)
	wantIstioInit := istioInit(want, t)
	gotIstioInit.Image = wantIstioInit.Image
	gotIstioInit.TerminationMessagePath = wantIstioInit.TerminationMessagePath
	gotIstioInit.TerminationMessagePolicy = wantIstioInit.TerminationMessagePolicy
	gotIstioInit.SecurityContext.Privileged = wantIstioInit.SecurityContext.Privileged
	gotIstioProxy := istioProxy(got, t)

	wantIstioProxy := istioProxy(want, t)

	gotIstioProxy.Image = wantIstioProxy.Image
	gotIstioProxy.TerminationMessagePath = wantIstioProxy.TerminationMessagePath
	gotIstioProxy.TerminationMessagePolicy = wantIstioProxy.TerminationMessagePolicy

	envVars := make([]corev1.EnvVar, 0)
	for _, env := range gotIstioProxy.Env {
		if env.ValueFrom != nil {
			env.ValueFrom.FieldRef.APIVersion = ""
		}
		// check if metajson is encoded correctly
		if strings.HasPrefix(env.Name, "ISTIO_METAJSON_") {
			var mm map[string]string
			if err := json.Unmarshal([]byte(env.Value), &mm); err != nil {
				t.Fatalf("unable to unmarshal %s: %v", env.Value, err)
			}
			continue
		}
		envVars = append(envVars, env)
	}
	gotIstioProxy.Env = envVars

	marshaler := jsonpb.Marshaler{
		Indent: "  ",
	}
	gotString, err := marshaler.MarshalToString(got)
	if err != nil {
		t.Fatal(err)
	}
	wantString, err := marshaler.MarshalToString(want)
	if err != nil {
		t.Fatal(err)
	}

	return util.Compare([]byte(gotString), []byte(wantString))
}

func getAnnotations(d *appsv1.Deployment) map[string]string {
	return d.Spec.Template.ObjectMeta.Annotations
}

func istioInit(d *appsv1.Deployment, t *testing.T) *corev1.Container {
	t.Helper()
	for i := 0; i < len(d.Spec.Template.Spec.InitContainers); i++ {
		c := &d.Spec.Template.Spec.InitContainers[i]
		if c.Name == "istio-init" {
			return c
		}
	}
	t.Fatal("Failed to find istio-init container")
	return nil
}

func istioProxy(d *appsv1.Deployment, t *testing.T) *corev1.Container {
	t.Helper()
	for i := 0; i < len(d.Spec.Template.Spec.Containers); i++ {
		c := &d.Spec.Template.Spec.Containers[i]
		if c.Name == "istio-proxy" {
			return c
		}
	}
	t.Fatal("Failed to find istio-proxy container")
	return nil
}

func makeTestData(t testing.TB, skip bool, apiVersion string) []byte {
	t.Helper()

	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test",
			Annotations: map[string]string{},
		},
		Spec: corev1.PodSpec{
			Volumes:          []corev1.Volume{{Name: "v0"}},
			InitContainers:   []corev1.Container{{Name: "c0"}},
			Containers:       []corev1.Container{{Name: "c1"}},
			ImagePullSecrets: []corev1.LocalObjectReference{{Name: "p0"}},
		},
	}

	if skip {
		pod.ObjectMeta.Annotations[annotation.SidecarInject.Name] = "false"
	}

	raw, err := json.Marshal(&pod)
	if err != nil {
		t.Fatalf("Could not create test pod: %v", err)
	}

	review := v1beta1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AdmissionReview",
			APIVersion: fmt.Sprintf("admission.k8s.io/%s", apiVersion),
		},
		Request: &v1beta1.AdmissionRequest{
			Kind: metav1.GroupVersionKind{
				Group:   v1beta1.GroupName,
				Version: apiVersion,
				Kind:    "AdmissionRequest",
			},
			Object: runtime.RawExtension{
				Raw: raw,
			},
			Operation: v1beta1.Create,
		},
	}
	reviewJSON, err := json.Marshal(review)
	if err != nil {
		t.Fatalf("Failed to create AdmissionReview: %v", err)
	}
	return reviewJSON
}

func createWebhook(t testing.TB, cfg *Config) (*Webhook, func()) {
	t.Helper()
	dir, err := ioutil.TempDir("", "webhook_test")
	if err != nil {
		t.Fatalf("TempDir() failed: %v", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(dir)
	}

	configBytes, err := yaml.Marshal(cfg)
	if err != nil {
		cleanup()
		t.Fatalf("Could not marshal test injection config: %v", err)
	}
	_, values, _ := loadInjectionSettings(t, nil, "")
	var (
		configFile     = filepath.Join(dir, "config-file.yaml")
		valuesFile     = filepath.Join(dir, "values-file.yaml")
		port           = 0
		monitoringPort = 0
	)

	if err := ioutil.WriteFile(configFile, configBytes, 0644); err != nil { // nolint: vetshadow
		cleanup()
		t.Fatalf("WriteFile(%v) failed: %v", configFile, err)
	}

	if err := ioutil.WriteFile(valuesFile, []byte(values), 0644); err != nil { // nolint: vetshadow
		cleanup()
		t.Fatalf("WriteFile(%v) failed: %v", valuesFile, err)
	}

	// mesh config
	m := mesh.DefaultMeshConfig()
	env := model.Environment{
		Watcher: mesh.NewFixedWatcher(&m),
	}
	wh, err := NewWebhook(WebhookParameters{
		ConfigFile:     configFile,
		ValuesFile:     valuesFile,
		Port:           port,
		MonitoringPort: monitoringPort,
		Env:            &env,
		Mux:            http.NewServeMux(),
	})
	if err != nil {
		cleanup()
		t.Fatalf("NewWebhook() failed: %v", err)
	}
	return wh, cleanup
}

func TestRunAndServe(t *testing.T) {
	// TODO: adjust the test to match prod defaults instead of fake defaults.
	wh, cleanup := createWebhook(t, minimalSidecarTemplate)
	defer cleanup()
	stop := make(chan struct{})
	defer func() { close(stop) }()
	go wh.Run(stop)

	validReview := makeTestData(t, false, "v1beta1")
	validReviewV1 := makeTestData(t, false, "v1")
	skipReview := makeTestData(t, true, "v1beta1")

	// nolint: lll
	validPatch := []byte(`[
   {
      "op":"add",
      "path":"/spec/initContainers/-",
      "value":{
         "name":"istio-init",
         "resources":{

         }
      }
   },
   {
      "op":"add",
      "path":"/spec/containers/-",
      "value":{
         "name":"istio-proxy",
         "resources":{

         }
      }
   },
   {
      "op":"add",
      "path":"/spec/volumes/-",
      "value":{
         "name":"istio-envoy"
      }
   },
   {
      "op":"add",
      "path":"/spec/imagePullSecrets/-",
      "value":{
         "name":"istio-image-pull-secrets"
      }
   },
   {
      "op":"add",
      "path":"/spec/securityContext",
      "value":{
         "fsGroup":1337
      }
   },
   {
      "op":"add",
      "path":"/metadata/annotations",
      "value":{
         "prometheus.io/path":"/stats/prometheus"
      }
   },
   {
      "op": "add",
      "path": "/metadata/annotations/prometheus.io~1port",
      "value": "15020"
   },
   {
      "op": "add",
      "path": "/metadata/annotations/prometheus.io~1scrape",
      "value": "true"
   },
   {
      "op":"add",
      "path":"/metadata/annotations/sidecar.istio.io~1status",
      "value": "{\"version\":\"461c380844de8df1d1e2a80a09b6d7b58b8313c4a7d6796530eb124740a1440f\",\"initContainers\":[\"istio-init\"],\"containers\":[\"istio-proxy\"],\"volumes\":[\"istio-envoy\"],\"imagePullSecrets\":[\"istio-image-pull-secrets\"]}"
   },
    {
      "op":"add",
      "path":"/metadata/labels",
      "value":{
         "istio.io/rev":""
      }
    },
    {
      "op":"add",
      "path":"/metadata/labels/security.istio.io~1tlsMode",
      "value":"istio"
    },
    {
      "op": "add",
      "path": "/metadata/labels/service.istio.io~1canonical-name",
      "value": "test"
	},
	{
		"op": "add",
		"path": "/metadata/labels/service.istio.io~1canonical-revision",
		"value": "latest"
	}
]`)

	cases := []struct {
		name           string
		body           []byte
		contentType    string
		wantAllowed    bool
		wantStatusCode int
		wantPatch      []byte
	}{
		{
			name:           "valid",
			body:           validReview,
			contentType:    "application/json",
			wantAllowed:    true,
			wantStatusCode: http.StatusOK,
			wantPatch:      validPatch,
		},
		{
			name:           "valid(v1 version)",
			body:           validReviewV1,
			contentType:    "application/json",
			wantAllowed:    true,
			wantStatusCode: http.StatusOK,
			wantPatch:      validPatch,
		},
		{
			name:           "skipped",
			body:           skipReview,
			contentType:    "application/json",
			wantAllowed:    true,
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "wrong content-type",
			body:           validReview,
			contentType:    "application/yaml",
			wantAllowed:    false,
			wantStatusCode: http.StatusUnsupportedMediaType,
		},
		{
			name:           "bad content",
			body:           []byte{0, 1, 2, 3, 4, 5}, // random data
			contentType:    "application/json",
			wantAllowed:    false,
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "missing body",
			contentType:    "application/json",
			wantAllowed:    false,
			wantStatusCode: http.StatusBadRequest,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("[%d] %s", i, c.name), func(t *testing.T) {
			req := httptest.NewRequest("POST", "http://sidecar-injector/inject", bytes.NewReader(c.body))
			req.Header.Add("Content-Type", c.contentType)

			w := httptest.NewRecorder()
			wh.serveInject(w, req)
			res := w.Result()

			if res.StatusCode != c.wantStatusCode {
				t.Fatalf("wrong status code: \ngot %v \nwant %v", res.StatusCode, c.wantStatusCode)
			}

			if res.StatusCode != http.StatusOK {
				return
			}

			gotBody, err := ioutil.ReadAll(res.Body)
			if err != nil {
				t.Fatalf("could not read body: %v", err)
			}
			var gotReview v1beta1.AdmissionReview
			if err := json.Unmarshal(gotBody, &gotReview); err != nil {
				t.Fatalf("could not decode response body: %v", err)
			}
			if gotReview.Response.Allowed != c.wantAllowed {
				t.Fatalf("AdmissionReview.Response.Allowed is wrong : got %v want %v",
					gotReview.Response.Allowed, c.wantAllowed)
			}

			var gotPatch bytes.Buffer
			if len(gotReview.Response.Patch) > 0 {
				if err := json.Compact(&gotPatch, gotReview.Response.Patch); err != nil {
					t.Fatalf(err.Error())
				}
			}
			var wantPatch bytes.Buffer
			if len(c.wantPatch) > 0 {
				if err := json.Compact(&wantPatch, c.wantPatch); err != nil {
					t.Fatalf(err.Error())
				}
			}

			if !bytes.Equal(gotPatch.Bytes(), wantPatch.Bytes()) {
				t.Fatalf("got bad patch: \n got  %v \n want %v", gotPatch.String(), wantPatch.String())
			}
		})
	}
	// Now Validate that metrics are created.
	testSideCarInjectorMetrics(t, wh)
}

func testSideCarInjectorMetrics(t *testing.T, wh *Webhook) {
	srv := httptest.NewServer(wh.mon.exporter)
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("failed to get /metrics: %v", err)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("failed to close: %v", err)
	}

	output := string(body)

	if !strings.Contains(output, "sidecar_injection_requests_total") {
		t.Fatalf("metric sidecar_injection_requests_total not found")
	}

	if !strings.Contains(output, "sidecar_injection_success_total") {
		t.Fatalf("metric sidecar_injection_success_total not found")
	}

	if !strings.Contains(output, "sidecar_injection_skip_total") {
		t.Fatalf("metric sidecar_injection_skip_total not found")
	}

	if !strings.Contains(output, "sidecar_injection_failure_total") {
		t.Fatalf("incorrect value for metric sidecar_injection_failure_total")
	}
}

func BenchmarkInjectServe(b *testing.B) {
	sidecarTemplate, _, _ := loadInjectionSettings(b, nil, "")
	wh, cleanup := createWebhook(b, sidecarTemplate)
	defer cleanup()

	stop := make(chan struct{})
	defer func() { close(stop) }()
	go wh.Run(stop)

	body := makeTestData(b, false, "v1beta1")
	req := httptest.NewRequest("POST", "http://sidecar-injector/inject", bytes.NewReader(body))
	req.Header.Add("Content-Type", "application/json")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wh.serveInject(httptest.NewRecorder(), req)
	}
}

func TestEnablePrometheusAggregation(t *testing.T) {
	tests := []struct {
		name string
		mesh *meshconfig.MeshConfig
		anno map[string]string
		want bool
	}{
		{
			"no settings",
			nil,
			nil,
			true,
		},
		{
			"mesh on",
			&meshconfig.MeshConfig{EnablePrometheusMerge: &types.BoolValue{Value: true}},
			nil,
			true,
		},
		{
			"mesh off",
			&meshconfig.MeshConfig{EnablePrometheusMerge: &types.BoolValue{Value: false}},
			nil,
			false,
		},
		{
			"annotation on",
			&meshconfig.MeshConfig{EnablePrometheusMerge: &types.BoolValue{Value: false}},
			map[string]string{annotation.PrometheusMergeMetrics.Name: "true"},
			true,
		},
		{
			"annotation off",
			&meshconfig.MeshConfig{EnablePrometheusMerge: &types.BoolValue{Value: true}},
			map[string]string{annotation.PrometheusMergeMetrics.Name: "false"},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := enablePrometheusMerge(tt.mesh, tt.anno); got != tt.want {
				t.Errorf("enablePrometheusMerge() = %v, want %v", got, tt.want)
			}
		})
	}
}

// defaultInstallPackageDir returns a path to a snapshot of the helm charts used for testing.
func defaultInstallPackageDir() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return filepath.Join(wd, "../../../operator/cmd/mesh/testdata/manifest-generate/data-snapshot")
}
