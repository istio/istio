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
	"go.uber.org/atomic"

	"istio.io/istio/galley/pkg/config/testing/fixtures"
	"istio.io/istio/pilot/pkg/config/aggregate"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/test/util/retry"
)

func TestAggregateStoreBasicMake(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	schema1 := collections.K8SServiceApisV1Alpha1Httproutes
	schema2 := collections.K8SServiceApisV1Alpha1Gatewayclasses
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

	store1 := memory.Make(collection.SchemasFor(collections.K8SServiceApisV1Alpha1Gatewayclasses))
	store2 := memory.Make(collection.SchemasFor(collections.K8SServiceApisV1Alpha1Gatewayclasses))

	configReturn := &model.Config{
		ConfigMeta: model.ConfigMeta{
			GroupVersionKind: collections.K8SServiceApisV1Alpha1Gatewayclasses.Resource().GroupVersionKind(),
			Name:             "other",
		},
	}

	store1.Create(*configReturn)

	stores := []model.ConfigStore{store1, store2}

	store, err := aggregate.Make(stores)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c := store.Get(collections.K8SServiceApisV1Alpha1Gatewayclasses.Resource().GroupVersionKind(), "other", "")
	g.Expect(c.Name).To(gomega.Equal(configReturn.Name))
}

func TestAggregateStoreList(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	store1 := memory.Make(collection.SchemasFor(collections.K8SServiceApisV1Alpha1Httproutes))
	store2 := memory.Make(collection.SchemasFor(collections.K8SServiceApisV1Alpha1Httproutes))

	if _, err := store1.Create(model.Config{
		ConfigMeta: model.ConfigMeta{
			GroupVersionKind: collections.K8SServiceApisV1Alpha1Httproutes.Resource().GroupVersionKind(),
			Name:             "other",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store2.Create(model.Config{
		ConfigMeta: model.ConfigMeta{
			GroupVersionKind: collections.K8SServiceApisV1Alpha1Httproutes.Resource().GroupVersionKind(),
			Name:             "another",
		},
	}); err != nil {
		t.Fatal(err)
	}

	stores := []model.ConfigStore{store1, store2}

	store, err := aggregate.Make(stores)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	l, err := store.List(collections.K8SServiceApisV1Alpha1Httproutes.Resource().GroupVersionKind(), "")
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
	stop := make(chan struct{})
	defer func() { close(stop) }()

	store1 := memory.Make(collection.SchemasFor(collections.K8SServiceApisV1Alpha1Httproutes))
	controller1 := memory.NewController(store1)
	go controller1.Run(stop)

	store2 := memory.Make(collection.SchemasFor(collections.K8SServiceApisV1Alpha1Gatewayclasses))
	controller2 := memory.NewController(store2)
	go controller2.Run(stop)

	stores := []model.ConfigStoreCache{controller1, controller2}

	cacheStore, err := aggregate.MakeCache(stores)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("it registers an event handler", func(t *testing.T) {
		handled := atomic.NewBool(false)
		cacheStore.RegisterEventHandler(collections.K8SServiceApisV1Alpha1Httproutes.Resource().GroupVersionKind(), func(model.Config, model.Config, model.Event) {
			handled.Store(true)
		})

		controller1.Create(model.Config{
			ConfigMeta: model.ConfigMeta{
				GroupVersionKind: collections.K8SServiceApisV1Alpha1Httproutes.Resource().GroupVersionKind(),
				Name:             "another",
			},
		})
		retry.UntilSuccessOrFail(t, func() error {
			if !handled.Load() {
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
