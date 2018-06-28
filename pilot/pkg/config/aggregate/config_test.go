// Copyright 2017 Istio Authors
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
		MessageName: "istio.routing.v1alpha1.IngressRule",
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
			MessageName: "istio.routing.v1alpha1.IngressRule",
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
