// Copyright Istio Authors.
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

package opa

// NOTE: This test will eventually be auto-generated so that it automatically supports all CHECK and QUOTA
//       templates known to Mixer. For now, it's manually curated.

import (
	"context"
	"reflect"
	"testing"

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"

	"istio.io/istio/mixer/adapter/opa/config"
	"istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template/authorization"
)

func TestCovertActionObjectToMap(t *testing.T) {
	cases := map[string]struct {
		action   authorization.Action
		expected map[string]interface{}
	}{
		"Good": {
			action: authorization.Action{
				Namespace: "namespace",
				Service:   "service",
				Method:    "GET",
				Path:      "/test/path",
				Properties: map[string]interface{}{
					"key1": "val1",
					"key2": "val2",
				},
			},
			expected: map[string]interface{}{
				"namespace": "namespace",
				"service":   "service",
				"method":    "GET",
				"path":      "/test/path",
				"properties": map[string]interface{}{
					"key1": "val1",
					"key2": "val2",
				},
			},
		},
	}

	for id, c := range cases {
		actual := convertActionObjectToMap(&c.action)
		if !reflect.DeepEqual(c.expected, actual) {
			t.Errorf("%v: expected: %v, received: %v", id, c.expected, actual)
		}
	}
}

func TestConvertSubjectObjectToMap(t *testing.T) {
	cases := map[string]struct {
		subject  authorization.Subject
		expected map[string]interface{}
	}{
		"Good": {
			subject: authorization.Subject{
				User:   "user",
				Groups: "groups",
				Properties: map[string]interface{}{
					"key1": "val1",
					"key2": "val2",
				},
			},
			expected: map[string]interface{}{
				"user": "user",
				"groups": []interface{}{
					"groups",
				},
				"properties": map[string]interface{}{
					"key1": "val1",
					"key2": "val2",
				},
			},
		},
	}

	for id, c := range cases {
		actual := convertSubjectObjectToMap(&c.subject)
		if !reflect.DeepEqual(c.expected, actual) {
			t.Errorf("%v: expected: %v, received: %v", id, c.expected, actual)
		}
	}
}

/*
//https://github.com/istio/istio/issues/2300
func TestValidateError(t *testing.T) {
	cases := map[string]struct {
		cfg      adapter.Config
		expected []string
	}{
		"Empty check method": {
			cfg: &config.Params{
				Policy: []string{`package mixerauthz
	    		allow = true `},
				CheckMethod: "",
			},
			expected: []string{
				"CheckMethod: check method is not configured",
			},
		},
		"Invalid policies": {
			cfg: &config.Params{
				Policy: []string{`package mixerauthz
				  +
	    		allow = true `},
				CheckMethod: "test",
			},
			expected: []string{
				"Policy: 1 error occurred: opa_policy.0:2: rego_parse_error: no match found, expected: \"#\", " +
					"\"-\", \".\", \"0\", \"[\", \"\\\"\", \"`\", \"default\", \"false\", \"import\", \"not\", " +
					"\"null\", \"package\", \"set(\", \"true\", \"{\", [ \\t\\r\\n], [ \\t], [1-9], [A-Za-z_] or EOF",
			},
		},
		"Empty policy": {
			cfg: &config.Params{
				CheckMethod: "test",
			},
			expected: []string{
				"Policy: policies are not configured",
			},
		},
	}

	info := GetInfo()

	for id, c := range cases {
		b := info.NewBuilder().(*builder)

		// OPA policy with invalid syntax
		b.SetAdapterConfig(c.cfg)

		err := b.Validate()

		if err == nil {
			t.Errorf("%v: Succeeded. Expected errors: %v", id, c.expected)
		}

		if len(c.expected) != len(err.Multi.Errors) {
			t.Errorf("%v: expected: %v, received: %v", id, c.expected, err.Multi.Errors)
		}

		for idx, msg := range c.expected {
			if msg != err.Multi.Errors[idx].Error() {
				t.Errorf("%v: expected: %v, received: %v", id, msg, err.Multi.Errors[idx])
			}
		}
	}
}
*/

