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
	"testing"

	fuzz "github.com/AdaLogics/go-fuzz-headers"
	rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	"k8s.io/apimachinery/pkg/types"

	authzpb "istio.io/api/security/v1beta1"
	"istio.io/istio/pilot/pkg/security/trustdomain"
)

// FuzzModel_New_Migrate_Generate creates a new model New(..)
// and then invokes (*Model).MigrateTrustDomain(..), and (*Model).Generate(..).
func FuzzModel_New_Migrate_Generate(f *testing.F) {
	// Seed corpus example
	f.Add(
		[]byte("seed"),  // data for GenerateStruct(rule)
		"default",       // ns
		"policy",        // name
		"cluster.local", // td
		"alias-1",       // a1
		"alias-2",       // a2
		"alias-3",       // a3
		"",              // a4
		"",              // a5
		"",              // a6
		uint8(2),        // aliasCount
		true,            // forTCP
		false,           // useAuthenticated
		byte(0),         // actionByte
	)

	f.Fuzz(func(
		t *testing.T,
		data []byte,
		ns, name string,
		td string,
		a1, a2, a3, a4, a5, a6 string,
		aliasCount uint8,
		forTCP, useAuthenticated bool,
		actionByte byte,
	) {
		rule := &authzpb.Rule{}
		if err := fuzz.NewConsumer(data).GenerateStruct(rule); err != nil {
			t.Skip()
		}
		for _, from := range rule.From {
			if from == nil {
				t.Skip()
			}
		}
		for _, to := range rule.To {
			if to == nil {
				t.Skip()
			}
		}

		if ns == "" {
			ns = "default"
		}
		if name == "" {
			name = "policy"
		}
		policyName := types.NamespacedName{Namespace: ns, Name: name}

		// Create the model
		m, err := New(policyName, rule)
		if err != nil || m == nil {
			t.Skip()
		}

		if td == "" {
			td = "cluster.local"
		}
		if aliasCount > 6 {
			aliasCount = 6
		}
		allAliases := []string{a1, a2, a3, a4, a5, a6}
		aliases := make([]string, 0, aliasCount)
		for i := uint8(0); i < aliasCount; i++ {
			aliases = append(aliases, allAliases[i])
		}
		tdBundle := trustdomain.NewBundle(td, aliases)
		m.MigrateTrustDomain(tdBundle)

		// 4) Action from fuzzed byte
		var action rbacpb.RBAC_Action
		if actionByte%2 == 0 {
			action = rbacpb.RBAC_ALLOW
		} else {
			action = rbacpb.RBAC_DENY
		}

		_, _ = m.Generate(forTCP, useAuthenticated, action)
	})
}
