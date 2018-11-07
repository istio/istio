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

package validation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ghodss/yaml"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	"k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	fcache "k8s.io/client-go/tools/cache/testing"

	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/test"
	"istio.io/istio/pilot/test/mock"
	"istio.io/istio/pkg/mcp/testing/testcerts"
)

const (
	// testDomainSuffix is the default DNS domain suffix for Istio
	// CRD resources.
	testDomainSuffix = "local.cluster"
)

type fakeValidator struct{ err error }

func (fv *fakeValidator) Validate(*store.BackendEvent) error {
	return fv.err
}

var (
	dummyConfig = &admissionregistrationv1beta1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "config1",
		},
		Webhooks: []admissionregistrationv1beta1.Webhook{
			{
				Name: "hook-foo",
				ClientConfig: admissionregistrationv1beta1.WebhookClientConfig{
					Service: &admissionregistrationv1beta1.ServiceReference{
						Name:      "hook1",
						Namespace: "default",
					},
					CABundle: testcerts.CACert,
				},
				Rules: []admissionregistrationv1beta1.RuleWithOperations{
					{
						Operations: []admissionregistrationv1beta1.OperationType{
							admissionregistrationv1beta1.Create,
							admissionregistrationv1beta1.Update,
						},
						Rule: admissionregistrationv1beta1.Rule{
							APIGroups:   []string{"g1"},
							APIVersions: []string{"v1"},
							Resources:   []string{"r1"},
						},
					},
				},
				FailurePolicy:     failurePolicyFail,
				NamespaceSelector: &metav1.LabelSelector{},
			},
		},
	}

	dummyDeployment = &extensionsv1beta1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "istio-galley",
			Namespace: "istio-system",
			UID:       "deadbeef",
		},
	}

	dummyClient = fake.NewSimpleClientset(dummyDeployment)

	createFakeWebhookSource   = fcache.NewFakeControllerSource
	createFakeEndpointsSource = func() cache.ListerWatcher {
		source := fcache.NewFakeControllerSource()
		source.Add(&v1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dummyDeployment.Name,
				Namespace: dummyDeployment.Namespace,
			},
			Subsets: []v1.EndpointSubset{{
				Addresses: []v1.EndpointAddress{{
					IP: "1.2.3.4",
				}},
			}},
		})
		return source
	}
)

func createTestWebhook(
	t testing.TB,
	cl clientset.Interface,
	fakeWebhookSource, fakeEndpointSource cache.ListerWatcher,
	config *admissionregistrationv1beta1.ValidatingWebhookConfiguration) (*Webhook, func()) {

	t.Helper()
	dir, err := ioutil.TempDir("", "galley_validation_webhook")
	if err != nil {
		t.Fatalf("TempDir() failed: %v", err)
	}
	cleanup := func() {
		os.RemoveAll(dir) // nolint: errcheck
	}

	var (
		certFile   = filepath.Join(dir, "cert-file.yaml")
		keyFile    = filepath.Join(dir, "key-file.yaml")
		caFile     = filepath.Join(dir, "ca-file.yaml")
		configFile = filepath.Join(dir, "config-file.yaml")
		port       = uint(0)
	)

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
	// ca
	if err := ioutil.WriteFile(caFile, testcerts.CACert, 0644); err != nil { // nolint: vetshadow
		cleanup()
		t.Fatalf("WriteFile(%v) failed: %v", caFile, err)
	}

	configBytes, err := yaml.Marshal(&config)
	if err != nil {
		cleanup()
		t.Fatalf("could not create fake webhook configuration data: %v", err)
	}
	if err := ioutil.WriteFile(configFile, configBytes, 0644); err != nil { // nolint: vetshadow
		cleanup()
		t.Fatalf("WriteFile(%v) failed: %v", configFile, err)
	}

	options := WebhookParameters{
		CertFile:                      certFile,
		KeyFile:                       keyFile,
		Port:                          port,
		DomainSuffix:                  testDomainSuffix,
		PilotDescriptor:               mock.Types,
		MixerValidator:                &fakeValidator{},
		WebhookConfigFile:             configFile,
		CACertFile:                    caFile,
		Clientset:                     cl,
		DeploymentName:                dummyDeployment.Name,
		ServiceName:                   dummyDeployment.Name,
		DeploymentAndServiceNamespace: dummyDeployment.Namespace,
	}
	wh, err := NewWebhook(options)
	if err != nil {
		cleanup()
		t.Fatalf("NewWebhook() failed: %v", err)
	}

	wh.createInformerWebhookSource = func(cl clientset.Interface, name string) cache.ListerWatcher {
		return fakeWebhookSource
	}
	wh.createInformerEndpointSource = func(cl clientset.Interface, namespace, name string) cache.ListerWatcher {
		return fakeEndpointSource
	}

	return wh, func() {
		cleanup()
		wh.stop()
	}
}

