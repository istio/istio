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

package sync

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestMapping(t *testing.T) {
	m := Mapping()
	source, destination, found := m.GetGroupVersion("config.istio.io")
	if !found {
		t.Fatalf("Expected the mapping to be found")
	}

	expected := schema.GroupVersion{Group: "config.istio.io", Version: "v1alpha2"}
	if source != expected {
		t.Fatalf("mapping mismatch: got:'%v', wanted:'%v'", source, expected)
	}

	expected = schema.GroupVersion{Group: "internal.istio.io", Version: "v1alpha2"}
	if destination != expected {
		t.Fatalf("mapping mismatch: got:'%v', wanted:'%v'", source, expected)
	}
}

func TestPanic(t *testing.T) {
	oldData := mappingData
	defer func() {
		mappingData = oldData
		r := recover()
		if r == nil {
			t.Fatalf("Expected panic not found")
		}
	}()

	// create cycle to trigger panic
	mappingData = map[schema.GroupVersion]schema.GroupVersion{
		{
			Version: "v1",
			Group:   "g1",
		}: {
			Version: "v1",
			Group:   "g1",
		},
	}

	_ = Mapping()
}
