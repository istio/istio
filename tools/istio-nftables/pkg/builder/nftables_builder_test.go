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
	"context"
	"path/filepath"
	"testing"

	"sigs.k8s.io/knftables"

	testutil "istio.io/istio/pilot/test/util"
	"istio.io/istio/tools/common/config"
)

var (
	testTable = "inet-table"
	testChain = "test-chain"
)

func compareToGolden(t *testing.T, name string, actual string) {
	t.Helper()
	gotBytes := []byte(actual)
	goldenFile := filepath.Join("testdata", name+".golden")
	testutil.CompareContent(t, gotBytes, goldenFile)
}

// buildRules creates a knftables.Fake interface for testing. It does not apply rules to the target system.
func buildRules(t *testing.T, builder *NftablesRuleBuilder) string {
	nft := knftables.NewFake("", "")
	tx := nft.NewTransaction()
	tx.Add(&knftables.Table{Name: testTable, Family: knftables.InetFamily})
	tx.Add(&knftables.Chain{
		Name:   testChain,
		Table:  testTable,
		Family: knftables.InetFamily,
	})

	// we use chainRuleCount to keep track of how many rules have been added to each chain.
	chainRuleCount := make(map[string]int)
	for _, rule := range builder.Rules[testTable] {
		// In IPtables, inserting a rule at position 1 means it gets placed at the head of the chain. In contrast,
		// nftables starts rule indexing at 0. However, nftables does not allow inserting a rule at index 0 when
		// the chain is empty, nor does it allow inserting a rule at position N if the chain already contains N rules.
		// To handle these cases, we check if the chain is empty and convert it to an append operation.
		if rule.Index != nil && (chainRuleCount[rule.Chain] == 0 || *rule.Index >= chainRuleCount[rule.Chain]) {
			rule.Index = nil
		}
		// When a rule includes the Index, its considered as an Insert request.
		if rule.Index != nil {
			tx.Insert(&rule)
		} else {
			tx.Add(&rule)
		}
		chainRuleCount[rule.Chain]++
	}
	if err := nft.Run(context.TODO(), tx); err != nil {
		t.Fatalf("error from TestBuilder buildRules: %v", err)
	}
	return nft.Dump()
}

func TestBuilder(t *testing.T) {
	cases := []struct {
		name   string
		config func(builder *NftablesRuleBuilder)
	}{
		{
			"append-single-v4",
			func(builder *NftablesRuleBuilder) {
				builder.AppendRule(testChain, testTable, "ip", "daddr", "127.0.0.1", "return")
			},
		},
		{
			"append-single-v6",
			func(builder *NftablesRuleBuilder) {
				builder.AppendV6RuleIfSupported(testChain, testTable, "ip6", "daddr", "::ffff:7f00:1", "return")
			},
		},
		{
			"append-multi",
			func(builder *NftablesRuleBuilder) {
				builder.AppendRule(testChain, testTable, "tcp", "dport", "15008", "return")
				builder.AppendRule(testChain, testTable, "meta", "l4proto", "tcp", "jump", testChain)
				builder.AppendRule(testChain, testTable, "meta", "l4proto", "tcp", "redirect", "to", ":15001")
			},
		},
		{
			"insert-single-v4",
			func(builder *NftablesRuleBuilder) {
				builder.AppendRule(testChain, testTable, "tcp", "dport", "15008", "return")
				builder.InsertRule(testChain, testTable, 0, "ip", "daddr", "127.0.0.1", "return")
			},
		},
		{
			"insert-single-v6",
			func(builder *NftablesRuleBuilder) {
				builder.AppendRule(testChain, testTable, "tcp", "dport", "15008", "return")
				builder.InsertV6RuleIfSupported(testChain, testTable, 0, "ip6", "daddr", "::ffff:7f00:1", "return")
			},
		},
		{
			"insert-multi",
			func(builder *NftablesRuleBuilder) {
				builder.AppendRule(testChain, testTable, "tcp", "dport", "15008", "return")
				builder.InsertRule(testChain, testTable, 0, "meta", "l4proto", "tcp", "jump", testChain)
				builder.InsertRule(testChain, testTable, 1, "meta", "l4proto", "tcp", "redirect", "to", ":15001")
			},
		},
		{
			"append-insert-multi",
			func(builder *NftablesRuleBuilder) {
				builder.AppendRule(testChain, testTable, "tcp", "dport", "15008", "return")
				builder.InsertRule(testChain, testTable, 0, "meta", "l4proto", "tcp", "jump", testChain)
				builder.AppendRule(testChain, testTable, "meta", "l4proto", "tcp", "redirect", "to", ":15001")
			},
		},
		{
			"multi-rules-v4-v6",
			func(builder *NftablesRuleBuilder) {
				builder.AppendRule(testChain, testTable, "ip", "daddr", "127.0.0.1", "return")
				builder.InsertRule(testChain, testTable, 0, "tcp", "dport", "15008", "return")
				builder.AppendV6RuleIfSupported(testChain, testTable, "ip6", "daddr", "::ffff:7f00:1", "return")
			},
		},
	}

	builderConfig := &config.Config{
		EnableIPv6: true,
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			nftables := NewNftablesRuleBuilder(builderConfig)
			tt.config(nftables)
			compareToGolden(t, tt.name, buildRules(t, nftables))
		})
	}
}
