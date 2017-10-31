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

package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	rpc "github.com/googleapis/googleapis/google/rpc"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/config/descriptor"
	pb "istio.io/istio/mixer/pkg/config/proto"
	"istio.io/istio/mixer/pkg/template"
)

func makeAPIRequest(handler http.Handler, method, url string, data []byte, t *testing.T) (int, []byte) {
	httpRequest, err := http.NewRequest(method, url, bytes.NewBuffer(data))
	httpRequest.Header.Set("Content-Type", "application/json")
	if err != nil {
		t.Fatal(err)
	}
	httpWriter := httptest.NewRecorder()
	handler.ServeHTTP(httpWriter, httpRequest)
	result := httpWriter.Result()
	body, err := ioutil.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	return result.StatusCode, body
}

var rulesSC = &pb.ServiceConfig{
	Rules: []*pb.AspectRule{
		{
			Selector: "true",
			Aspects: []*pb.Aspect{
				{
					Kind: "quotas",
					Params: map[string]interface{}{
						"quotas": []map[string]interface{}{
							{"descriptor_name": "RequestCount"},
						},
					},
				},
			},
		},
	},
}

func TestAPI_getRules(t *testing.T) {
	uri := "/scopes/scope/subjects/subject/rules"
	bval, _ := json.Marshal(rulesSC)
	val := string(bval)

	for _, ctx := range []struct {
		key    string
		val    string
		status int
	}{
		{uri, val, http.StatusOK},
		{uri, "<Unparseable> </Unparseable>", http.StatusInternalServerError},
		{"/scopes/cluster/subjects/DOESNOTEXIST.cluster.local/rules", val, http.StatusNotFound},
		{"/unable_to_route_url_should_get_json_response", val, http.StatusNotFound},
	} {
		t.Run(ctx.key, func(t *testing.T) {
			store := &fakeMemStore{
				data: map[string]string{
					uri: ctx.val,
				},
			}
			api := NewAPI("v1", 0, nil, nil,
				nil, nil, nil, store, template.NewRepository(nil))

			sc, body := makeAPIRequest(api.handler, "GET", api.rootPath+ctx.key, []byte{}, t)
			apiResp := &struct {
				Data *pb.ServiceConfig `json:"data,omitempty"`
				*APIResponse
			}{}
			if err := json.Unmarshal(body, apiResp); err != nil {
				t.Fatalf("Unable to unmarshal json %s\n%s", err.Error(), string(body))
			}
			if rpc.Code(apiResp.Status.Code) != httpStatusToRPC(ctx.status) {
				t.Errorf("got %s, want %s", rpc.Code(apiResp.Status.Code), httpStatusToRPC(ctx.status))
			}
			if sc != ctx.status {
				t.Errorf("http status got %d, want %d", sc, ctx.status)
			}
			if sc != http.StatusOK {
				return
			}

			bd, _ := json.Marshal(apiResp.Data)
			if string(bd) != ctx.val {
				t.Errorf("incorrect value got [%#v]\nwant [%#v]", string(bd), ctx.val)
			}
		})
	}

}

func TestAPI_getScopes(t *testing.T) {

	for _, ctx := range []struct {
		key    string
		val    string
		status int
	}{
		{"/scopes", "", http.StatusNotImplemented}, // TODO expect http.StatusOK
	} {
		t.Run(ctx.key, func(t *testing.T) {
			store := &fakeMemStore{
				data: map[string]string{
					ctx.key: ctx.val,
				},
			}
			api := NewAPI("v1", 0, nil, nil,
				nil, nil, nil, store, template.NewRepository(nil))

			sc, _ := makeAPIRequest(api.handler, "GET", api.rootPath+ctx.key, []byte{}, t)
			if sc != ctx.status {
				t.Errorf("http status got %d, want %d", sc, ctx.status)
			}
			// TODO validate the returned body
		})
	}
}

func TestAPI_createPolicy(t *testing.T) {

	for _, ctx := range []struct {
		key    string
		body   []byte
		status int
	}{
		{"/scopes/global/subjects/newTestSubject",
			[]byte(`{"scope":"global","subject":"subject001","revision":"some-uuid","rules":[]}`),
			http.StatusNotImplemented}, // TODO expect http.StatusCreated
	} {
		t.Run(ctx.key, func(t *testing.T) {
			store := &fakeMemStore{
				data: map[string]string{},
			}
			api := NewAPI("v1", 0, nil, nil,
				nil, nil, nil, store, template.NewRepository(nil))

			sc, _ := makeAPIRequest(api.handler, "POST", api.rootPath+ctx.key, ctx.body, t)
			if sc != ctx.status {
				t.Errorf("http status got %d, want %d", sc, ctx.status)
			} else if sc <= http.StatusAccepted {
				// TODO verify the expected data is present in store rather than just non-empty
				if len(store.data) == 0 {
					t.Errorf("%v did not create %v", ctx.key, string(ctx.body))
				}
			}
		})
	}
}

