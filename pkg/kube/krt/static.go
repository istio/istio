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

package krt

import "go.uber.org/atomic"

type static[T any] struct {
	*singleton[T]
	state *atomic.Pointer[T]
}

func (s static[T]) Set(t *T) {
	s.state.Store(t)
	// Retrigger
	s.execute()
}

type StaticSingleton[T any] interface {
	Singleton[T]
	Set(*T)
}

func NewStatic[T any](initial *T) StaticSingleton[T] {
	state := atomic.NewPointer(initial)
	s := NewSingleton[T](func(ctx HandlerContext) *T {
		return state.Load()
	})
	x := &static[T]{
		singleton: s.(*singleton[T]),
		state:     state,
	}
	return x
}
