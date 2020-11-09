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

package labels_test

import (
	"testing"

	"istio.io/istio/pkg/config/labels"
)

func TestCollection(t *testing.T) {
	a := labels.Instance{"app": "a"}
	b := labels.Instance{"app": "b"}
	a1 := labels.Instance{"app": "a", "prod": "env"}
	ab := labels.Collection{a, b}
	a1b := labels.Collection{a1, b}
	none := labels.Collection{}

	// equivalent to empty tag collection
	singleton := labels.Collection{nil}

	if (labels.Collection{a}).HasSubsetOf(b) {
		t.Errorf("{a}.HasSubsetOf(b) => Got true")
	}

	matching := []struct {
		tag        labels.Instance
		collection labels.Collection
	}{
		{a, ab},
		{b, ab},
		{a, none},
		{a, nil},
		{a, singleton},
		{a1, ab},
		{b, a1b},
	}
	for _, pair := range matching {
		if !pair.collection.HasSubsetOf(pair.tag) {
			t.Errorf("%v.HasSubsetOf(%v) => Got false", pair.collection, pair.tag)
		}
	}

	// Test not panic
	var nilInstance labels.Instance
	if ab.HasSubsetOf(nilInstance) {
		t.Errorf("%v.HasSubsetOf(%v) => Got true", ab, nilInstance)
	}
}
