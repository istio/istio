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

package config

import (
	"istio.io/mixer/pkg/config/crd"
	"istio.io/mixer/pkg/config/store"
)

// StoreInventory returns the inventory of store backends.
func StoreInventory() []store.RegisterFunc {
	return nil
}

// Store2Inventory returns the inventory of Store2Backend.
func Store2Inventory() []store.RegisterFunc2 {
	return []store.RegisterFunc2{
		crd.Register,
	}
}
