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

package status

import (
	"strconv"

	"istio.io/istio/pkg/config"
	"istio.io/pkg/ledger"
)

func tryLedgerPut(configLedger ledger.Ledger, obj config.Config) {
	key := config.Key(obj.GroupVersionKind.Kind, obj.Name, obj.Namespace)
	if _, err := configLedger.Put(key, strconv.FormatInt(obj.Generation, 10)); err != nil {
		scope.Errorf("Failed to update %s in ledger, status will be out of date.", key)
	}
}

func tryLedgerDelete(configLedger ledger.Ledger, obj config.Config) {
	key := config.Key(obj.GroupVersionKind.Kind, obj.Name, obj.Namespace)
	if err := configLedger.Delete(key); err != nil {
		scope.Errorf("Failed to delete %s in ledger, status will be out of date.", key)
	}
}
