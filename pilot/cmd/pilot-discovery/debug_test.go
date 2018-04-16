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
	StatusCode int
	Response   string
}

func (p *pilotStubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.Lock()
	switch r.URL.Path {
	case "/debug/cdsz", "/debug/ldsz", "/debug/rdsz", "/debug/edsz":
		w.WriteHeader(p.States[0].StatusCode)
		_, _ = w.Write([]byte(p.States[0].Response))
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
			name: "debug with all configType does not error",
			args: []string{"proxyID", "all"},
			pilotStates: []pilotStubState{
				{StatusCode: 200, Response: "fine"},
				{StatusCode: 200, Response: "fine"},
				{StatusCode: 200, Response: "fine"},
				{StatusCode: 200, Response: "fine"},
			},
		},
		{
			name:              "debug with all configType errors when unable to reach to pilot",
			args:              []string{"proxyID", "all"},
			pilotNotReachable: true,
			wantError:         true,
		},
		{
			name: "debug with all configType errors when one request to pilot returns a non 200 response",
			args: []string{"proxyID", "all"},
			pilotStates: []pilotStubState{
				{StatusCode: 200, Response: "fine"},
				{StatusCode: 200, Response: "fine"},
				{StatusCode: 200, Response: "fine"},
				{StatusCode: 404, Response: "not fine"},
			},
			wantError: true,
		},
		{
			name: "debug with clusters configType does not error",
			args: []string{"proxyID", "clusters"},
			pilotStates: []pilotStubState{
				{StatusCode: 200, Response: "fine"},
			},
		},
		{
			name:              "debug with clusters configType errors if pilot unreachable",
			args:              []string{"proxyID", "clusters"},
			pilotNotReachable: true,
			wantError:         true,
		},
		{
			name: "debug with listeners configType does not error",
			args: []string{"proxyID", "listeners"},
			pilotStates: []pilotStubState{
				{StatusCode: 200, Response: "fine"},
			},
		},
		{
			name:              "debug with listeners configType errors if pilot unreachable",
			args:              []string{"proxyID", "listeners"},
			pilotNotReachable: true,
			wantError:         true,
		},
		{
			name: "debug with routes configType does not error",
			args: []string{"proxyID", "routes"},
			pilotStates: []pilotStubState{
				{StatusCode: 200, Response: "fine"},
			},
		},
		{
			name:              "debug with routes configType errors if pilot unreachable",
			args:              []string{"proxyID", "routes"},
			pilotNotReachable: true,
			wantError:         true,
		},
		{
			name: "debug with endpoints configType does not error",
			args: []string{"proxyID", "endpoints"},
			pilotStates: []pilotStubState{
				{StatusCode: 200, Response: "fine"},
			},
		},
		{
			name:              "debug with endpoints configType errors if pilot unreachable",
			args:              []string{"proxyID", "endpoints"},
			pilotNotReachable: true,
			wantError:         true,
		},
		{
			name:      "debug with invalid configType returns an error",
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
