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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

type dbgNamed struct {
	Name string
}

func (n dbgNamed) ResourceName() string { return n.Name }

// blockingSyncer never reports as synced; WaitUntilSynced only returns (false) once stopped.
// It is used to drive collections into their stop-before-sync code paths.
type blockingSyncer struct{}

func (blockingSyncer) HasSynced() bool { return false }

func (blockingSyncer) WaitUntilSynced(stop <-chan struct{}) bool {
	<-stop
	return false
}

// debugCount returns the number of collections currently registered with the debugger.
func debugCount(dh *DebugHandler) int {
	dh.mu.RLock()
	defer dh.mu.RUnlock()
	return len(dh.debugCollections)
}

func merge(ts []dbgNamed) *dbgNamed {
	if len(ts) == 0 {
		return nil
	}
	return &ts[0]
}

// TestDebuggerUnregistration verifies that every collection type unregisters itself from the
// debugger once its stop channel is closed. Otherwise, collections that are created and destroyed
// over the lifetime of the process (e.g. per-cluster ambient collections) leak debugger
// registrations forever.
func TestDebuggerUnregistration(t *testing.T) {
	// Each case builds one or more collections against a fresh DebugHandler, waits for them to
	// register, then closes the stop channel and asserts every registration is cleaned up.
	cases := map[string]func(_ *testing.T, opts OptionsBuilder){
		"informer": func(t *testing.T, opts OptionsBuilder) {
			c := kube.NewFakeClient()
			kc := kclient.New[*corev1.ConfigMap](c)
			_ = WrapClient(kc, opts.WithName("cms")...)
			// Run the client on a separate, cleanup-closed stop so the informer's workqueue drains
			// at test cleanup rather than when the collection stop closes. This avoids a spurious
			// goroutine-leak report from the fake client's slow-draining workqueue.
			c.RunAndWait(test.NewStop(t))
		},
		"static": func(_ *testing.T, opts OptionsBuilder) {
			_ = NewStaticCollection[dbgNamed](nil, nil, opts.WithName("static")...)
		},
		"singleton": func(_ *testing.T, opts OptionsBuilder) {
			_ = NewStatic[dbgNamed](nil, true, opts.WithName("singleton")...).AsCollection()
		},
		"map": func(_ *testing.T, opts OptionsBuilder) {
			base := NewStaticCollection[dbgNamed](nil, nil, opts.WithName("base")...)
			_ = MapCollection(base, func(n dbgNamed) dbgNamed { return n }, opts.WithName("map")...)
		},
		"index": func(_ *testing.T, opts OptionsBuilder) {
			base := NewStaticCollection[dbgNamed](nil, nil, opts.WithName("base")...)
			idx := NewIndex(base, "name", func(n dbgNamed) []string { return []string{n.Name} })
			_ = idx.AsCollection(opts.WithName("idx")...)
		},
		"manyCollection": func(_ *testing.T, opts OptionsBuilder) {
			base := NewStaticCollection[dbgNamed](nil, nil, opts.WithName("base")...)
			_ = NewCollection(base, func(ctx HandlerContext, n dbgNamed) *dbgNamed {
				return &n
			}, opts.WithName("many")...)
		},
		"mergejoin": func(_ *testing.T, opts OptionsBuilder) {
			c1 := NewStaticCollection[dbgNamed](nil, nil, opts.WithName("c1")...)
			c2 := NewStaticCollection[dbgNamed](nil, nil, opts.WithName("c2")...)
			_ = JoinWithMergeCollection([]Collection[dbgNamed]{c1, c2}, merge, opts.WithName("merge")...)
		},
		"nestedjoinmerge": func(_ *testing.T, opts OptionsBuilder) {
			inner := NewStaticCollection[dbgNamed](nil, nil, opts.WithName("inner")...)
			multi := NewStaticCollection(nil, []Collection[dbgNamed]{inner}, opts.WithName("multi")...)
			_ = NestedJoinWithMergeCollection(multi, merge, opts.WithName("nested")...)
		},
	}

	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			dh := &DebugHandler{}
			stop := make(chan struct{})
			opts := NewOptionsBuilder(stop, "test", dh)

			build(t, opts)

			// Collections register synchronously on construction.
			assert.EventuallyEqual(t, func() bool { return debugCount(dh) > 0 }, true)

			close(stop)
			assert.EventuallyEqual(t, func() int { return debugCount(dh) }, 0)
		})
	}
}