func makePilotConfig(t *testing.T, i int, validKind, validConfig bool) []byte {
	t.Helper()

	var key string
	if validConfig {
		key = "key"
	}

	name := fmt.Sprintf("%s%d", "mock-config", i)
	config := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type: model.MockConfig.Type,
			Name: name,
			Labels: map[string]string{
				"key": name,
			},
			Annotations: map[string]string{
				"annotationkey": name,
			},
		},
		Spec: &test.MockConfig{
			Key: key,
			Pairs: []*test.ConfigPair{{
				Key:   key,
				Value: strconv.Itoa(i),
			}},
		},
	}
	obj, err := crd.ConvertConfig(model.MockConfig, config)
	if err != nil {
		t.Fatalf("ConvertConfig(%v) failed: %v", config.Name, err)
	}
	raw, err := json.Marshal(&obj)
	if err != nil {
		t.Fatalf("Marshal(%v) failed: %v", config.Name, err)
	}
	return raw
}

func TestAdmitPilot(t *testing.T) {
	valid := makePilotConfig(t, 0, true, true)
	invalidConfig := makePilotConfig(t, 0, true, false)

	wh, cancel := createTestWebhook(t, dummyClient, createFakeWebhookSource(), createFakeEndpointsSource(), dummyConfig)
	defer cancel()

	cases := []struct {
		name    string
		admit   admitFunc
		in      *admissionv1beta1.AdmissionRequest
		allowed bool
	}{
		{
			name:  "valid create",
			admit: wh.admitPilot,
			in: &admissionv1beta1.AdmissionRequest{
				Kind:      metav1.GroupVersionKind{}, // TODO
				Object:    runtime.RawExtension{Raw: valid},
				Operation: admissionv1beta1.Create,
			},
			allowed: true,
		},
		{
			name:  "valid update",
			admit: wh.admitPilot,
			in: &admissionv1beta1.AdmissionRequest{
				Kind:      metav1.GroupVersionKind{}, // TODO
				Object:    runtime.RawExtension{Raw: valid},
				Operation: admissionv1beta1.Create,
			},
			allowed: true,
		},
		{
			name:  "unsupported operation",
			admit: wh.admitPilot,
			in: &admissionv1beta1.AdmissionRequest{
				Kind:      metav1.GroupVersionKind{}, // TODO
				Object:    runtime.RawExtension{Raw: valid},
				Operation: admissionv1beta1.Delete,
			},
			allowed: true,
		},
		{
			name:  "invalid spec",
			admit: wh.admitPilot,
			in: &admissionv1beta1.AdmissionRequest{
				Kind:      metav1.GroupVersionKind{}, // TODO
				Object:    runtime.RawExtension{Raw: invalidConfig},
				Operation: admissionv1beta1.Create,
			},
			allowed: false,
		},
		{
			name:  "corrupt object",
			admit: wh.admitPilot,
			in: &admissionv1beta1.AdmissionRequest{
				Kind:      metav1.GroupVersionKind{}, // TODO
				Object:    runtime.RawExtension{Raw: append([]byte("---"), valid...)},
				Operation: admissionv1beta1.Create,
			},
			allowed: false,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("[%d] %s", i, c.name), func(t *testing.T) {
			got := wh.admitPilot(c.in)
			if got.Allowed != c.allowed {
				t.Fatalf("got %v want %v", got.Allowed, c.allowed)
			}
		})
	}
}

