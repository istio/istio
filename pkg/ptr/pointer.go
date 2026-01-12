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
	"fmt"
)

// Of returns a pointer to the input. In most cases, callers should just do &t. However, in some cases
// Go cannot take a pointer. For example, `ptr.Of(f())`.
func Of[T any](t T) *T {
	return &t
}

// OrEmpty returns *t if its non-nil, or else an empty T
func OrEmpty[T any](t *T) T {
	if t != nil {
		return *t
	}
	var empty T
	return empty
}

// OrDefault returns *t if its non-nil, or else def.
func OrDefault[T any](t *T, def T) T {
	if t != nil {
		return *t
	}
	return def
}

// NonEmptyOrDefault returns t if its non-empty, or else def.
func NonEmptyOrDefault[T comparable](t T, def T) T {
	var empty T
	if t != empty {
		return t
	}
	return def
}

// Empty returns an empty T type
func Empty[T any]() T {
	var empty T
	return empty
}

// ToList returns an empty list if t is nil, or a list with a single element
func ToList[T any](t *T) []T {
	if t == nil {
		return nil
	}
	return []T{*t}
}

// TypeName returns the name of the type
func TypeName[T any]() string {
	var empty T
	return fmt.Sprintf("%T", empty)
}

// Flatten converts a double pointer to a single pointer by referencing if its non-nil
func Flatten[T any](t **T) *T {
	if t == nil {
		return nil
	}
	return *t
}

func Equal[T comparable](a, b *T) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	return *a == *b
}
