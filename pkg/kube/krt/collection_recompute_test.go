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

import (
	"fmt"
	"sync/atomic"
	"testing"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

type routeCount struct {
	Named
	Routes int
}

type namedValue struct {
	Named
	Value string
}

// TestCollectionSkipsObservedEvents verifies that events queued behind a recomputation do not recompute an input
// again once a later recomputation has observed them. Without this, a transformation that fetches a whole
// collection runs once per queued event, each time against the same latest state.
func TestCollectionSkipsObservedEvents(t *testing.T) {
	stop := test.NewStop(t)
	opts := NewOptionsBuilder(stop, "test", GlobalDebugHandler)
	routes := NewMutableCollection[Named](nil, nil, opts.WithName("Routes")...)
	gateways := NewStaticCollection(nil, []Named{{Namespace: "ns", Name: "gw"}}, opts.WithName("Gateways")...)

	var runs atomic.Int32
	blocked := make(chan struct{})
	release := make(chan struct{})
	counts := NewCollection(gateways, func(ctx HandlerContext, gw Named) *routeCount {
		if runs.Add(1) == 2 {
			// Hold the first event-driven run, so later events queue up behind it.
			close(blocked)
			<-release
		}
		return &routeCount{Named: gw, Routes: len(Fetch(ctx, routes.AsCollection()))}
	}, opts.WithName("RouteCounts")...)
	assert.Equal(t, counts.WaitUntilSynced(stop), true)
	h := counts.internal().(*manyCollection[Named, routeCount])

	routes.UpdateObject(Named{Namespace: "ns", Name: "r0"})
	<-blocked
	const n = 100
	stamped := h.seq.Load()
	for i := 1; i < n; i++ {
		routes.UpdateObject(Named{Namespace: "ns", Name: fmt.Sprint("r", i)})
	}
	// Wait until every event is stamped, so all of them precede the next recomputation.
	assert.EventuallyEqual(t, h.seq.Load, stamped+n-1)
	close(release)

	drained := make(chan struct{})
	h.queue.Push(func() error {
		close(drained)
		return nil
	})
	<-drained
	assert.Equal(t, counts.GetKey("ns/gw").Routes, n)
	// The held run was stamped before the queued events, so the first of them recomputes once more.
	// That recomputation observed all the others, so they are skipped.
	assert.Equal(t, runs.Load(), int32(3))
}

// TestCollectionRecomputesInputChangedDuringBatch verifies that an input changing while its batch is being processed
// is recomputed, even when the change is queued before that input's own transformation runs. The batch read the
// input before the change, so its recomputation must not count as having observed it.
func TestCollectionRecomputesInputChangedDuringBatch(t *testing.T) {
	stop := test.NewStop(t)
	opts := NewOptionsBuilder(stop, "test", GlobalDebugHandler)
	inputs := NewMutableCollection(nil, []namedValue{
		{Named: Named{Namespace: "ns", Name: "a"}, Value: "old"},
		{Named: Named{Namespace: "ns", Name: "b"}, Value: "old"},
	}, opts.WithName("Inputs")...)

	var runs atomic.Int32
	first := make(chan string, 1)
	release := make(chan struct{})
	outputs := NewCollection(inputs.AsCollection(), func(ctx HandlerContext, i namedValue) *namedValue {
		if runs.Add(1) == 1 {
			// Both inputs arrive in the initial batch. Hold the first, so the other changes before its own turn.
			first <- i.Name
			<-release
		}
		return &i
	}, opts.WithName("Outputs")...)
	h := outputs.internal().(*manyCollection[namedValue, namedValue])

	other := "a"
	if <-first == "a" {
		other = "b"
	}
	stamped := h.seq.Load()
	inputs.UpdateObject(namedValue{Named: Named{Namespace: "ns", Name: other}, Value: "new"})
	assert.EventuallyEqual(t, h.seq.Load, stamped+1)
	close(release)

	assert.EventuallyEqual(t, func() string {
		if o := outputs.GetKey("ns/" + other); o != nil {
			return o.Value
		}
		return ""
	}, "new")
}