func TestAPI_putAspect(t *testing.T) {

	for _, ctx := range []struct {
		key    string
		body   []byte
		status int
	}{
		{"/scopes/global/subjects/subject001/rules/rule001/aspects/aspect001",
			[]byte(`{"scope":"global","subject":"subject001","ruleid":"rule001","aspect":"aspect001","revision":"some-uuid","aspects":[]}`),
			http.StatusNotImplemented}, // TODO expect http.StatusCreated
	} {
		t.Run(ctx.key, func(t *testing.T) {
			store := &fakeMemStore{
				data: map[string]string{},
			}
			api := NewAPI("v1", 0, nil, nil,
				nil, nil, nil, store, template.NewRepository(nil))

			sc, _ := makeAPIRequest(api.handler, "PUT", api.rootPath+ctx.key, ctx.body, t)
			if sc != ctx.status {
				t.Errorf("http status got %d, want %d", sc, ctx.status)
			} else if sc <= http.StatusAccepted {
				// TODO verify the expected data is present in store rather than just non-empty
				if len(store.data) == 0 {
					t.Errorf("%v did not create %v", ctx.key, string(ctx.body))
				}
			}
		})
	}
}

func TestAPI_deleteRules(t *testing.T) {

	for _, ctx := range []struct {
		key    string
		val    string
		err    error
		status int
	}{
		{"/scopes/global/subjects/testSubject/rules", "", nil, http.StatusOK},
		// cannot use http.StatusNoContent, because our response always contains json
		{"/scopes/global/subjects/testSubject/rules", "", errors.New("could not delete key"), http.StatusInternalServerError},
	} {
		t.Run(fmt.Sprintf("%s_%s", ctx.key, ctx.err), func(t *testing.T) {
			store := &fakeMemStore{
				data: map[string]string{
					ctx.key: ctx.val,
				},
				err: ctx.err,
			}
			api := NewAPI("v1", 0, nil, nil,
				nil, nil, nil, store, template.NewRepository(nil))

			sc, _ := makeAPIRequest(api.handler, "DELETE", api.rootPath+ctx.key, []byte{}, t)
			if sc != ctx.status {
				t.Errorf("http status got %d, want %d", sc, ctx.status)
			}
		})
	}
}

func TestAPI_deleteRule(t *testing.T) {

	for _, ctx := range []struct {
		key    string
		val    string
		status int
	}{
		{"/scopes/global/subjects/testSubject/rules/rule001", "", http.StatusNotImplemented}, // TODO expect http.StatusNoContent
	} {
		t.Run(ctx.key, func(t *testing.T) {
			store := &fakeMemStore{
				data: map[string]string{
					ctx.key: ctx.val,
				},
			}
			api := NewAPI("v1", 0, nil, nil,
				nil, nil, nil, store, template.NewRepository(nil))

			sc, _ := makeAPIRequest(api.handler, "DELETE", api.rootPath+ctx.key, []byte{}, t)
			if sc != ctx.status {
				t.Errorf("http status got %d, want %d", sc, ctx.status)
			}
		})
	}
}

func TestAPI_putAdapter(t *testing.T) {

	for _, ctx := range []struct {
		key    string
		body   []byte
		status int
	}{
		{"/scopes/global/adapters/adapter001/config001",
			[]byte(`{"params":{}}`),
			http.StatusNotImplemented}, // TODO expect http.StatusCreated
	} {
		t.Run(ctx.key, func(t *testing.T) {
			store := &fakeMemStore{
				data: map[string]string{},
			}
			api := NewAPI("v1", 0, nil, nil,
				nil, nil, nil, store, template.NewRepository(nil))

			sc, _ := makeAPIRequest(api.handler, "PUT", api.rootPath+ctx.key, ctx.body, t)
			if sc != ctx.status {
				t.Errorf("http status got %d, want %d", sc, ctx.status)
			} else if sc <= http.StatusAccepted {
				// TODO verify the expected data is present in store rather than just non-empty
				if len(store.data) == 0 {
					t.Errorf("%v did not create %v", ctx.key, string(ctx.body))
				}
			}
		})
	}
}

