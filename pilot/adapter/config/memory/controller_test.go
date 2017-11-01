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
	"testing"

	"istio.io/istio/pilot/adapter/config/memory"
	"istio.io/istio/pilot/test/mock"
)

const (
	// TestNamespace specifies the namespace for testing
	TestNamespace = "istio-memory-test"
)

func TestControllerEvents(t *testing.T) {
	store := memory.Make(mock.Types)
	ctl := memory.NewController(store)
	// Note that the operations must go through the controller since the store does not trigger back events
	mock.CheckCacheEvents(ctl, ctl, TestNamespace, 5, t)
}

func TestControllerCacheFreshness(t *testing.T) {
	store := memory.Make(mock.Types)
	ctl := memory.NewController(store)
	mock.CheckCacheFreshness(ctl, TestNamespace, t)
}

func TestControllerClientSync(t *testing.T) {
	store := memory.Make(mock.Types)
	ctl := memory.NewController(store)
	mock.CheckCacheSync(store, ctl, TestNamespace, 5, t)
}
