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

package opa

import (
	"context"
	"testing"

	"google.golang.org/grpc"

	api_mixer_v1 "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/pkg/adapter"
	attr "istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/config/storetest"
	"istio.io/istio/mixer/pkg/server"
	"istio.io/istio/mixer/template"
	"istio.io/pkg/attribute"
)

const (
	globalCfg = `
apiVersion: "config.istio.io/v1alpha2"
kind: attributemanifest
metadata:
  name: istio-proxy
  namespace: default
spec:
  attributes:
    source.uid:
      value_type: STRING
    source.groups:
      value_type: STRING
    destination.namespace:
      value_type: STRING
    destination.service:
      value_type: STRING
    request.method:
      value_type: STRING
    request.path:
      value_type: STRING
    source.service:
      value_type: STRING
    destination.service:
      value_type: STRING

---
`
	serviceCfg = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: opa_authorization
  namespace: istio-system
spec:
  match: "true"
  actions:
  - handler: opaHandler.opa
    instances:
    - authzInstance.authorization

---

apiVersion: "config.istio.io/v1alpha2"
kind: authorization
metadata:
  name: authzInstance
  namespace: istio-system
spec:
  subject:
    user: source.uid | ""
    groups: source.groups | ""
  action:
    namespace: destination.namespace | "default"
    service: destination.service | ""
    method: request.method | ""
    path: request.path | ""
    properties:
      source: source.service | ""
      target: destination.service | ""
---

apiVersion: "config.istio.io/v1alpha2"
kind: opa
metadata:
  name: opaHandler
  namespace: istio-system
spec:
  policy:
    - |+
      package mixerauthz
      import data.service_graph
      import data.org_chart

      # Deny request by default.
      default allow = false

      # Allow request if...
      allow {
          service_graph.allow  # service graph policy allows, and...
          org_chart.allow      # org chart policy allows.
      }
    - |+
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
    - |+
      package service_graph

      service_graph = {
          "landing_page": ["details", "reviews"],
          "reviews": ["ratings"],
      }

      default allow = false

      allow {
          input.action.properties.target = "landing_page"
      }

      allow {
          allowed_targets = service_graph[input.action.properties.source]
          input.action.properties.target = allowed_targets[_]
      }
  checkMethod: "data.mixerauthz.allow"
  failClose: true

---
`
)

func TestServer(t *testing.T) {
	args := server.DefaultArgs()

	args.APIPort = 0
	args.MonitoringPort = 0
	args.Templates = template.SupportedTmplInfo
	args.Adapters = []adapter.InfoFn{
		GetInfo,
	}
	var cerr error
	if args.ConfigStore, cerr = storetest.SetupStoreForTest(globalCfg, serviceCfg); cerr != nil {
		t.Fatal(cerr)
	}

	mixerServer, err := server.New(args)
	if err != nil {
		t.Fatalf("Unable to create server: %v", err)
	}

	mixerServer.Run()

	conn, err := grpc.Dial(mixerServer.Addr().String(), grpc.WithInsecure())
	if err != nil {
		t.Errorf("Creating client failed: %v", err)
	}
	mixerClient := api_mixer_v1.NewMixerClient(conn)

	cases := map[string]struct {
		attrs              map[string]interface{}
		expectedStatusCode int32
	}{
		"Not important API": {
			attrs: map[string]interface{}{
				"source.uid":          "janet",
				"request.path":        "/detail/alice",
				"destination.service": "landing_page",
				"source.service":      "details",
			},
			expectedStatusCode: 0,
		},
		"Self permission": {
			attrs: map[string]interface{}{
				"source.uid":          "janet",
				"request.path":        "/detail/janet",
				"destination.service": "landing_page",
				"source.service":      "details",
			},
			expectedStatusCode: 0,
		},
		"Manager permission": {
			attrs: map[string]interface{}{
				"source.uid":          "janet",
				"request.path":        "/reviews/alice",
				"destination.service": "landing_page",
				"source.service":      "details",
			},
			expectedStatusCode: 0,
		},
		"HR permission": {
			attrs: map[string]interface{}{
				"source.uid":          "ken",
				"request.path":        "/reviews/janet",
				"destination.service": "landing_page",
				"source.service":      "details",
			},
			expectedStatusCode: 0,
		},
		"Denied request": {
			attrs: map[string]interface{}{
				"source.uid":          "janet",
				"request.path":        "/detail/ken",
				"destination.service": "landing_pages",
				"source.service":      "invalid",
			},
			expectedStatusCode: 7,
		},
	}

	for id, c := range cases {
		requestBag := attribute.GetMutableBag(nil)

		for k, v := range c.attrs {
			requestBag.Set(k, v)
		}
		var attrProto api_mixer_v1.CompressedAttributes
		attr.ToProto(requestBag, &attrProto, nil, 0)

		req := &api_mixer_v1.CheckRequest{
			Attributes: attrProto,
		}

		res, err := mixerClient.Check(context.Background(), req)

		if err != nil {
			t.Errorf("%v: Got error during Check: %v", id, err)
		}

		if res.Precondition.Status.Code != c.expectedStatusCode {
			t.Errorf("%v: Expected: %v, Got: %v", id, c.expectedStatusCode, res.Precondition.Status.Code)
		}
	}
}
