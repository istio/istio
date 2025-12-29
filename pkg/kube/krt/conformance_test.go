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
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	krtfiles "istio.io/istio/pkg/kube/krt/files"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

type Rig[T any] interface {
	krt.Collection[T]
	CreateObject(key string)
}

type informerRig struct {
	krt.Collection[*corev1.ConfigMap]
	client kclient.Client[*corev1.ConfigMap]
}

func (r *informerRig) CreateObject(key string) {
	ns, name, _ := strings.Cut(key, "/")
	_, _ = r.client.Create(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
	})
}

type staticRig struct {
	krt.StaticCollection[Named]
}

func (r *staticRig) CreateObject(key string) {
	ns, name, _ := strings.Cut(key, "/")
	r.UpdateObject(Named{Namespace: ns, Name: name})
}

type joinRig struct {
	krt.Collection[Named]
	inner [2]krt.StaticCollection[Named]
	idx   int
}

func (r *joinRig) CreateObject(key string) {
	// Switch which collection we add to each time
	idx := r.idx
	r.idx = (r.idx + 1) % len(r.inner)
	ns, name, _ := strings.Cut(key, "/")
	r.inner[idx].UpdateObject(Named{Namespace: ns, Name: name})
}

// TODO: Add conformance for nested join collection

type manyRig struct {
	krt.Collection[Named]
	names      krt.StaticCollection[string]
	namespaces krt.StaticCollection[string]
}

func (r *manyRig) CreateObject(key string) {
	// Add to our dependency collections, the collection should merge them together
	ns, name, _ := strings.Cut(key, "/")
	r.namespaces.UpdateObject(ns)
	r.names.UpdateObject(name)
}

type fileRig struct {
	krtfiles.FileCollection[Named]
	rootPath string
	t        test.Failer
}

var metadata = krt.Metadata{"foo": "bar"}

// CreateObject is a stub
func (r *fileRig) CreateObject(key string) {
	fp := filepath.Join(r.rootPath, strings.ReplaceAll(key, "/", "_")+".yaml")
	ns, name, _ := strings.Cut(key, "/")
	contents, _ := yaml.Marshal(Named{
		Namespace: ns,
		Name:      name,
	})
	err := os.WriteFile(fp, contents, 0o600)
	assert.NoError(r.t, err)
}

// TestConformance aims to provide a 'conformance' suite for Collection implementations to ensure each collection behaves
// the same way.
// This is done by having each collection implement a small test rig that can be used to exercise various standardized paths.
// The test assumes a collection with items of any type, but with keys in the form of '<string>/<string>'; all current
// collection types can handle some type with this key.
func TestConformance(t *testing.T) {
	t.Run("informer", func(t *testing.T) {
		fc := kube.NewFakeClient()
		kc := kclient.New[*corev1.ConfigMap](fc)
		col := krt.WrapClient(kc, krt.WithStop(test.NewStop(t)), krt.WithDebugging(krt.GlobalDebugHandler), krt.WithMetadata(metadata))
		rig := &informerRig{
			Collection: col,
			client:     kc,
		}
		fc.RunAndWait(test.NewStop(t))
		runConformance[*corev1.ConfigMap](t, rig)
	})
	t.Run("static list", func(t *testing.T) {
		col := krt.NewStaticCollection[Named](nil, nil, krt.WithStop(test.NewStop(t)), krt.WithDebugging(krt.GlobalDebugHandler), krt.WithMetadata(metadata))
		rig := &staticRig{
			StaticCollection: col,
		}
		runConformance[Named](t, rig)
	})
	t.Run("join", func(t *testing.T) {
		col1 := krt.NewStaticCollection[Named](nil, nil, krt.WithStop(test.NewStop(t)), krt.WithDebugging(krt.GlobalDebugHandler))
		col2 := krt.NewStaticCollection[Named](nil, nil, krt.WithStop(test.NewStop(t)), krt.WithDebugging(krt.GlobalDebugHandler))
		j := krt.JoinCollection(
			[]krt.Collection[Named]{col1, col2},
			krt.WithStop(test.NewStop(t)),
			krt.WithDebugging(krt.GlobalDebugHandler),
			krt.WithMetadata(metadata),
		)
		rig := &joinRig{
			Collection: j,
			inner:      [2]krt.StaticCollection[Named]{col1, col2},
		}
		runConformance[Named](t, rig)
	})
	t.Run("manyCollection", func(t *testing.T) {
		namespaces := krt.NewStaticCollection[string](nil, nil, krt.WithStop(test.NewStop(t)), krt.WithDebugging(krt.GlobalDebugHandler))
		names := krt.NewStaticCollection[string](nil, nil, krt.WithStop(test.NewStop(t)), krt.WithDebugging(krt.GlobalDebugHandler))
		col := krt.NewManyCollection(namespaces, func(ctx krt.HandlerContext, ns string) []Named {
			names := krt.Fetch[string](ctx, names)
			return slices.Map(names, func(e string) Named {
				return Named{Namespace: ns, Name: e}
			})
		}, krt.WithStop(test.NewStop(t)), krt.WithDebugging(krt.GlobalDebugHandler), krt.WithMetadata(metadata))
		rig := &manyRig{
			Collection: col,
			namespaces: namespaces,
			names:      names,
		}
		runConformance[Named](t, rig)
	})
	t.Run("files", func(t *testing.T) {
		stop := test.NewStop(t)
		root := t.TempDir()
		fw, err := krtfiles.NewFolderWatch[[]byte](root, func(bytes []byte) ([][]byte, error) {
			return [][]byte{bytes}, nil
		}, stop)
		assert.NoError(t, err)
		col := krtfiles.NewFileCollection[[]byte, Named](fw, func(f []byte) *Named {
			var res Named
			err := yaml.Unmarshal(f, &res)
			if err != nil {
				return nil
			}
			return &res
		}, krt.WithStop(stop), krt.WithDebugging(krt.GlobalDebugHandler), krt.WithMetadata(metadata))
		rig := &fileRig{
			FileCollection: col,
			rootPath:       root,
			t:              t,
		}
		runConformance[Named](t, rig)
	})
}

