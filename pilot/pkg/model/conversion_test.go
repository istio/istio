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

package model_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/davecgh/go-spew/spew"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/protomarshal"
)

func TestApplyJSON(t *testing.T) {
	cases := []struct {
		in      string
		want    *meshconfig.MeshConfig
		wantErr bool
	}{
		{
			in:   `{"enableTracing": true}`,
			want: &meshconfig.MeshConfig{EnableTracing: true},
		},
		{
			in:   `{"enableTracing": true, "unknownField": "unknownValue"}`,
			want: &meshconfig.MeshConfig{EnableTracing: true},
		},
	}
	for i, c := range cases {
		t.Run(fmt.Sprintf("[%v]", i), func(tt *testing.T) {
			var got meshconfig.MeshConfig
			err := protomarshal.ApplyJSON(c.in, &got)
			if err != nil {
				if !c.wantErr {
					tt.Fatalf("got unexpected error: %v", err)
				}
			} else {
				if c.wantErr {
					tt.Fatal("unexpected success, expected error")
				}
				assert.Equal(t, &got, c.want)
			}
		})
	}
}

func TestProtoSchemaConversions(t *testing.T) {
	destinationRuleSchema := collections.IstioNetworkingV1Alpha3Destinationrules

	msg := &networking.DestinationRule{
		Host: "something.svc.local",
		TrafficPolicy: &networking.TrafficPolicy{
			LoadBalancer: &networking.LoadBalancerSettings{
				LbPolicy: &networking.LoadBalancerSettings_Simple{},
			},
		},
		Subsets: []*networking.Subset{
			{
				Name: "foo",
				Labels: map[string]string{
					"test": "label",
				},
			},
		},
	}

	wantJSON := `
		{
      "host":"something.svc.local",
      "trafficPolicy": {
        "loadBalancer":{"simple":"UNSPECIFIED"}
       },
       "subsets": [
         {"name":"foo","labels":{"test":"label"}}
       ]
		}`

	wantYAML := `host: something.svc.local
subsets:
- labels:
    test: label
  name: foo
trafficPolicy:
  loadBalancer:
    simple: UNSPECIFIED
`

	wantJSONMap := map[string]interface{}{
		"host": "something.svc.local",
		"trafficPolicy": map[string]interface{}{
			"loadBalancer": map[string]interface{}{
				"simple": "UNSPECIFIED",
			},
		},
		"subsets": []interface{}{
			map[string]interface{}{
				"name": "foo",
				"labels": map[string]interface{}{
					"test": "label",
				},
			},
		},
	}

	badSchema := schemaFor("bad", "bad-name")
	if _, err := crd.FromYAML(badSchema, wantYAML); err == nil {
		t.Errorf("FromYAML should have failed using Schema with bad MessageName")
	}

	gotJSON, err := protomarshal.ToJSON(msg)
	if err != nil {
		t.Errorf("ToJSON failed: %v", err)
	}
	if gotJSON != strings.Join(strings.Fields(wantJSON), "") {
		t.Errorf("ToJSON failed: got %s, want %s", gotJSON, wantJSON)
	}

	if _, err = protomarshal.ToJSON(nil); err == nil {
		t.Error("should produce an error")
	}

	gotFromJSON, err := crd.FromJSON(destinationRuleSchema, wantJSON)
	if err != nil {
		t.Errorf("FromJSON failed: %v", err)
	}
	if !reflect.DeepEqual(gotFromJSON, msg) {
		t.Errorf("FromYAML failed: got %+v want %+v", spew.Sdump(gotFromJSON), spew.Sdump(msg))
	}

	gotYAML, err := protomarshal.ToYAML(msg)
	if err != nil {
		t.Errorf("ToYAML failed: %v", err)
	}
	if !reflect.DeepEqual(gotYAML, wantYAML) {
		t.Errorf("ToYAML failed: got %+v want %+v", spew.Sdump(gotYAML), spew.Sdump(wantYAML))
	}

	if _, err = protomarshal.ToYAML(nil); err == nil {
		t.Error("should produce an error")
	}

	gotFromYAML, err := crd.FromYAML(destinationRuleSchema, wantYAML)
	if err != nil {
		t.Errorf("FromYAML failed: %v", err)
	}
	if !reflect.DeepEqual(gotFromYAML, msg) {
		t.Errorf("FromYAML failed: got %+v want %+v", spew.Sdump(gotFromYAML), spew.Sdump(msg))
	}

	if _, err = crd.FromYAML(destinationRuleSchema, ":"); err == nil {
		t.Errorf("should produce an error")
	}

	gotJSONMap, err := protomarshal.ToJSONMap(msg)
	if err != nil {
		t.Errorf("ToJSONMap failed: %v", err)
	}
	if !reflect.DeepEqual(gotJSONMap, wantJSONMap) {
		t.Errorf("ToJSONMap failed: \ngot %vwant %v", spew.Sdump(gotJSONMap), spew.Sdump(wantJSONMap))
	}

	if _, err = protomarshal.ToJSONMap(nil); err == nil {
		t.Error("should produce an error")
	}

	gotFromJSONMap, err := crd.FromJSONMap(destinationRuleSchema, wantJSONMap)
	if err != nil {
		t.Errorf("FromJSONMap failed: %v", err)
	}
	if !reflect.DeepEqual(gotFromJSONMap, msg) {
		t.Errorf("FromJSONMap failed: got %+v want %+v", spew.Sdump(gotFromJSONMap), spew.Sdump(msg))
	}

	if _, err = crd.FromJSONMap(destinationRuleSchema, 1); err == nil {
		t.Error("should produce an error")
	}
	if _, err = crd.FromJSONMap(destinationRuleSchema, ":"); err == nil {
		t.Errorf("should produce an error")
	}
}
