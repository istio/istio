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
	"testing"

	"github.com/onsi/gomega"

	"istio.io/istio/pilot/pkg/config/aggregate"
	"istio.io/istio/pilot/pkg/config/aggregate/fakes"
	"istio.io/istio/pilot/pkg/model"
)

func TestAggregateStoreBasicMake(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	storeOne := &fakes.ConfigStoreCache{}
	storeTwo := &fakes.ConfigStoreCache{}

	storeOne.ConfigDescriptorReturns([]model.ProtoSchema{{
		Type:        "some-config",
		Plural:      "some-configs",
		MessageName: "istio.networking.v1alpha3.DestinationRule",
	}})

	storeTwo.ConfigDescriptorReturns([]model.ProtoSchema{{
		Type:        "other-config",
		Plural:      "other-configs",
		MessageName: "istio.networking.v1alpha3.Gateway",
	}})

	stores := []model.ConfigStore{storeOne, storeTwo}

	store, err := aggregate.Make(stores)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	descriptors := store.ConfigDescriptor()
	g.Expect(descriptors).To(gomega.HaveLen(2))
	g.Expect(descriptors).To(gomega.ConsistOf([]model.ProtoSchema{
		{
			Type:        "some-config",
			Plural:      "some-configs",
			MessageName: "istio.networking.v1alpha3.DestinationRule",
		},
		{
			Type:        "other-config",
			Plural:      "other-configs",
			MessageName: "istio.networking.v1alpha3.Gateway",
		},
	}))
}

func TestAggregateStoreMakeValidationFailure(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	storeOne := &fakes.ConfigStoreCache{}
	storeOne.ConfigDescriptorReturns([]model.ProtoSchema{{
		Type:        "some-config",
		Plural:      "some-configs",
		MessageName: "broken message name",
	}})

	stores := []model.ConfigStore{storeOne}

	store, err := aggregate.Make(stores)
	g.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("cannot discover proto message type")))
	g.Expect(store).To(gomega.BeNil())
}

func TestAggregateStoreGet(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	storeOne := &fakes.ConfigStoreCache{}
	storeTwo := &fakes.ConfigStoreCache{}

	storeOne.ConfigDescriptorReturns([]model.ProtoSchema{{
		Type:        "some-config",
		Plural:      "some-configs",
		MessageName: "istio.networking.v1alpha3.DestinationRule",
	}})

	configReturn := &model.Config{
		ConfigMeta: model.ConfigMeta{
			Type: "some-config",
			Name: "other",
		},
	}

	storeOne.GetReturns(configReturn, true)

	stores := []model.ConfigStore{storeOne, storeTwo}

	store, err := aggregate.Make(stores)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c, exists := store.Get("some-config", "other", "")
	g.Expect(exists).To(gomega.BeTrue())
	g.Expect(c).To(gomega.Equal(configReturn))
}

func TestAggregateStoreList(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	storeOne := &fakes.ConfigStoreCache{}
	storeTwo := &fakes.ConfigStoreCache{}

	storeTwo.ConfigDescriptorReturns([]model.ProtoSchema{{
		Type:        "some-config",
		Plural:      "some-configs",
		MessageName: "istio.networking.v1alpha3.Gateway",
	}})

	storeOne.ConfigDescriptorReturns([]model.ProtoSchema{{
		Type:        "some-config",
		Plural:      "some-configs",
		MessageName: "istio.networking.v1alpha3.DestinationRule",
	}})

	storeOne.ListReturns([]model.Config{
		{
			ConfigMeta: model.ConfigMeta{
				Type: "some-config",
				Name: "other",
			},
		},
	}, nil)
	storeTwo.ListReturns([]model.Config{
		{
			ConfigMeta: model.ConfigMeta{
				Type: "some-config",
				Name: "another",
			},
		},
	}, nil)

	stores := []model.ConfigStore{storeOne, storeTwo}

	store, err := aggregate.Make(stores)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	l, err := store.List("some-config", "")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(l).To(gomega.HaveLen(2))
}

func TestAggregateStoreFails(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	storeOne := &fakes.ConfigStoreCache{}
	storeOne.ConfigDescriptorReturns([]model.ProtoSchema{{
		Type:        "other-config",
		Plural:      "other-configs",
		MessageName: "istio.networking.v1alpha3.Gateway",
	}})

	stores := []model.ConfigStore{storeOne}

	store, err := aggregate.Make(stores)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	t.Run("Fails to Delete", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		err = store.Delete("not", "gonna", "work")
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

	storeOne := &fakes.ConfigStoreCache{}
	storeTwo := &fakes.ConfigStoreCache{}

	storeOne.ConfigDescriptorReturns([]model.ProtoSchema{{
		Type:        "some-config",
		Plural:      "some-configs",
		MessageName: "istio.networking.v1alpha3.DestinationRule",
	}})

	storeTwo.ConfigDescriptorReturns([]model.ProtoSchema{{
		Type:        "other-config",
		Plural:      "other-configs",
		MessageName: "istio.networking.v1alpha3.Gateway",
	}})

	stores := []model.ConfigStoreCache{storeOne, storeTwo}

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
			storeOne.HasSyncedReturns(syncStatus.storeOne)
			storeTwo.HasSyncedReturns(syncStatus.storeTwo)
			ss := cacheStore.HasSynced()
			g.Expect(ss).To(gomega.Equal(syncStatus.expect))
		}
	})

	t.Run("it registers an event handler", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		cacheStore.RegisterEventHandler("some-config", func(model.Config, model.Event) {})

		typ, h := storeOne.RegisterEventHandlerArgsForCall(0)
		g.Expect(typ).To(gomega.Equal("some-config"))
		g.Expect(h).ToNot(gomega.BeNil())
	})
}
