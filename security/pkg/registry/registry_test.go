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

func TestIdentityRegistry(t *testing.T) {
	reg := &IdentityRegistry{
		Map: make(map[string]string),
	}

	_ = reg.AddMapping("id1", "id2")
	if !reg.Check("id1", "id2") {
		t.Errorf("add mapping: id1 -> id2 should be in registry")
	}

	_ = reg.DeleteMapping("id1", "id2")
	if reg.Check("id1", "id2") {
		t.Errorf("delete mapping: id1 -> id2 should not be in registry")
	}
}
