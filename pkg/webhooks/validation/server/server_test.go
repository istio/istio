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

package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	kubeApiAdmission "k8s.io/api/admission/v1beta1"
	kubeApisMeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/config"
	"istio.io/istio/pkg/testcerts"
)

const (
	// testDomainSuffix is the default DNS domain suffix for Istio
	// CRD resources.
	testDomainSuffix = "local.cluster"
)

func TestArgs_String(t *testing.T) {
	p := DefaultArgs()
	// Should not crash
	_ = p.String()
}

func createTestWebhook(t testing.TB) *Webhook {
	t.Helper()
	dir := t.TempDir()

	var (
		certFile = filepath.Join(dir, "cert-file.yaml")
		keyFile  = filepath.Join(dir, "key-file.yaml")
		port     = uint(0)
	)

	// cert
	if err := os.WriteFile(certFile, testcerts.ServerCert, 0o644); err != nil { // nolint: vetshadow
		t.Fatalf("WriteFile(%v) failed: %v", certFile, err)
	}
	// key
	if err := os.WriteFile(keyFile, testcerts.ServerKey, 0o644); err != nil { // nolint: vetshadow
		t.Fatalf("WriteFile(%v) failed: %v", keyFile, err)
	}

	options := Options{
		Port:         port,
		DomainSuffix: testDomainSuffix,
		Schemas:      collections.Mocks,
		Mux:          http.NewServeMux(),
	}
	wh, err := New(options)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	return wh
}

func makePilotConfig(t *testing.T, i int, validConfig bool, includeBogusKey bool) []byte { // nolint: unparam
	t.Helper()

	var key string
	if validConfig {
		key = "key"
	}

	name := fmt.Sprintf("%s%d", "mock-config", i)

	r := collections.Mock.Resource()
	var un unstructured.Unstructured
	un.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   r.Group(),
		Version: r.Version(),
		Kind:    r.Kind(),
	})
	un.SetName(name)
	un.SetLabels(map[string]string{"key": name})
	un.SetAnnotations(map[string]string{"annotationKey": name})
	un.Object["spec"] = &config.MockConfig{
		Key: key,
		Pairs: []*config.ConfigPair{{
			Key:   key,
			Value: strconv.Itoa(i),
		}},
	}
	raw, err := json.Marshal(&un)
	if err != nil {
		t.Fatalf("Marshal(%v) failed: %v", name, err)
	}
	if includeBogusKey {
		trial := make(map[string]any)
		if err := json.Unmarshal(raw, &trial); err != nil {
			t.Fatalf("Unmarshal(%v) failed: %v", name, err)
		}
		trial["unexpected_key"] = "any value"
		if raw, err = json.Marshal(&trial); err != nil {
			t.Fatalf("re-Marshal(%v) failed: %v", name, err)
		}
	}
	return raw
}

func TestAdmitPilot(t *testing.T) {
	valid := makePilotConfig(t, 0, true, false)
	invalidConfig := makePilotConfig(t, 0, false, false)
	extraKeyConfig := makePilotConfig(t, 0, true, true)

	wh := createTestWebhook(t)

	cases := []struct {
		name    string
		admit   admitFunc
		in      *kube.AdmissionRequest
		allowed bool
	}{
		{
			name:  "valid create",
			admit: wh.validate,
			in: &kube.AdmissionRequest{
				Kind:      kubeApisMeta.GroupVersionKind{Kind: collections.Mock.Resource().Kind()},
				Object:    runtime.RawExtension{Raw: valid},
				Operation: kube.Create,
			},
			allowed: true,
		},
		{
			name:  "valid update",
			admit: wh.validate,
			in: &kube.AdmissionRequest{
				Kind:      kubeApisMeta.GroupVersionKind{Kind: collections.Mock.Resource().Kind()},
				Object:    runtime.RawExtension{Raw: valid},
				Operation: kube.Update,
			},
			allowed: true,
		},
		{
			name:  "unsupported operation",
			admit: wh.validate,
			in: &kube.AdmissionRequest{
				Kind:      kubeApisMeta.GroupVersionKind{Kind: collections.Mock.Resource().Kind()},
				Object:    runtime.RawExtension{Raw: valid},
				Operation: kube.Delete,
			},
			allowed: true,
		},
		{
			name:  "invalid spec",
			admit: wh.validate,
			in: &kube.AdmissionRequest{
				Kind:      kubeApisMeta.GroupVersionKind{Kind: collections.Mock.Resource().Kind()},
				Object:    runtime.RawExtension{Raw: invalidConfig},
				Operation: kube.Create,
			},
			allowed: false,
		},
		{
			name:  "corrupt object",
			admit: wh.validate,
			in: &kube.AdmissionRequest{
				Kind:      kubeApisMeta.GroupVersionKind{Kind: collections.Mock.Resource().Kind()},
				Object:    runtime.RawExtension{Raw: append([]byte("---"), valid...)},
				Operation: kube.Create,
			},
			allowed: false,
		},
		{
			name:  "invalid extra key create",
			admit: wh.validate,
			in: &kube.AdmissionRequest{
				Kind:      kubeApisMeta.GroupVersionKind{Kind: collections.Mock.Resource().Kind()},
				Object:    runtime.RawExtension{Raw: extraKeyConfig},
				Operation: kube.Create,
			},
			allowed: false,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("[%d] %s", i, c.name), func(t *testing.T) {
			got := wh.validate(c.in)
			if got.Allowed != c.allowed {
				t.Fatalf("got %v want %v", got.Allowed, c.allowed)
			}
		})
	}
}

