// Copyright 2020 Istio Authors
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

package v2

import (
	"reflect"
	"testing"
	"time"
)

func TestFixedDebouncer(t *testing.T) {
	type result struct {
		debounced     bool
		events        int
		debounceAfter time.Duration
	}
	tests := []struct {
		name string
		test func(debouncer, func(expected, got result))
	}{
		{
			name: "first request",
			test: func(fd debouncer, validate func(expected, got result)) {
				debounced, debounceAfter := fd.newRequest()
				validate(result{true, 1, 100 * time.Millisecond}, result{debounced, fd.events(), debounceAfter})
			},
		},
		{
			name: "two requests and both should be debounced",
			test: func(fd debouncer, validate func(expected, got result)) {
				debounced, debounceAfter := fd.newRequest()
				validate(result{true, 1, 100 * time.Millisecond}, result{debounced, fd.events(), debounceAfter})
				debounced, _ = fd.newRequest()
				validate(result{true, 2, 100 * time.Millisecond}, result{debounced, fd.events(), debounceAfter})
			},
		},
		{
			name: "multiple requests check fixed debounce is applied",
			test: func(fd debouncer, validate func(expected, got result)) {
				debounced, debounceAfter := fd.newRequest()
				validate(result{true, 1, 100 * time.Millisecond}, result{debounced, fd.events(), debounceAfter})
				debounced, debounceAfter = fd.newRequest()
				validate(result{true, 2, 100 * time.Millisecond}, result{debounced, fd.events(), debounceAfter})
				debounced, debounceAfter = fd.newRequest()
				validate(result{true, 3, 100 * time.Millisecond}, result{debounced, fd.events(), debounceAfter})
				debounced, debounceAfter = fd.newRequest()
				validate(result{true, 4, 100 * time.Millisecond}, result{debounced, fd.events(), debounceAfter})
			},
		},
		{
			name: "should not backoff when max backoff reached",
			test: func(fd debouncer, validate func(expected, got result)) {
				debounced, debounceAfter := fd.newRequest()
				validate(result{true, 1, 100 * time.Millisecond}, result{debounced, fd.events(), debounceAfter})
				debounced, debounceAfter = fd.newRequest()
				validate(result{true, 2, 100 * time.Millisecond}, result{debounced, fd.events(), debounceAfter})
				debounced, debounceAfter = fd.newRequest()
				validate(result{true, 3, 100 * time.Millisecond}, result{debounced, fd.events(), debounceAfter})
				time.Sleep(800 * time.Millisecond)
				debounced, _ = fd.tryDebounce()
				validate(result{false, 0, 100 * time.Millisecond}, result{debounced, fd.events(), debounceAfter})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			debounceDelay := time.Millisecond * 100
			debounceMax := debounceDelay * 5
			bd := newFixedDebouncer(debounceDelay, debounceMax)
			tt.test(bd, func(expected, got result) {
				if !reflect.DeepEqual(expected, got) {
					t.Fatalf("Unexpected backoff result. Expected %v, Got %v", expected, got)
				}
			})
		})
	}
}

func TestBackoffDebouncer(t *testing.T) {
	type result struct {
		debounced     bool
		events        int
		debounceAfter time.Duration
	}
	tests := []struct {
		name string
		test func(debouncer, func(expected, got result))
	}{
		{
			name: "first request",
			test: func(bd debouncer, validate func(expected, got result)) {
				debounced, debounceAfter := bd.newRequest()
				validate(result{false, 1, 0}, result{debounced, bd.events(), debounceAfter})
			},
		},
		{
			name: "two requests second request should be debounced",
			test: func(bd debouncer, validate func(expected, got result)) {
				debounced, debounceAfter := bd.newRequest()
				validate(result{false, 1, 0}, result{debounced, bd.events(), debounceAfter})
				debounced, debounceAfter = bd.newRequest()
				validate(result{true, 2, 100 * time.Millisecond}, result{debounced, bd.events(), debounceAfter})
			},
		},
		{
			name: "multiple requests check backoff",
			test: func(bd debouncer, validate func(expected, got result)) {
				debounced, debounceAfter := bd.newRequest()
				validate(result{false, 1, 0}, result{debounced, bd.events(), debounceAfter})
				debounced, debounceAfter = bd.newRequest()
				validate(result{true, 2, 100 * time.Millisecond}, result{debounced, bd.events(), debounceAfter})
				debounced, debounceAfter = bd.newRequest()
				validate(result{true, 3, 200 * time.Millisecond}, result{debounced, bd.events(), debounceAfter})
				debounced, debounceAfter = bd.newRequest()
				validate(result{true, 4, 400 * time.Millisecond}, result{debounced, bd.events(), debounceAfter})
			},
		},
		{
			name: "should not backoff when max backoff reached",
			test: func(bd debouncer, validate func(expected, got result)) {
				debounced, debounceAfter := bd.newRequest()
				validate(result{false, 1, 0}, result{debounced, bd.events(), debounceAfter})
				debounced, debounceAfter = bd.newRequest()
				validate(result{true, 2, 100 * time.Millisecond}, result{debounced, bd.events(), debounceAfter})
				debounced, debounceAfter = bd.newRequest()
				validate(result{true, 3, 200 * time.Millisecond}, result{debounced, bd.events(), debounceAfter})
				time.Sleep(800 * time.Millisecond)
				debounced, debounceAfter = bd.tryDebounce()
				validate(result{false, 0, 0}, result{debounced, bd.events(), debounceAfter})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			debounceDelay := time.Millisecond * 100
			debounceMax := debounceDelay * 5
			bd := newBackoffDebouncer(debounceDelay, debounceMax)
			tt.test(bd, func(expected, got result) {
				if !reflect.DeepEqual(expected, got) {
					t.Fatalf("Unexpected backoff result. Expected %v, Got %v", expected, got)
				}
			})
		})
	}
}
