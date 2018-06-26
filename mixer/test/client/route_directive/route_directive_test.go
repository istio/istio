// Copyright 2018 Istio Authors. All Rights Reserved.
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

package client_test

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"reflect"
	"testing"
	"time"

	"istio.io/api/mixer/v1"
	"istio.io/istio/mixer/test/client/env"
)

// signifies a certain map has to be empty
const mustBeEmpty = "MUST_BE_EMPTY"

var expectedStats = map[string]int{
	"http_mixer_filter.total_check_calls":        12,
	"http_mixer_filter.total_remote_check_calls": 6,
}

func TestRouteDirective(t *testing.T) {
	s := env.NewTestSetup(env.RouteDirectiveTest, t)
	env.SetStatsUpdateInterval(s.MfConfig(), 1)
	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()
	s.SetMixerCheckReferenced(&v1.ReferencedAttributes{
		Words: []string{"request.path"},
		AttributeMatches: []v1.ReferencedAttributes_AttributeMatch{{
			Name:      -1,
			Condition: v1.EXACT,
		}},
	})

	client := &http.Client{Timeout: time.Second}

	testCases := []struct {
		desc      string
		path      string
		method    string
		directive *v1.RouteDirective

		// expectations
		request  http.Header // headers as received by backend
		response http.Header // headers as received by client
		body     string      // body as received by client
	}{{
		desc:   "override HTTP pseudo headers",
		path:   "/",
		method: "GET",
		directive: &v1.RouteDirective{
			RequestHeaderOperations: []v1.HeaderOperation{{
				Name:  ":path",
				Value: "/override_path",
			}, {
				Name:  ":authority",
				Value: "override_host",
			}, {
				Name:  ":method",
				Value: "POST",
			}},
		},
		request: http.Header{
			":path":      []string{"/override_path"},
			":authority": []string{"override_host"},
			":method":    []string{"POST"},
		},
		response: http.Header{
			"Server":         []string{"envoy"},
			"Content-Length": []string{"0"},
		},
	}, {
		desc:   "request header operations",
		path:   "/request",
		method: "POST",
		directive: &v1.RouteDirective{
			RequestHeaderOperations: []v1.HeaderOperation{{
				Name:  "x-istio-request",
				Value: "value",
			}, {
				Name:      "x-istio-request",
				Value:     "value2",
				Operation: v1.APPEND,
			}, {
				Name:      "user-agent",
				Operation: v1.REMOVE,
			}},
		},
		request: http.Header{
			"X-Istio-Request": []string{"value", "value2"},
			"User-Agent":      nil,
		},
		response: http.Header{
			"X-Istio-Request": nil,
		},
	}, {
		desc:   "response header operations",
		path:   "/response",
		method: "GET",
		directive: &v1.RouteDirective{
			ResponseHeaderOperations: []v1.HeaderOperation{{
				Name:  "x-istio-response",
				Value: "value",
			}, {
				Name:      "x-istio-response",
				Value:     "value2",
				Operation: v1.APPEND,
			}, {
				Name:      "content-length",
				Operation: v1.REMOVE,
			}},
		},
		request: http.Header{
			"X-Istio-Response": nil,
		},
		response: http.Header{
			"X-Istio-Response": []string{"value", "value2"},
			"Content-Length":   nil,
		},
	}, {
		desc:   "combine operations",
		path:   "/combine",
		method: "PUT",
		directive: &v1.RouteDirective{
			RequestHeaderOperations:  []v1.HeaderOperation{{Name: "istio-request", Value: "test"}},
			ResponseHeaderOperations: []v1.HeaderOperation{{Name: "istio-response", Value: "case"}},
		},
		request:  http.Header{"Istio-Request": []string{"test"}},
		response: http.Header{"Istio-Response": []string{"case"}},
	}, {
		desc:   "direct response",
		path:   "/direct",
		method: "GET",
		directive: &v1.RouteDirective{
			DirectResponseBody:       "hello!",
			DirectResponseCode:       200,
			ResponseHeaderOperations: []v1.HeaderOperation{{Name: "istio-response", Value: "case"}},
		},
		body:     "hello!",
		request:  http.Header{mustBeEmpty: nil},
		response: http.Header{"Istio-Response": []string{"case"}},
	}, {
		desc:   "redirect",
		path:   "/redirect",
		method: "GET",
		directive: &v1.RouteDirective{
			DirectResponseBody: "Moved!",
			DirectResponseCode: 301,
			ResponseHeaderOperations: []v1.HeaderOperation{{
				Name:  "location",
				Value: fmt.Sprintf("http://localhost:%d/", s.Ports().BackendPort),
			}},
		},
		request: http.Header{
			"Referer": []string{fmt.Sprintf("http://localhost:%d/redirect", s.Ports().ServerProxyPort)},
		},
	}}

	for _, cs := range testCases {
		t.Run(cs.desc, func(t *testing.T) {
			s.SetMixerRouteDirective(cs.directive)
			req, err := http.NewRequest(cs.method, fmt.Sprintf("http://localhost:%d%s", s.Ports().ServerProxyPort, cs.path), nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				t.Error(err)
			}
			if string(body) != cs.body {
				t.Errorf("response body: got %q, want %q", string(body), cs.body)
			}
			compareHeaders(t, s.LastRequestHeaders(), cs.request)
			compareHeaders(t, resp.Header, cs.response)
		})
	}

	// run the queries again to exercise caching
	for _, cs := range testCases {
		s.SetMixerRouteDirective(cs.directive)
		req, _ := http.NewRequest(cs.method, fmt.Sprintf("http://localhost:%d%s", s.Ports().ServerProxyPort, cs.path), nil)
		_, _ = client.Do(req)
	}

	s.VerifyStats(expectedStats)
}

func compareHeaders(t *testing.T, actual, expected http.Header) {
	if _, shouldBeEmpty := expected[mustBeEmpty]; shouldBeEmpty {
		if actual != nil {
			t.Errorf("got %#v, expect empty", actual)
		}
		return
	}

	log.Printf("actual %#v, expected %#v", actual, expected)
	for name, want := range expected {
		got := actual[name]
		if !reflect.DeepEqual(got, want) {
			t.Errorf("header %q got %#v, want %#v", name, got, want)
		}
	}
}
