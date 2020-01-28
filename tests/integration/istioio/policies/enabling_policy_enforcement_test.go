// Copyright 2020 Istio Authors
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

package policies

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/istioio"
)

// https://preliminary.istio.io/docs/tasks/policy-enforcement/enabling-policy/
func TestEnablingPolicyEnforcement(t *testing.T) {
	framework.
		NewTest(t).
		Run(istioio.NewBuilder("tasks__policy_enforcement__enabling_policy").
			Add(istioio.Script{
				Input: istioio.Path("scripts/enabling_policy_enforcement.txt"),
			}).
			Defer(istioio.Script{
				Input: istioio.Path("scripts/revert_policy_enforcement.txt"),
			}).
			Build())
}
