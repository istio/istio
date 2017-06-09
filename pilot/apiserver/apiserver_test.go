// Copyright 2017 Istio Authors
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

package apiserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	restful "github.com/emicklei/go-restful"

	"istio.io/pilot/adapter/config/memory"
	"istio.io/pilot/model"
	test_util "istio.io/pilot/test/util"
)

var (
	routeRuleKey = key{Name: "name", Namespace: "namespace", Kind: "route-rule"}

	validRouteRuleJSON = []byte(`{"type":"route-rule","name":"name",` +
		`"spec":{"destination":"service.namespace.svc.cluster.local","precedence":1,` +
		`"route":[{"tags":{"version":"v1"},"weight":25}]}}`)
	validUpdatedRouteRuleJSON = []byte(`{"type":"route-rule","name":"name",` +
		`"spec":{"destination":"service.namespace.svc.cluster.local","precedence":1,` +
		`"route":[{"tags":{"version":"v2"},"weight":25}]}}`)
	validDiffNamespaceRouteRuleJSON = []byte(`{"type":"route-rule","name":"name",` +
		`"spec":{"destination":"service.differentnamespace.svc.cluster.local","precedence":1,` +
		`"route":[{"tags":{"version":"v3"},"weight":25}]}}`)

	errItemExists  = &model.ItemAlreadyExistsError{Key: routeRuleKey.Name}
	errNotFound    = &model.ItemNotFoundError{Key: routeRuleKey.Name}
	errInvalidBody = errors.New("invalid character 'J' looking for beginning of value")
	errInvalidSpec = errors.New("cannot parse proto message: json: " +
		"cannot unmarshal string into Go value of type map[string]json.RawMessage")
	errInvalidType = fmt.Errorf("unknown configuration type not-a-route-rule; use one of %v",
		model.IstioConfigTypes.Types())
)

func makeAPIServer(r model.ConfigStore) *API {
	return &API{
		version:  "test",
		registry: r,
	}
}

func makeAPIRequest(api *API, method, url string, data []byte, t *testing.T) (int, []byte) {
	httpRequest, err := http.NewRequest(method, url, bytes.NewBuffer(data))
	httpRequest.Header.Set("Content-Type", "application/json")
	if err != nil {
		t.Fatal(err)
	}
	httpWriter := httptest.NewRecorder()
	container := restful.NewContainer()
	api.Register(container)
	container.ServeHTTP(httpWriter, httpRequest)
	result := httpWriter.Result()
	body, err := ioutil.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	return result.StatusCode, body
}

func TestNewAPIThenRun(t *testing.T) {
	apiserver := NewAPI(APIServiceOptions{
		Version:  "v1alpha1",
		Port:     8081,
		Registry: memory.Make(model.IstioConfigTypes),
	})
	go apiserver.Run()
}

func TestHealthcheckt(t *testing.T) {
	api := makeAPIServer(nil)
	url := "/test/health"
	status, _ := makeAPIRequest(api, "GET", url, nil, t)
	compareStatus(status, http.StatusOK, t)
}

func TestAddUpdateGetDeleteConfig(t *testing.T) {
	// TODO: disable temporarily
	t.Skip()
	mockReg := memory.Make(model.IstioConfigTypes)
	api := makeAPIServer(mockReg)
	url := "/test/config/route-rule/namespace/name"

	// Add the route-rule
	status, body := makeAPIRequest(api, "POST", url, validRouteRuleJSON, t)
	compareStatus(status, http.StatusCreated, t)
	test_util.CompareContent(body, "testdata/route-rule.json.golden", t)
	compareStoredConfig(mockReg, routeRuleKey, true, t)

	// Update the route-rule
	status, body = makeAPIRequest(api, "PUT", url, validUpdatedRouteRuleJSON, t)
	compareStatus(status, http.StatusOK, t)
	test_util.CompareContent(body, "testdata/route-rule-v2.json.golden", t)
	compareStoredConfig(mockReg, routeRuleKey, true, t)

	// Get the route-rule
	status, body = makeAPIRequest(api, "GET", url, nil, t)
	compareStatus(status, http.StatusOK, t)
	test_util.CompareContent(body, "testdata/route-rule-v2.json.golden", t)
	makeAPIRequestWriteFails(api, "GET", url, nil, t)

	// Delete the route-rule
	status, _ = makeAPIRequest(api, "DELETE", url, nil, t)
	compareStatus(status, http.StatusOK, t)
	compareStoredConfig(mockReg, routeRuleKey, false, t)
}