func makeMixerConfig(t *testing.T, i int) []byte {
	t.Helper()
	uns := &unstructured.Unstructured{}
	name := fmt.Sprintf("%s%d", "mock-config", i)
	uns.SetName(name)
	uns.SetKind("mock")
	uns.Object["spec"] = map[string]interface{}{"foo": 1}
	raw, err := json.Marshal(uns)
	if err != nil {
		t.Fatalf("Marshal(%v) failed: %v", uns, err)
	}
	return raw
}

func TestAdmitMixer(t *testing.T) {
	rawConfig := makeMixerConfig(t, 0)
	wh, cancel := createTestWebhook(
		t,
		fake.NewSimpleClientset(),
		createFakeWebhookSource(),
		createFakeEndpointsSource(),
		dummyConfig)
	defer cancel()

	cases := []struct {
		name      string
		in        *admissionv1beta1.AdmissionRequest
		allowed   bool
		validator store.BackendValidator
	}{
		{
			name: "valid create",
			in: &admissionv1beta1.AdmissionRequest{
				Kind:      metav1.GroupVersionKind{Kind: "mock"},
				Name:      "valid-create",
				Object:    runtime.RawExtension{Raw: rawConfig},
				Operation: admissionv1beta1.Create,
			},
			validator: &fakeValidator{},
			allowed:   true,
		},
		{
			name: "valid update",
			in: &admissionv1beta1.AdmissionRequest{
				Kind:      metav1.GroupVersionKind{Kind: "mock"},
				Name:      "valid-update",
				Object:    runtime.RawExtension{Raw: rawConfig},
				Operation: admissionv1beta1.Update,
			},
			validator: &fakeValidator{},
			allowed:   true,
		},
		{
			name: "valid delete",
			in: &admissionv1beta1.AdmissionRequest{
				Kind:      metav1.GroupVersionKind{Kind: "mock"},
				Name:      "valid-delete",
				Object:    runtime.RawExtension{Raw: rawConfig},
				Operation: admissionv1beta1.Delete,
			},
			validator: &fakeValidator{},
			allowed:   true,
		},
		{
			name: "invalid update",
			in: &admissionv1beta1.AdmissionRequest{
				Kind:      metav1.GroupVersionKind{Kind: "mock"},
				Name:      "invalid-update",
				Object:    runtime.RawExtension{Raw: rawConfig},
				Operation: admissionv1beta1.Update,
			},
			validator: &fakeValidator{errors.New("fail")},
			allowed:   false,
		},
		{
			name: "invalid delete",
			in: &admissionv1beta1.AdmissionRequest{
				Kind:      metav1.GroupVersionKind{Kind: "mock"},
				Name:      "to-be-deleted",
				Object:    runtime.RawExtension{Raw: rawConfig},
				Operation: admissionv1beta1.Delete,
			},
			validator: &fakeValidator{errors.New("fail")},
			allowed:   false,
		},
		{
			name: "invalid delete (missing name)",
			in: &admissionv1beta1.AdmissionRequest{
				Kind:      metav1.GroupVersionKind{Kind: "mock"},
				Object:    runtime.RawExtension{Raw: rawConfig},
				Operation: admissionv1beta1.Delete,
			},
			validator: &fakeValidator{errors.New("fail")},
			allowed:   false,
		},
		{
			name: "invalid create",
			in: &admissionv1beta1.AdmissionRequest{
				Kind:      metav1.GroupVersionKind{Kind: "mock"},
				Name:      "invalid create",
				Object:    runtime.RawExtension{Raw: rawConfig},
				Operation: admissionv1beta1.Create,
			},
			validator: &fakeValidator{errors.New("fail")},
			allowed:   false,
		},
		{
			name: "invalid operation",
			in: &admissionv1beta1.AdmissionRequest{
				Kind:      metav1.GroupVersionKind{Kind: "mock"},
				Name:      "invalid operation",
				Object:    runtime.RawExtension{Raw: rawConfig},
				Operation: admissionv1beta1.Connect,
			},
			validator: &fakeValidator{},
			allowed:   true,
		},
		{
			name: "invalid object",
			in: &admissionv1beta1.AdmissionRequest{
				Kind:      metav1.GroupVersionKind{Kind: "mock"},
				Name:      "invalid object",
				Object:    runtime.RawExtension{Raw: append([]byte("---"), rawConfig...)},
				Operation: admissionv1beta1.Create,
			},
			validator: &fakeValidator{},
			allowed:   false,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("[%d] %s", i, c.name), func(t *testing.T) {
			wh.validator = c.validator // override mixer backend validator
			got := wh.admitMixer(c.in)
			if c.allowed != got.Allowed {
				t.Fatalf("got %v want %v", got, c.allowed)
			}
		})
	}
}

