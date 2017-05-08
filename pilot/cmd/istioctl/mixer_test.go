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

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"istio.io/manager/client/proxy"
	"istio.io/manager/platform/kube"
	"istio.io/manager/test/util"

	"github.com/ghodss/yaml"
	rpc "github.com/googleapis/googleapis/google/rpc"
)

func TestMixerRuleCreate(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/scopes/good/subjects/good/rules" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			response := mixerAPIResponse{Status: rpc.Status{Message: "bad message"}}
			data, err := json.Marshal(response)
			if err != nil {
				t.Fatalf("Error generating bad test response: %v", err)
			}
			if _, err := w.Write(data); err != nil {
				t.Fatalf("Error writing bad test response: %v", err)
			}
		}
	}))
	defer ts.Close()

	mixerRESTRequester = &proxy.BasicHTTPRequester{
		BaseURL: ts.URL,
		Client:  &http.Client{Timeout: 1 * time.Second},
		Version: kube.IstioResourceVersion,
	}

	cases := []struct {
		name    string
		url     string
		scope   string
		subject string
		// rule is validated on server so contents don't matter for
		// testing.
		rule      string
		wantError bool
	}{
		{
			name:      "good request with http prefix",
			url:       ts.URL,
			scope:     "good",
			subject:   "good",
			rule:      "- good",
			wantError: false,
		},
		{
			name:      "good request without http:// prefix",
			url:       strings.TrimPrefix(ts.URL, "http://"),
			scope:     "good",
			subject:   "good",
			rule:      "- good",
			wantError: false,
		},
		{
			name:      "bad request",
			url:       ts.URL,
			scope:     "bad",
			subject:   "bad",
			rule:      "- bad",
			wantError: true,
		},
	}

	for _, c := range cases {
		err := mixerRuleCreate(c.scope, c.subject, []byte(c.rule))
		if c.wantError && err == nil {
			t.Errorf("%s: expected error but got success", c.name)
		}
		if !c.wantError && err != nil {
			t.Errorf("%s: expected success but got error: %v", c.name, err)
		}
	}
}

func TestMixerRuleGet(t *testing.T) {
	wantRule := map[string]interface{}{
		"subject":  "namespace:ns",
		"revision": "2022",
		"rules": map[string]interface{}{
			"aspects": map[string]interface{}{
				"kinds": "denials",
			},
		},
	}
	wantYAML, err := yaml.Marshal(wantRule)
	if err != nil {
		t.Fatalf("Failed to generate test data: %v", err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/scopes/good/subjects/good/rules" {
			w.WriteHeader(http.StatusOK)
			response := mixerAPIResponse{Data: wantRule}
			data, err := json.Marshal(response)
			if err != nil {
				t.Fatalf("Error generating good test response: %v", err)
			}
			if _, err := w.Write(data); err != nil {
				t.Fatalf("Error writing good test response: %v", err)
			}
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	mixerRESTRequester = &proxy.BasicHTTPRequester{
		BaseURL: ts.URL,
		Client:  &http.Client{Timeout: 1 * time.Second},
		Version: kube.IstioResourceVersion,
	}

	cases := []struct {
		name      string
		url       string
		scope     string
		subject   string
		wantRule  string
		wantError bool
	}{
		{
			name:      "good request",
			url:       ts.URL,
			scope:     "good",
			subject:   "good",
			wantRule:  string(wantYAML),
			wantError: false,
		},
		{
			name:      "bad request",
			url:       ts.URL,
			scope:     "bad",
			subject:   "bad",
			wantError: true,
		},
	}

	for _, c := range cases {
		gotRule, err := mixerRuleGet(c.scope, c.subject)
		if c.wantError && err == nil {
			t.Errorf("%s: expected error but got success", c.name)
		}
		if !c.wantError && err != nil {
			t.Errorf("%s: expected success but got error: %v", c.name, err)
		}
		if err := util.Compare([]byte(gotRule), []byte(c.wantRule)); err != nil {
			t.Errorf("%s: bad rule: %v", c.name, err)
		}
	}
}
