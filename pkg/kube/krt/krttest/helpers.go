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

package krttest

import (
	"fmt"

	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

type MockCollection struct {
	t      test.Failer
	inputs []any
}

// NewMock creates a helper to build Collections of static inputs for use with testing.
// Example usage:
//
//	mock := krttest.NewMock(t, []any{serviceFoo, podBar, namespaceBaz})
//	pods := krttest.GetMockCollection[Pod](mock) // makes a collection of all Pod types from inputs
func NewMock(t test.Failer, inputs []any) *MockCollection {
	t.Helper()
	mc := &MockCollection{t: t, inputs: inputs}
	t.Cleanup(func() {
		t.Helper()
		types := slices.Map(mc.inputs, func(e any) string {
			return fmt.Sprintf("%T", e)
		})
		assert.Equal(t, len(mc.inputs), 0, fmt.Sprintf("some inputs were not consumed: %v (%v)", mc.inputs, types))
	})
	return mc
}

func GetMockCollection[T any](mc *MockCollection) krt.Collection[T] {
	return krt.NewStaticCollection(
		nil, // Always synced
		extractType[T](&mc.inputs),
		krt.WithStop(test.NewStop(mc.t)),
		krt.WithDebugging(krt.GlobalDebugHandler),
	)
}

func GetMockSingleton[T any](mc *MockCollection) krt.StaticSingleton[T] {
	t := extractType[T](&mc.inputs)
	if len(t) > 1 {
		mc.t.Helper()
		mc.t.Fatal("multiple types returned")
	}
	return krt.NewStatic(slices.First(t), true)
}

func extractType[T any](items *[]any) []T {
	var matched []T
	var unmatched []any
	arr := *items
	for _, val := range arr {
		if c, ok := val.(T); ok {
			matched = append(matched, c)
		} else {
			unmatched = append(unmatched, val)
		}
	}

	*items = unmatched
	return matched
}

func Options(t test.Failer) krt.OptionsBuilder {
	return krt.NewOptionsBuilder(test.NewStop(t), "test", krt.GlobalDebugHandler, nil)
}
