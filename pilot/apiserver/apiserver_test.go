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

	"istio.io/manager/model"
	"istio.io/manager/test/mock"
	test_util "istio.io/manager/test/util"
)

var (
	routeRuleKey = model.Key{Name: "name", Namespace: "namespace", Kind: "route-rule"}

	validRouteRuleJSON = []byte(`{"type":"route-rule","name":"name",` +
		`"spec":{"destination":"service.namespace.svc.cluster.local","precedence":1,` +
		`"route":[{"tags":{"version":"v1"},"weight":25}]}}`)
	validUpdatedRouteRuleJSON = []byte(`{"type":"route-rule","name":"name",` +
		`"spec":{"destination":"service.namespace.svc.cluster.local","precedence":1,` +
		`"route":[{"tags":{"version":"v2"},"weight":25}]}}`)
	validDiffNamespaceRouteRuleJSON = []byte(`{"type":"route-rule","name":"name",` +
		`"spec":{"destination":"service.differentnamespace.svc.cluster.local","precedence":1,` +
		`"route":[{"tags":{"version":"v3"},"weight":25}]}}`)

	errItemExists  = &model.ItemAlreadyExistsError{Key: routeRuleKey}
	errNotFound    = &model.ItemNotFoundError{Key: routeRuleKey}
	errInvalidBody = errors.New("invalid character 'J' looking for beginning of value")
	errInvalidSpec = errors.New("cannot parse proto message: json: " +
		"cannot unmarshal string into Go value of type map[string]json.RawMessage")
	errInvalidType = fmt.Errorf("unknown configuration type not-a-route-rule; use one of %v",
		model.IstioConfig.Kinds())
)

func makeAPIServer(r *model.IstioRegistry) *API {
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
		Registry: mock.MakeRegistry(),
	})
	go apiserver.Run()
}

func TestAddUpdateGetDeleteConfig(t *testing.T) {
	mockReg := mock.MakeRegistry()
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

	// Delete the route-rule
	status, _ = makeAPIRequest(api, "DELETE", url, nil, t)
	compareStatus(status, http.StatusOK, t)
	compareStoredConfig(mockReg, routeRuleKey, false, t)
}

func TestListConfig(t *testing.T) {

	mockReg := mock.MakeRegistry()
	api := makeAPIServer(mockReg)

	// Add in two configs
	_, _ = makeAPIRequest(api, "POST", "/test/config/route-rule/namespace/v1", validRouteRuleJSON, t)
	_, _ = makeAPIRequest(api, "POST", "/test/config/route-rule/namespace/v2", validUpdatedRouteRuleJSON, t)

	// List them for a namespace
	status, body := makeAPIRequest(api, "GET", "/test/config/route-rule/namespace", nil, t)
	compareStatus(status, http.StatusOK, t)
	compareListCount(body, 2, t)

	// Add in third
	_, _ = makeAPIRequest(api, "POST", "/test/config/route-rule/differentnamespace/v3", validDiffNamespaceRouteRuleJSON, t)

	// List for all namespaces
	status, body = makeAPIRequest(api, "GET", "/test/config/route-rule", nil, t)
	compareStatus(status, http.StatusOK, t)
	compareListCount(body, 3, t)

}

func TestConfigErrors(t *testing.T) {
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
		api := makeAPIServer(mock.MakeRegistry())
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

func compareStoredConfig(mockReg *model.IstioRegistry, key model.Key, present bool, t *testing.T) {
	_, ok := mockReg.Get(key)
	if !ok && present {
		t.Errorf("Expected config wasn't present in the registry for key: %+v", key)
	} else if ok && !present {
		t.Errorf("Unexpected config was present in the registry for key: %+v", key)
	}
	// To Do: compare protos
}
