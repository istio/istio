// Copyright 2019 Istio Authors
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

package v2

import (
	"testing"

	"github.com/davecgh/go-spew/spew"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/security/authz/policy"
)

func TestBuilder_buildV2(t *testing.T) {
	labelFoo := map[string]string{
		"app": "foo",
	}
	serviceFooInNamespaceA := policy.NewServiceMetadata("foo.a.svc.cluster.local", labelFoo, t)
	testCases := []struct {
		name         string
		policies     []*model.Config
		wantRules    map[string][]string
		forTCPFilter bool
	}{
		{
			name: "no policy",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			authzPolicies := policy.NewAuthzPolicies(tc.policies, t)
			if authzPolicies == nil {
				t.Fatal("failed to create authz policies")
			}
			b := NewGenerator(serviceFooInNamespaceA, authzPolicies, false)
			if b == nil {
				t.Fatal("failed to create builder")
			}

			got := b.Generate(tc.forTCPFilter)
			gotStr := spew.Sdump(got)

			if got.GetRules() == nil {
				t.Fatal("rule must not be nil")
			}
			if err := policy.Verify(got.GetRules(), tc.wantRules); err != nil {
				t.Fatalf("%s\n%s", err, gotStr)
			}
		})
	}
}
