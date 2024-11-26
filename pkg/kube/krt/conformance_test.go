package krt_test

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
	"testing"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
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

// TestConformance aims to provide a 'conformance' suite for Collection implementations to ensure each collection behaves
// the same way.
// This is done by having each collection implement a small test rig that can be used to exercise various standardized paths.
// The test assumes a collection with items of any type, but with keys in the form of '<string>/<string>'; all current
// collection types can handle some type with this key.
func TestConformance(t *testing.T) {
	t.Run("informer", func(t *testing.T) {
		t.Skip()
		fc := kube.NewFakeClient()
		kc := kclient.New[*corev1.ConfigMap](fc)
		col := krt.WrapClient(kc)
		rig := &informerRig{
			Collection: col,
			client:     kc,
		}
		fc.RunAndWait(test.NewStop(t))
		runConformance[*corev1.ConfigMap](t, rig)
	})
	t.Run("static list", func(t *testing.T) {
		col := krt.NewStaticCollection[Named](nil)
		rig := &staticRig{
			StaticCollection: col,
		}
		runConformance[Named](t, rig)
	})
	t.Run("join", func(t *testing.T) {
		col1 := krt.NewStaticCollection[Named](nil)
		col2 := krt.NewStaticCollection[Named](nil)
		j := krt.JoinCollection[Named]([]krt.Collection[Named]{col1, col2})
		rig := &joinRig{
			Collection: j,
			inner:      [2]krt.StaticCollection[Named]{col1, col2},
		}
		runConformance[Named](t, rig)
	})
	t.Run("manyCollection", func(t *testing.T) {
		namespaces := krt.NewStaticCollection[string](nil)
		names := krt.NewStaticCollection[string](nil)
		col := krt.NewManyCollection(namespaces, func(ctx krt.HandlerContext, ns string) []Named {
			names := krt.Fetch[string](ctx, names)
			return slices.Map(names, func(e string) Named {
				return Named{Namespace: ns, Name: e}
			})
		})
		rig := &manyRig{
			Collection: col,
			namespaces: namespaces,
			names:      names,
		}
		runConformance[Named](t, rig)
	})
}

func runConformance[T any](t *testing.T, collection Rig[T]) {
	stop := test.NewStop(t)
	// Collection should start empty...
	assert.Equal(t, len(collection.List()), 0)

	// Register a handler at the start of the collection
	earlyHandler := assert.NewTracker[string](t)
	earlyHandlerSynced := collection.Register(TrackerHandler[T](earlyHandler))

	// Ensure the collection and handler are synced
	assert.Equal(t, collection.Synced().WaitUntilSynced(stop), true)
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
	assert.Equal(t, len(collection.List()), 2)
	assert.Equal(t, collection.GetKey("a/b") != nil, true)
	assert.Equal(t, collection.GetKey("a/c") != nil, true)

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
}