func TestAPI_getDescriptor(t *testing.T) {

	for _, ctx := range []struct {
		key    string
		val    string
		status int
	}{
		{"/scopes/global/descriptors/type001/descriptor001", "", http.StatusNotImplemented}, // TODO expect http.StatusOK
	} {
		t.Run(ctx.key, func(t *testing.T) {
			store := &fakeMemStore{
				data: map[string]string{
					ctx.key: ctx.val,
				},
			}
			api := NewAPI("v1", 0, nil, nil,
				nil, nil, nil, store, template.NewRepository(nil))

			sc, _ := makeAPIRequest(api.handler, "GET", api.rootPath+ctx.key, []byte{}, t)
			if sc != ctx.status {
				t.Errorf("http status got %d, want %d", sc, ctx.status)
			}
			// TODO validate the returned body
		})
	}
}

func TestAPI_putDescriptor(t *testing.T) {

	for _, ctx := range []struct {
		key    string
		body   []byte
		status int
	}{
		{"/scopes/global/descriptors/type001/descriptor001",
			[]byte(`{}`),
			http.StatusNotImplemented}, // TODO expect http.StatusCreated
	} {
		t.Run(ctx.key, func(t *testing.T) {
			store := &fakeMemStore{
				data: map[string]string{},
			}
			api := NewAPI("v1", 0, nil, nil,
				nil, nil, nil, store, template.NewRepository(nil))

			sc, _ := makeAPIRequest(api.handler, "PUT", api.rootPath+ctx.key, ctx.body, t)
			if sc != ctx.status {
				t.Errorf("http status got %d, want %d", sc, ctx.status)
			} else if sc <= http.StatusAccepted {
				// TODO verify the expected data is present in store rather than just non-empty
				if len(store.data) == 0 {
					t.Errorf("%v did not create %v", ctx.key, string(ctx.body))
				}
			}
		})
	}
}

func readBody(err string) readBodyFunc {
	return func(r io.Reader) (b []byte, e error) {
		if err != "" {
			e = errors.New(err)
		}
		return
	}
}

func validate(err string) validateFunc {
	return func(cfg map[string]string) (rt *Validated, desc descriptor.Finder, ce *adapter.ConfigErrors) {
		if err != "" {
			ce = ce.Appendf("main", err)
		}
		return
	}
}

type errorPhase int

const (
	errNone errorPhase = iota
	errStoreRead
	errStoreWrite
	errReadyBody
	errValidate
)

func TestAPI_getAdaptersOrDescriptors(t *testing.T) {
	key := "/scopes/%s/adapters"
	val := "{}"

	for _, tst := range []struct {
		msg    string
		scope  string
		status int
	}{
		{"ok", "global", http.StatusOK},
	} {
		t.Run(tst.msg, func(t *testing.T) {
			k := fmt.Sprintf(key, tst.scope)
			store := &fakeMemStore{
				data: map[string]string{
					k: val,
				},
			}
			api := NewAPI("v1", 0, nil, nil,
				nil, nil, nil, store, template.NewRepository(nil))
			sc, bbody := makeAPIRequest(api.handler, "GET", "/api/v1"+k, []byte{}, t)
			if sc != tst.status {
				t.Fatalf("http status got %d\nwant %d \nmsg: %s", sc, tst.status, string(bbody))
			}
			apiResp := &APIResponse{}
			if err := json.Unmarshal(bbody, apiResp); err != nil {
				t.Fatalf("Unable to unmarshal json %s", err.Error())
			}

			if !strings.Contains(apiResp.Status.Message, tst.msg) {
				t.Errorf("got %v\nwant %s", apiResp.Status, tst.msg)
			}
		})
	}

}

func TestAPI_putAdaptersOrDescriptors(t *testing.T) {
	key := "/scopes/%s/adapters"
	val := "Value"

	for _, tst := range []struct {
		msg    string
		scope  string
		status int
	}{
		{"Created", "global", http.StatusOK},
		{"only supports global", "local", http.StatusBadRequest},
	} {
		t.Run(tst.msg, func(t *testing.T) {
			k := fmt.Sprintf(key, tst.scope)
			store := &fakeMemStore{
				data: map[string]string{
					k: val,
				},
			}
			api := NewAPI("v1", 0, nil, nil,
				nil, nil, nil, store, template.NewRepository(nil))
			sc, bbody := makeAPIRequest(api.handler, "PUT", "/api/v1"+k, []byte{}, t)
			if sc != tst.status {
				t.Fatalf("http status got %d\nwant %d", sc, tst.status)
			}
			apiResp := &APIResponse{}
			if err := json.Unmarshal(bbody, apiResp); err != nil {
				t.Fatalf("Unable to unmarshal json %s", err.Error())
			}

			if !strings.Contains(apiResp.Status.Message, tst.msg) {
				t.Errorf("got %v\nwant %s", apiResp.Status, tst.msg)
			}
		})
	}

}

