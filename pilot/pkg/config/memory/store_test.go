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

package memory

import (
	"strconv"
	"testing"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/mock"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
)

func TestStoreInvariant(t *testing.T) {
	store := Make(collections.Mocks)
	mock.CheckMapInvariant(store, t, "some-namespace", 10)
}

func TestIstioConfig(t *testing.T) {
	store := Make(collections.Pilot)
	mock.CheckIstioConfigTypes(store, "some-namespace", t)
}

func BenchmarkStoreGet(b *testing.B) {
	s := initStore(b)
	gvk := config.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1alpha3",
		Kind:    "ServiceEntry",
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		// get one thousand times
		for i := 0; i < 1000; i++ {
			if s.Get(gvk, strconv.Itoa(i), "ns") == nil {
				b.Fatal("get failed")
			}
		}
	}
}

func BenchmarkStoreList(b *testing.B) {
	s := initStore(b)
	gvk := config.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1alpha3",
		Kind:    "ServiceEntry",
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		s.List(gvk, "")
	}
}

func BenchmarkStoreCreate(b *testing.B) {
	for n := 0; n < b.N; n++ {
		initStore(b)
	}
}

func BenchmarkStoreUpdate(b *testing.B) {
	cfg := config.Config{
		Meta: config.Meta{
			GroupVersionKind: config.GroupVersionKind{
				Group:   "networking.istio.io",
				Version: "v1alpha3",
				Kind:    "ServiceEntry",
			},
			Namespace: "ns",
		},
		Spec: &v1alpha3.ServiceEntry{
			Hosts: []string{"www.foo.com"},
		},
	}
	for n := 0; n < b.N; n++ {
		b.StopTimer()
		s := initStore(b)
		b.StartTimer()
		// update one thousand times
		for i := 0; i < 1000; i++ {
			cfg.Name = strconv.Itoa(i)
			cfg.Spec.(*v1alpha3.ServiceEntry).Hosts[0] = cfg.Name
			if _, err := s.Update(cfg); err != nil {
				b.Fatalf("update failed: %v", err)
			}
		}
	}
}

func BenchmarkStoreDelete(b *testing.B) {
	gvk := config.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1alpha3",
		Kind:    "ServiceEntry",
	}
	for n := 0; n < b.N; n++ {
		b.StopTimer()
		s := initStore(b)
		b.StartTimer()
		// delete one thousand times
		for i := 0; i < 1000; i++ {
			if err := s.Delete(gvk, strconv.Itoa(i), "ns", nil); err != nil {
				b.Fatalf("delete failed: %v", err)
			}
		}
	}
}

// make a new store and add 1000 configs to it
func initStore(b *testing.B) model.ConfigStore {
	s := MakeSkipValidation(collections.Pilot)
	cfg := config.Config{
		Meta: config.Meta{
			GroupVersionKind: config.GroupVersionKind{
				Group:   "networking.istio.io",
				Version: "v1alpha3",
				Kind:    "ServiceEntry",
			},
			Namespace: "ns",
		},
		Spec: &v1alpha3.ServiceEntry{
			Hosts: []string{"www.foo.com"},
		},
	}
	for i := 0; i < 1000; i++ {
		cfg.Name = strconv.Itoa(i)
		if _, err := s.Create(cfg); err != nil {
			b.Fatalf("create failed: %v", err)
		}
	}
	return s
}