func makeTestReview(t *testing.T, valid bool, apiVersion string) []byte {
	t.Helper()
	review := kubeApiAdmission.AdmissionReview{
		TypeMeta: kubeApisMeta.TypeMeta{
			Kind:       "AdmissionReview",
			APIVersion: fmt.Sprintf("admission.k8s.io/%s", apiVersion),
		},
		Request: &kubeApiAdmission.AdmissionRequest{
			Kind: kubeApisMeta.GroupVersionKind{
				Group:   kubeApiAdmission.GroupName,
				Version: apiVersion,
				Kind:    "AdmissionRequest",
			},
			Object: runtime.RawExtension{
				Raw: makePilotConfig(t, 0, valid, false),
			},
			Operation: kubeApiAdmission.Create,
		},
	}
	reviewJSON, err := json.Marshal(review)
	if err != nil {
		t.Fatalf("Failed to create AdmissionReview: %v", err)
	}
	return reviewJSON
}

func TestServe(t *testing.T) {
	_ = createTestWebhook(t)
	stop := make(chan struct{})
	defer func() {
		close(stop)
	}()

	validReview := makeTestReview(t, true, "v1beta1")
	validReviewV1 := makeTestReview(t, true, "v1")
	invalidReview := makeTestReview(t, false, "v1beta1")

	cases := []struct {
		name            string
		body            []byte
		contentType     string
		wantStatusCode  int
		wantAllowed     bool
		allowedResponse bool
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
			name:            "valid(v1 version)",
			body:            validReviewV1,
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

			serve(w, req, func(*kube.AdmissionRequest) *kube.AdmissionResponse {
				return &kube.AdmissionResponse{Allowed: c.allowedResponse}
			})

			res := w.Result()

			if res.StatusCode != c.wantStatusCode {
				t.Fatalf("%v: wrong status code: \ngot %v \nwant %v", c.name, res.StatusCode, c.wantStatusCode)
			}

			if res.StatusCode != http.StatusOK {
				return
			}

			gotBody, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatalf("%v: could not read body: %v", c.name, err)
			}
			var gotReview kubeApiAdmission.AdmissionReview
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

// scenario is a common struct used by many tests in this context.
type scenario struct {
	wrapFunc      func(*Options)
	expectedError string
}

func TestValidate(t *testing.T) {
	scenarios := map[string]scenario{
		"valid": {
			wrapFunc:      func(args *Options) {},
			expectedError: "",
		},
		"invalid port": {
			wrapFunc:      func(args *Options) { args.Port = 100000 },
			expectedError: "port number 100000 must be in the range 1..65535",
		},
	}

	for name, scenario := range scenarios {
		t.Run(name, func(tt *testing.T) {
			runTestCode(name, tt, scenario)
		})
	}
}

func runTestCode(name string, t *testing.T, test scenario) {
	args := DefaultArgs()

	test.wrapFunc(&args)
	err := args.Validate()
	if err == nil && test.expectedError != "" {
		t.Errorf("Test %q failed: expected error: %q, got nil", name, test.expectedError)
	}
	if err != nil {
		if test.expectedError == "" {
			t.Errorf("Test %q failed: expected nil error, got %v", name, err)
		}
		if !strings.Contains(err.Error(), test.expectedError) {
			t.Errorf("Test %q failed: expected error: %q, got %q", name, test.expectedError, err.Error())
		}
	}
}
