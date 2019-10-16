// Copyright 2019 Istio Authors
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
	"testing"
	"time"

	"github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
)

// Expect calls gomega.Eventually to wait until the accumulator accumulated specified events.
func Expect(t *testing.T, acc *Accumulator, expected ...event.Event) {
	ExpectFilter(t, acc, nil, expected...)
}

// ExpectFullSync expects the given full sync event.
func ExpectFullSync(t *testing.T, acc *Accumulator, c collection.Name) {
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
