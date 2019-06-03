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

package memory_test

import (
	"strconv"
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/test"
	"istio.io/istio/pilot/test/mock"
)

func TestStoreInvariant(t *testing.T) {
	store := memory.Make(mock.Types)
	mock.CheckMapInvariant(store, t, "some-namespace", 10)
}

func TestIstioConfig(t *testing.T) {
	store := memory.Make(model.IstioConfigTypes)
	mock.CheckIstioConfigTypes(store, "some-namespace", t)
}

func TestStoreCreateConfig(t *testing.T) {
	g := NewGomegaWithT(t)

	store := memory.Make(mock.Types)
	name := "dummy"
	config := createDummyConfig(name)

	_, err := store.Create(config)
	g.Expect(err).NotTo(HaveOccurred())

	retrievedConfig := store.Get(model.MockConfig.Type, "dummy", "")
	g.Expect(retrievedConfig.Spec).To(Equal(config.Spec))
}

func TestStoreGetReturnsCopies(t *testing.T) {
	g := NewGomegaWithT(t)

	store := memory.Make(mock.Types)
	name := "dummy"
	config := createDummyConfig(name)
	store.Create(config)

	copiedConfig := store.Get(model.MockConfig.Type, "dummy", "")

	config.Spec.(*test.MockConfig).Key = "xxx"
	g.Expect(copiedConfig.Spec).NotTo(Equal(config.Spec))
}

func TestStoreListReturnsCopy(t *testing.T) {
	g := NewGomegaWithT(t)

	store := memory.Make(mock.Types)
	name := "dummy"
	config := createDummyConfig(name)
	store.Create(config)

	storedConfigs, err := store.List(model.MockConfig.Type, "")

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(storedConfigs).To(HaveLen(1))

	config.Spec.(*test.MockConfig).Key = "xxx"
	g.Expect(storedConfigs[0].Spec).NotTo(Equal(config.Spec))
}

func createDummyConfig(name string) model.Config {
	config := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type: model.MockConfig.Type,
			Name: name,
			Labels: map[string]string{
				"key": name,
			},
			Annotations: map[string]string{
				"annotationkey": name,
			},
		},
		Spec: &test.MockConfig{
			Key: "dummy",
			Pairs: []*test.ConfigPair{{
				Key:   "dummy",
				Value: strconv.Itoa(42),
			}},
		},
	}
	return config
}
