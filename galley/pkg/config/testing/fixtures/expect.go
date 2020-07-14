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

package fixtures

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/onsi/gomega"

	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/test/util/retry"
)

// ExpectEventsEventually waits for the Accumulator.Events() to contain the expected events.
func ExpectEventsEventually(t *testing.T, acc *Accumulator, expected ...event.Event) {
	t.Helper()
	expectEventsEventually(t, acc.Events, expected...)
}

// ExpectEventsWithoutOriginsEventually waits for the Accumulator.EventsWithoutOrigins() to contain the expected events.
func ExpectEventsWithoutOriginsEventually(t *testing.T, acc *Accumulator, expected ...event.Event) {
	t.Helper()
	expectEventsEventually(t, acc.EventsWithoutOrigins, expected...)
}

func expectEventsEventually(t *testing.T, getActuals func() []event.Event, expected ...event.Event) {
	t.Helper()
	retry.UntilSuccessOrFail(t, func() error {
		return CheckContainEvents(getActuals(), expected...)
	}, retry.Delay(time.Millisecond*100), retry.Timeout(time.Second*2))
}

// ExpectContainEvents calls CheckContainEvents and fails the test if an error is returned.
func ExpectContainEvents(t *testing.T, actuals []event.Event, expected ...event.Event) {
	t.Helper()
	if err := CheckContainEvents(actuals, expected...); err != nil {
		t.Fatal(err)
	}
}

// CheckContainEvents checks that the expected elements are all contained within the actual events list (order independent).
func CheckContainEvents(actuals []event.Event, expected ...event.Event) error {
	sort.SliceStable(expected, func(i, j int) bool {
		return strings.Compare(expected[i].String(), expected[j].String()) < 0
	})

	sort.SliceStable(actuals, func(i, j int) bool {
		return strings.Compare(actuals[i].String(), actuals[j].String()) < 0
	})

	for _, e := range expected {
		found := false
		for _, a := range actuals {
			if cmp.Equal(a, e) {
				found = true
				break
			}
		}

		if !found {
			return fmt.Errorf("element %s not found. Diff:\n%s", e, cmp.Diff(actuals, expected))
		}
	}
	return nil
}

// ExpectEqual calls CheckEqual and fails the test if it returns an error.
func ExpectEqual(t *testing.T, o1 interface{}, o2 interface{}) {
	t.Helper()
	if err := CheckEqual(o1, o2); err != nil {
		t.Fatal(err)
	}
}

// CheckEqual checks that o1 and o2 are equal. If not, returns an error with the diff.
func CheckEqual(o1 interface{}, o2 interface{}) error {
	if diff := cmp.Diff(o1, o2); diff != "" {
		return fmt.Errorf(diff)
	}
	return nil
}

// Expect calls gomega.Eventually to wait until the accumulator accumulated specified events.
func Expect(t *testing.T, acc *Accumulator, expected ...event.Event) {
	ExpectFilter(t, acc, nil, expected...)
}

// ExpectFullSync expects the given full sync event.
func ExpectFullSync(t *testing.T, acc *Accumulator, c collection.Schema) {
	e := event.FullSyncFor(c)
	Expect(t, acc, e)
}

// ExpectNone expects no events to arrive.
func ExpectNone(t *testing.T, acc *Accumulator) {
	time.Sleep(time.Second) // Sleep for a long time to avoid missing any events that might be accumulated
	Expect(t, acc)
}

// ExpectFilter works similar to Expect, except it filters out events based on the given function.
func ExpectFilter(t *testing.T, acc *Accumulator, fn FilterFn, expected ...event.Event) {
	t.Helper()
	g := gomega.NewGomegaWithT(t)

	wrapFn := func() []event.Event {
		e := acc.Events()
		if fn != nil {
			e = fn(e)
		}

		if len(e) == 0 {
			e = nil
		}
		return e
	}
	g.Eventually(wrapFn).Should(gomega.Equal(expected))
}
