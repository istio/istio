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

	"istio.io/pilot/adapter/config/tpr"
	"istio.io/pilot/client/proxy"
	"istio.io/pilot/test/util"

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
		Version: tpr.IstioResourceVersion,
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
		{
			name:      "illformed",
			url:       ts.URL,
			scope:     "good/subjects/good/rules?with=query",
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

func TestMixerRuleDelete(t *testing.T) {
	goodpath := "/" + mixerRulePath("global", "global")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path == goodpath {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer ts.Close()
	mixerRESTRequester = &proxy.BasicHTTPRequester{
		BaseURL: ts.URL,
		Client:  &http.Client{Timeout: 1 * time.Second},
		Version: tpr.IstioResourceVersion,
	}

	cases := []struct {
		subject   string
		errorCode int
	}{
		{"global", http.StatusOK},
		{"badsubject", http.StatusInternalServerError},
	}

	for _, c := range cases {
		t.Run(c.subject, func(t *testing.T) {
			err := mixerRuleDelete("global", c.subject)
			wantErr := c.errorCode != http.StatusOK
			if wantErr {
				if err == nil {
					t.Errorf("%v expected error", c.errorCode)
				}
			} else {
				if err != nil {
					t.Errorf("%v unexpected error", err.Error())
				}
			}
		})
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
		Version: tpr.IstioResourceVersion,
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

func TestMixerAdapterOrDescriptorCreate(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/scopes/good/adapters" {
			w.WriteHeader(http.StatusOK)
		} else if r.URL.Path == "/api/v1/scopes/good/descriptors" {
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
		Version: tpr.IstioResourceVersion,
	}

	cases := []struct {
		name  string
		scope string
		// rule is validated on server so contents don't matter for
		// testing.
		rule      string
		wantError bool
	}{
		{
			name:      "good request with http prefix",
			scope:     "good",
			rule:      "- good",
			wantError: false,
		},
		{
			name:      "bad request",
			scope:     "bad",
			rule:      "- bad",
			wantError: true,
		},
		{
			name:      "illformed",
			scope:     "good/adapters?with=query",
			rule:      "- bad",
			wantError: true,
		},
	}

	for _, n := range []string{"adapters", "descriptors"} {
		for _, c := range cases {
			err := mixerAdapterOrDescriptorCreate(c.scope, n, []byte(c.rule))
			if c.wantError && err == nil {
				t.Errorf("%s %s: expected error but got success", n, c.name)
			}
			if !c.wantError && err != nil {
				t.Errorf("%s %s: expected success but got error: %v", n, c.name, err)
			}
		}
	}
}

func TestMixerAdapterOrDescriptorsGet(t *testing.T) {
	wantAdapters := map[string]interface{}{
		"subject": "global",
		"adapters": []map[string]interface{}{
			{
				"name":   "default",
				"kind":   "quotas",
				"impl":   "memQuota",
				"params": nil,
			},
			{
				"name":   "default",
				"impl":   "stdioLogger",
				"params": map[string]string{"logStream": "STDERR"},
			},
		},
	}

	wantDescriptors := map[string]interface{}{
		"subject":  "namespace:ns",
		"revision": "2022",
		"manifests": []map[string]interface{}{
			{"name": "kubernetes", "revision": "1", "attributes": nil},
		},
		"metrics": []map[string]interface{}{
			{
				"name":  "request_count",
				"kind":  "COUNTER",
				"value": "INT64",
			},
		},
		"quotas": []map[string]interface{}{
			{"name": "RequestCount", "rate_limit": true},
		},
	}

	wantAdaptersYAML, err := yaml.Marshal(wantAdapters)
	if err != nil {
		t.Fatalf("Failed to unmarshal test data: %v", err)
	}

	wantDescriptorsYAML, err := yaml.Marshal(wantDescriptors)
	if err != nil {
		t.Fatalf("Failed to unmarshal test data: %v", err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/scopes/good/") {
			var data interface{}
			n := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			if n == "adapters" {
				data = wantAdapters
			} else if n == "descriptors" {
				data = wantDescriptors
			} else {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusOK)
			response := mixerAPIResponse{Data: data}
			marshalled, err := json.Marshal(response)
			if err != nil {
				t.Fatalf("Error generating good test response: %v", err)
			}
			if _, err := w.Write(marshalled); err != nil {
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
		Version: tpr.IstioResourceVersion,
	}

	cases := []struct {
		name      string
		scope     string
		confType  string
		wantData  []byte
		wantError bool
	}{
		{
			name:      "good adapter",
			scope:     "good",
			confType:  "adapters",
			wantData:  wantAdaptersYAML,
			wantError: false,
		},
		{
			name:      "bad adapter",
			scope:     "bad",
			confType:  "adapters",
			wantData:  []byte{},
			wantError: true,
		},
		{
			name:      "good descriptor",
			scope:     "good",
			confType:  "descriptors",
			wantData:  wantDescriptorsYAML,
			wantError: false,
		},
		{
			name:      "bad descriptor",
			scope:     "bad",
			confType:  "descriptors",
			wantData:  []byte{},
			wantError: true,
		},
		{
			name:      "bad type",
			scope:     "good",
			confType:  "no-such-thing",
			wantData:  []byte{},
			wantError: true,
		},
	}

	for _, c := range cases {
		gotData, err := mixerAdapterOrDescriptorGet(c.scope, c.confType)
		if c.wantError && err == nil {
			t.Errorf("%s: expected error but got success", c.name)
		}
		if !c.wantError && err != nil {
			t.Errorf("%s: expected success but got error: %v", c.name, err)
		}
		if err := util.Compare([]byte(gotData), []byte(c.wantData)); err != nil {
			t.Errorf("%s: bad response data: %v", c.name, err)
		}
	}
}
