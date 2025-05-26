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
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	jsonpatch "github.com/evanphx/json-patch/v5"
	openshiftv1 "github.com/openshift/api/apps/v1"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"k8s.io/api/admission/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	"istio.io/api/annotation"
	"istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	v1beta12 "istio.io/api/networking/v1beta1"
	"istio.io/istio/operator/pkg/render"
	"istio.io/istio/pilot/cmd/pilot-agent/status"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/multicluster"
	"istio.io/istio/pkg/monitoring/monitortest"
	"istio.io/istio/pkg/test"
)

const yamlSeparator = "\n---"

var minimalSidecarTemplate = &Config{
	Policy:           InjectionPolicyEnabled,
	DefaultTemplates: []string{SidecarTemplateName},
	RawTemplates: map[string]string{SidecarTemplateName: `
spec:
  initContainers:
  - name: istio-init
  containers:
  - name: istio-proxy
  volumes:
  - name: istio-envoy
  imagePullSecrets:
  - name: istio-image-pull-secrets
`},
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
		meta    metav1.ObjectMeta
		want    bool
	}{
		{
			config: &Config{
				Policy: InjectionPolicyEnabled,
			},
			podSpec: podSpec,
			meta: metav1.ObjectMeta{
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
			meta: metav1.ObjectMeta{
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
			meta: metav1.ObjectMeta{
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
			meta: metav1.ObjectMeta{
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
			meta: metav1.ObjectMeta{
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
			meta: metav1.ObjectMeta{
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
			meta: metav1.ObjectMeta{
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
			meta: metav1.ObjectMeta{
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
			podSpec: podSpec,
			meta: metav1.ObjectMeta{
				Name:        "invalid-inject-value-yes",
				Namespace:   "test-namespace",
				Annotations: map[string]string{annotation.SidecarInject.Name: "yes"},
			},
			want: true,
		},
		{
			config: &Config{
				Policy: InjectionPolicyDisabled,
			},
			podSpec: podSpec,
			meta: metav1.ObjectMeta{
				Name:        "invalid-inject-value-on",
				Namespace:   "test-namespace",
				Annotations: map[string]string{annotation.SidecarInject.Name: "on"},
			},
			want: false,
		},
		{
			config: &Config{
				Policy: InjectionPolicyEnabled,
			},
			podSpec: podSpec,
			meta: metav1.ObjectMeta{
				Name:      "invalid-inject-value-random",
				Namespace: "test-namespace",
				Labels:    map[string]string{label.SidecarInject.Name: "random"},
			},
			want: true,
		},
		{
			config: &Config{
				Policy: InjectionPolicyEnabled,
			},
			podSpec: podSpecHostNetwork,
			meta: metav1.ObjectMeta{
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
			meta: metav1.ObjectMeta{
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
			meta: metav1.ObjectMeta{
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
			meta: metav1.ObjectMeta{
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
			meta: metav1.ObjectMeta{
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
			meta: metav1.ObjectMeta{
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
			meta: metav1.ObjectMeta{
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
			meta: metav1.ObjectMeta{
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
			meta: metav1.ObjectMeta{
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
			meta: metav1.ObjectMeta{
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
			meta: metav1.ObjectMeta{
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
			meta: metav1.ObjectMeta{
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
			meta: metav1.ObjectMeta{
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
			meta: metav1.ObjectMeta{
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
			meta: metav1.ObjectMeta{
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
			meta: metav1.ObjectMeta{
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
			meta: metav1.ObjectMeta{
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
			meta: metav1.ObjectMeta{
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
			meta: metav1.ObjectMeta{
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
			meta: metav1.ObjectMeta{
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
			meta: metav1.ObjectMeta{
				Name:        "policy-disabled-annotation-true-never-inject",
				Namespace:   "test-namespace",
				Annotations: map[string]string{annotation.SidecarInject.Name: "true"},
				Labels:      map[string]string{"foo": "", "foo2": "bar2"},
			},
			want: true,
		},
		{
			config: &Config{
				Policy: InjectionPolicyDisabled,
			},
			podSpec: podSpec,
			meta: metav1.ObjectMeta{
				Name:      "policy-disabled-label-enabled",
				Namespace: "test-namespace",
				Labels:    map[string]string{label.SidecarInject.Name: "true"},
			},
			want: true,
		},
		{
			config: &Config{
				Policy: InjectionPolicyDisabled,
			},
			podSpec: podSpec,
			meta: metav1.ObjectMeta{
				Name:        "policy-disabled-both-enabled",
				Namespace:   "test-namespace",
				Annotations: map[string]string{annotation.SidecarInject.Name: "true"},
				Labels:      map[string]string{label.SidecarInject.Name: "true"},
			},
			want: true,
		},
		{
			config: &Config{
				Policy: InjectionPolicyDisabled,
			},
			podSpec: podSpec,
			meta: metav1.ObjectMeta{
				Name:        "policy-disabled-label-enabled-annotation-disabled",
				Namespace:   "test-namespace",
				Annotations: map[string]string{annotation.SidecarInject.Name: "false"},
				Labels:      map[string]string{label.SidecarInject.Name: "true"},
			},
			want: true,
		},
		{
			config: &Config{
				Policy: InjectionPolicyDisabled,
			},
			podSpec: podSpec,
			meta: metav1.ObjectMeta{
				Name:        "policy-disabled-label-disabled-annotation-enabled",
				Namespace:   "test-namespace",
				Annotations: map[string]string{annotation.SidecarInject.Name: "true"},
				Labels:      map[string]string{label.SidecarInject.Name: "false"},
			},
			want: false,
		},
	}

	for _, c := range cases {
		if got := injectRequired(IgnoredNamespaces.UnsortedList(), c.config, c.podSpec, c.meta); got != c.want {
			t.Errorf("injectRequired(%v, %v) got %v want %v", c.config, c.meta, got, c.want)
		}
	}
}

func simulateOwnerRef(m metav1.ObjectMeta, name string, gvk schema.GroupVersionKind) metav1.ObjectMeta {
	controller := true
	m.GenerateName = name
	m.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Name:       name,
		Controller: &controller,
	}}
	return m
}

func objectToPod(t testing.TB, obj runtime.Object) *corev1.Pod {
	gvk := obj.GetObjectKind().GroupVersionKind()
	defaultConversion := func(template corev1.PodTemplateSpec, name string) *corev1.Pod {
		template.ObjectMeta = simulateOwnerRef(template.ObjectMeta, name, gvk)
		return &corev1.Pod{
			ObjectMeta: template.ObjectMeta,
			Spec:       template.Spec,
		}
	}
	switch o := obj.(type) {
	case *corev1.Pod:
		return o
	case *batchv1.CronJob:
		o.Spec.JobTemplate.Spec.Template.ObjectMeta = simulateOwnerRef(o.Spec.JobTemplate.Spec.Template.ObjectMeta, o.Name, gvk)
		return &corev1.Pod{
			ObjectMeta: o.Spec.JobTemplate.Spec.Template.ObjectMeta,
			Spec:       o.Spec.JobTemplate.Spec.Template.Spec,
		}
	case *appsv1.DaemonSet:
		return defaultConversion(o.Spec.Template, o.Name)
	case *appsv1.ReplicaSet:
		return defaultConversion(o.Spec.Template, o.Name)
	case *corev1.ReplicationController:
		return defaultConversion(*o.Spec.Template, o.Name)
	case *appsv1.StatefulSet:
		return defaultConversion(o.Spec.Template, o.Name)
	case *batchv1.Job:
		return defaultConversion(o.Spec.Template, o.Name)
	case *openshiftv1.DeploymentConfig:
		return defaultConversion(*o.Spec.Template, o.Name)
	case *appsv1.Deployment:
		// Deployment is special since its a double nested resource
		rsgvk := schema.GroupVersionKind{Kind: "ReplicaSet", Group: "apps", Version: "v1"}
		o.Spec.Template.ObjectMeta = simulateOwnerRef(o.Spec.Template.ObjectMeta, o.Name+"-fake", rsgvk)
		o.Spec.Template.ObjectMeta.GenerateName += "-"
		if o.Spec.Template.ObjectMeta.Labels == nil {
			o.Spec.Template.ObjectMeta.Labels = map[string]string{}
		}
		o.Spec.Template.ObjectMeta.Labels["pod-template-hash"] = "fake"
		return &corev1.Pod{
			ObjectMeta: o.Spec.Template.ObjectMeta,
			Spec:       o.Spec.Template.Spec,
		}
	}
	t.Fatalf("unknown type: %T", obj)
	return nil
}

// loadInjectionSettings will render the charts using the operator, with given yaml overrides.
// This allows us to fully simulate what will actually happen at run time.
func getInjectionSettings(t testing.TB, setFlags []string, inFilePath string) (config *Config, valuesConfig ValuesConfig, meshConfig *meshconfig.MeshConfig) {
	// add --set installPackagePath=<path to charts snapshot>
	setFlags = append(setFlags, "installPackagePath="+defaultInstallPackageDir(), "profile=empty", "components.pilot.enabled=true")
	var inFilenames []string
	if inFilePath != "" {
		inFilenames = []string{"testdata/inject/" + inFilePath}
	}

	manifests, _, err := render.GenerateManifest(inFilenames, setFlags, false, nil, nil)
	if err != nil {
		t.Fatalf("failed to generate manifests: %v", err)
	}
	for _, object := range manifests {
		for _, o := range object.Manifests {
			if o.GetName() == "istio-sidecar-injector" && o.GetKind() == gvk.ConfigMap.Kind {
				data, ok := o.Object["data"].(map[string]any)
				if !ok {
					t.Fatalf("failed to convert %v", o)
				}
				rawConfig, ok := data["config"].(string)
				if !ok {
					t.Fatalf("failed to config %v", data)
				}
				vs, ok := data["values"].(string)
				if !ok {
					t.Fatalf("failed to config %v", data)
				}
				vc, err := NewValuesConfig(vs)
				if err != nil {
					t.Fatal(err)
				}
				valuesConfig = vc

				cfg, err := UnmarshalConfig([]byte(rawConfig))
				if err != nil {
					t.Fatalf("failed to unmarshal injectionConfig: %v", err)
				}

				config = &cfg
			} else if o.GetName() == "istio" && o.GetKind() == gvk.ConfigMap.Kind {
				data, ok := o.Object["data"].(map[string]any)
				if !ok {
					t.Fatalf("failed to convert %v", o)
				}
				meshdata, ok := data["mesh"].(string)
				if !ok {
					t.Fatalf("failed to get meshconfig %v", data)
				}
				mcfg, err := mesh.ApplyMeshConfig(meshdata, mesh.DefaultMeshConfig())
				if err != nil {
					t.Fatalf("failed to unmarshal meshconfig: %v", err)
				}
				meshConfig = mcfg
			}
		}
	}
	return config, valuesConfig, meshConfig
}

func splitYamlFile(yamlFile string, t *testing.T) [][]byte {
	t.Helper()
	yamlBytes := util.ReadFile(t, yamlFile)
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
		t.Fatal("Found no injectable parts")
	}
	return byteParts
}

func getInjectableYamlDocs(yamlDoc string, t *testing.T) [][]byte {
	t.Helper()
	m := make(map[string]any)
	if err := yaml.Unmarshal([]byte(yamlDoc), &m); err != nil {
		t.Fatal(err)
	}
	switch m["kind"] {
	case "Deployment", "DeploymentConfig", "DaemonSet", "StatefulSet", "Job", "ReplicaSet",
		"ReplicationController", "CronJob", "Pod":
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

func convertToJSON(i any, t test.Failer) []byte {
	t.Helper()
	outputJSON, err := json.Marshal(i)
	if err != nil {
		t.Fatal(err)
	}
	return prettyJSON(outputJSON, t)
}

func prettyJSON(inputJSON []byte, t test.Failer) []byte {
	t.Helper()
	// Pretty-print the JSON
	var prettyBuffer bytes.Buffer
	if err := json.Indent(&prettyBuffer, inputJSON, "", "  "); err != nil {
		t.Fatal(err.Error())
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

func jsonToUnstructured(obj []byte, t *testing.T) *unstructured.Unstructured {
	r := bytes.NewReader(obj)
	decoder := k8syaml.NewYAMLOrJSONDecoder(r, 1024)

	out := &unstructured.Unstructured{}
	err := decoder.Decode(out)
	if err != nil {
		t.Fatalf("error decoding object: %v", err)
	}
	return out
}

func normalizeAndCompareDeployments(got, want *corev1.Pod, ignoreIstioMetaJSONAnnotationsEnv bool, t *testing.T) error {
	t.Helper()
	// Scrub unimportant fields that tend to differ.
	delete(got.Annotations, annotation.SidecarStatus.Name)
	delete(want.Annotations, annotation.SidecarStatus.Name)

	for _, c := range got.Spec.Containers {
		for _, env := range c.Env {
			if env.ValueFrom != nil && env.ValueFrom.FieldRef != nil {
				env.ValueFrom.FieldRef.APIVersion = ""
			}
			// check if metajson is encoded correctly
			if strings.HasPrefix(env.Name, "ISTIO_METAJSON_") {
				var mm map[string]string
				if err := json.Unmarshal([]byte(env.Value), &mm); err != nil {
					t.Fatalf("unable to unmarshal %s: %v", env.Value, err)
				}
			}
		}
	}

	if ignoreIstioMetaJSONAnnotationsEnv {
		removeContainerEnvEntry(got, "ISTIO_METAJSON_ANNOTATIONS")
		removeContainerEnvEntry(want, "ISTIO_METAJSON_ANNOTATIONS")
	}

	gotString, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	wantString, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}

	return util.Compare(gotString, wantString)
}

func removeContainerEnvEntry(pod *corev1.Pod, envVarName string) {
	for i, c := range pod.Spec.Containers {
		for j, v := range c.Env {
			if v.Name == envVarName {
				pod.Spec.Containers[i].Env = append(c.Env[:j], c.Env[j+1:]...)
				break
			}
		}
	}
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

func createWebhook(t testing.TB, cfg *Config, pcResources int) *Webhook {
	t.Helper()
	dir := t.TempDir()

	configBytes, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("Could not marshal test injection config: %v", err)
	}
	_, values, _ := getInjectionSettings(t, nil, "")
	var (
		configFile = filepath.Join(dir, "config-file.yaml")
		valuesFile = filepath.Join(dir, "values-file.yaml")
		port       = 0
	)

	if err := os.WriteFile(configFile, configBytes, 0o644); err != nil { // nolint: vetshadow
		t.Fatalf("WriteFile(%v) failed: %v", configFile, err)
	}

	if err := os.WriteFile(valuesFile, []byte(values.raw), 0o644); err != nil { // nolint: vetshadow
		t.Fatalf("WriteFile(%v) failed: %v", valuesFile, err)
	}

	// mesh config
	m := mesh.DefaultMeshConfig()
	store := model.NewFakeStore()
	for i := 0; i < pcResources; i++ {
		store.Create(newProxyConfig(fmt.Sprintf("pc-%d", i), "istio-system", &v1beta12.ProxyConfig{
			Concurrency: &wrapperspb.Int32Value{Value: int32(i % 5)},
			EnvironmentVariables: map[string]string{
				fmt.Sprintf("VAR_%d", i): fmt.Sprint(i),
			},
		}))
	}
	pcs := model.GetProxyConfigs(store, m)
	env := model.Environment{
		Watcher:     meshwatcher.NewTestWatcher(m),
		ConfigStore: store,
	}
	env.SetPushContext(&model.PushContext{
		ProxyConfigs: pcs,
	})
	watcher, err := NewFileWatcher(configFile, valuesFile)
	if err != nil {
		t.Fatalf("NewFileWatcher() failed: %v", err)
	}

	wh, err := NewWebhook(WebhookParameters{
		Watcher:      watcher,
		Port:         port,
		Env:          &env,
		Mux:          http.NewServeMux(),
		MultiCluster: multicluster.NewFakeController(),
	})
	if err != nil {
		t.Fatalf("NewWebhook() failed: %v", err)
	}
	return wh
}

func TestRunAndServe(t *testing.T) {
	multi := multicluster.NewFakeController()
	client := kube.NewFakeClient()
	stop := test.NewStop(t)
	multi.Add(constants.DefaultClusterName, client, stop)
	client.RunAndWait(stop)
	// TODO: adjust the test to match prod defaults instead of fake defaults.
	wh := createWebhook(t, minimalSidecarTemplate, 0)
	stop = make(chan struct{})
	defer func() { close(stop) }()
	wh.Run(stop)

	validReview := makeTestData(t, false, "v1beta1")
	validReviewV1 := makeTestData(t, false, "v1")
	skipReview := makeTestData(t, true, "v1beta1")

	cases := []struct {
		name           string
		body           []byte
		contentType    string
		wantAllowed    bool
		wantStatusCode int
	}{
		{
			name:           "valid",
			body:           validReview,
			contentType:    "application/json",
			wantAllowed:    true,
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "valid(v1 version)",
			body:           validReviewV1,
			contentType:    "application/json",
			wantAllowed:    true,
			wantStatusCode: http.StatusOK,
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
	mt := monitortest.New(t)
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

			gotBody, err := io.ReadAll(res.Body)
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
					t.Fatal(err.Error())
				}
			}
		})
	}
	// Now Validate that metrics are created.
	testSideCarInjectorMetrics(mt)
}

func testSideCarInjectorMetrics(mt *monitortest.MetricsTest) {
	expected := []string{
		"sidecar_injection_requests_total",
		"sidecar_injection_success_total",
		"sidecar_injection_skip_total",
		"sidecar_injection_failure_total",
	}
	for _, e := range expected {
		mt.Assert(e, nil, monitortest.AtLeast(1))
	}
}

func benchmarkInjectServe(pcs int, b *testing.B) {
	sidecarTemplate, _, _ := getInjectionSettings(b, nil, "")
	wh := createWebhook(b, sidecarTemplate, pcs)

	stop := make(chan struct{})
	defer func() { close(stop) }()
	wh.Run(stop)

	body := makeTestData(b, false, "v1beta1")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "http://sidecar-injector/inject", bytes.NewReader(body))
		req.Header.Add("Content-Type", "application/json")

		wh.serveInject(httptest.NewRecorder(), req)
	}
}

func BenchmarkInjectServePC0(b *testing.B) {
	benchmarkInjectServe(0, b)
}

func BenchmarkInjectServePC5(b *testing.B) {
	benchmarkInjectServe(5, b)
}

func BenchmarkInjectServePC15(b *testing.B) {
	benchmarkInjectServe(15, b)
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
			&meshconfig.MeshConfig{EnablePrometheusMerge: &wrapperspb.BoolValue{Value: true}},
			nil,
			true,
		},
		{
			"mesh off",
			&meshconfig.MeshConfig{EnablePrometheusMerge: &wrapperspb.BoolValue{Value: false}},
			nil,
			false,
		},
		{
			"annotation on",
			&meshconfig.MeshConfig{EnablePrometheusMerge: &wrapperspb.BoolValue{Value: false}},
			map[string]string{annotation.PrometheusMergeMetrics.Name: "true"},
			true,
		},
		{
			"annotation off",
			&meshconfig.MeshConfig{EnablePrometheusMerge: &wrapperspb.BoolValue{Value: true}},
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

func TestParseInjectEnvs(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want map[string]string
	}{
		{
			name: "empty",
			in:   "/",
			want: map[string]string{},
		},
		{
			name: "no-kv",
			in:   "/inject",
			want: map[string]string{},
		},
		{
			name: "no-kv-with-tail",
			in:   "/inject/",
			want: map[string]string{},
		},
		{
			name: "one-kv",
			in:   "/inject/cluster/cluster1",
			want: map[string]string{"ISTIO_META_CLUSTER_ID": "cluster1"},
		},
		{
			name: "two-kv",
			in:   "/inject/cluster/cluster1/net/network1/",
			want: map[string]string{"ISTIO_META_CLUSTER_ID": "cluster1", "ISTIO_META_NETWORK": "network1"},
		},
		{
			name: "kv-with-slashes",
			in:   "/inject/cluster/cluster--slash--1/net/network--slash--1",
			want: map[string]string{"ISTIO_META_CLUSTER_ID": "cluster/1", "ISTIO_META_NETWORK": "network/1"},
		},
		{
			name: "not-predefined-kv",
			in:   "/inject/cluster/cluster1/custom_env/foo",
			want: map[string]string{"ISTIO_META_CLUSTER_ID": "cluster1", "CUSTOM_ENV": "foo"},
		},
		{
			name: "one-key-without-value",
			in:   "/inject/cluster",
			want: map[string]string{},
		},
		{
			name: "key-without-value",
			in:   "/inject/cluster/cluster1/network",
			want: map[string]string{"ISTIO_META_CLUSTER_ID": "cluster1"},
		},
		{
			name: "key-with-values-contain-slashes",
			in:   "/inject/:ENV:cluster=cluster2:ENV:rootpage=/foo/bar",
			want: map[string]string{"ISTIO_META_CLUSTER_ID": "cluster2", "ROOTPAGE": "/foo/bar"},
		},
		{
			// this is to test the path not following :ENV: format, the
			// path will be considered using slash as separator
			name: "no-predefined-kv-with-values-contain-ENV-separator",
			in:   "/inject/rootpage1/value1/rootpage2/:ENV:abcd=efgh",
			want: map[string]string{"ROOTPAGE1": "value1", "ROOTPAGE2": ":ENV:abcd=efgh"},
		},
		{
			// this is to test the path following :ENV: format, but two variables
			// do not have correct format, thus they will be ignored. Eg. :ENV:=abb
			// :ENV:=, these two are not correct variables.
			name: "no-predefined-kv-with-values-contain-ENV-separator-invalid-format",
			in:   "/inject/:ENV:rootpage1=efgh:ENV:=abb:ENV:=",
			want: map[string]string{"ROOTPAGE1": "efgh"},
		},
		{
			// this is to test that the path is an url encoded string, still using
			// slash as separators
			name: "no-predefined-kv-with-mixed-values",
			in: func() string {
				req, _ := http.NewRequest(http.MethodGet,
					"%2Finject%2Frootpage1%2Ffoo%2Frootpage2%2Fbar", nil)
				return req.URL.Path
			}(),
			want: map[string]string{"ROOTPAGE1": "foo", "ROOTPAGE2": "bar"},
		},
		{
			// this is to test that the path is an url encoded string and :ENV: as separator
			// eg. /inject/:ENV:rootpage1=/foo/bar:ENV:rootpage2=/bar/toe but url encoded.
			name: "no-predefined-kv-with-slashes",
			in: func() string {
				req, _ := http.NewRequest(http.MethodGet,
					"%2Finject%2F%3AENV%3Arootpage1%3D%2Ffoo%2Fbar%3AENV%3Arootpage2%3D%2Fbar%2Ftoe", nil)
				return req.URL.Path
			}(),
			want: map[string]string{"ROOTPAGE1": "/foo/bar", "ROOTPAGE2": "/bar/toe"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actual := parseInjectEnvs(tc.in)
			if !reflect.DeepEqual(actual, tc.want) {
				t.Fatalf("Expected result %#v, but got %#v", tc.want, actual)
			}
		})
	}
}

func TestMergeOrAppendProbers(t *testing.T) {
	cases := []struct {
		name               string
		perviouslyInjected bool
		in                 []corev1.EnvVar
		probers            string
		want               []corev1.EnvVar
	}{
		{
			name:               "Append Prober",
			perviouslyInjected: false,
			in:                 []corev1.EnvVar{},
			probers: `{"/app-health/bar/livez":{"httpGet":{"path":"/","port":9000,"scheme":"HTTP"}},` +
				`"/app-health/foo/livez":{"httpGet":{"path":"/","port":8000,"scheme":"HTTP"}}}`,
			want: []corev1.EnvVar{{
				Name: status.KubeAppProberEnvName,
				Value: `{"/app-health/bar/livez":{"httpGet":{"path":"/","port":9000,"scheme":"HTTP"}},` +
					`"/app-health/foo/livez":{"httpGet":{"path":"/","port":8000,"scheme":"HTTP"}}}`,
			}},
		},
		{
			name:               "Merge Prober",
			perviouslyInjected: true,
			in: []corev1.EnvVar{
				{
					Name:  "TEST_ENV_VAR1",
					Value: "value1",
				},
				{
					Name:  status.KubeAppProberEnvName,
					Value: `{"/app-health/foo/livez":{"httpGet":{"path":"/","port":8000,"scheme":"HTTP"}}}`,
				},
				{
					Name:  "TEST_ENV_VAR2",
					Value: "value2",
				},
			},
			probers: `{"/app-health/bar/livez":{"httpGet":{"path":"/","port":9000,"scheme":"HTTP"}}}`,
			want: []corev1.EnvVar{
				{
					Name:  "TEST_ENV_VAR1",
					Value: "value1",
				},
				{
					Name: status.KubeAppProberEnvName,
					Value: `{"/app-health/bar/livez":{"httpGet":{"path":"/","port":9000,"scheme":"HTTP"}},` +
						`"/app-health/foo/livez":{"httpGet":{"path":"/","port":8000,"scheme":"HTTP"}}}`,
				},
				{
					Name:  "TEST_ENV_VAR2",
					Value: "value2",
				},
			},
		},
		{
			// this is to test previously injected without probe rewrites
			name:               "Merge Prober with absent of KubeAppProberEnv",
			perviouslyInjected: true,
			in: []corev1.EnvVar{
				{
					Name:  "TEST_ENV_VAR1",
					Value: "value1",
				},
				{
					Name:  "TEST_ENV_VAR2",
					Value: "value2",
				},
			},
			probers: `{"/app-health/bar/livez":{"httpGet":{"path":"/","port":9000,"scheme":"HTTP"}}}`,
			want: []corev1.EnvVar{
				{
					Name:  "TEST_ENV_VAR1",
					Value: "value1",
				},
				{
					Name:  "TEST_ENV_VAR2",
					Value: "value2",
				},
				{
					Name:  status.KubeAppProberEnvName,
					Value: `{"/app-health/bar/livez":{"httpGet":{"path":"/","port":9000,"scheme":"HTTP"}}}`,
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actual := mergeOrAppendProbers(tc.perviouslyInjected, tc.in, tc.probers)
			if !reflect.DeepEqual(actual, tc.want) {
				t.Fatalf("Expected result %#v, but got %#v", tc.want, actual)
			}
		})
	}
}

func TestParseStatus(t *testing.T) {
	cases := []struct {
		name   string
		status string
		want   ParsedContainers
	}{
		{
			name: "Regular Containers only",
			status: `{"containers":["istio-proxy", "random-container"],` +
				`"volumes":["workload-socket","istio-token","istiod-ca-cert"]}`,
			want: ParsedContainers{
				Containers: []corev1.Container{
					{Name: "istio-proxy"},
					{Name: "random-container"},
				},
				InitContainers: []corev1.Container{},
			},
		},
		{
			name: "Init Containers only",
			status: `{"initContainers":["istio-init", "istio-validation"],` +
				`"volumes":["workload-socket","istio-token","istiod-ca-cert"]}`,
			want: ParsedContainers{
				Containers: []corev1.Container{},
				InitContainers: []corev1.Container{
					{Name: "istio-init"},
					{Name: "istio-validation"},
				},
			},
		},
		{
			name: "All Containers",
			status: `{"containers":["istio-proxy", "random-container"],"initContainers":["istio-init",` +
				` "istio-validation"],"volumes":["workload-socket","istio-token","istiod-ca-cert"]}`,
			want: ParsedContainers{
				Containers: []corev1.Container{
					{Name: "istio-proxy"},
					{Name: "random-container"},
				},
				InitContainers: []corev1.Container{
					{Name: "istio-init"},
					{Name: "istio-validation"},
				},
			},
		},
		{
			name: "Containers is null",
			status: `{"containers":null,"initContainers":["istio-init",` +
				` "istio-validation"],"volumes":["workload-socket","istio-token","istiod-ca-cert"]}`,
			want: ParsedContainers{
				Containers: []corev1.Container{},
				InitContainers: []corev1.Container{
					{Name: "istio-init"},
					{Name: "istio-validation"},
				},
			},
		},
		{
			name:   "Empty String",
			status: ``,
			want:   ParsedContainers{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actual := parseStatus(tc.status)

			if !reflect.DeepEqual(actual, tc.want) {
				t.Fatalf("Expected result %#v, but got %#v", tc.want, actual)
			}
		})
	}
}

func newProxyConfig(name, ns string, spec config.Spec) config.Config {
	return config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.ProxyConfig,
			Name:             name,
			Namespace:        ns,
		},
		Spec: spec,
	}
}

// defaultInstallPackageDir returns a path to a snapshot of the helm charts used for testing.
func defaultInstallPackageDir() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return filepath.Join(wd, "../../../manifests/")
}

func TestNewWebhookConfigParsingError(t *testing.T) {
	// Create a watcher that returns valid sidecarConfig but invalid valuesConfig
	faultyWatcher := &FaultyWatcher{
		sidecarConfig: &Config{},
		valuesConfig:  "invalid: values: config",
	}

	whParams := WebhookParameters{
		Watcher:      faultyWatcher,
		Port:         0,
		Env:          &model.Environment{},
		Mux:          http.NewServeMux(),
		MultiCluster: multicluster.NewFakeController(),
	}

	_, err := NewWebhook(whParams)
	if err == nil || !strings.Contains(err.Error(), "failed to process webhook config") {
		t.Fatalf("Expected error when creating webhook with faulty valuesConfig, but got: %v", err)
	}
}

// FaultyWatcher is a mock Watcher that returns predefined sidecarConfig and valuesConfig
type FaultyWatcher struct {
	sidecarConfig *Config
	valuesConfig  string
}

func (fw *FaultyWatcher) Run(stop <-chan struct{}) {}

func (fw *FaultyWatcher) Get() (*Config, string, error) {
	return fw.sidecarConfig, fw.valuesConfig, nil
}

func (fw *FaultyWatcher) SetHandler(handler func(*Config, string) error) {}

func TestNewWebhookConfigParsingSuccess(t *testing.T) {
	// Create a watcher that returns valid sidecarConfig and valid valuesConfig
	validValuesConfig := `
global:
  proxy:
    image: proxyv2
`
	faultyWatcher := &FaultyWatcher{
		sidecarConfig: &Config{},
		valuesConfig:  validValuesConfig,
	}

	whParams := WebhookParameters{
		Watcher: faultyWatcher,
		Port:    0,
		Env: &model.Environment{
			Watcher: meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{}),
		},
		Mux:          http.NewServeMux(),
		MultiCluster: multicluster.NewFakeController(),
	}

	wh, err := NewWebhook(whParams)
	if err != nil {
		t.Fatalf("Expected no error when creating webhook with valid valuesConfig, but got: %v", err)
	}

	if wh.valuesConfig.raw != validValuesConfig {
		t.Fatalf("Expected valuesConfig to be set correctly, but got: %v", wh.valuesConfig.raw)
	}
}

func TestDetectNativeSidecar(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t test.Failer)
		nodes []*corev1.Node
		want  bool
	}{
		{
			name: "env disabled should be always disabled",
			nodes: []*corev1.Node{
				{Status: corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{KubeletVersion: "1.33.0"}}},
				{Status: corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{KubeletVersion: "1.32.0"}}},
			},
			setup: func(t test.Failer) {
				test.SetForTest(t, &features.EnableNativeSidecars, false)
			},
			want: false,
		},
		{
			name: "kube versions greater than 1.28 with env enabled should enable native sidecar",
			nodes: []*corev1.Node{
				{Status: corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{KubeletVersion: "1.33.0"}}},
				{Status: corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{KubeletVersion: "1.32.0"}}},
			},
			setup: func(t test.Failer) {
				test.SetForTest(t, &features.EnableNativeSidecars, true)
			},
			want: true,
		},
		{
			name: "kube versions less than or equal to 1.28 with env enabled should enable native sidecar",
			nodes: []*corev1.Node{
				{Status: corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{KubeletVersion: "1.28.0"}}},
				{Status: corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{KubeletVersion: "1.28.0"}}},
			},
			setup: func(t test.Failer) {
				test.SetForTest(t, &features.EnableNativeSidecars, true)
			},
			want: false,
		},
		{
			name: "clusters with at least one node on unsupported version should disable native sidecar",
			nodes: []*corev1.Node{
				{Status: corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{KubeletVersion: "1.29.0"}}},
				{Status: corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{KubeletVersion: "1.28.0"}}},
			},
			setup: func(t test.Failer) {
				test.SetForTest(t, &features.EnableNativeSidecars, true)
			},
			want: false,
		},
		{
			name:  "no nodes should disable native sidecar",
			nodes: []*corev1.Node{{}},
			setup: func(t test.Failer) {
				test.SetForTest(t, &features.EnableNativeSidecars, true)
			},
			want: false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup(t)
			var objects []runtime.Object
			for i, n := range tt.nodes {
				nodeCopy := n.DeepCopy()
				nodeCopy.Name = fmt.Sprintf("node-%d", i+1)
				objects = append(objects, nodeCopy)
			}
			kubeClient := kube.NewFakeClient(objects...)
			nodes := kclient.New[*corev1.Node](kubeClient)
			kubeClient.RunAndWait(test.NewStop(t))
			kube.WaitForCacheSync("test", test.NewStop(t), nodes.HasSynced)

			if got := detectNativeSidecar(nodes); got != tt.want {
				t.Errorf("detectNativeSidecar() = %v, want %v", got, tt.want)
			}
		})
	}
}
