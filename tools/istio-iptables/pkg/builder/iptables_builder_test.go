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
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	testutil "istio.io/istio/pilot/test/util"
	"istio.io/istio/tools/istio-iptables/pkg/config"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
	iptableslog "istio.io/istio/tools/istio-iptables/pkg/log"
)

func compareToGolden(t *testing.T, name string, actual string) {
	t.Helper()
	gotBytes := []byte(actual)
	goldenFile := filepath.Join("testdata", name+".golden")
	testutil.CompareContent(t, gotBytes, goldenFile)
}

func TestBuilder(t *testing.T) {
	cases := []struct {
		name         string
		expectV4     bool
		expectV6     bool
		config       func(builder *IptablesRuleBuilder)
		sharedGolden bool
	}{
		{
			"insert-single-v4",
			true,
			false,
			func(builder *IptablesRuleBuilder) {
				builder.InsertRuleV4(iptableslog.UndefinedCommand, "chain", "table", 2, "-f", "foo", "-b", "bar")
			},
			true,
		},
		{
			"insert-single-v6",
			false,
			true,
			func(builder *IptablesRuleBuilder) {
				builder.InsertRuleV6(iptableslog.UndefinedCommand, "chain", "table", 2, "-f", "foo", "-b", "bar")
			},
			true,
		},
		{
			"append-single-v4",
			true,
			false,
			func(builder *IptablesRuleBuilder) {
				builder.AppendRuleV4(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "bar")
			},
			true,
		},
		{
			"append-single-v6",
			false,
			true,
			func(builder *IptablesRuleBuilder) {
				builder.AppendRuleV6(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "bar")
			},
			true,
		},
		{
			"append-multi-v4",
			true,
			false,
			func(builder *IptablesRuleBuilder) {
				builder.AppendRuleV4(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "bar")
				builder.AppendRuleV4(iptableslog.UndefinedCommand, "chain", "table", "-f", "fu", "-b", "bar")
				builder.AppendRuleV4(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "baz")
			},
			true,
		},
		{
			"append-multi-v6",
			false,
			true,
			func(builder *IptablesRuleBuilder) {
				builder.AppendRuleV6(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "bar")
				builder.AppendRuleV6(iptableslog.UndefinedCommand, "chain", "table", "-f", "fu", "-b", "bar")
				builder.AppendRuleV6(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "baz")
			},
			true,
		},
		{
			"insert-multi-v4",
			true,
			false,
			func(builder *IptablesRuleBuilder) {
				builder.InsertRuleV4(iptableslog.UndefinedCommand, "chain", "table", 1, "-f", "foo", "-b", "bar")
				builder.InsertRuleV4(iptableslog.UndefinedCommand, "chain", "table", 2, "-f", "foo", "-b", "baaz")
				builder.InsertRuleV4(iptableslog.UndefinedCommand, "chain", "table", 3, "-f", "foo", "-b", "baz")
			},
			true,
		},
		{
			"insert-multi-v6",
			false,
			true,
			func(builder *IptablesRuleBuilder) {
				builder.InsertRuleV6(iptableslog.UndefinedCommand, "chain", "table", 1, "-f", "foo", "-b", "bar")
				builder.InsertRuleV6(iptableslog.UndefinedCommand, "chain", "table", 2, "-f", "foo", "-b", "baaz")
				builder.InsertRuleV6(iptableslog.UndefinedCommand, "chain", "table", 3, "-f", "foo", "-b", "baz")
			},
			true,
		},
		{
			"append-insert-multi-v4",
			true,
			false,
			func(builder *IptablesRuleBuilder) {
				builder.AppendRuleV4(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "bar")
				builder.InsertRuleV4(iptableslog.UndefinedCommand, "chain", "table", 2, "-f", "foo", "-b", "bar")
				builder.AppendRuleV4(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "baz")
			},
			true,
		},
		{
			"append-insert-multi-v6",
			false,
			true,
			func(builder *IptablesRuleBuilder) {
				builder.AppendRuleV6(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "bar")
				builder.InsertRuleV6(iptableslog.UndefinedCommand, "chain", "table", 2, "-f", "foo", "-b", "bar")
				builder.AppendRuleV6(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "baz")
			},
			true,
		},
		{
			"multi-rules-new-chain",
			true,
			true,
			func(builder *IptablesRuleBuilder) {
				builder.AppendRuleV4(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "bar")
				builder.InsertRuleV4(iptableslog.UndefinedCommand, "chain", "table", 2, "-f", "foo", "-b", "bar")
				builder.AppendRuleV4(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "baz")
				builder.AppendRuleV6(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "bar")
				builder.InsertRuleV6(iptableslog.UndefinedCommand, "chain", "table", 2, "-f", "foo", "-b", "bar")
				builder.InsertRuleV6(iptableslog.UndefinedCommand, "chain", "table", 1, "-f", "foo", "-b", "bar")
				builder.AppendRuleV4(iptableslog.UndefinedCommand, constants.PREROUTING, constants.NAT, "-f", "foo", "-b", "bar")
			},
			false,
		},
	}
	builderConfig := &config.Config{
		EnableIPv6: true,
	}
	checkFunc := func(goldenName string, rules [][]string, rulesRestore string, expected bool) {
		// check that rules are set
		if expected {
			var b strings.Builder
			for _, rule := range rules {
				fmt.Fprintln(&b, strings.Join(rule, " "))
			}
			compareToGolden(t, goldenName, b.String())

			// Check build restore
			compareToGolden(t, goldenName+"-restore", rulesRestore)
		} else {
			if len(rules) > 0 {
				t.Errorf("Unexpected rules to be empty but instead got %#v", rules)
			}
			if rulesRestore != "" {
				t.Errorf("Unexpected rulesRestore to be empty but instead got %s", rulesRestore)
			}
		}
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			iptables := NewIptablesRuleBuilder(builderConfig)
			tt.config(iptables)
			var goldenNameV4 string
			var goldenNameV6 string
			if tt.sharedGolden {
				goldenName := strings.ReplaceAll(tt.name, "-v4", "")
				goldenName = strings.ReplaceAll(goldenName, "-v6", "")
				goldenNameV4 = goldenName
				goldenNameV6 = goldenName
			} else {
				goldenNameV4 = tt.name + "-v4"
				goldenNameV6 = tt.name + "-v6"
			}
			checkFunc(goldenNameV4, iptables.BuildV4(), iptables.BuildV4Restore(), tt.expectV4)
			checkFunc(goldenNameV6, iptables.BuildV6(), iptables.BuildV6Restore(), tt.expectV6)
		})
	}
}