func runConformance[T any](t *testing.T, collection Rig[T]) {
	stop := test.NewStop(t)
	// Collection should start empty...
	assert.Equal(t, len(collection.List()), 0)
	// Collection should have its metadata
	assert.Equal(t, collection.Metadata(), metadata)

	// Register a handler at the start of the collection
	earlyHandler := assert.NewTracker[string](t)
	earlyHandlerSynced := collection.Register(TrackerHandler[T](earlyHandler))

	// Ensure the collection and handler are synced
	assert.Equal(t, collection.WaitUntilSynced(stop), true)
	assert.Equal(t, earlyHandlerSynced.WaitUntilSynced(stop), true)

	// Create an object
	collection.CreateObject("a/b")
	earlyHandler.WaitOrdered("add/a/b")
	assert.Equal(t, len(collection.List()), 1)
	assert.Equal(t, collection.GetKey("a/b") != nil, true)

	// Now register one later
	lateHandler := assert.NewTracker[string](t)
	assert.Equal(t, collection.Register(TrackerHandler[T](lateHandler)).WaitUntilSynced(stop), true)
	// It should get the initial state
	lateHandler.WaitOrdered("add/a/b")

	// Handler that will be removed later
	removeHandler := assert.NewTracker[string](t)
	removeHandlerRegistration := collection.Register(TrackerHandler[T](removeHandler))
	assert.Equal(t, removeHandlerRegistration.WaitUntilSynced(stop), true)
	removeHandler.WaitOrdered("add/a/b")

	// Add a new handler that blocks events. This should not block the other handlers
	delayedSynced := collection.Register(func(o krt.Event[T]) {
		<-stop
	})
	// This should never be synced
	assert.Equal(t, delayedSynced.HasSynced(), false)

	// add another object. We should get it from both handlers
	collection.CreateObject("a/c")
	earlyHandler.WaitOrdered("add/a/c")
	lateHandler.WaitOrdered("add/a/c")
	removeHandler.WaitOrdered("add/a/c")
	assert.Equal(t, len(collection.List()), 2)
	assert.Equal(t, collection.GetKey("a/b") != nil, true)
	assert.Equal(t, collection.GetKey("a/c") != nil, true)

	removeHandlerRegistration.UnregisterHandler()

	// Add another object. Some bad implementations could handle 1 event with a blocked handler, but not >1, so make sure we catch that
	collection.CreateObject("a/d")
	earlyHandler.WaitOrdered("add/a/d")
	lateHandler.WaitOrdered("add/a/d")
	assert.Equal(t, len(collection.List()), 3)

	// Test another handler can be added even though one is blocked
	endHandler := assert.NewTracker[string](t)
	endHandlerSynced := collection.Register(TrackerHandler[T](endHandler))
	assert.Equal(t, endHandlerSynced.WaitUntilSynced(stop), true)
	endHandler.WaitUnordered("add/a/b", "add/a/c", "add/a/d")

	// Now, we want to test some race conditions.
	// We will trigger a bunch of ADD operations, and register a handler sometime in-between
	// The handler should not get any duplicates or missed events.
	keys := []string{}
	for n := range 20 {
		keys = append(keys, fmt.Sprintf("a/%v", n))
	}
	raceHandler := assert.NewTracker[string](t)
	go func() {
		for _, k := range keys {
			collection.CreateObject(k)
		}
	}()
	// Introduce some small jitter to help ensure we don't always just register first
	// nolint: gosec // just for testing
	time.Sleep(time.Microsecond * time.Duration(rand.Int31n(100)))
	raceHandlerSynced := collection.Register(TrackerHandler[T](raceHandler))
	assert.Equal(t, raceHandlerSynced.WaitUntilSynced(stop), true)
	want := []string{"add/a/b", "add/a/c", "add/a/d"}
	for _, k := range keys {
		want = append(want, fmt.Sprintf("add/%v", k))
	}
	// We should get every event exactly one time
	raceHandler.WaitUnordered(want...)
	raceHandler.Empty()

	removeHandler.Empty()
}
