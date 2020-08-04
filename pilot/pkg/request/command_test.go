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

package request

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"istio.io/istio/tests/util"
)

type pilotStubHandler struct {
	sync.Mutex
	States []pilotStubState
}

type pilotStubState struct {
	wantMethod string
	wantPath   string
	wantBody   []byte
	StatusCode int
	Response   string
}

func (p *pilotStubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.Lock()
	if r.Method == p.States[0].wantMethod {
		if r.URL.Path == p.States[0].wantPath {
			defer r.Body.Close()
			body, _ := ioutil.ReadAll(r.Body)
			if err := util.Compare(body, p.States[0].wantBody); err == nil {
				w.WriteHeader(p.States[0].StatusCode)
				w.Write([]byte(p.States[0].Response))
			} else {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(fmt.Sprintf("wanted body %q got %q", string(p.States[0].wantBody), string(body))))
			}
		} else {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(fmt.Sprintf("wanted path %q got %q", p.States[0].wantPath, r.URL.Path)))
		}
	} else {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf("wanted method %q got %q", p.States[0].wantMethod, r.Method)))
	}
	p.States = p.States[1:]
	p.Unlock()
}

func Test_command_do(t *testing.T) {
	tests := []struct {
		name              string
		method            string
		path              string
		body              string
		pilotStates       []pilotStubState
		pilotNotReachable bool
		wantError         bool
	}{
		{
			name:   "makes a request using passed method, url and body",
			method: "POST",
			path:   "/want/path",
			body:   "body",
			pilotStates: []pilotStubState{
				{StatusCode: 200, Response: "fine", wantMethod: "POST", wantPath: "/want/path", wantBody: []byte("body")},
			},
		},
		{
			name:   "adds / prefix to path if required",
			method: "POST",
			path:   "want/path",
			body:   "body",
			pilotStates: []pilotStubState{
				{StatusCode: 200, Response: "fine", wantMethod: "POST", wantPath: "/want/path", wantBody: []byte("body")},
			},
		},
		{
			name:   "handles empty string body in args",
			method: "GET",
			path:   "/want/path",
			body:   "",
			pilotStates: []pilotStubState{
				{StatusCode: 200, Response: "fine", wantMethod: "GET", wantPath: "/want/path", wantBody: nil},
			},
		},
		{
			name:   "doesn't error on 404",
			method: "GET",
			path:   "/want/path",
			body:   "",
			pilotStates: []pilotStubState{
				{StatusCode: 404, Response: "not-found", wantMethod: "GET", wantPath: "/want/path", wantBody: nil},
			},
		},
		{
			name:              "errors if Pilot is unreachable",
			method:            "GET",
			path:              "/want/path",
			pilotNotReachable: true,
			wantError:         true,
		},
		{
			name:   "errors if Pilot responds with a non success status",
			method: "GET",
			path:   "/not/wanted/path",
			body:   "",
			pilotStates: []pilotStubState{
				{StatusCode: 200, Response: "fine", wantMethod: "GET", wantPath: "/want/path", wantBody: nil},
			},
			wantError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pilotStub := httptest.NewServer(
				&pilotStubHandler{States: tt.pilotStates},
			)
			stubURL, _ := url.Parse(pilotStub.URL)
			if tt.pilotNotReachable {
				stubURL, _ = url.Parse("http://notpilot")
			}
			c := &Command{
				Address: stubURL.Host,
				Client:  &http.Client{},
			}
			err := c.Do(tt.method, tt.path, tt.body)
			if (err == nil) && tt.wantError {
				t.Errorf("Expected an error but received none")
			} else if (err != nil) && !tt.wantError {
				t.Errorf("Unexpected err: %v", err)
			}
		})
	}
}
