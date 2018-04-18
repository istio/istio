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

type envoyStubHandler struct {
	sync.Mutex
	States []envoyStubState
}

type envoyStubState struct {
	StatusCode int
	Response   string
}

func (p *envoyStubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.Lock()
	switch r.URL.Path {
	case "/clusters", "/listeners", "/config_dump":
		w.WriteHeader(p.States[0].StatusCode)
		_, _ = w.Write([]byte(p.States[0].Response))
		p.States = p.States[1:]
	default:
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf("%q is not a valid path", r.URL.Path)))
	}
	p.Unlock()
}

func TestDebug_Run(t *testing.T) {
	tests := []struct {
		name                 string
		args                 []string
		staticConfigLocation string
		envoyNotReachable    bool
		envoyStates          []envoyStubState
		wantError            bool
	}{
		{
			name:                 "all configType no error",
			args:                 []string{"all"},
			staticConfigLocation: "./testdata",
			envoyStates: []envoyStubState{
				{StatusCode: 200, Response: "fine"},
				{StatusCode: 200, Response: "fine"},
				{StatusCode: 200, Response: "fine"},
			},
		},
		{
			name:                 "all configType errors when unable to reach envoy",
			args:                 []string{"all"},
			staticConfigLocation: "./testdata",
			envoyNotReachable:    true,
			wantError:            true,
		},
		{
			name:                 "all configType errors when unable to find static config",
			args:                 []string{"all"},
			staticConfigLocation: "",
			envoyStates: []envoyStubState{
				{StatusCode: 200, Response: "fine"},
				{StatusCode: 200, Response: "fine"},
				{StatusCode: 200, Response: "fine"},
			},
			wantError: true,
		},
		{
			name:                 "all configType errors envoy returns 404",
			args:                 []string{"all"},
			staticConfigLocation: "./testdata",
			envoyStates: []envoyStubState{
				{StatusCode: 200, Response: "fine"},
				{StatusCode: 200, Response: "fine"},
				{StatusCode: 404, Response: "not fine"},
			},
			wantError: true,
		},
		{
			name:                 "static configType no error",
			args:                 []string{"static"},
			staticConfigLocation: "./testdata",
		},
		{
			name:                 "static configType no config file returns error",
			args:                 []string{"static"},
			staticConfigLocation: "",
			wantError:            true,
		},
		{
			name: "listeners configType no error",
			args: []string{"listeners"},
			envoyStates: []envoyStubState{
				{StatusCode: 200, Response: "fine"},
			},
		},
		{
			name:              "listeners configType errors if envoy unreachable",
			args:              []string{"listeners"},
			envoyNotReachable: true,
			wantError:         true,
		},
		{
			name: "clusters configType no error",
			args: []string{"routes"},
			envoyStates: []envoyStubState{
				{StatusCode: 200, Response: "fine"},
			},
		},
		{
			name:              "clusters configType error if envoy unreachable",
			args:              []string{"clusters"},
			envoyNotReachable: true,
			wantError:         true,
		},
		{
			name: "routes configType no error",
			args: []string{"routes"},
			envoyStates: []envoyStubState{
				{StatusCode: 200, Response: "fine"},
			},
		},
		{
			name:              "routes configType error if envoy unreachable",
			args:              []string{"routes"},
			envoyNotReachable: true,
			wantError:         true,
		},
		{
			name:      "invalid configType returns error",
			args:      []string{"not-a-config-type"},
			wantError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envoyStub := httptest.NewServer(
				&envoyStubHandler{States: tt.envoyStates},
			)
			stubURL, _ := url.Parse(envoyStub.URL)
			if tt.envoyNotReachable {
				stubURL, _ = url.Parse("http://notenvoy")
			}
			d := &debug{
				envoyAdminAddress:    stubURL.Host,
				staticConfigLocation: tt.staticConfigLocation,
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
