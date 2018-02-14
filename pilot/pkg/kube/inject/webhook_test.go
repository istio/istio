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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/golang/protobuf/jsonpb"
	"k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"istio.io/istio/pilot/pkg/kube/admit/testcerts"
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

func TestInject(t *testing.T) {
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
		{
			inputFile: "TestWebhookInject_replace.yaml",
			wantFile:  "TestWebhookInject_replace.patch",
		},
		{
			inputFile: "TestWebhookInject_replace_backwards_compat.yaml",
			wantFile:  "TestWebhookInject_replace_backwards_compat.patch",
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

func makeTestData(t testing.TB, skip bool) []byte {
	t.Helper()

	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test",
			Annotations: map[string]string{},
		},
		Spec: corev1.PodSpec{
			Volumes:        []corev1.Volume{{Name: "v0"}},
			InitContainers: []corev1.Container{{Name: "c0"}},
			Containers:     []corev1.Container{{Name: "c1"}},
		},
	}

	if skip {
		pod.ObjectMeta.Annotations[istioSidecarAnnotationPolicyKey] = "false"
	}

	raw, err := json.Marshal(&pod)
	if err != nil {
		t.Fatalf("Could not create test pod: %v", err)
	}

	review := v1beta1.AdmissionReview{
		Request: &v1beta1.AdmissionRequest{
			Kind: metav1.GroupVersionKind{},
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

func createWebhook(t testing.TB, sidecarTemplate string) (*Webhook, func()) {
	t.Helper()
	dir, err := ioutil.TempDir("", "webhook_test")
	if err != nil {
		t.Fatalf("TempDir() failed: %v", err)
	}
	cleanup := func() {
		os.RemoveAll(dir) // nolint: errcheck
	}

	config := &Config{
		Policy:   InjectionPolicyEnabled,
		Template: sidecarTemplate,
	}
	configBytes, err := yaml.Marshal(config)
	if err != nil {
		cleanup()
		t.Fatalf("Could not marshal test injection config: %v", err)
	}

	var (
		configFile = filepath.Join(dir, "config-file.yaml")
		meshFile   = filepath.Join(dir, "mesh-file.yaml")
		certFile   = filepath.Join(dir, "cert-file.yaml")
		keyFile    = filepath.Join(dir, "key-file.yaml")
		port       = 0
	)

	if err := ioutil.WriteFile(configFile, configBytes, 0644); err != nil { // nolint: vetshadow
		cleanup()
		t.Fatalf("WriteFile(%v) failed: %v", configFile, err)
	}

	// mesh
	mesh := model.DefaultMeshConfig()
	m := jsonpb.Marshaler{}
	var meshBytes bytes.Buffer
	if err := m.Marshal(&meshBytes, &mesh); err != nil { // nolint: vetshadow
		cleanup()
		t.Fatalf("yaml.Marshal(mesh) failed: %v", err)
	}
	if err := ioutil.WriteFile(meshFile, meshBytes.Bytes(), 0644); err != nil { // nolint: vetshadow
		cleanup()
		t.Fatalf("WriteFile(%v) failed: %v", meshFile, err)
	}

	// cert
	if err := ioutil.WriteFile(certFile, testcerts.ServerCert, 0644); err != nil { // nolint: vetshadow
		cleanup()
		t.Fatalf("WriteFile(%v) failed: %v", certFile, err)
	}
	// key
	if err := ioutil.WriteFile(keyFile, testcerts.ServerKey, 0644); err != nil { // nolint: vetshadow
		cleanup()
		t.Fatalf("WriteFile(%v) failed: %v", keyFile, err)
	}

	wh, err := NewWebhook(WebhookParameters{
		ConfigFile: configFile, MeshFile: meshFile, CertFile: certFile, KeyFile: keyFile, Port: port})
	if err != nil {
		cleanup()
		t.Fatalf("NewWebhook() failed: %v", err)
	}
	return wh, cleanup
}

func TestRunAndServe(t *testing.T) {
	var (
		minimalSidecarTemplate = `
initContainers:
- name: istio-init
containers:
- name: istio-proxy
volumes:
- name: istio-envoy
`
	)

	wh, cleanup := createWebhook(t, minimalSidecarTemplate)
	defer cleanup()
	stop := make(chan struct{})
	defer func() { close(stop) }()
	go wh.Run(stop)

	validReview := makeTestData(t, false)
	skipReview := makeTestData(t, true)

	// nolint: lll
	validPatch := []byte(`[
   {
      "op":"add",
      "path":"/spec/initContainers/-",
      "value":{
         "name":"istio-init",
         "resources":{

         },
         "terminationMessagePath":"/dev/termination-log",
         "terminationMessagePolicy":"File",
         "imagePullPolicy":"IfNotPresent"
      }
   },
   {
      "op":"add",
      "path":"/spec/containers/-",
      "value":{
         "name":"istio-proxy",
         "resources":{

         },
         "terminationMessagePath":"/dev/termination-log",
         "terminationMessagePolicy":"File",
         "imagePullPolicy":"IfNotPresent"
      }
   },
   {
      "op":"add",
      "path":"/spec/volumes/-",
      "value":{
         "name":"istio-envoy",
         "emptyDir":{

         }
      }
   },
   {
      "op":"add",
      "path":"/metadata/annotations",
      "value":{
         "sidecar.istio.io/status":"{\"version\":\"d4935f8d2cac8a0d1549d3c6c61d512ba2e6822965f4f6b0863a98f73993dbf8\",\"initContainers\":[\"istio-init\"],\"containers\":[\"istio-proxy\"],\"volumes\":[\"istio-envoy\"]}"
      }
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
				t.Fatalf("got bad patch: \n got %v \n want %v", gotPatch, wantPatch)
			}
		})
	}
}

func BenchmarkInjectServe(b *testing.B) {
	mesh := model.DefaultMeshConfig()
	params := &Params{
		InitImage:       InitImageName(unitTestHub, unitTestTag, false),
		ProxyImage:      ProxyImageName(unitTestHub, unitTestTag, false),
		ImagePullPolicy: "IfNotPresent",
		Verbosity:       DefaultVerbosity,
		SidecarProxyUID: DefaultSidecarProxyUID,
		Version:         "12345678",
		Mesh:            &mesh,
	}
	sidecarTemplate, err := GenerateTemplateFromParams(params)
	if err != nil {
		b.Fatalf("GenerateTemplateFromParams(%v) failed: %v", params, err)
	}
	wh, cleanup := createWebhook(b, sidecarTemplate)
	defer cleanup()

	stop := make(chan struct{})
	defer func() { close(stop) }()
	go wh.Run(stop)

	body := makeTestData(b, false)
	req := httptest.NewRequest("POST", "http://sidecar-injector/inject", bytes.NewReader(body))
	req.Header.Add("Content-Type", "application/json")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wh.serveInject(httptest.NewRecorder(), req)
	}
}
