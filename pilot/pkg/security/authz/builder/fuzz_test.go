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
	fuzz.BaseCases(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		fg := fuzz.New(t, data)
		bundle := fuzz.Struct[trustdomain.Bundle](fg)
		push := fuzz.Struct[*model.PushContext](fg, validatePush)
		node := fuzz.Struct[*model.Proxy](fg)
		policies := push.AuthzPolicies.ListAuthorizationPolicies(node.ConfigNamespace, node.Metadata.Labels)
		option := fuzz.Struct[Option](fg)
		New(bundle, push, policies, option).BuildHTTP()
	})
}

func FuzzBuildTCP(f *testing.F) {
	fuzz.BaseCases(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		fg := fuzz.New(t, data)
		bundle := fuzz.Struct[trustdomain.Bundle](fg)
		push := fuzz.Struct[*model.PushContext](fg, validatePush)
		node := fuzz.Struct[*model.Proxy](fg)
		policies := push.AuthzPolicies.ListAuthorizationPolicies(node.ConfigNamespace, node.Metadata.Labels)
		option := fuzz.Struct[Option](fg)
		New(bundle, push, policies, option).BuildTCP()
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