func TestSinglePolicy(t *testing.T) {
	info := GetInfo()

	if !contains(info.SupportedTemplates, authorization.TemplateName) {
		t.Error("Didn't find all expected supported templates")
	}

	b := info.NewBuilder().(*builder)

	b.SetAdapterConfig(&config.Params{
		Policy: []string{`package mixerauthz
	    policy = [
	      {
	        "rule": {
	          "verbs": [
	            "storage.buckets.get"
	          ],
	          "users": [
	            "bucket-admins"
	          ]
	        }
	      }
	    ]

	    default allow = false

	    allow = true {
	      rule = policy[_].rule
	      input.subject.user = rule.users[_]
	      input.action.method = rule.verbs[_]
	    }`},
		CheckMethod: "data.mixerauthz.allow",
	})

	if err := b.Validate(); err != nil {
		t.Fatalf("Got error %v, expecting success", err)
	}

	handler, err := b.Build(context.Background(), test.NewEnv(t))
	if err != nil {
		t.Fatalf("Got error %v, expecting success", err)
	}

	cases := []struct {
		user     string
		verb     string
		expected rpc.Code
	}{
		{"bucket-admins", "storage.buckets.get", rpc.OK},
		{"bucket-admins", "storage.buckets.put", rpc.PERMISSION_DENIED},
		{"bucket-users", "storage.buckets.get", rpc.PERMISSION_DENIED},
	}

	authzHandler := handler.(authorization.Handler)

	for id, c := range cases {
		instance := authorization.Instance{
			Subject: &authorization.Subject{
				User: c.user,
			},
			Action: &authorization.Action{
				Method: c.verb,
			},
		}

		result, err := authzHandler.HandleAuthorization(context.Background(), &instance)
		if err != nil {
			t.Errorf("%v: Got error %v, expecting success", id, err)
		}

		if result.Status.Code != int32(c.expected) {
			t.Errorf("%v: Got error %v, expecting %v", id, result.Status.Code, c.expected)
		}
	}
}

