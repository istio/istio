// Copyright 2017 Istio Authors.
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

package aspect

// ManagerInventory holds a set of aspect managers.
type ManagerInventory struct {
	Preprocess []PreprocessManager
	Quota      []QuotaManager
}

// Inventory returns the authoritative set of aspect managers used by Mixer.
func Inventory() ManagerInventory {
	return ManagerInventory{
		Preprocess: []PreprocessManager{
			newAttrGenMgr(),
		},
		Quota: []QuotaManager{
			newQuotasManager(),
		},
	}
}