func TestAPI_putRules(t *testing.T) {
	key := "/scopes/scope/subjects/subject/rules"
	val := "Value"

	for _, tst := range []struct {
		msg    string
		phase  errorPhase
		status int
	}{
		{"Created ", errNone, http.StatusOK},
		{"store write error", errStoreWrite, http.StatusInternalServerError},
		{"store read error", errStoreRead, http.StatusInternalServerError},
		{"request read error", errReadyBody, http.StatusInternalServerError},
		{"config validation error", errValidate, http.StatusBadRequest},
	} {
		t.Run(tst.msg, func(t *testing.T) {

			store := &fakeMemStore{
				data: map[string]string{
					key: val,
				},
			}
			api := NewAPI("v1", 0, nil, nil,
				nil, nil, nil, store, template.NewRepository(nil))

			switch tst.phase {
			case errStoreRead:
				store.err = errors.New(tst.msg)
			case errStoreWrite:
				store.writeErr = errors.New(tst.msg)
			case errReadyBody:
				api.readBody = readBody(tst.msg)
			case errValidate:
				api.validate = validate(tst.msg)
			}
			sc, bbody := makeAPIRequest(api.handler, "PUT", "/api/v1"+key, []byte{}, t)
			if sc != tst.status {
				t.Fatalf("http status got %d\nwant %d", sc, tst.status)
			}
			apiResp := &APIResponse{}
			if err := json.Unmarshal(bbody, apiResp); err != nil {
				t.Fatalf("Unable to unmarshal json %s", err.Error())
			}

			if !strings.Contains(apiResp.Status.Message, tst.msg) {
				t.Errorf("got %v\nwant %s", apiResp.Status, tst.msg)
			}
		})
	}
}

// TODO define a new envelope message
// with a place for preparsed message?

func TestAPI_httpStatusToRPC(t *testing.T) {
	for _, tst := range []struct {
		http int
		code rpc.Code
	}{
		{http.StatusOK, rpc.OK},
		{http.StatusTeapot, rpc.UNKNOWN},
	} {
		t.Run(tst.code.String(), func(t *testing.T) {
			c := httpStatusToRPC(tst.http)
			if c != tst.code {
				t.Errorf("got %s\nwant %s", c, tst.code)
			}
		})
	}
}

type fakeresp struct {
	err   error
	value interface{}
}

// bin/linter.sh has a lint exclude for this.
func (f *fakeresp) WriteHeaderAndJson(status int, value interface{}, contentType string) error {
	f.value = value
	return f.err
}
func (f *fakeresp) Write(bytes []byte) (int, error) {
	f.value = bytes
	return 0, f.err
}

// Ensures that writes responses correctly and error are logged
func TestAPI_writeErrorResponse(t *testing.T) {
	// ensures that logging is performed
	err := errors.New("always error")
	for _, tst := range []struct {
		http    int
		code    rpc.Code
		message string
	}{
		{http.StatusTeapot, rpc.UNKNOWN, "Teapot"},
		{http.StatusOK, rpc.OK, "OK"},
		{http.StatusNotFound, rpc.NOT_FOUND, "Not Found"},
	} {
		t.Run(tst.code.String(), func(t *testing.T) {
			resp := &fakeresp{err: err}
			writeErrorResponse(tst.http, tst.message, resp)
			apiResp, ok := resp.value.(*APIResponse)
			if !ok {
				t.Error("failed to produce APIResponse")
				return
			}
			if apiResp.Status.Code != int32(tst.code) {
				t.Errorf("got %d\nwant %s", apiResp.Status.Code, tst.code)
			}
		})
	}

}

func TestAPI_Run(t *testing.T) {
	api := NewAPI("v1", 9094, nil, nil, nil, nil, nil, nil, template.NewRepository(nil))
	go api.Run()
	err := api.server.Close()
	if err != nil {
		t.Errorf("unexpected failure while closing %s", err)
	}
}

// CURL examples
// curl  http://localhost:9094/api/v1/scopes/global/subjects/svc2.ns.cluster.local/rules
//      --data-binary "{}<code></code>" -X PUT -H "Content-Type: application/yaml"
