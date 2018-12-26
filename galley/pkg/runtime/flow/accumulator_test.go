//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package flow

import (
	"fmt"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"

	"istio.io/istio/galley/pkg/runtime/resource"
)

var schema *resource.Schema
var emptyInfo resource.Info
var structInfo resource.Info

func init() {
	b := resource.NewSchemaBuilder()
	emptyInfo = b.Register("type.googleapis.com/google.protobuf.Empty")
	structInfo = b.Register("type.googleapis.com/google.protobuf.Struct")
	schema = b.Build()
}

func addRes1V1() resource.Event {
	return resource.Event{
		Kind: resource.Added,
		Entry: resource.Entry{
			ID: resource.VersionedKey{
				Version: "v1",
				Key: resource.Key{
					TypeURL:  emptyInfo.TypeURL,
					FullName: resource.FullNameFromNamespaceAndName("ns1", "res1"),
				},
				CreateTime: time.Unix(1, 1),
			},
			Item: &types.Empty{},
		},
	}
}

func addRes2V1() resource.Event {
	return resource.Event{
		Kind: resource.Added,
		Entry: resource.Entry{
			ID: resource.VersionedKey{
				Version: "v1",
				Key: resource.Key{
					TypeURL:  emptyInfo.TypeURL,
					FullName: resource.FullNameFromNamespaceAndName("ns1", "res2"),
				},
				CreateTime: time.Unix(2, 1),
			},
			Item: &types.Empty{},
		},
	}
}

func addRes3V1() resource.Event {
	return resource.Event{
		Kind: resource.Added,
		Entry: resource.Entry{
			ID: resource.VersionedKey{
				Version: "v1",
				Key: resource.Key{
					TypeURL:  structInfo.TypeURL,
					FullName: resource.FullNameFromNamespaceAndName("ns2", "res1"),
				},
				CreateTime: time.Unix(3, 1),
			},
			Item: &types.Empty{},
		},
	}
}

func updateRes1V2() resource.Event {
	return resource.Event{
		Kind: resource.Updated,
		Entry: resource.Entry{
			ID: resource.VersionedKey{
				Version: "v2",
				Key: resource.Key{
					TypeURL:  emptyInfo.TypeURL,
					FullName: resource.FullNameFromNamespaceAndName("ns1", "res1"),
				},
				CreateTime: time.Unix(1, 2),
			},
			Item: &types.Empty{},
		},
	}
}

func delete1() resource.Event {
	return resource.Event{
		Kind: resource.Deleted,
		Entry: resource.Entry{
			ID: resource.VersionedKey{
				Version: "v1",
				Key: resource.Key{
					TypeURL:  emptyInfo.TypeURL,
					FullName: resource.FullNameFromNamespaceAndName("ns1", "res1"),
				},
				CreateTime: time.Unix(4, 4),
			},
		},
	}
}

func bogusEvent() resource.Event {
	return resource.Event{
		Kind: resource.None,
	}
}

func TestNewAccumulator_NilConversion(t *testing.T) {
	c := NewTable()
	a := NewAccumulator(c, nil)

	changed := a.Handle(addRes1V1())
	if !changed {
		t.Fatal("Expected a change after add event")
	}
}

func TestNewAccumulator_ErrorInConversion(t *testing.T) {
	c := NewTable()

	called := false
	a := NewAccumulator(c, func(e resource.Entry) (interface{}, error) {
		called = true
		return nil, fmt.Errorf("we don't believe in conversions")
	})

	changed := a.Handle(addRes1V1())

	if !called {
		t.Fatal("Expected a call to the transformer")
	}

	if changed {
		t.Fatal("Expected no change due to failure")
	}
}

func TestNewAccumulator_BogusEvent(t *testing.T) {
	c := NewTable()
	a := NewAccumulator(c, nil)

	changed := a.Handle(bogusEvent())
	if changed {
		t.Fatal("Expected no change due to failure")
	}
}

func TestNewAccumulator_DoubleAdd(t *testing.T) {
	c := NewTable()
	a := NewAccumulator(c, nil)

	changed := a.Handle(addRes1V1())
	if !changed {
		t.Fatal("Expected a change after add event")
	}

	changed = a.Handle(addRes1V1())
	if changed {
		t.Fatal("Expected no change after second add")
	}
}

func TestNewAccumulator_AddRemove(t *testing.T) {
	c := NewTable()
	a := NewAccumulator(c, nil)

	changed := a.Handle(addRes1V1())
	if !changed {
		t.Fatal("Expected a change after add event")
	}

	changed = a.Handle(delete1())
	if !changed {
		t.Fatal("Expected a change after add event")
	}
}

func TestNewAccumulator_Remove(t *testing.T) {
	c := NewTable()
	a := NewAccumulator(c, nil)

	changed := a.Handle(delete1())
	if changed {
		t.Fatal("Expected no change after delete")
	}
}