func makeTestReview(t *testing.T, valid bool) []byte {
	t.Helper()
	review := admissionv1beta1.AdmissionReview{
		Request: &admissionv1beta1.AdmissionRequest{
			Kind: metav1.GroupVersionKind{},
			Object: runtime.RawExtension{
				Raw: makePilotConfig(t, 0, true, valid),
			},
			Operation: admissionv1beta1.Create,
		},
	}
	reviewJSON, err := json.Marshal(review)
	if err != nil {
		t.Fatalf("Failed to create AdmissionReview: %v", err)
	}
	return reviewJSON
}

func TestServe(t *testing.T) {
	wh, cleanup := createTestWebhook(t,
		fake.NewSimpleClientset(),
		createFakeWebhookSource(),
		createFakeEndpointsSource(),
		dummyConfig)
	defer cleanup()
	stop := make(chan struct{})
	defer func() { close(stop) }()
	go wh.Run(stop)

	validReview := makeTestReview(t, true)
	invalidReview := makeTestReview(t, false)

	cases := []struct {
		name            string
		body            []byte
		contentType     string
		wantAllowed     bool
		wantStatusCode  int
		allowedResponse bool //
	}{
		{
			name:            "valid",
			body:            validReview,
			contentType:     "application/json",
			wantAllowed:     true,
			wantStatusCode:  http.StatusOK,
			allowedResponse: true,
		},
		{
			name:           "invalid",
			body:           invalidReview,
			contentType:    "application/json",
			wantAllowed:    false,
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
			name:           "no content",
			body:           []byte{},
			contentType:    "application/json",
			wantAllowed:    false,
			wantStatusCode: http.StatusBadRequest,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("[%d] %s", i, c.name), func(t *testing.T) {
			req := httptest.NewRequest("POST", "http://validator", bytes.NewReader(c.body))
			req.Header.Add("Content-Type", c.contentType)
			w := httptest.NewRecorder()

			serve(w, req, func(*admissionv1beta1.AdmissionRequest) *admissionv1beta1.AdmissionResponse {
				return &admissionv1beta1.AdmissionResponse{Allowed: c.allowedResponse}
			})

			res := w.Result()

			if res.StatusCode != c.wantStatusCode {
				t.Fatalf("%v: wrong status code: \ngot %v \nwant %v", c.name, res.StatusCode, c.wantStatusCode)
			}

			if res.StatusCode != http.StatusOK {
				return
			}

			gotBody, err := ioutil.ReadAll(res.Body)
			if err != nil {
				t.Fatalf("%v: could not read body: %v", c.name, err)
			}
			var gotReview admissionv1beta1.AdmissionReview
			if err := json.Unmarshal(gotBody, &gotReview); err != nil {
				t.Fatalf("%v: could not decode response body: %v", c.name, err)
			}
			if gotReview.Response.Allowed != c.wantAllowed {
				t.Fatalf("%v: AdmissionReview.Response.Allowed is wrong : got %v want %v",
					c.name, gotReview.Response.Allowed, c.wantAllowed)
			}
		})
	}
}
