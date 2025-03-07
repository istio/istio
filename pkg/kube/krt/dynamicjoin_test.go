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

package krt_test

import (
	"testing"

	"go.uber.org/atomic"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test/util/assert"
)

func TestDynamicJoinCollection(t *testing.T) {
	opts := testOptions(t)
	c1 := krt.NewStatic[Named](nil, true, opts.WithName("c1")...)
	c2 := krt.NewStatic[Named](nil, true, opts.WithName("c2")...)
	c3 := krt.NewStatic[Named](nil, true, opts.WithName("c3")...)
	dj := krt.DynamicJoinCollection(
		[]krt.Collection[Named]{c1.AsCollection(), c2.AsCollection(), c3.AsCollection()},
		opts.WithName("DynamicJoin")...,
	)

	last := atomic.NewString("")
	dj.Register(func(o krt.Event[Named]) {
		last.Store(o.Latest().ResourceName())
	})

	assert.EventuallyEqual(t, last.Load, "")
	c1.Set(&Named{"c1", "a"})
	assert.EventuallyEqual(t, last.Load, "c1/a")

	c2.Set(&Named{"c2", "a"})
	assert.EventuallyEqual(t, last.Load, "c2/a")

	c3.Set(&Named{"c3", "a"})
	assert.EventuallyEqual(t, last.Load, "c3/a")

	c1.Set(&Named{"c1", "b"})
	assert.EventuallyEqual(t, last.Load, "c1/b")
	// ordered by c1, c2, c3
	sortf := func(a Named) string {
		return a.ResourceName()
	}
	assert.Equal(
		t,
		slices.SortBy(dj.List(), sortf),
		slices.SortBy([]Named{
			{"c1", "b"},
			{"c2", "a"},
			{"c3", "a"},
		}, sortf),
	)

	// add c4
	c4 := krt.NewStatic[Named](nil, true, opts.WithName("c4")...)
	dj.AddOrUpdateCollection(c4.AsCollection())
	c4.Set(&Named{"c4", "a"})
	assert.EventuallyEqual(t, last.Load, "c4/a") // Test that events from the new collection make it to the join

	// remove c1
	dj.RemoveCollection(c1.AsCollection())
	c1.Set(&Named{"c1", "c"})
	assert.EventuallyEqual(t, last.Load, "c4/a") // Test that events from removed collections do not make it to the join
}
