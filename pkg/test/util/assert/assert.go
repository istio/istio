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

package assert

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"

	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
)

var compareErrors = cmp.Comparer(func(x, y error) bool {
	switch {
	case x == nil && y == nil:
		return true
	case x != nil && y == nil:
		return false
	case x == nil && y != nil:
		return false
	case x != nil && y != nil:
		return x.Error() == y.Error()
	default:
		panic("unreachable")
	}
})

// cmpOptioner can be implemented to provide custom options that should be used when comparing a type.
// Warning: this is no recursive, unfortunately. So a type `A{B}` cannot rely on `B` implementing this to customize comparing `B`.
type cmpOptioner interface {
	CmpOpts() []cmp.Option
}

// opts gets the comparison opts for a type. This includes some defaults, but allows each type to explicitly append their own.
func opts[T any](a T) []cmp.Option {
	if o, ok := any(a).(cmpOptioner); ok {
		opts := append([]cmp.Option{}, cmpOpts...)
		opts = append(opts, o.CmpOpts()...)
		return opts
	}
	// if T is actually a slice (ex: []A), check that and get the opts for the element type (A).
	t := reflect.TypeOf(a)
	if t != nil && t.Kind() == reflect.Slice {
		v := reflect.New(t.Elem()).Elem().Interface()
		if o, ok := v.(cmpOptioner); ok {
			opts := append([]cmp.Option{}, cmpOpts...)
			opts = append(opts, o.CmpOpts()...)
			return opts
		}
	}
	return cmpOpts
}

var cmpOpts = []cmp.Option{protocmp.Transform(), cmpopts.EquateEmpty(), compareErrors}

// Compare compares two objects and returns and error if they are not the same.
func Compare[T any](a, b T) error {
	if !cmp.Equal(a, b, opts(a)...) {
		return fmt.Errorf("found diff: %v\nLeft: %v\nRight: %v", cmp.Diff(a, b, opts(a)...), a, b)
	}
	return nil
}

// Equal compares two objects and fails if they are not the same.
func Equal[T any](t test.Failer, a, b T, context ...string) {
	t.Helper()
	if !cmp.Equal(a, b, opts(a)...) {
		cs := ""
		if len(context) > 0 {
			cs = " " + strings.Join(context, ", ") + ":"
		}
		t.Fatalf("found diff:%s %v\nLeft:  %v\nRight: %v", cs, cmp.Diff(a, b, opts(a)...), a, b)
	}
}

// EventuallyEqual compares repeatedly calls the fetch function until the result matches the expectation.
func EventuallyEqual[T any](t test.Failer, fetch func() T, expected T, retryOpts ...retry.Option) {
	t.Helper()
	var a T
	// Unit tests typically need shorter default; opts can override though
	ro := []retry.Option{retry.Timeout(time.Second * 2), retry.BackoffDelay(time.Millisecond * 2)}
	ro = append(ro, retryOpts...)
	err := retry.UntilSuccess(func() error {
		a = fetch()
		if !cmp.Equal(a, expected, opts(expected)...) {
			return fmt.Errorf("not equal")
		}
		return nil
	}, ro...)
	if err != nil {
		t.Fatalf("found diff: %v\nGot: %v\nWant: %v", cmp.Diff(a, expected, opts(expected)...), a, expected)
	}
}

// Consistently polls the fetch function for a duration and fails if the value ever changes from the expected value.
// Use sparingly to avoid flakiness, as this is inherently timing-dependent.
// This is useful for asserting that no events occur (e.g., that a value stays at 0).
// Default duration is 20ms with 2ms polling interval.
func Consistently[T any](t test.Failer, fetch func() T, expected T, duration ...time.Duration) {
	t.Helper()
	d := time.Millisecond * 20
	if len(duration) > 0 {
		d = duration[0]
	}
	interval := time.Millisecond * 2
	deadline := time.Now().Add(d)

	for time.Now().Before(deadline) {
		a := fetch()
		if !cmp.Equal(a, expected, opts(expected)...) {
			t.Fatalf("value changed unexpectedly: %v\nGot: %v\nWant: %v", cmp.Diff(a, expected, opts(expected)...), a, expected)
		}
		time.Sleep(interval)
	}
}

// Error asserts the provided err is non-nil
func Error(t test.Failer, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error but got nil")
	}
}

// NoError asserts the provided err is nil
func NoError(t test.Failer, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("expected no error but got: %v", err)
	}
}

// ChannelHasItem asserts a channel has an element within 5s and returns the element
func ChannelHasItem[T any](t test.Failer, c <-chan T) T {
	t.Helper()
	select {
	case r := <-c:
		return r
	case <-time.After(time.Second * 5):
		t.Fatal("failed to receive event after 5s")
	}
	// Not reachable
	return ptr.Empty[T]()
}

// ChannelIsEmpty asserts a channel is empty for at least 20ms
func ChannelIsEmpty[T any](t test.Failer, c <-chan T) {
	t.Helper()
	select {
	case r := <-c:
		t.Fatalf("channel had element, expected empty: %v", r)
	case <-time.After(time.Millisecond * 20):
	}
}

// ChannelIsClosed asserts a channel is closed
func ChannelIsClosed[T any](t test.Failer, c <-chan T) {
	t.Helper()
	select {
	case obj, ok := <-c:
		if ok {
			t.Fatalf("channel had element, expected closed: %v", obj)
		}
	default:
		t.Fatalf("channel was not closed")
	}
}
