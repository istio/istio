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

package schemas_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/gogo/protobuf/proto"

	meshAPI "istio.io/api/mesh/v1alpha1"
	mixerAPI "istio.io/api/mixer/v1"
	mixerClientAPI "istio.io/api/mixer/v1/config/client"
	networkingAPI "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/config/schemas"
	"istio.io/istio/pkg/test/config"
	"istio.io/istio/pkg/util/gogoprotomarshal"
)

const (
	// Config name for testing
	someName = "foo"
	// Config namespace for testing.
	someNamespace = "bar"
)

func TestApplyJSON(t *testing.T) {
	cases := []struct {
		in      string
		want    *meshAPI.MeshConfig
		wantErr bool
	}{
		{
			in:   `{"enableTracing": true}`,
			want: &meshAPI.MeshConfig{EnableTracing: true},
		},
		{
			in:   `{"enableTracing": true, "unknownField": "unknownValue"}`,
			want: &meshAPI.MeshConfig{EnableTracing: true},
		},
	}
	for i, c := range cases {
		t.Run(fmt.Sprintf("[%v]", i), func(tt *testing.T) {
			var got meshAPI.MeshConfig
			err := gogoprotomarshal.ApplyJSON(c.in, &got)
			if err != nil {
				if !c.wantErr {
					tt.Fatalf("got unexpected error: %v", err)
				}
			} else {
				if c.wantErr {
					tt.Fatal("unexpected success, expected error")
				}
				if !reflect.DeepEqual(&got, c.want) {
					tt.Fatalf("ApplyJSON(%v): got %v want %v", c.in, &got, c.want)
				}
			}
		})
	}
}

func TestGogoInstanceConversions(t *testing.T) {
	msg := &mixerClientAPI.HTTPAPISpec{
		Attributes: &mixerAPI.Attributes{
			Attributes: map[string]*mixerAPI.Attributes_AttributeValue{
				"api.service": {
					Value: &mixerAPI.Attributes_AttributeValue_StringValue{
						StringValue: "my-service",
					},
				},
				"api.version": {
					Value: &mixerAPI.Attributes_AttributeValue_StringValue{
						StringValue: "1.0.0",
					},
				},
			},
		},
		Patterns: []*mixerClientAPI.HTTPAPISpecPattern{
			{
				Attributes: &mixerAPI.Attributes{
					Attributes: map[string]*mixerAPI.Attributes_AttributeValue{
						"api.operation": {
							Value: &mixerAPI.Attributes_AttributeValue_StringValue{
								StringValue: "createPet",
							},
						},
					},
				},
				HttpMethod: "POST",
				Pattern: &mixerClientAPI.HTTPAPISpecPattern_UriTemplate{
					UriTemplate: "/pet/{id}",
				},
			},
		},
		ApiKeys: []*mixerClientAPI.APIKey{
			{
				Key: &mixerClientAPI.APIKey_Query{
					Query: "api_key",
				},
			},
		},
	}

	wantYAML := `apiKeys:
- query: api_key
attributes:
  attributes:
    api.service:
      stringValue: my-service
    api.version:
      stringValue: 1.0.0
patterns:
- attributes:
    attributes:
      api.operation:
        stringValue: createPet
  httpMethod: POST
  uriTemplate: /pet/{id}
`

	wantJSON := `
{
   "attributes": {
      "attributes": {
         "api.service": {
            "stringValue": "my-service"
         },
         "api.version": {
            "stringValue": "1.0.0"
         }
      }
   },
   "patterns": [
      {
         "attributes": {
            "attributes": {
               "api.operation": {
                  "stringValue": "createPet"
               }
            }
         },
         "httpMethod": "POST",
         "uriTemplate": "/pet/{id}"
      }
   ],
   "apiKeys": [
      {
         "query": "api_key"
      }
   ]
}
`
	wantJSONMap := map[string]interface{}{
		"attributes": map[string]interface{}{
			"attributes": map[string]interface{}{
				"api.service": map[string]interface{}{
					"stringValue": "my-service",
				},
				"api.version": map[string]interface{}{
					"stringValue": "1.0.0",
				},
			},
		},
		"patterns": []interface{}{
			map[string]interface{}{
				"httpMethod":  "POST",
				"uriTemplate": "/pet/{id}",
				"attributes": map[string]interface{}{
					"attributes": map[string]interface{}{
						"api.operation": map[string]interface{}{
							"stringValue": "createPet",
						},
					},
				},
			},
		},
		"apiKeys": []interface{}{
			map[string]interface{}{
				"query": "api_key",
			},
		},
	}

	gotJSON, err := gogoprotomarshal.ToJSON(msg)
	if err != nil {
		t.Errorf("ToJSON failed: %v", err)
	}
	if gotJSON != strings.Join(strings.Fields(wantJSON), "") {
		t.Errorf("ToJSON failed: \ngot %s, \nwant %s", gotJSON, strings.Join(strings.Fields(wantJSON), ""))
	}

	if _, err = gogoprotomarshal.ToJSON(nil); err == nil {
		t.Error("should produce an error")
	}

	gotFromJSON, err := schemas.HTTPAPISpec.FromJSON(wantJSON)
	if err != nil {
		t.Errorf("FromJSON failed: %v", err)
	}
	if !reflect.DeepEqual(gotFromJSON, msg) {
		t.Errorf("FromYAML failed: got %+v want %+v", spew.Sdump(gotFromJSON), spew.Sdump(msg))
	}

	gotYAML, err := gogoprotomarshal.ToYAML(msg)
	if err != nil {
		t.Errorf("ToYAML failed: %v", err)
	}
	if !reflect.DeepEqual(gotYAML, wantYAML) {
		t.Errorf("ToYAML failed: \ngot %+v \nwant %+v", spew.Sdump(gotYAML), spew.Sdump(wantYAML))
	}

	if _, err = gogoprotomarshal.ToYAML(nil); err == nil {
		t.Error("should produce an error")
	}

	gotFromYAML, err := schemas.HTTPAPISpec.FromYAML(wantYAML)
	if err != nil {
		t.Errorf("FromYAML failed: %v", err)
	}
	if !reflect.DeepEqual(gotFromYAML, msg) {
		t.Errorf("FromYAML failed: got %+v want %+v", spew.Sdump(gotFromYAML), spew.Sdump(msg))
	}

	if _, err = schemas.HTTPAPISpec.FromYAML(":"); err == nil {
		t.Errorf("should produce an error")
	}

	gotJSONMap, err := gogoprotomarshal.ToJSONMap(msg)
	if err != nil {
		t.Errorf("ToJSONMap failed: %v", err)
	}
	if !reflect.DeepEqual(gotJSONMap, wantJSONMap) {
		t.Errorf("ToJSONMap failed: \ngot %vwant %v", spew.Sdump(gotJSONMap), spew.Sdump(wantJSONMap))
	}

	if _, err = gogoprotomarshal.ToJSONMap(nil); err == nil {
		t.Error("should produce an error")
	}

	gotFromJSONMap, err := schemas.HTTPAPISpec.FromJSONMap(wantJSONMap)
	if err != nil {
		t.Errorf("FromJSONMap failed: %v", err)
	}
	if !reflect.DeepEqual(gotFromJSONMap, msg) {
		t.Errorf("FromJSONMap failed: got %+v want %+v", spew.Sdump(gotFromJSONMap), spew.Sdump(msg))
	}

	if _, err = schemas.HTTPAPISpec.FromJSONMap(1); err == nil {
		t.Error("should produce an error")
	}
	if _, err = schemas.HTTPAPISpec.FromJSON(":"); err == nil {
		t.Errorf("should produce an error")
	}
}

