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

package builder

import (
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/security/trustdomain"
	"istio.io/istio/pkg/fuzz"
)

func FuzzBuildHTTP(f *testing.F) {
	fuzz.Fuzz(f, func(fg fuzz.Helper) {
		bundle := fuzz.Struct[trustdomain.Bundle](fg)
		push := fuzz.Struct[*model.PushContext](fg, validatePush)
		node := fuzz.Struct[*model.Proxy](fg)
		selectionOpts := model.WorkloadSelectionOpts{
			Namespace:      node.ConfigNamespace,
			WorkloadLabels: node.Labels,
		}
		policies := push.AuthzPolicies.ListAuthorizationPolicies(selectionOpts)
		option := fuzz.Struct[Option](fg)
		b := New(bundle, push, policies, option)
		if b == nil {
			fg.T().Skip()
			return // To help linter
		}
		b.BuildHTTP()
	})
}

func FuzzBuildTCP(f *testing.F) {
	fuzz.Fuzz(f, func(fg fuzz.Helper) {
		bundle := fuzz.Struct[trustdomain.Bundle](fg)
		push := fuzz.Struct[*model.PushContext](fg, validatePush)
		node := fuzz.Struct[*model.Proxy](fg)
		selectionOpts := model.WorkloadSelectionOpts{
			Namespace:      node.ConfigNamespace,
			WorkloadLabels: node.Labels,
		}
		policies := push.AuthzPolicies.ListAuthorizationPolicies(selectionOpts)
		option := fuzz.Struct[Option](fg)
		b := New(bundle, push, policies, option)
		if b == nil {
			fg.T().Skip()
			return // To help linter
		}
		b.BuildTCP()
	})
}

func validatePush(in *model.PushContext) bool {
	if in == nil {
		return false
	}
	if in.AuthzPolicies == nil {
		return false
	}
	return true
}
