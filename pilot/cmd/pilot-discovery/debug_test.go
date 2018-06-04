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

package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
)

type pilotStubHandler struct {
	sync.Mutex
	States []pilotStubState
}

type pilotStubState struct {
	wantProxyID string
	StatusCode  int
	Response    string
}

func (p *pilotStubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.Lock()
	proxyID := r.URL.Query().Get("proxyID")
	switch r.URL.Path {
	case "/debug/adsz", "/debug/edsz":
		if proxyID == p.States[0].wantProxyID {
			w.WriteHeader(p.States[0].StatusCode)
			_, _ = w.Write([]byte(p.States[0].Response))
		} else {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(fmt.Sprintf("wanted proxyID %q got %q", p.States[0].wantProxyID, proxyID)))
		}

		p.States = p.States[1:]
	default:
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf("%q is not a valid path", r.URL.Path)))
	}
	p.Unlock()
}

func Test_debug_run(t *testing.T) {
	tests := []struct {
		name              string
		args              []string
		pilotNotReachable bool
		pilotStates       []pilotStubState
		wantError         bool
	}{
		{
			name: "all configType no error with 200 or 404",
			args: []string{"proxyID", "all"},
			pilotStates: []pilotStubState{
				{StatusCode: 200, Response: "fine", wantProxyID: "proxyID"},
				{StatusCode: 404, Response: "fine", wantProxyID: "proxyID"},
			},
		},
		{
			name: "all configType with all proxyID passes no proxyID",
			args: []string{"all", "all"},
			pilotStates: []pilotStubState{
				{StatusCode: 200, Response: "fine", wantProxyID: ""},
				{StatusCode: 200, Response: "fine", wantProxyID: ""},
			},
		},
		{
			name:              "all configType errors if pilot unreachable",
			args:              []string{"proxyID", "all"},
			pilotNotReachable: true,
			wantError:         true,
		},
		{
			name: "all configType error when pilot returns !200",
			args: []string{"proxyID", "all"},
			pilotStates: []pilotStubState{
				{StatusCode: 200, Response: "fine", wantProxyID: "proxyID"},
				{StatusCode: 500, Response: "not fine", wantProxyID: "proxyID"},
			},
			wantError: true,
		},
		{
			name: "ads configType does not error with 200",
			args: []string{"proxyID", "ads"},
			pilotStates: []pilotStubState{
				{StatusCode: 200, Response: "fine", wantProxyID: "proxyID"},
			},
		},
		{
			name: "ads configType does not error with 404",
			args: []string{"proxyID", "ads"},
			pilotStates: []pilotStubState{
				{StatusCode: 404, Response: "fine", wantProxyID: "proxyID"},
			},
		},
		{
			name:              "ads configType errors if pilot unreachable",
			args:              []string{"proxyID", "ads"},
			pilotNotReachable: true,
			wantError:         true,
		},
		{
			name: "eds configType does not error with 200",
			args: []string{"proxyID", "eds"},
			pilotStates: []pilotStubState{
				{StatusCode: 200, Response: "fine", wantProxyID: "proxyID"},
			},
		},
		{
			name: "eds configType does not error with 404",
			args: []string{"proxyID", "eds"},
			pilotStates: []pilotStubState{
				{StatusCode: 404, Response: "fine", wantProxyID: "proxyID"},
			},
		},
		{
			name:              "eds configType errors if pilot unreachable",
			args:              []string{"proxyID", "eds"},
			pilotNotReachable: true,
			wantError:         true,
		},
		{
			name:      "invalid configType returns an error",
			args:      []string{"proxyID", "not-a-config-type"},
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
			d := &debug{
				pilotAddress: stubURL.Host,
				client:       &http.Client{},
			}
			err := d.run(tt.args)
			if (err == nil) && tt.wantError {
				t.Errorf("Expected an error but received none")
			} else if (err != nil) && !tt.wantError {
				t.Errorf("Unexpected err: %v", err)
			}
		})
	}
}
