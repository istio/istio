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

package aggregate

import (
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/gomega"
	"go.uber.org/atomic"

	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/test/util/retry"
)

func TestAggregateStoreBasicMake(t *testing.T) {
	g := NewWithT(t)

	schema1 := collections.HTTPRoute
	schema2 := collections.GatewayClass
	store1 := memory.NewController(memory.Make(collection.SchemasFor(schema1)))
	store2 := memory.NewController(memory.Make(collection.SchemasFor(schema2)))

	stores := []model.ConfigStoreController{store1, store2}

	store, err := makeStore(stores, nil)
	g.Expect(err).NotTo(HaveOccurred())

	schemas := store.Schemas()
	g.Expect(cmp.Diff(schemas, collection.SchemasFor(schema1, schema2))).To(BeEmpty())
}

func TestAggregateStoreMakeValidationFailure(t *testing.T) {
	g := NewWithT(t)

	store1 := memory.NewController(memory.Make(collection.SchemasFor(schemaFor("SomeConfig", "broken message name"))))

	stores := []model.ConfigStoreController{store1}

	store, err := makeStore(stores, nil)
	g.Expect(err).To(MatchError(ContainSubstring("not found: broken message name")))
	g.Expect(store).To(BeNil())
}

func TestAggregateStoreGet(t *testing.T) {
	g := NewWithT(t)

	store1 := memory.NewController(memory.Make(collection.SchemasFor(collections.GatewayClass)))
	store2 := memory.NewController(memory.Make(collection.SchemasFor(collections.GatewayClass)))

	configReturn := &config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.GatewayClass,
			Name:             "other",
		},
	}

	_, err := store1.Create(*configReturn)
	g.Expect(err).NotTo(HaveOccurred())

	stores := []model.ConfigStoreController{store1, store2}

	store, err := makeStore(stores, nil)
	g.Expect(err).NotTo(HaveOccurred())

	c := store.Get(gvk.GatewayClass, "other", "")
	g.Expect(c.Name).To(Equal(configReturn.Name))
}

func TestAggregateStoreList(t *testing.T) {
	g := NewWithT(t)

	store1 := memory.NewController(memory.Make(collection.SchemasFor(collections.HTTPRoute)))
	store2 := memory.NewController(memory.Make(collection.SchemasFor(collections.HTTPRoute)))

	if _, err := store1.Create(config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.HTTPRoute,
			Name:             "other",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store2.Create(config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.HTTPRoute,
			Name:             "another",
		},
	}); err != nil {
		t.Fatal(err)
	}

	stores := []model.ConfigStoreController{store1, store2}

	store, err := makeStore(stores, nil)
	g.Expect(err).NotTo(HaveOccurred())

	l := store.List(gvk.HTTPRoute, "")
	g.Expect(l).To(HaveLen(2))
}

func TestAggregateStoreWrite(t *testing.T) {
	g := NewWithT(t)

	store1 := memory.NewController(memory.Make(collection.SchemasFor(collections.HTTPRoute)))
	store2 := memory.NewController(memory.Make(collection.SchemasFor(collections.HTTPRoute)))

	stores := []model.ConfigStoreController{store1, store2}

	store, err := makeStore(stores, store1)
	g.Expect(err).NotTo(HaveOccurred())

	if _, err := store.Create(config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.HTTPRoute,
			Name:             "other",
		},
	}); err != nil {
		t.Fatal(err)
	}

	la := store.List(gvk.HTTPRoute, "")
	g.Expect(la).To(HaveLen(1))
	g.Expect(la[0].Name).To(Equal("other"))

	l := store1.List(gvk.HTTPRoute, "")
	g.Expect(l).To(HaveLen(1))
	g.Expect(l[0].Name).To(Equal("other"))

	// Check the aggregated and individual store return identical response
	g.Expect(la).To(BeEquivalentTo(l))

	l = store2.List(gvk.HTTPRoute, "")
	g.Expect(l).To(HaveLen(0))
}

func TestAggregateStoreWriteWithoutWriter(t *testing.T) {
	g := NewWithT(t)

	store1 := memory.NewController(memory.Make(collection.SchemasFor(collections.HTTPRoute)))
	store2 := memory.NewController(memory.Make(collection.SchemasFor(collections.HTTPRoute)))

	stores := []model.ConfigStoreController{store1, store2}

	store, err := makeStore(stores, nil)
	g.Expect(err).NotTo(HaveOccurred())

	if _, err := store.Create(config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.HTTPRoute,
			Name:             "other",
		},
	}); err != errorUnsupported {
		t.Fatalf("unexpected error, want %v got %v", errorUnsupported, err)
	}
}

func TestAggregateStoreFails(t *testing.T) {
	g := NewWithT(t)

	store1 := memory.NewController(memory.Make(collection.SchemasFor(schemaFor("OtherConfig", "istio.networking.v1alpha3.Gateway"))))

	stores := []model.ConfigStoreController{store1}

	store, err := makeStore(stores, nil)
	g.Expect(err).NotTo(HaveOccurred())

	t.Run("Fails to Delete", func(t *testing.T) {
		g := NewWithT(t)

		err = store.Delete(config.GroupVersionKind{Kind: "not"}, "gonna", "work", nil)
		g.Expect(err).To(MatchError(ContainSubstring("unsupported operation")))
	})

	t.Run("Fails to Create", func(t *testing.T) {
		g := NewWithT(t)

		c, err := store.Create(config.Config{})
		g.Expect(err).To(MatchError(ContainSubstring("unsupported operation")))
		g.Expect(c).To(BeEmpty())
	})

	t.Run("Fails to Update", func(t *testing.T) {
		g := NewWithT(t)

		c, err := store.Update(config.Config{})
		g.Expect(err).To(MatchError(ContainSubstring("unsupported operation")))
		g.Expect(c).To(BeEmpty())
	})
}

func TestAggregateStoreCache(t *testing.T) {
	stop := make(chan struct{})
	defer func() { close(stop) }()

	store1 := memory.Make(collection.SchemasFor(collections.HTTPRoute))
	controller1 := memory.NewController(store1)
	go controller1.Run(stop)

	store2 := memory.Make(collection.SchemasFor(collections.HTTPRoute))
	controller2 := memory.NewController(store2)
	go controller2.Run(stop)

	stores := []model.ConfigStoreController{controller1, controller2}

	cacheStore, err := MakeCache(stores)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("it registers an event handler", func(t *testing.T) {
		handled := atomic.NewBool(false)
		cacheStore.RegisterEventHandler(gvk.HTTPRoute, func(config.Config, config.Config, model.Event) {
			handled.Store(true)
		})

		_, err := controller1.Create(config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.HTTPRoute,
				Name:             "another",
			},
		})
		if err != nil {
			t.Fatal(err)
		}

		retry.UntilOrFail(t, handled.Load, retry.Timeout(time.Second))
	})
}

func schemaFor(kind, proto string) resource.Schema {
	return resource.Builder{
		Kind:   kind,
		Plural: strings.ToLower(kind) + "s",
		Proto:  proto,
	}.BuildNoValidate()
}
