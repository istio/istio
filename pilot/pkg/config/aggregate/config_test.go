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

package aggregate_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/testing/fixtures"
	"istio.io/istio/pilot/pkg/config/aggregate"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/test/util/retry"
)

func TestAggregateStoreBasicMake(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	schema1 := schemaFor("SomeConfig", "istio.networking.v1alpha3.DestinationRule")
	schema2 := schemaFor("OtherConfig", "istio.networking.v1alpha3.Gateway")
	store1 := memory.Make(collection.SchemasFor(schema1))
	store2 := memory.Make(collection.SchemasFor(schema2))

	stores := []model.ConfigStore{store1, store2}

	store, err := aggregate.Make(stores)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	schemas := store.Schemas()
	g.Expect(schemas.All()).To(gomega.HaveLen(2))
	fixtures.ExpectEqual(t, schemas, collection.SchemasFor(schema1, schema2))
}

func TestAggregateStoreMakeValidationFailure(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	store1 := memory.Make(collection.SchemasFor(schemaFor("SomeConfig", "broken message name")))

	stores := []model.ConfigStore{store1}

	store, err := aggregate.Make(stores)
	g.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("proto message not found")))
	g.Expect(store).To(gomega.BeNil())
}

func TestAggregateStoreGet(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	store1 := memory.Make(collection.SchemasFor(schemaFor("SomeConfig", "istio.networking.v1alpha3.DestinationRule")))
	store2 := memory.Make(collection.SchemasFor(schemaFor("SomeConfig", "istio.networking.v1alpha3.DestinationRule")))

	configReturn := &model.Config{
		ConfigMeta: model.ConfigMeta{
			Type: "SomeConfig",
			Name: "other",
		},
	}

	store1.Create(*configReturn)

	stores := []model.ConfigStore{store1, store2}

	store, err := aggregate.Make(stores)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c := store.Get(resource.GroupVersionKind{Kind: "SomeConfig"}, "other", "")
	g.Expect(c.Name).To(gomega.Equal(configReturn.Name))
}

func TestAggregateStoreList(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	store1 := memory.Make(collection.SchemasFor(schemaFor("SomeConfig", "istio.networking.v1alpha3.Gateway")))
	store2 := memory.Make(collection.SchemasFor(schemaFor("SomeConfig", "istio.networking.v1alpha3.DestinationRule")))

	store1.Create(model.Config{
		ConfigMeta: model.ConfigMeta{
			Type: "SomeConfig",
			Name: "other",
		},
	})
	store2.Create(model.Config{
		ConfigMeta: model.ConfigMeta{
			Type: "SomeConfig",
			Name: "another",
		},
	})

	stores := []model.ConfigStore{store1, store2}

	store, err := aggregate.Make(stores)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	l, err := store.List(resource.GroupVersionKind{Kind: "SomeConfig"}, "")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(l).To(gomega.HaveLen(2))
}

func TestAggregateStoreFails(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	store1 := memory.Make(collection.SchemasFor(schemaFor("OtherConfig", "istio.networking.v1alpha3.Gateway")))

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

	stop := make(chan struct{})
	defer func() { close(stop) }()

	store1 := memory.Make(collection.SchemasFor(schemaFor("SomeConfig", "istio.networking.v1alpha3.DestinationRule")))
	controller1 := memory.NewController(store1)
	go controller1.Run(stop)

	store2 := memory.Make(collection.SchemasFor(schemaFor("OtherConfig", "istio.networking.v1alpha3.Gateway")))
	controller2 := memory.NewController(store2)
	go controller2.Run(stop)

	stores := []model.ConfigStoreCache{controller1, controller2}

	cacheStore, err := aggregate.MakeCache(stores)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	t.Run("it registers an event handler", func(t *testing.T) {
		handled := false
		cacheStore.RegisterEventHandler(resource.GroupVersionKind{Kind: "SomeConfig"}, func(model.Config, model.Config, model.Event) {
			handled = true
		})

		controller1.Create(model.Config{
			ConfigMeta: model.ConfigMeta{
				Type: "SomeConfig",
				Name: "another",
			},
		})
		retry.UntilSuccessOrFail(t, func() error {
			if !handled {
				return fmt.Errorf("not handled")
			}
			return nil
		}, retry.Timeout(time.Second))
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