func TestListConfig(t *testing.T) {
	// TODO: disable temporarily
	t.Skip()

	mockReg := memory.Make(model.IstioConfigTypes)
	api := makeAPIServer(mockReg)

	// Add in two configs
	_, _ = makeAPIRequest(api, "POST", "/test/config/route-rule/namespace/v1", validRouteRuleJSON, t)
	_, _ = makeAPIRequest(api, "POST", "/test/config/route-rule/namespace/v2", validUpdatedRouteRuleJSON, t)

	// List them for a namespace
	status, body := makeAPIRequest(api, "GET", "/test/config/route-rule/namespace", nil, t)
	compareStatus(status, http.StatusOK, t)
	compareListCount(body, 2, t)
	makeAPIRequestWriteFails(api, "GET", "/test/config/route-rule/namespace", nil, t)

	// Add in third
	_, _ = makeAPIRequest(api, "POST", "/test/config/route-rule/differentnamespace/v3", validDiffNamespaceRouteRuleJSON, t)

	// List for all namespaces
	status, body = makeAPIRequest(api, "GET", "/test/config/route-rule", nil, t)
	compareStatus(status, http.StatusOK, t)
	compareListCount(body, 3, t)
	makeAPIRequestWriteFails(api, "GET", "/test/config/route-rule", nil, t)
}

func TestConfigErrors(t *testing.T) {
	// TODO: disable temporarily
	t.Skip()
	cases := []struct {
		name       string
		url        string
		method     string
		data       []byte
		wantStatus int
		wantBody   string
		duplicate  bool
	}{
		{
			name:       "TestNotFoundGetConfig",
			url:        "/test/config/route-rule/namespace/name",
			method:     "GET",
			wantStatus: http.StatusNotFound,
			wantBody:   errNotFound.Error(),
		},
		{
			name:       "TestInvalidConfigTypeGetConfig",
			url:        "/test/config/not-a-route-rule/namespace/name",
			method:     "GET",
			wantStatus: http.StatusBadRequest,
			wantBody:   errInvalidType.Error(),
		},
		{
			name:       "TestMultipleAddConfigsReturnConflict",
			url:        "/test/config/route-rule/namespace/name",
			method:     "POST",
			data:       validRouteRuleJSON,
			wantStatus: http.StatusConflict,
			wantBody:   errItemExists.Error(),
			duplicate:  true,
		},
		{
			name:       "TestInvalidConfigTypeAddConfig",
			url:        "/test/config/not-a-route-rule/namespace/name",
			method:     "POST",
			data:       validRouteRuleJSON,
			wantStatus: http.StatusBadRequest,
			wantBody:   errInvalidType.Error(),
		},
		{
			name:       "TestInvalidBodyAddConfig",
			url:        "/test/config/route-rule/namespace/name",
			method:     "POST",
			data:       []byte("JUSTASTRING"),
			wantStatus: http.StatusBadRequest,
			wantBody:   errInvalidBody.Error(),
		},
		{
			name:       "TestInvalidSpecAddConfig",
			url:        "/test/config/route-rule/namespace/name",
			method:     "POST",
			data:       []byte(`{"type":"route-rule","name":"name","spec":"NOTASPEC"}`),
			wantStatus: http.StatusBadRequest,
			wantBody:   errInvalidSpec.Error(),
		},
		{
			name:       "TestNotFoundConfigUpdateConfig",
			url:        "/test/config/route-rule/namespace/name",
			method:     "PUT",
			data:       validRouteRuleJSON,
			wantStatus: http.StatusNotFound,
			wantBody:   errNotFound.Error(),
		},
		{
			name:       "TestInvalidConfigTypeUpdateConfig",
			url:        "/test/config/not-a-route-rule/namespace/name",
			method:     "PUT",
			data:       validRouteRuleJSON,
			wantStatus: http.StatusBadRequest,
			wantBody:   errInvalidType.Error(),
		},
		{
			name:       "TestInvalidBodyUpdateConfig",
			url:        "/test/config/route-rule/namespace/name",
			method:     "PUT",
			data:       []byte("JUSTASTRING"),
			wantStatus: http.StatusBadRequest,
			wantBody:   errInvalidBody.Error(),
		},
		{
			name:       "TestInvalidSpecUpdateConfig",
			url:        "/test/config/route-rule/namespace/name",
			method:     "PUT",
			data:       []byte(`{"type":"route-rule","name":"name","spec":"NOTASPEC"}`),
			wantStatus: http.StatusBadRequest,
			wantBody:   errInvalidSpec.Error(),
		},
		{
			name:       "TestNotFoundDeleteConfig",
			url:        "/test/config/route-rule/namespace/name",
			method:     "DELETE",
			wantStatus: http.StatusNotFound,
			wantBody:   errNotFound.Error(),
		},
		{
			name:       "TestInvalidConfigTypeDeleteConfig",
			url:        "/test/config/not-a-route-rule/namespace/name",
			method:     "DELETE",
			wantStatus: http.StatusBadRequest,
			wantBody:   errInvalidType.Error(),
		},
		{
			name:       "TestInvalidConfigTypeWithNamespaceListConfig",
			url:        "/test/config/not-a-route-rule/namespace",
			method:     "GET",
			wantStatus: http.StatusBadRequest,
			wantBody:   errInvalidType.Error(),
		},
		{
			name:       "TestInvalidConfigTypeWithoutNamespaceListConfig",
			url:        "/test/config/not-a-route-rule",
			method:     "GET",
			wantStatus: http.StatusBadRequest,
			wantBody:   errInvalidType.Error(),
		},
	}

	for _, c := range cases {
		api := makeAPIServer(memory.Make(model.IstioConfigTypes))
		if c.duplicate {
			makeAPIRequest(api, c.method, c.url, c.data, t)
		}
		gotStatus, gotBody := makeAPIRequest(api, c.method, c.url, c.data, t)
		if gotStatus != c.wantStatus {
			t.Errorf("%s: got status code %v, want %v", c.name, gotStatus, c.wantStatus)
		}
		if string(gotBody) != c.wantBody {
			t.Errorf("%s: got body %q, want %q", c.name, string(gotBody), c.wantBody)
		}
	}
}

