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

package ptr

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"istio.io/istio/pkg/test"
)

// Cannot use assert.assertEqual due to import loop
func assertEqual[T any](t test.Failer, a, b T) {
	t.Helper()
	if !cmp.Equal(a, b) {
		t.Fatalf("Left: %v\nRight: %v", a, b)
	}
}

func TestEmpty(t *testing.T) {
	type ts struct{}
	assertEqual(t, Empty[string](), "")
	assertEqual(t, Empty[int](), 0)
	assertEqual(t, Empty[ts](), ts{})
	assertEqual(t, Empty[*ts](), nil)
}

func TestOf(t *testing.T) {
	one := 1
	assertEqual(t, Of(1), &one)
}

func TestOrDefault(t *testing.T) {
	one := 1
	assertEqual(t, OrDefault(nil, 2), 2)
	assertEqual(t, OrDefault(&one, 2), 1)
}

func TestOrEmpty(t *testing.T) {
	one := 1
	assertEqual(t, OrEmpty[int](nil), 0)
	assertEqual(t, OrEmpty(&one), 1)
}

func TestTypeName(t *testing.T) {
	type ts struct{}
	assertEqual(t, TypeName[int](), "int")
	assertEqual(t, TypeName[string](), "string")
	assertEqual(t, TypeName[ts](), "ptr.ts")
	assertEqual(t, TypeName[*ts](), "*ptr.ts")
}
