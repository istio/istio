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

	"github.com/onsi/gomega"
	"go.uber.org/atomic"

	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/legacy/testing/fixtures"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/test/util/retry"
)

func TestAggregateStoreBasicMake(t *testing.T) {
	g := gomega.NewWithT(t)

	schema1 := collections.K8SGatewayApiV1Alpha2Httproutes
	schema2 := collections.K8SGatewayApiV1Alpha2Gatewayclasses
	store1 := memory.Make(collection.SchemasFor(schema1))
	store2 := memory.Make(collection.SchemasFor(schema2))

	stores := []model.ConfigStore{store1, store2}

	store, err := makeStore(stores, nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	schemas := store.Schemas()
	g.Expect(schemas.All()).To(gomega.HaveLen(2))
	fixtures.ExpectEqual(t, schemas, collection.SchemasFor(schema1, schema2))
}

func TestAggregateStoreMakeValidationFailure(t *testing.T) {
	g := gomega.NewWithT(t)

	store1 := memory.Make(collection.SchemasFor(schemaFor("SomeConfig", "broken message name")))

	stores := []model.ConfigStore{store1}

	store, err := makeStore(stores, nil)
	g.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("not found: broken message name")))
	g.Expect(store).To(gomega.BeNil())
}

func TestAggregateStoreGet(t *testing.T) {
	g := gomega.NewWithT(t)

	store1 := memory.Make(collection.SchemasFor(collections.K8SGatewayApiV1Alpha2Gatewayclasses))
	store2 := memory.Make(collection.SchemasFor(collections.K8SGatewayApiV1Alpha2Gatewayclasses))

	configReturn := &config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.GatewayClass,
			Name:             "other",
		},
	}

	_, err := store1.Create(*configReturn)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	stores := []model.ConfigStore{store1, store2}

	store, err := makeStore(stores, nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c := store.Get(gvk.GatewayClass, "other", "")
	g.Expect(c.Name).To(gomega.Equal(configReturn.Name))
}

func TestAggregateStoreList(t *testing.T) {
	g := gomega.NewWithT(t)

	store1 := memory.Make(collection.SchemasFor(collections.K8SGatewayApiV1Alpha2Httproutes))
	store2 := memory.Make(collection.SchemasFor(collections.K8SGatewayApiV1Alpha2Httproutes))

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

	stores := []model.ConfigStore{store1, store2}

	store, err := makeStore(stores, nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	l, err := store.List(gvk.HTTPRoute, "")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(l).To(gomega.HaveLen(2))
}

func TestAggregateStoreWrite(t *testing.T) {
	g := gomega.NewWithT(t)

	store1 := memory.Make(collection.SchemasFor(collections.K8SGatewayApiV1Alpha2Httproutes))
	store2 := memory.Make(collection.SchemasFor(collections.K8SGatewayApiV1Alpha2Httproutes))

	stores := []model.ConfigStore{store1, store2}

	store, err := makeStore(stores, store1)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	if _, err := store.Create(config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.HTTPRoute,
			Name:             "other",
		},
	}); err != nil {
		t.Fatal(err)
	}

	la, err := store.List(gvk.HTTPRoute, "")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(la).To(gomega.HaveLen(1))
	g.Expect(la[0].Name).To(gomega.Equal("other"))

	l, err := store1.List(gvk.HTTPRoute, "")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(l).To(gomega.HaveLen(1))
	g.Expect(l[0].Name).To(gomega.Equal("other"))

	// Check the aggregated and individual store return identical response
	g.Expect(la).To(gomega.BeEquivalentTo(l))

	l, err = store2.List(gvk.HTTPRoute, "")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(l).To(gomega.HaveLen(0))
}

func TestAggregateStoreWriteWithoutWriter(t *testing.T) {
	g := gomega.NewWithT(t)

	store1 := memory.Make(collection.SchemasFor(collections.K8SGatewayApiV1Alpha2Httproutes))
	store2 := memory.Make(collection.SchemasFor(collections.K8SGatewayApiV1Alpha2Httproutes))

	stores := []model.ConfigStore{store1, store2}

	store, err := makeStore(stores, nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())

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
	g := gomega.NewWithT(t)

	store1 := memory.Make(collection.SchemasFor(schemaFor("OtherConfig", "istio.networking.v1alpha3.Gateway")))

	stores := []model.ConfigStore{store1}

	store, err := makeStore(stores, nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	t.Run("Fails to Delete", func(t *testing.T) {
		g := gomega.NewWithT(t)

		err = store.Delete(config.GroupVersionKind{Kind: "not"}, "gonna", "work", nil)
		g.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("unsupported operation")))
	})

	t.Run("Fails to Create", func(t *testing.T) {
		g := gomega.NewWithT(t)

		c, err := store.Create(config.Config{})
		g.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("unsupported operation")))
		g.Expect(c).To(gomega.BeEmpty())
	})

	t.Run("Fails to Update", func(t *testing.T) {
		g := gomega.NewWithT(t)

		c, err := store.Update(config.Config{})
		g.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("unsupported operation")))
		g.Expect(c).To(gomega.BeEmpty())
	})
}

func TestAggregateStoreCache(t *testing.T) {
	stop := make(chan struct{})
	defer func() { close(stop) }()

	store1 := memory.Make(collection.SchemasFor(collections.K8SGatewayApiV1Alpha2Httproutes))
	controller1 := memory.NewController(store1)
	go controller1.Run(stop)

	store2 := memory.Make(collection.SchemasFor(collections.K8SGatewayApiV1Alpha2Gatewayclasses))
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

func schemaFor(kind, proto string) collection.Schema {
	return collection.Builder{
		Name: strings.ToLower(kind),
		Resource: resource.Builder{
			Kind:   kind,
			Plural: strings.ToLower(kind) + "s",
			Proto:  proto,
		}.BuildNoValidate(),
	}.MustBuild()
}
