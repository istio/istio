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

package attribute_test

import (
	"reflect"
	"testing"

	"istio.io/istio/mixer/pkg/attribute"
)

func TestStringMapEqual(t *testing.T) {
	a := attribute.NewStringMap("attr")
	a.Set("x", "y")
	b := attribute.WrapStringMap(map[string]string{"x": "y"})
	if !a.Equal(b) {
		t.Errorf("%v.Equal(%v) => got false", a, b)
	}
	if !attribute.Equal(a, b) {
		t.Errorf("Equal(%v, %v) => got false", a, b)
	}
	if !reflect.DeepEqual(map[string]string{"x": "y"}, b.Entries()) {
		t.Errorf("Entries() => got %#v, want 'x': 'y'", b.Entries())
	}
}