// TestVersion verifies that the server responds to /version
func TestVersion(t *testing.T) {
	api := makeAPIServer(nil)

	status, body := makeAPIRequest(api, "GET", "/test/version", nil, t)
	compareStatus(status, http.StatusOK, t)
	compareObjectHasKeys(body, []string{
		"version", "revision", "branch", "golang_version"}, t)

	// Test write failure (boost code coverage)
	makeAPIRequestWriteFails(api, "GET", "/test/version", nil, t)
}

// An http.ResponseWriter that always fails.
// (For testing handler method write failure handling.)
type grouchyWriter struct{}

func (gr grouchyWriter) Header() http.Header {
	return http.Header(make(map[string][]string))
}

func (gr grouchyWriter) Write([]byte) (int, error) {
	return 0, fmt.Errorf("Write() failed")
}

func (gr grouchyWriter) WriteHeader(int) {
}

// makeAPIRequestWriteFails invokes a handler, but any writes the handler does fail.
func makeAPIRequestWriteFails(api *API, method, url string, data []byte, t *testing.T) {
	httpRequest, err := http.NewRequest(method, url, bytes.NewBuffer(data))
	httpRequest.Header.Set("Content-Type", "application/json")
	if err != nil {
		t.Fatal(err)
	}
	httpWriter := grouchyWriter{}
	container := restful.NewContainer()
	api.Register(container)
	container.ServeHTTP(httpWriter, httpRequest)
}

func compareObjectHasKeys(body []byte, expectedKeys []string, t *testing.T) {
	version := make(map[string]interface{})
	if err := json.Unmarshal(body, &version); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, expectedKey := range expectedKeys {
		if _, ok := version[expectedKey]; !ok {
			t.Errorf("/version did not include %q: %v", expectedKey, string(body))
		}
	}
}

func compareListCount(body []byte, expected int, t *testing.T) {
	configSlice := []Config{}
	if err := json.Unmarshal(body, &configSlice); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(configSlice) != expected {
		t.Errorf("expected %v elements back but got %v", expected, len(configSlice))
	}
}

func compareStatus(received, expected int, t *testing.T) {
	if received != expected {
		t.Errorf("Expected status code: %d, received: %d", expected, received)
	}
}

func compareStoredConfig(mockReg model.ConfigStore, key key, present bool, t *testing.T) {
	// TODO: deprecated, please update
	_, ok, _ := mockReg.Get(key.Kind, key.Name)
	if !ok && present {
		t.Errorf("Expected config wasn't present in the registry for key: %+v", key)
	} else if ok && !present {
		t.Errorf("Unexpected config was present in the registry for key: %+v", key)
	}
	// To Do: compare protos
}
