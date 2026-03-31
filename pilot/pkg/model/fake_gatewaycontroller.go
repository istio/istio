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

package model

import (
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/model/credentials"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/util/sets"
)

type FakeController struct {
	ConfigStoreController
	GatewaysWithInferencePools sets.Set[types.NamespacedName]
}

func (f FakeController) HasInferencePool(gw types.NamespacedName) bool {
	return f.GatewaysWithInferencePools.Contains(gw)
}

func (f FakeController) Reconcile(_ *PushContext) {}

// NOTE: To simplify test setup, if CredentialName contains 'allowed-ns', always return true.
func (f FakeController) SecretAllowed(_ config.GroupVersionKind, resourceName string, namespace string) bool {
	parse, err := credentials.ParseResourceName(resourceName, namespace, "", "")
	if err != nil {
		return false
	}
	return parse.Namespace == "allowed-ns"
}
