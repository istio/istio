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

package inject

import (
	"bytes"
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	restful "github.com/emicklei/go-restful"
	v1 "k8s.io/api/core/v1"

	"istio.io/pilot/proxy"
	"istio.io/pilot/test/util"
)

var (
	mesh           = proxy.DefaultMeshConfig()
	httpTestConfig = &Config{
		Policy:     InjectionPolicyEnabled,
		Namespaces: []string{v1.NamespaceAll},
		Params: Params{
			InitImage:         InitImageName(unitTestHub, unitTestTag, unitTestDebugMode),
			ProxyImage:        ProxyImageName(unitTestHub, unitTestTag, unitTestDebugMode),
			ImagePullPolicy:   "IfNotPresent",
			Verbosity:         DefaultVerbosity,
			SidecarProxyUID:   DefaultSidecarProxyUID,
			Version:           "12345678",
			Mesh:              &mesh,
			MeshConfigMapName: "istio",
			DebugMode:         unitTestDebugMode,
		},
	}
)

func TestHTTPServer(t *testing.T) {
	stop := make(chan struct{})
	server := NewHTTPServer(0, httpTestConfig)
	go server.Run(stop)
	time.Sleep(time.Second)
	close(stop)
}

func TestHTTPServer_inject(t *testing.T) {
	server := NewHTTPServer(0, httpTestConfig)

	// Don't run the server. Use container.ServeHTTP() to verify
	// inject method.

	cases := []struct {
		name         string
		method       string
		url          string
		contentType  string
		bodyFilename string

		wantStatus       int
		wantContentType  string
		wantBodyFilename string
	}{
		{
			name:             "expect injection",
			method:           http.MethodPost,
			url:              "/inject",
			contentType:      contentTypeYAML,
			bodyFilename:     "testdata/hello.yaml",
			wantStatus:       http.StatusOK,
			wantContentType:  contentTypeYAML,
			wantBodyFilename: "testdata/hello.yaml.injected",
		},
		{
			name:         "wrong method (GET)",
			method:       http.MethodGet,
			url:          "/inject",
			bodyFilename: "testdata/hello.yaml",
			wantStatus:   http.StatusMethodNotAllowed,
		},
		{
			name:         "wrong path (/)",
			method:       http.MethodPost,
			url:          "/",
			contentType:  contentTypeYAML,
			bodyFilename: "testdata/hello.yaml",
			wantStatus:   http.StatusNotFound,
		},
		{
			name:         "wrong Content-Type (JSON)",
			method:       http.MethodPost,
			url:          "/inject",
			contentType:  "application/json",
			bodyFilename: "testdata/hello.yaml",
			wantStatus:   http.StatusUnsupportedMediaType,
		},
	}

	for _, c := range cases {
		body, err := ioutil.ReadFile(c.bodyFilename)
		if err != nil {
			t.Errorf("%v: could not read test body from %q%v", c.name, c.bodyFilename, err)
			continue
		}

		request, err := http.NewRequest(c.method, c.url, bytes.NewBuffer(body))
		if err != nil {
			t.Errorf("%v: http.NewRequest(%v, %v) failed: %v", c.name, c.method, c.url, err)
			continue
		}
		request.Header.Set("Content-Type", c.contentType)

		recorder := httptest.NewRecorder()
		server.container.ServeHTTP(recorder, request)
		gotResponse := recorder.Result()
		if gotResponse.StatusCode != c.wantStatus {
			t.Errorf("%v: wrong status code received: got %v want %v", c.name, gotResponse.StatusCode, c.wantStatus)
		}
		if c.wantContentType != "" {
			gotContentTypes := gotResponse.Header["Content-Type"]
			if !reflect.DeepEqual(gotContentTypes, []string{c.wantContentType}) {
				t.Errorf("%v: wrong 'Content-Type': got %q want %q", c.name, gotContentTypes, []string{c.wantContentType})
			}
		}
		if c.wantBodyFilename != "" {
			gotBody, err := ioutil.ReadAll(gotResponse.Body)
			if err != nil {
				t.Errorf("%v: could not receive response body: %v", c.name, err)
				continue
			}
			util.CompareContent(gotBody, c.wantBodyFilename, t)
		}
	}
}

func TestOnError(t *testing.T) {
	cases := []struct {
		body   []byte
		err    error
		status int
	}{
		{[]byte{}, errors.New("test"), http.StatusInternalServerError},
		{nil, errors.New("test"), http.StatusBadRequest},
	}
	for _, c := range cases {
		recorder := httptest.NewRecorder()
		onError(errors.New("test"), c.status, c.body, restful.NewResponse(recorder))
		if recorder.Code != c.status {
			t.Errorf("wrong code for %v: got %v want %v", c, recorder.Code, c.status)
		}
	}
}
