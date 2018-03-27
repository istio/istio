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

package registry

import (
	"testing"
)

func TestIdentityRegistryAddMapping(t *testing.T) {
	testCases := []struct {
		id1     string
		id2     string
		success bool
	}{
		{
			id1:     "foo",
			id2:     "bar",
			success: true,
		},
		{
			id1:     "dead",
			id2:     "bar",
			success: false,
		},
		{
			id1:     "dead",
			id2:     "beef",
			success: true,
		},
	}

	for _, test := range testCases {
		reg := &IdentityRegistry{
			Map: make(map[string]string),
		}
		// Seeding registry.
		if err := reg.AddMapping("dead", "beef"); err != nil {
			t.Fatalf("Fail to seed registry.")
		}
		err := reg.AddMapping(test.id1, test.id2)
		if test.success != (err == nil) {
			t.Errorf("AddMaping %s -> %s: expected %v, got error: \"%v\"", test.id1, test.id2, test.success, err)
		}
		if checkRes := reg.Check(test.id1, test.id2); test.success != checkRes {
			t.Errorf("Checking %s -> %s in registry: expected %v, got %v", test.id1, test.id2, test.success, checkRes)
		}
	}
}

func TestIdentityRegistryDeleteMapping(t *testing.T) {
	testCases := []struct {
		id1     string
		id2     string
		success bool
	}{
		{
			id1:     "foo",
			id2:     "bar",
			success: false,
		},
		{
			id1:     "dead",
			id2:     "bar",
			success: false,
		},
		{
			id1:     "dead",
			id2:     "beef",
			success: true,
		},
	}

	for _, test := range testCases {
		reg := &IdentityRegistry{
			Map: make(map[string]string),
		}
		// Seeding registry.
		if err := reg.AddMapping("dead", "beef"); err != nil {
			t.Fatalf("Fail to seed registry.")
		}
		err := reg.DeleteMapping(test.id1, test.id2)
		if test.success != (err == nil) {
			t.Errorf("DeleteMapping %s -> %s: expected %v, got error: \"%v\"", test.id1, test.id2, test.success, err)
		}
	}
}

func TestGetIdentityRegistry(t *testing.T) {
	first := GetIdentityRegistry()
	second := GetIdentityRegistry()
	if first != second {
		t.Errorf("Registry is not singleton")
	}
}

func TestIdentityRegistry(t *testing.T) {
	reg := &IdentityRegistry{
		Map: make(map[string]string),
	}

	_ = reg.AddMapping("id1", "id2")
	if !reg.Check("id1", "id2") {
		t.Errorf("add mapping: id1 -> id2 should be in registry")
	}

	if err := reg.AddMapping("id1", "id2"); err != nil {
		t.Errorf("add mapping: id1 -> id2 should fail as id1 is already mapped")
	}

	_ = reg.DeleteMapping("id1", "id2")
	if reg.Check("id1", "id2") {
		t.Errorf("delete mapping: id1 -> id2 should not be in registry")
	}
}