// TestDebuggerUnregistrationBeforeSync verifies the stop-before-sync code paths: a collection that
// is stopped before it ever finishes syncing must still unregister from the debugger. Each of the
// queue-backed collections returns early (without running its queue) when its inputs never sync, so
// this exercises the unregistration on those early-return paths. Inputs use a never-syncing static
// collection so no informer/workqueue is involved.
func TestDebuggerUnregistrationBeforeSync(t *testing.T) {
	cases := map[string]func(_ *testing.T, opts OptionsBuilder){
		"manyCollection": func(_ *testing.T, opts OptionsBuilder) {
			// Primary never syncs, so runQueue returns before running the queue.
			base := NewStaticCollection[dbgNamed](blockingSyncer{}, nil, opts.WithName("base")...)
			_ = NewCollection(base, func(ctx HandlerContext, n dbgNamed) *dbgNamed {
				return &n
			}, opts.WithName("many")...)
		},
		"mergejoin": func(_ *testing.T, opts OptionsBuilder) {
			c1 := NewStaticCollection[dbgNamed](blockingSyncer{}, nil, opts.WithName("c1")...)
			c2 := NewStaticCollection[dbgNamed](blockingSyncer{}, nil, opts.WithName("c2")...)
			_ = JoinWithMergeCollection([]Collection[dbgNamed]{c1, c2}, merge, opts.WithName("merge")...)
		},
		"nestedjoinmerge": func(_ *testing.T, opts OptionsBuilder) {
			inner := NewStaticCollection[dbgNamed](blockingSyncer{}, nil, opts.WithName("inner")...)
			multi := NewStaticCollection(blockingSyncer{}, []Collection[dbgNamed]{inner}, opts.WithName("multi")...)
			_ = NestedJoinWithMergeCollection(multi, merge, opts.WithName("nested")...)
		},
	}

	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			dh := &DebugHandler{}
			stop := make(chan struct{})
			opts := NewOptionsBuilder(stop, "test", dh)

			build(t, opts)

			assert.EventuallyEqual(t, func() bool { return debugCount(dh) > 0 }, true)

			close(stop)
			assert.EventuallyEqual(t, func() int { return debugCount(dh) }, 0)
		})
	}
}

// TestDebugHandlerWriteJSON verifies that the streamed dump is the same as the buffered one.
func TestDebugHandlerWriteJSON(t *testing.T) {
	stop := test.NewStop(t)
	dh := &DebugHandler{}
	opts := NewOptionsBuilder(stop, "test", dh)
	a := NewStaticCollection(nil, []dbgNamed{{Name: "a1"}, {Name: "a2"}}, opts.WithName("a")...)
	_ = MapCollection(a, func(n dbgNamed) dbgNamed { return n }, opts.WithName("b")...)
	_ = NewStaticCollection(nil, []dbgNamed{{Name: "<c>"}}, opts.WithName("c")...)
	assert.EventuallyEqual(t, func() int { return debugCount(dh) }, 3)

	dh.mu.RLock()
	collections := maps.Values(dh.debugCollections)
	dh.mu.RUnlock()

	// Map order is random, so compare one collection at a time.
	for _, c := range collections {
		single := &DebugHandler{debugCollections: map[collectionUID]DebugCollection{c.uid: c}}
		want, err := json.Marshal(single)
		assert.NoError(t, err)
		got := &bytes.Buffer{}
		assert.NoError(t, single.WriteJSON(context.Background(), got, ""))
		assert.Equal(t, got.String(), string(want)+"\n")
	}

	// With several collections, the output must still be valid JSON with every collection.
	got := &bytes.Buffer{}
	assert.NoError(t, dh.WriteJSON(context.Background(), got, ""))
	var parsed []map[string]any
	assert.NoError(t, json.Unmarshal(got.Bytes(), &parsed))
	assert.Equal(t, len(parsed), 3)

	t.Run("nil", func(t *testing.T) {
		var nilHandler *DebugHandler
		want, err := json.Marshal(nilHandler)
		assert.NoError(t, err)
		got := &bytes.Buffer{}
		assert.NoError(t, nilHandler.WriteJSON(context.Background(), got, ""))
		assert.Equal(t, got.String(), string(want))
	})

	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		assert.Error(t, dh.WriteJSON(ctx, &bytes.Buffer{}, ""))
	})
}

type dbgBadJSON struct {
	Name string
}

func (n dbgBadJSON) ResourceName() string { return n.Name }

func (n dbgBadJSON) MarshalJSON() ([]byte, error) {
	return nil, fmt.Errorf("cannot marshal %s", n.Name)
}

// TestDebugHandlerWriteJSONError verifies that an output that fails to marshal stops the dump with an error.
func TestDebugHandlerWriteJSONError(t *testing.T) {
	dh := &DebugHandler{}
	opts := NewOptionsBuilder(test.NewStop(t), "test", dh)
	_ = NewStaticCollection(nil, []dbgBadJSON{{Name: "bad"}}, opts.WithName("bad")...)
	assert.EventuallyEqual(t, func() int { return debugCount(dh) }, 1)

	err := dh.WriteJSON(context.Background(), &bytes.Buffer{}, "")
	assert.Error(t, err)
	assert.Equal(t, strings.Contains(err.Error(), "cannot marshal bad"), true, err.Error())
}
