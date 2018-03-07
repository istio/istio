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

package e2e

import (
	"context"
	"reflect"
	"testing"

	"google.golang.org/grpc"

	istio_mixer_v1 "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/pkg/config/storetest"
	testEnv "istio.io/istio/mixer/pkg/server"
	spyAdapter "istio.io/istio/mixer/test/spyAdapter"
	e2eTmpl "istio.io/istio/mixer/test/spyAdapter/template"
)

const (
	refTrackingGlobalCfg = `
apiVersion: "config.istio.io/v1alpha2"
kind: attributemanifest
metadata:
  name: istio-proxy
  namespace: default
spec:
    attributes:
      destination.service:
        value_type: STRING
      source.service:
        value_type: STRING
      request.headers:
        value_type: STRING_MAP
---
`
	refTrackingSvcCfg = `
apiVersion: "config.istio.io/v1alpha2"
kind: fakeHandler
metadata:
  name: fakeHandlerConfig
  namespace: istio-system

---

apiVersion: "config.istio.io/v1alpha2"
kind: samplecheck
metadata:
  name: checkInstance
  namespace: istio-system
spec:
  stringPrimitive: "destination.service"
---

apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rule1
  namespace: istio-system
spec:
  match: destination.service == "echosrv1.istio.svc.cluster.local" && request.headers["x-request-id"] == "foo"
  actions:
  - handler: fakeHandlerConfig.fakeHandler
    instances:
    - checkInstance.samplecheck

---
`
)

// TestRefTracking is an end-to-end test for ensuring that we do appropriate short-circuiting and attribute
// reference tracking on certain circumstances.
// When an expression with a logical operator is evaluated, the current semantics of our expression language
// short-circuits the right-hand-side. This means, any attributes that are appearing on the RHS should *not*
// be referenced. This test captures the behavior.
func TestRefTracking(t *testing.T) {
	tests := []testData{
		{
			name:      "RefTracking",
			cfg:       refTrackingSvcCfg,
			behaviors: []spyAdapter.AdapterBehavior{{Name: "fakeHandler"}},
			templates: e2eTmpl.SupportedTmplInfo,
			attrs: map[string]interface{}{
				"destination.service": "echosrv2.istio.svc.cluster.local",
				"source.service":      "foo",
				"request.headers": map[string]interface{}{
					"x-request-id": "foo",
				},
			},
			validate: func(t *testing.T, err error, spyAdpts []*spyAdapter.Adapter) {
				adptr := spyAdpts[0]

				if adptr.HandlerData.HandleSampleCheckInstance != nil {
					t.Fatalf("Unexpected instance encountered: %v", adptr.HandlerData.HandleSampleCheckInstance)
				}
			},
			validateCheckResponse: func(t *testing.T, response *istio_mixer_v1.CheckResponse, err error) {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}

				// Expect destination.service (and context.protocol) to be referenced, but not request.headers, or source.service)
				// due to short-circuiting of the logical and.

				// Ordering can happen either way.
				expected1 := istio_mixer_v1.ReferencedAttributes{
					Words: []string{"context.protocol", "destination.service"},
					AttributeMatches: []istio_mixer_v1.ReferencedAttributes_AttributeMatch{
						{
							Name:      -1,
							Condition: istio_mixer_v1.ABSENCE,
							MapKey:    0,
						},
						{
							Name:      -2,
							Condition: istio_mixer_v1.EXACT,
							MapKey:    0,
						},
					},
				}

				expected2 := istio_mixer_v1.ReferencedAttributes{
					Words: []string{"destination.service", "context.protocol"},
					AttributeMatches: []istio_mixer_v1.ReferencedAttributes_AttributeMatch{
						{
							Name:      -1,
							Condition: istio_mixer_v1.EXACT,
							MapKey:    0,
						},
						{
							Name:      -2,
							Condition: istio_mixer_v1.ABSENCE,
							MapKey:    0,
						},
					},
				}

				actual := response.Precondition.ReferencedAttributes
				if !reflect.DeepEqual(expected1, actual) && !reflect.DeepEqual(expected2, actual) {
					t.Fatalf("got: \n%v\nwanted:\n%v\nor\n%v\n", actual, expected1, expected2)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapterInfos, spyAdapters := ConstructAdapterInfos(tt.behaviors)

			args := testEnv.DefaultArgs()
			args.APIPort = 0
			args.MonitoringPort = 0
			args.Templates = tt.templates
			args.Adapters = adapterInfos
			var cerr error
			if args.ConfigStore, cerr = storetest.SetupStoreForTest(refTrackingGlobalCfg, tt.cfg); cerr != nil {
				t.Fatal(cerr)
			}

			env, err := testEnv.New(args)
			if err != nil {
				t.Fatalf("fail to create mixer: %v", err)
			}

			env.Run()

			defer closeHelper(env)

			conn, err := grpc.Dial(env.Addr().String(), grpc.WithInsecure())
			if err != nil {
				t.Fatalf("Unable to connect to gRPC server: %v", err)
			}

			client := istio_mixer_v1.NewMixerClient(conn)
			defer closeHelper(conn)

			req := istio_mixer_v1.CheckRequest{
				Attributes: getAttrBag(tt.attrs,
					args.ConfigIdentityAttribute,
					args.ConfigIdentityAttributeDomain),
			}
			tt.validate(t, err, spyAdapters)
			response, err := client.Check(context.Background(), &req)
			if tt.validateCheckResponse != nil {
				tt.validateCheckResponse(t, response, err)
			}
		})
	}
}
