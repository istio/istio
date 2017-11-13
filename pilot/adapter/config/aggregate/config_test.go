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

	"istio.io/istio/pilot/adapter/config/aggregate"
	"istio.io/istio/pilot/adapter/config/memory"
	"istio.io/istio/pilot/model"
	"istio.io/istio/pilot/test/mock"
)

const (
	// TestNamespace for testing
	TestNamespace = "test"
)

func TestStoreInvariant(t *testing.T) {
	store, _ := makeCache(t)
	mock.CheckMapInvariant(store, t, "", 10)
}

func TestStoreValidation(t *testing.T) {
	mockStore := memory.Make(mock.Types)
	if _, err := aggregate.Make([]model.ConfigStore{mockStore, mockStore}); err == nil {
		t.Error("expected error in duplicate types in the config store")
	}
}

func makeCache(t *testing.T) (model.ConfigStore, model.ConfigStoreCache) {
	mockStore := memory.Make(mock.Types)
	mockStoreCache := memory.NewController(mockStore)
	istioStore := memory.Make(model.IstioConfigTypes)
	istioStoreCache := memory.NewController(istioStore)

	store, err := aggregate.Make([]model.ConfigStore{mockStore, istioStore})
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	ctl, err := aggregate.MakeCache([]model.ConfigStoreCache{mockStoreCache, istioStoreCache})
	if err != nil {
		t.Fatal("unexpected error %v", err)
	}
	return store, ctl
}

// TODO: fix race conditions and re-enable tests.
//func TestControllerEvents(t *testing.T) {
//	_, ctl := makeCache(t)
//	mock.CheckCacheEvents(ctl, ctl, TestNamespace, 5, t)
//}
//
//func TestControllerCacheFreshness(t *testing.T) {
//	_, ctl := makeCache(t)
//	mock.CheckCacheFreshness(ctl, TestNamespace, t)
//}
//
// func TestControllerClientSync(t *testing.T) {
// 	store, ctl := makeCache(t)
// 	mock.CheckCacheSync(store, ctl, TestNamespace, 5, t)
// }
