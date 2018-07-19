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
	"crypto/tls"
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
	"time"

	"github.com/onsi/gomega"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"istio.io/istio/galley/pkg/crd/validation/testcerts"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/test"
	"istio.io/istio/pilot/test/mock"
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

func createTestWebhook(t testing.TB) (*Webhook, func()) {
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
		healthFile = filepath.Join(dir, "health-file.yaml")
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

	options := WebhookParameters{
		CertFile:            certFile,
		KeyFile:             keyFile,
		Port:                port,
		DomainSuffix:        testDomainSuffix,
		PilotDescriptor:     mock.Types,
		MixerValidator:      &fakeValidator{},
		HealthCheckFile:     healthFile,
		HealthCheckInterval: 10 * time.Millisecond,
	}
	wh, err := NewWebhook(options)
	if err != nil {
		cleanup()
		t.Fatalf("NewController() failed: %v", err)
	}

	return wh, cleanup
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

	wh, cancel := createTestWebhook(t)
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
			if c.allowed != got.Allowed {
				t.Fatalf("got %v want %v", got, c.allowed)
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
	wh, cancel := createTestWebhook(t)
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
	wh, cleanup := createTestWebhook(t)
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

var (
	rotatedKey = []byte(`-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA3Tr24CaBegyfkdDGWckqMHEWvpJBThjXlMz/FKcg1bgq57OD
oNHXN4dcyPCHWWEY3Eo3YG1es4pqTkvzK0+1JoY6/K88Lu1ePj5PeSFuWfPWi1BW
9oyWJW+AAzqqGkZmSo4z26N+E7N8ht5bTBMNVD3jqz9+MaqCTVmQ6dAgdFKH07wd
XWh6kKoh2g9bgBKB+qWrezmVRb31i93sM1pJos35cUmIbWgiSQYuSXEInitejcGZ
cBjqRy61SiZB7nbmMoew0G0aGXe20Wx+QiyJjbt9XNUm0IvjAJ1SSiPqfFQ4F+tx
K4q3xAwp1smyiMv57RNC2ny8YMntZYgQDDkhBQIDAQABAoIBAQDZHK396yw0WEEd
vFN8+CRUaBfXLPe0KkMgAFLxtNdPhy9sNsueP3HESC7x8MQUHmtkfd1837kJ4HRV
pMnfnpj8Vs17AIrCzycnVMVv7jQ7SUcrb8v4qJ4N3TA3exJHOQHYd1hDXF81/Hbg
cUYOEcCKBTby8BvrqBe6y4ShQiUnoaeeM5j9x32+QB652/9PMuZJ9xfwyoEBjoVA
cccp+u3oBX864ztaG9Gn0wbgRVeafsPfuAOUmShykohV1mVJddiA0wayxGi0TmoK
dwrltdToI7BmpmmTLc59O44JFGwO67aJQHsrHBjEnpWlxFDwbfZuf93FgdFUFFjr
tVx2dPF9AoGBAPkIaUYxMSW78MD9862eJlGS6F/SBfPLTve3l9M+UFnEsWapNz+V
anlupaBtlfRJxLDzjheMVZTv/TaFIrrMdN/xc/NoYj6lw5RV8PEfKPB0FjSAqSEl
iVOA5q4kuI1xEeV7xLE4uJUF3wdoHz9cSmjrXDVZXq/KsaInABlAl1xjAoGBAONr
bgFUlQ+fbsGNjeefrIXBSU93cDo5tkd4+lefrmUVTwCo5V/AvMMQi88gV/sz6qCJ
gR0hXOqdQyn++cuqfMTDj4lediDGFEPho+n4ToIvkA020NQ05cKxcmn/6Ei+P9pk
v+zoT9RiVnkBje2n/KU2d/PEL9Nl4gvvAgPLt8V3AoGAZ6JZdQ15n3Nj0FyecKz0
01Oogl+7fGYqGap8cztmYsUY8lkPFdXPNnOWV3njQoMEaIMiqagL4Wwx2uNyvXvi
U2N+1lelMt720h8loqJN/irBJt44BARD7s0gsm2zo6DfSrnD8+Bf6BxGYSWyg0Kb
8KepesYTQmK+o3VJdDjOBHMCgYAIxbwYkQqu75d2H9+5b49YGXyadCEAHfnKCACg
IKi5fXjurZUrfGPLonfCJZ0/M2F5j9RLK15KLobIt+0qzgjCDkkbI2mrGfjuJWYN
QGbG3s7Ps62age/a8r1XGWf8ZlpQMlK08MEjkCeFw2mWIUS9mrxFyuuNXAC8NRv+
yXztQQKBgQDWTFFQdeYfuiKHrNmgOmLVuk1WhAaDgsdK8RrnNZgJX9bd7n7bm7No
GheN946AYsFge4DX7o0UXXJ3h5hTFn/hSWASI54cO6WyWNEiaP5HRlZqK7Jfej7L
mz+dlU3j/BY19RLmYeg4jFV4W66CnkDqpneOJs5WdmFFoWnHn7gRBw==
-----END RSA PRIVATE KEY-----`)

	// ServerCert is a test cert for dynamic admission controller.
	rotatedCert = []byte(`-----BEGIN CERTIFICATE-----
MIIDATCCAemgAwIBAgIJAJwGb32Zn8sDMA0GCSqGSIb3DQEBCwUAMA4xDDAKBgNV
BAMMA19jYTAgFw0xODAzMTYxNzI0NDJaGA8yMjkxMTIzMDE3MjQ0MlowEjEQMA4G
A1UEAwwHX3NlcnZlcjCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAN06
9uAmgXoMn5HQxlnJKjBxFr6SQU4Y15TM/xSnINW4Kuezg6DR1zeHXMjwh1lhGNxK
N2BtXrOKak5L8ytPtSaGOvyvPC7tXj4+T3khblnz1otQVvaMliVvgAM6qhpGZkqO
M9ujfhOzfIbeW0wTDVQ946s/fjGqgk1ZkOnQIHRSh9O8HV1oepCqIdoPW4ASgfql
q3s5lUW99Yvd7DNaSaLN+XFJiG1oIkkGLklxCJ4rXo3BmXAY6kcutUomQe525jKH
sNBtGhl3ttFsfkIsiY27fVzVJtCL4wCdUkoj6nxUOBfrcSuKt8QMKdbJsojL+e0T
Qtp8vGDJ7WWIEAw5IQUCAwEAAaNcMFowCQYDVR0TBAIwADALBgNVHQ8EBAMCBeAw
HQYDVR0lBBYwFAYIKwYBBQUHAwIGCCsGAQUFBwMBMCEGA1UdEQQaMBiHBH8AAAGH
EAAAAAAAAAAAAAAAAAAAAAEwDQYJKoZIhvcNAQELBQADggEBACbBlWo/pY/OIJaW
RwkfSRVzEIWpHt5OF6p93xfyy4/zVwwhH1AQB7Euji8vOaVNOpMfGYLNH3KIRReC
CIvGEH4yZDbpiH2cOshqMCuV1CMRUTdl4mq6M0PtGm6b8OG3uIFTLIR973LBWOl5
wCR1yrefT1NHuIScGaBXUGAV4JAx37pfg84hDD73T2j1TDD3Lrmsb9WCP+L26TG6
ICN61cIhgz8wChQpF8/fFAI5Fjbjrz5C1Xw/EUHLf/TTn/7Yfp2BHsGm126Et+k+
+MLBzBfrHKwPaGqDvNHUDrI6c3GI0Qp7jW93FbL5ul8JQ+AowoMF2dIEbN9qQEVP
ZOQ5UvU=
-----END CERTIFICATE-----`)
)

func checkCert(t *testing.T, wh *Webhook, cert, key []byte) bool {
	t.Helper()
	actual, err := wh.getCert(nil)
	if err != nil {
		t.Fatalf("fail to get certificate from webhook: %s", err)
	}
	expected, err := tls.X509KeyPair(cert, key)
	if err != nil {
		t.Fatalf("fail to load test certs.")
	}
	return bytes.Equal(actual.Certificate[0], expected.Certificate[0])
}

func TestReloadCert(t *testing.T) {
	wh, cleanup := createTestWebhook(t)
	defer cleanup()
	stop := make(chan struct{})
	defer func() { close(stop) }()
	go wh.Run(stop)
	checkCert(t, wh, testcerts.ServerCert, testcerts.ServerKey)
	// Update cert/key files.
	if err := ioutil.WriteFile(wh.certFile, rotatedCert, 0644); err != nil { // nolint: vetshadow
		cleanup()
		t.Fatalf("WriteFile(%v) failed: %v", wh.certFile, err)
	}
	if err := ioutil.WriteFile(wh.keyFile, rotatedKey, 0644); err != nil { // nolint: vetshadow
		cleanup()
		t.Fatalf("WriteFile(%v) failed: %v", wh.keyFile, err)
	}
	g := gomega.NewGomegaWithT(t)
	g.Eventually(func() bool {
		return checkCert(t, wh, rotatedCert, rotatedKey)
	}, "10s", "100ms").Should(gomega.BeTrue())
}
