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

var cmpOpts = []cmp.Option{protocmp.Transform(), cmpopts.EquateEmpty(), compareErrors}

// Equal
func Equal[T any](t test.Failer, a, b T, context ...string) {
	t.Helper()
	if !cmp.Equal(a, b, cmpOpts...) {
		cs := ""
		if len(context) > 0 {
			cs = " " + strings.Join(context, ", ") + ":"
		}
		t.Fatalf("found diff:%s %v\nLeft: %v\nRight: %v", cs, cmp.Diff(a, b, cmpOpts...), a, b)
	}
}

func EventuallyEqual[T any](t test.Failer, fetchA func() T, b T, opts ...retry.Option) {
	t.Helper()
	var a T
	// Unit tests typically need shorter default; opts can override though
	ro := []retry.Option{retry.Timeout(time.Second * 2)}
	ro = append(ro, opts...)
	err := retry.UntilSuccess(func() error {
		a = fetchA()
		if !cmp.Equal(a, b, cmpOpts...) {
			return fmt.Errorf("not equal")
		}
		return nil
	}, ro...)
	if err != nil {
		t.Fatalf("found diff: %v\nLeft: %v\nRight: %v", cmp.Diff(a, b, cmpOpts...), a, b)
	}
}

func Error(t test.Failer, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error but got nil")
	}
}

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
		t.Fatalf("failed to receive event after 5s")
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
