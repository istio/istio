// Copyright 2018 Istio Authors
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

package aggregate_test

import (
	"strings"
	"testing"

	"github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/testing/fixtures"
	"istio.io/istio/pilot/pkg/config/aggregate"
	"istio.io/istio/pilot/pkg/config/aggregate/fakes"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/resource"
)

func TestAggregateStoreBasicMake(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	store1 := &fakes.ConfigStoreCache{}
	store2 := &fakes.ConfigStoreCache{}

	schema1 := schemaFor("SomeConfig", "istio.networking.v1alpha3.DestinationRule")
	schema2 := schemaFor("OtherConfig", "istio.networking.v1alpha3.Gateway")

	store1.SchemasReturns(collection.SchemasFor(schema1))
	store2.SchemasReturns(collection.SchemasFor(schema2))

	stores := []model.ConfigStore{store1, store2}

	store, err := aggregate.Make(stores)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	schemas := store.Schemas()
	g.Expect(schemas.All()).To(gomega.HaveLen(2))
	fixtures.ExpectEqual(t, schemas, collection.SchemasFor(schema1, schema2))
}

func TestAggregateStoreMakeValidationFailure(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	store1 := &fakes.ConfigStoreCache{}
	store1.SchemasReturns(collection.SchemasFor(schemaFor("SomeConfig", "broken message name")))

	stores := []model.ConfigStore{store1}

	store, err := aggregate.Make(stores)
	g.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("proto message not found")))
	g.Expect(store).To(gomega.BeNil())
}

func TestAggregateStoreGet(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	store1 := &fakes.ConfigStoreCache{}
	store2 := &fakes.ConfigStoreCache{}

	store1.SchemasReturns(collection.SchemasFor(schemaFor("SomeConfig", "istio.networking.v1alpha3.DestinationRule")))

	configReturn := &model.Config{
		ConfigMeta: model.ConfigMeta{
			Type: "SomeConfig",
			Name: "other",
		},
	}

	store1.GetReturns(configReturn)

	stores := []model.ConfigStore{store1, store2}

	store, err := aggregate.Make(stores)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c := store.Get(resource.GroupVersionKind{Kind: "SomeConfig"}, "other", "")
	g.Expect(c).To(gomega.Equal(configReturn))
}

func TestAggregateStoreList(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	store1 := &fakes.ConfigStoreCache{}
	store2 := &fakes.ConfigStoreCache{}

	store2.SchemasReturns(collection.SchemasFor(schemaFor("SomeConfig", "istio.networking.v1alpha3.Gateway")))

	store1.SchemasReturns(collection.SchemasFor(schemaFor("SomeConfig", "istio.networking.v1alpha3.DestinationRule")))

	store1.ListReturns([]model.Config{
		{
			ConfigMeta: model.ConfigMeta{
				Type: "SomeConfig",
				Name: "other",
			},
		},
	}, nil)
	store2.ListReturns([]model.Config{
		{
			ConfigMeta: model.ConfigMeta{
				Type: "SomeConfig",
				Name: "another",
			},
		},
	}, nil)

	stores := []model.ConfigStore{store1, store2}

	store, err := aggregate.Make(stores)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	l, err := store.List(resource.GroupVersionKind{Kind: "SomeConfig"}, "")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(l).To(gomega.HaveLen(2))
}

func TestAggregateStoreFails(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	store1 := &fakes.ConfigStoreCache{}
	store1.SchemasReturns(collection.SchemasFor(schemaFor("OtherConfig", "istio.networking.v1alpha3.Gateway")))

	stores := []model.ConfigStore{store1}

	store, err := aggregate.Make(stores)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	t.Run("Fails to Delete", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		err = store.Delete(resource.GroupVersionKind{Kind: "not"}, "gonna", "work")
		g.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("unsupported operation")))
	})

	t.Run("Fails to Create", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		c, err := store.Create(model.Config{})
		g.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("unsupported operation")))
		g.Expect(c).To(gomega.BeEmpty())
	})

	t.Run("Fails to Update", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		c, err := store.Update(model.Config{})
		g.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("unsupported operation")))
		g.Expect(c).To(gomega.BeEmpty())
	})
}

func TestAggregateStoreCache(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	store1 := &fakes.ConfigStoreCache{}
	store2 := &fakes.ConfigStoreCache{}

	store1.SchemasReturns(collection.SchemasFor(schemaFor("SomeConfig", "istio.networking.v1alpha3.DestinationRule")))
	store2.SchemasReturns(collection.SchemasFor(schemaFor("OtherConfig", "istio.networking.v1alpha3.Gateway")))

	stores := []model.ConfigStoreCache{store1, store2}

	cacheStore, err := aggregate.MakeCache(stores)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	t.Run("it checks sync status", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		syncStatusCases := []struct {
			storeOne bool
			storeTwo bool
			expect   bool
		}{
			{true, true, true},
			{false, true, false},
			{true, false, false},
			{false, false, false},
		}
		for _, syncStatus := range syncStatusCases {
			store1.HasSyncedReturns(syncStatus.storeOne)
			store2.HasSyncedReturns(syncStatus.storeTwo)
			ss := cacheStore.HasSynced()
			g.Expect(ss).To(gomega.Equal(syncStatus.expect))
		}
	})

	t.Run("it registers an event handler", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		cacheStore.RegisterEventHandler(resource.GroupVersionKind{Kind: "SomeConfig"}, func(model.Config, model.Config, model.Event) {})

		typ, h := store1.RegisterEventHandlerArgsForCall(0)
		g.Expect(typ).To(gomega.Equal(resource.GroupVersionKind{Kind: "SomeConfig"}))
		g.Expect(h).ToNot(gomega.BeNil())
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
