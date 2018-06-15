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

type synczStubHandler struct {
	sync.Mutex
	States []pilotStubState
}

func (s *synczStubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.Lock()
	switch r.URL.Path {
	case "/debug/syncz":
		w.WriteHeader(s.States[0].StatusCode)
		_, _ = w.Write([]byte(s.States[0].Response))
		s.States = s.States[1:]
	default:
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf("%q is not a valid path", r.URL.Path)))
	}
	s.Unlock()
}

func Test_status_run(t *testing.T) {
	tests := []struct {
		name              string
		pilotNotReachable bool
		pilotStates       []pilotStubState
		wantError         bool
	}{
		{
			name: "calls syncz endpoint and dumps the response",
			pilotStates: []pilotStubState{
				{StatusCode: 200, Response: "fine"},
			},
		},
		{
			name: "errors if syncz endpoint returns non-200",
			pilotStates: []pilotStubState{
				{StatusCode: 500, Response: "nope"},
			},
			wantError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pilotStub := httptest.NewServer(
				&synczStubHandler{States: tt.pilotStates},
			)
			stubURL, _ := url.Parse(pilotStub.URL)
			if tt.pilotNotReachable {
				stubURL, _ = url.Parse("http://notpilot")
			}
			s := &status{
				pilotAddress: stubURL.Host,
				client:       &http.Client{},
			}
			err := s.run()
			if (err == nil) && tt.wantError {
				t.Errorf("Expected an error but received none")
			} else if (err != nil) && !tt.wantError {
				t.Errorf("Unexpected err: %v", err)
			}
		})
	}
}