/*
//https://github.com/istio/istio/issues/2300
func TestMultiplePolicy(t *testing.T) {
	info := GetInfo()

	if !contains(info.SupportedTemplates, authorization.TemplateName) {
		t.Error("Didn't find all expected supported templates")
	}

	b := info.NewBuilder().(*builder)

	b.SetAdapterConfig(&config.Params{
		Policy: []string{
			`
			package example
			import data.service_graph
			import data.org_chart

			# Deny request by default.
			default allow = false

			# Allow request if...
			allow {
			    service_graph.allow  # service graph policy allows, and...
			    org_chart.allow      # org chart policy allows.
			}
		`, `
			package org_chart

			parsed_path = p {
			    trim(input.action.path, "/", trimmed)
			    split(trimmed, "/", p)
			}

			employees = {
			    "bob": {"manager": "janet", "roles": ["engineering"]},
			    "alice": {"manager": "janet", "roles": ["engineering"]},
			    "janet": {"roles": ["engineering"]},
			    "ken": {"roles": ["hr"]},
			}

			# Allow access to non-sensitive APIs.
			allow { not is_sensitive_api }

			is_sensitive_api {
			    parsed_path[0] = "reviews"
			}

			# Allow users access to sensitive APIs serving their own data.
			allow {
			    parsed_path = ["reviews", user]
			    input.subject.user = user
			}

			# Allow managers access to sensitive APIs serving their reports' data.
			allow {
			    parsed_path = ["reviews", user]
			    input.subject.user = employees[user].manager
			}

			# Allow HR to access all APIs.
			allow {
			    is_hr
			}

			is_hr {
			    employees[input.subject.user].roles[_] = "hr"
			}
		`, `
			package service_graph

			service_graph = {
			    "landing_page": ["details", "reviews"],
			    "reviews": ["ratings"],
			}

			default allow = false

			allow {
			    #input.action.external = true
			    input.action.properties.target = "landing_page"
			}

			allow {
			    allowed_targets = service_graph[input.action.properties.source]
			    input.action.properties.target = allowed_targets[_]
			}
		`},
		CheckMethod: "data.example.allow",
	})

	if err := b.Validate(); err != nil {
		t.Fatalf("Got error %v, expecting success", err)
	}

	handler, err := b.Build(context.Background(), test.NewEnv(t))
	if err != nil {
		t.Fatalf("Got error %v, expecting success", err)
	}

	cases := []struct {
		method   string
		path     string
		target   string
		source   string
		user     string
		expected rpc.Code
	}{
		// manager
		{"GET", "/reviews/janet", "landing_page", "reviews", "janet", rpc.OK},
		{"GET", "/reviews/alice", "landing_page", "reviews", "janet", rpc.OK},
		{"GET", "/reviews/bob", "landing_page", "reviews", "janet", rpc.OK},
		{"GET", "/reviews/ken", "landing_page", "reviews", "janet", rpc.PERMISSION_DENIED},
		// self
		{"GET", "/reviews/alice", "landing_page", "reviews", "alice", rpc.OK},
		{"GET", "/reviews/janet", "landing_page", "reviews", "alice", rpc.PERMISSION_DENIED},
		{"GET", "/reviews/bob", "landing_page", "reviews", "alice", rpc.PERMISSION_DENIED},
		{"GET", "/reviews/ken", "landing_page", "reviews", "alice", rpc.PERMISSION_DENIED},
		// hr
		{"GET", "/reviews/janet", "landing_page", "reviews", "ken", rpc.OK},
		{"GET", "/reviews/alice", "landing_page", "reviews", "ken", rpc.OK},
		{"GET", "/reviews/bob", "landing_page", "reviews", "ken", rpc.OK},
		{"GET", "/reviews/ken", "landing_page", "reviews", "ken", rpc.OK},
		// service
		{"GET", "/reviews/janet", "landing_page", "review", "janet", rpc.OK},
		{"GET", "/reviews/janet", "source_page", "reviews", "janet", rpc.PERMISSION_DENIED},
	}

	authzHandler := handler.(authorization.Handler)

	for _, c := range cases {

		instance := authorization.Instance{
			Subject: &authorization.Subject{
				User: c.user,
			},
			Action: &authorization.Action{
				Method: c.method,
				Path:   c.path,
				Properties: map[string]interface{}{
					"source": c.source,
					"target": c.target,
				},
			},
		}

		result, err := authzHandler.HandleAuthorization(context.Background(), &instance)
		if err != nil {
			t.Errorf("Got error %v, expecting success", err)
		}

		if result.Status.Code != int32(c.expected) {
			t.Errorf("Got status %v, expecting %v", result.Status.Code, c.expected)
		}
	}
}
*/

func TestFailOpenClose(t *testing.T) {
	cases := []struct {
		policy    string
		failClose bool
		expected  rpc.Code
	}{
		{"bad policy", true, rpc.PERMISSION_DENIED},
	}

	info := GetInfo()

	if !contains(info.SupportedTemplates, authorization.TemplateName) {
		t.Error("Didn't find all expected supported templates")
	}

	for _, c := range cases {
		b := info.NewBuilder().(*builder)

		// OPA policy with invalid syntax
		b.SetAdapterConfig(&config.Params{
			Policy:      []string{c.policy},
			CheckMethod: "data.mixerauthz.allow",
			FailClose:   c.failClose,
		})

		if err := b.Validate(); err != nil {
			t.Errorf("Got error %v, expecting success", err)
		}

		handler, err := b.Build(context.Background(), test.NewEnv(t))
		if err != nil {
			t.Fatalf("Got error %v, expecting success", err)
		}

		instance := authorization.Instance{
			Subject: &authorization.Subject{},
			Action:  &authorization.Action{},
		}

		authzHandler := handler.(authorization.Handler)

		result, err := authzHandler.HandleAuthorization(context.Background(), &instance)
		if err != nil {
			t.Errorf("Got error %v, expecting success", err)
		}

		if result.Status.Code != int32(c.expected) {
			t.Errorf("Got error %v, expecting success", err)
		}
	}
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}