func TestInstanceConversions(t *testing.T) {
	destinationRuleSchema := &schema.Instance{MessageName: schemas.DestinationRule.MessageName}

	msg := &networkingAPI.DestinationRule{
		Host: "something.svc.local",
		TrafficPolicy: &networkingAPI.TrafficPolicy{
			LoadBalancer: &networkingAPI.LoadBalancerSettings{
				LbPolicy: &networkingAPI.LoadBalancerSettings_Simple{},
			},
		},
		Subsets: []*networkingAPI.Subset{
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
        "loadBalancer":{"simple":"ROUND_ROBIN"}
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
    simple: ROUND_ROBIN
`

	wantJSONMap := map[string]interface{}{
		"host": "something.svc.local",
		"trafficPolicy": map[string]interface{}{
			"loadBalancer": map[string]interface{}{
				"simple": "ROUND_ROBIN",
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

	badSchema := &schema.Instance{MessageName: "bad-name"}
	if _, err := badSchema.FromYAML(wantYAML); err == nil {
		t.Errorf("FromYAML should have failed using Schema with bad MessageName")
	}

	gotJSON, err := gogoprotomarshal.ToJSON(msg)
	if err != nil {
		t.Errorf("ToJSON failed: %v", err)
	}
	if gotJSON != strings.Join(strings.Fields(wantJSON), "") {
		t.Errorf("ToJSON failed: got %s, want %s", gotJSON, wantJSON)
	}

	if _, err = gogoprotomarshal.ToJSON(nil); err == nil {
		t.Error("should produce an error")
	}

	gotFromJSON, err := destinationRuleSchema.FromJSON(wantJSON)
	if err != nil {
		t.Errorf("FromJSON failed: %v", err)
	}
	if !reflect.DeepEqual(gotFromJSON, msg) {
		t.Errorf("FromYAML failed: got %+v want %+v", spew.Sdump(gotFromJSON), spew.Sdump(msg))
	}

	gotYAML, err := gogoprotomarshal.ToYAML(msg)
	if err != nil {
		t.Errorf("ToYAML failed: %v", err)
	}
	if !reflect.DeepEqual(gotYAML, wantYAML) {
		t.Errorf("ToYAML failed: got %+v want %+v", spew.Sdump(gotYAML), spew.Sdump(wantYAML))
	}

	if _, err = gogoprotomarshal.ToYAML(nil); err == nil {
		t.Error("should produce an error")
	}

	gotFromYAML, err := destinationRuleSchema.FromYAML(wantYAML)
	if err != nil {
		t.Errorf("FromYAML failed: %v", err)
	}
	if !reflect.DeepEqual(gotFromYAML, msg) {
		t.Errorf("FromYAML failed: got %+v want %+v", spew.Sdump(gotFromYAML), spew.Sdump(msg))
	}

	if _, err = destinationRuleSchema.FromYAML(":"); err == nil {
		t.Errorf("should produce an error")
	}

	gotJSONMap, err := gogoprotomarshal.ToJSONMap(msg)
	if err != nil {
		t.Errorf("ToJSONMap failed: %v", err)
	}
	if !reflect.DeepEqual(gotJSONMap, wantJSONMap) {
		t.Errorf("ToJSONMap failed: \ngot %vwant %v", spew.Sdump(gotJSONMap), spew.Sdump(wantJSONMap))
	}

	if _, err = gogoprotomarshal.ToJSONMap(nil); err == nil {
		t.Error("should produce an error")
	}

	gotFromJSONMap, err := destinationRuleSchema.FromJSONMap(wantJSONMap)
	if err != nil {
		t.Errorf("FromJSONMap failed: %v", err)
	}
	if !reflect.DeepEqual(gotFromJSONMap, msg) {
		t.Errorf("FromJSONMap failed: got %+v want %+v", spew.Sdump(gotFromJSONMap), spew.Sdump(msg))
	}

	if _, err = destinationRuleSchema.FromJSONMap(1); err == nil {
		t.Error("should produce an error")
	}
	if _, err = destinationRuleSchema.FromJSON(":"); err == nil {
		t.Errorf("should produce an error")
	}
}

func TestSetValidate(t *testing.T) {
	badLabel := strings.Repeat("a", labels.DNS1123LabelMaxLength+1)
	goodLabel := strings.Repeat("a", labels.DNS1123LabelMaxLength-1)

	cases := []struct {
		name       string
		descriptor schema.Set
		wantErr    bool
	}{{
		name:       "Valid ConfigDescriptor (IstioConfig)",
		descriptor: schemas.Istio,
		wantErr:    false,
	}, {
		name: "Invalid DNS11234Label in ConfigDescriptor",
		descriptor: schema.Set{schema.Instance{
			Type:        badLabel,
			MessageName: "istio.networking.v1alpha3.Gateway",
		}},
		wantErr: true,
	}, {
		name: "Bad MessageName in ProtoMessage",
		descriptor: schema.Set{schema.Instance{
			Type:        goodLabel,
			MessageName: "nonexistent",
		}},
		wantErr: true,
	}, {
		name: "Missing key function",
		descriptor: schema.Set{schema.Instance{
			Type:        "service-entry",
			MessageName: "istio.networking.v1alpha3.ServiceEtrny",
		}},
		wantErr: true,
	}, {
		name:       "Duplicate type and message",
		descriptor: schema.Set{schemas.DestinationRule, schemas.DestinationRule},
		wantErr:    true,
	}}

	for _, c := range cases {
		if err := c.descriptor.Validate(); (err != nil) != c.wantErr {
			t.Errorf("%v failed: got %v but wantErr=%v", c.name, err, c.wantErr)
		}
	}
}

// ValidateConfig ensures that the config object is well-defined
// TODO: also check name and namespace
func setValidateConfig(descriptor schema.Set, typ string, obj interface{}) error {
	if obj == nil {
		return fmt.Errorf("invalid nil configuration object")
	}

	t, ok := descriptor.GetByType(typ)
	if !ok {
		return fmt.Errorf("undeclared type: %q", typ)
	}

	v, ok := obj.(proto.Message)
	if !ok {
		return fmt.Errorf("cannot cast to a proto message")
	}

	if proto.MessageName(v) != t.MessageName {
		return fmt.Errorf("mismatched message type %q and type %q",
			proto.MessageName(v), t.MessageName)
	}
	return t.Validate(someName, someNamespace, v)
}

func TestSetValidateConfig(t *testing.T) {
	cases := []struct {
		name    string
		typ     string
		config  interface{}
		wantErr bool
	}{
		{
			name:    "bad configuration object",
			typ:     "policy",
			config:  nil,
			wantErr: true,
		},
		{
			name:    "undeclared kind",
			typ:     "special-type",
			config:  nil,
			wantErr: true,
		},
		{
			name:    "non-proto object configuration",
			typ:     "policy",
			config:  "non-proto objection configuration",
			wantErr: true,
		},
		{
			name:    "message type and kind mismatch",
			typ:     "policy",
			config:  schemas.ServiceEntry,
			wantErr: true,
		},
		{
			name:    "Schema validation1",
			typ:     "service-entry",
			config:  schemas.ServiceEntry,
			wantErr: true,
		},
		{
			name:    "Successful validation",
			typ:     schemas.MockConfig.Type,
			config:  &config.MockConfig{Key: "test"},
			wantErr: false,
		},
	}

	types := append(schemas.Istio, schemas.MockConfig)

	for _, c := range cases {
		if err := setValidateConfig(types, c.typ, c.config); (err != nil) != c.wantErr {
			t.Errorf("%v failed: got error=%v but wantErr=%v", c.name, err, c.wantErr)
		}
	}
}
