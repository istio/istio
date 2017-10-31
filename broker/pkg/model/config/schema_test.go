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

package config

import (
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"

	brokerconfig "istio.io/api/broker/v1/config"
)

func TestProtoSchemaConversions(t *testing.T) {
	s := &Schema{MessageName: ServiceClass.MessageName}

	msg := &brokerconfig.ServiceClass{
		Deployment: &brokerconfig.Deployment{
			Instance: "productpage",
		},
		Entry: &brokerconfig.CatalogEntry{
			Name:        "istio-bookinfo-productpage",
			Id:          "4395a443-f49a-41b0-8d14-d17294cf612f",
			Description: "A book info service",
		},
	}

	wantYAML := "deployment:\n" +
		"  instance: productpage\n" +
		"entry:\n" +
		"  description: A book info service\n" +
		"  id: 4395a443-f49a-41b0-8d14-d17294cf612f\n" +
		"  name: istio-bookinfo-productpage\n"

	wantJSONMap := map[string]interface{}{
		"deployment": map[string]interface{}{
			"instance": "productpage",
		},
		"entry": map[string]interface{}{
			"name":        "istio-bookinfo-productpage",
			"id":          "4395a443-f49a-41b0-8d14-d17294cf612f",
			"description": "A book info service",
		},
	}

	badSchema := &Schema{MessageName: "bad-name"}
	if _, err := badSchema.fromYAML(wantYAML); err == nil {
		t.Errorf("FromYAML should have failed using ProtoSchema with bad MessageName")
	}

	gotYAML, err := s.toYAML(msg)
	if err != nil {
		t.Errorf("ToYAML failed: %v", err)
	}
	if !reflect.DeepEqual(gotYAML, wantYAML) {
		t.Errorf("ToYAML failed: got %+v want %+v", spew.Sdump(gotYAML), spew.Sdump(wantYAML))
	}
	gotFromYAML, err := s.fromYAML(wantYAML)
	if err != nil {
		t.Errorf("FromYAML failed: %v", err)
	}
	if !reflect.DeepEqual(gotFromYAML, msg) {
		t.Errorf("FromYAML failed: got %+v want %+v", spew.Sdump(gotFromYAML), spew.Sdump(msg))
	}

	gotJSONMap, err := s.ToJSONMap(msg)
	if err != nil {
		t.Errorf("ToJSONMap failed: %v", err)
	}
	if !reflect.DeepEqual(gotJSONMap, wantJSONMap) {
		t.Errorf("ToJSONMap failed: \ngot %vwant %v", spew.Sdump(gotJSONMap), spew.Sdump(wantJSONMap))
	}
	gotFromJSONMap, err := s.FromJSONMap(wantJSONMap)
	if err != nil {
		t.Errorf("FromJSONMap failed: %v", err)
	}
	if !reflect.DeepEqual(gotFromJSONMap, msg) {
		t.Errorf("FromJSONMap failed: got %+v want %+v", spew.Sdump(gotFromJSONMap), spew.Sdump(msg))
	}
}

func TestConfigDescriptor(t *testing.T) {
	a := Schema{Type: "a", MessageName: "proxy.A"}
	descriptor := Descriptor{
		a,
		Schema{Type: "b", MessageName: "proxy.B"},
		Schema{Type: "c", MessageName: "proxy.C"},
	}
	want := []string{"a", "b", "c"}
	got := descriptor.Types()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("descriptor.Types() => got %+vwant %+v", spew.Sdump(got), spew.Sdump(want))
	}

	aType, aExists := descriptor.GetByType(a.Type)
	if !aExists || !reflect.DeepEqual(aType, a) {
		t.Errorf("descriptor.GetByType(a) => got %+v, want %+v", aType, a)
	}

	aSchema, aSchemaExists := descriptor.GetByMessageName(a.MessageName)
	if !aSchemaExists || !reflect.DeepEqual(aSchema, a) {
		t.Errorf("descriptor.GetByMessageName(a) => got %+v, want %+v", aType, a)
	}
	_, aSchemaNotExist := descriptor.GetByMessageName("blah")
	if aSchemaNotExist {
		t.Errorf("descriptor.GetByMessageName(blah) => got true, want false")
	}
}
