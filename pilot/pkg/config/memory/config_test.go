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

package memory_test

import (
	"testing"

	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/test/mock"
	"istio.io/istio/pkg/config/schema/collections"
)

func TestStoreInvariant(t *testing.T) {
	store := memory.Make(collections.Mocks)
	mock.CheckMapInvariant(store, t, "some-namespace", 10)
}

func TestIstioConfig(t *testing.T) {
	store := memory.Make(collections.Pilot)
	mock.CheckIstioConfigTypes(store, "some-namespace", t)
}
