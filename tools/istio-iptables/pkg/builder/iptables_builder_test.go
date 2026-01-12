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
	"reflect"
	"strings"
	"testing"

	testutil "istio.io/istio/pilot/test/util"
	"istio.io/istio/tools/common/config"
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
				builder.InsertRuleV4("chain", "table", 2, "-f", "foo", "-b", "bar")
			},
			true,
		},
		{
			"insert-single-v6",
			false,
			true,
			func(builder *IptablesRuleBuilder) {
				builder.InsertRuleV6("chain", "table", 2, "-f", "foo", "-b", "bar")
			},
			true,
		},
		{
			"append-single-v4",
			true,
			false,
			func(builder *IptablesRuleBuilder) {
				builder.AppendRuleV4("chain", "table", "-f", "foo", "-b", "bar")
			},
			true,
		},
		{
			"append-single-v6",
			false,
			true,
			func(builder *IptablesRuleBuilder) {
				builder.AppendRuleV6("chain", "table", "-f", "foo", "-b", "bar")
			},
			true,
		},
		{
			"append-multi-v4",
			true,
			false,
			func(builder *IptablesRuleBuilder) {
				builder.AppendRuleV4("chain", "table", "-f", "foo", "-b", "bar")
				builder.AppendRuleV4("chain", "table", "-f", "fu", "-b", "bar")
				builder.AppendRuleV4("chain", "table", "-f", "foo", "-b", "baz")
			},
			true,
		},
		{
			"append-multi-v6",
			false,
			true,
			func(builder *IptablesRuleBuilder) {
				builder.AppendRuleV6("chain", "table", "-f", "foo", "-b", "bar")
				builder.AppendRuleV6("chain", "table", "-f", "fu", "-b", "bar")
				builder.AppendRuleV6("chain", "table", "-f", "foo", "-b", "baz")
			},
			true,
		},
		{
			"insert-multi-v4",
			true,
			false,
			func(builder *IptablesRuleBuilder) {
				builder.InsertRuleV4("chain", "table", 1, "-f", "foo", "-b", "bar")
				builder.InsertRuleV4("chain", "table", 2, "-f", "foo", "-b", "baaz")
				builder.InsertRuleV4("chain", "table", 3, "-f", "foo", "-b", "baz")
			},
			true,
		},
		{
			"insert-multi-v6",
			false,
			true,
			func(builder *IptablesRuleBuilder) {
				builder.InsertRuleV6("chain", "table", 1, "-f", "foo", "-b", "bar")
				builder.InsertRuleV6("chain", "table", 2, "-f", "foo", "-b", "baaz")
				builder.InsertRuleV6("chain", "table", 3, "-f", "foo", "-b", "baz")
			},
			true,
		},
		{
			"append-insert-multi-v4",
			true,
			false,
			func(builder *IptablesRuleBuilder) {
				builder.AppendRuleV4("chain", "table", "-f", "foo", "-b", "bar")
				builder.InsertRuleV4("chain", "table", 2, "-f", "foo", "-b", "bar")
				builder.AppendRuleV4("chain", "table", "-f", "foo", "-b", "baz")
			},
			true,
		},
		{
			"append-insert-multi-v6",
			false,
			true,
			func(builder *IptablesRuleBuilder) {
				builder.AppendRuleV6("chain", "table", "-f", "foo", "-b", "bar")
				builder.InsertRuleV6("chain", "table", 2, "-f", "foo", "-b", "bar")
				builder.AppendRuleV6("chain", "table", "-f", "foo", "-b", "baz")
			},
			true,
		},
		{
			"multi-rules-new-chain",
			true,
			true,
			func(builder *IptablesRuleBuilder) {
				builder.AppendRuleV4("chain", "table", "-f", "foo", "-b", "bar")
				builder.InsertRuleV4("chain", "table", 2, "-f", "foo", "-b", "bar")
				builder.AppendRuleV4("chain", "table", "-f", "foo", "-b", "baz")
				builder.AppendRuleV6("chain", "table", "-f", "foo", "-b", "bar")
				builder.InsertRuleV6("chain", "table", 2, "-f", "foo", "-b", "bar")
				builder.InsertRuleV6("chain", "table", 1, "-f", "foo", "-b", "bar")
				builder.AppendRuleV4("PREROUTING", "nat", "-f", "foo", "-b", "bar")
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
			var actual strings.Builder
			for _, rule := range rules {
				fmt.Fprintln(&actual, strings.Join(rule, " "))
			}
			compareToGolden(t, goldenName, actual.String())

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

func TestCheckRulesV4V6(t *testing.T) {
	builderConfig := &config.Config{
		EnableIPv6: true,
	}
	iptables := NewIptablesRuleBuilder(builderConfig)
	iptables.InsertRuleV4("chain", "table", 2, "-f", "foo", "-b", "bar")
	iptables.AppendRuleV4("chain2", "table2", "-f", "foo", "-b", "baz")
	iptables.AppendRuleV4("chain2", "table", "-f", "foo", "-b", "baz", "-j", "chain")
	iptables.InsertRuleV6("chain", "table", 3, "-f", "foo", "-b", "baar")
	iptables.AppendRuleV6("chain2", "table2", "-f", "foo", "-b", "baaz")
	iptables.AppendRuleV6("chain2", "table", "-f", "foo", "-b", "baaz", "-j", "chain")

	actual := iptables.BuildCheckV4()
	expected := [][]string{
		{"-t", "table", "-C", "chain", "-f", "foo", "-b", "bar"},
		{"-t", "table2", "-C", "chain2", "-f", "foo", "-b", "baz"},
		{"-t", "table", "-C", "chain2", "-f", "foo", "-b", "baz", "-j", "chain"},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actual, expected)
	}
	actual = iptables.BuildCheckV6()
	expected = [][]string{
		{"-t", "table", "-C", "chain", "-f", "foo", "-b", "baar"},
		{"-t", "table2", "-C", "chain2", "-f", "foo", "-b", "baaz"},
		{"-t", "table", "-C", "chain2", "-f", "foo", "-b", "baaz", "-j", "chain"},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actual, expected)
	}
}

func TestCleanupRulesV4V6(t *testing.T) {
	builderConfig := &config.Config{
		EnableIPv6: true,
	}
	for _, tt := range []struct {
		name       string
		setupFunc  func(iptables *IptablesRuleBuilder)
		expectedv4 [][]string
		expectedv6 [][]string
	}{
		{
			"no-jumps",
			func(iptables *IptablesRuleBuilder) {
				iptables.InsertRuleV4("chain", "table", 2, "-f", "foo", "-b", "bar")
				iptables.AppendRuleV4("chain2", "table2", "-f", "foo", "-b", "baz")
				iptables.InsertRuleV6("chain", "table", 3, "-f", "foo", "-b", "baar")
				iptables.AppendRuleV6("chain2", "table2", "-f", "foo", "-b", "baaz")
			},
			[][]string{
				{"-t", "table2", "-D", "chain2", "-f", "foo", "-b", "baz"},
				{"-t", "table", "-D", "chain", "-f", "foo", "-b", "bar"},
				{"-t", "table2", "-F", "chain2"},
				{"-t", "table2", "-X", "chain2"},
				{"-t", "table", "-F", "chain"},
				{"-t", "table", "-X", "chain"},
			},
			[][]string{
				{"-t", "table2", "-D", "chain2", "-f", "foo", "-b", "baaz"},
				{"-t", "table", "-D", "chain", "-f", "foo", "-b", "baar"},
				{"-t", "table2", "-F", "chain2"},
				{"-t", "table2", "-X", "chain2"},
				{"-t", "table", "-F", "chain"},
				{"-t", "table", "-X", "chain"},
			},
		},
		{
			"with-jump",
			func(iptables *IptablesRuleBuilder) {
				iptables.InsertRuleV4("chain", "table", 2, "-f", "foo", "-b", "bar")
				iptables.AppendRuleV4("chain2", "table", "-f", "foo", "-b", "bar", "-j", "chain1")
				iptables.InsertRuleV4("chain2", "table", 1, "-f", "foo", "-b", "baz")
				iptables.AppendRuleV4("chain", "table2", "-f", "foo", "-b", "bar")
				iptables.InsertRuleV6("chain", "table", 2, "-f", "foo", "-b", "baar")
				iptables.AppendRuleV6("chain2", "table", "-f", "foo", "-b", "baar", "-j", "chain1")
				iptables.InsertRuleV6("chain2", "table", 1, "-f", "foo", "-b", "baaz")
				iptables.AppendRuleV6("chain", "table2", "-f", "foo", "-b", "baar")
			},
			[][]string{
				{"-t", "table2", "-D", "chain", "-f", "foo", "-b", "bar"},
				{"-t", "table", "-D", "chain2", "-f", "foo", "-b", "baz"},
				{"-t", "table", "-D", "chain2", "-f", "foo", "-b", "bar", "-j", "chain1"},
				{"-t", "table", "-D", "chain", "-f", "foo", "-b", "bar"},
				{"-t", "table2", "-F", "chain"},
				{"-t", "table2", "-X", "chain"},
				{"-t", "table", "-F", "chain2"},
				{"-t", "table", "-X", "chain2"},
				{"-t", "table", "-F", "chain"},
				{"-t", "table", "-X", "chain"},
			},
			[][]string{
				{"-t", "table2", "-D", "chain", "-f", "foo", "-b", "baar"},
				{"-t", "table", "-D", "chain2", "-f", "foo", "-b", "baaz"},
				{"-t", "table", "-D", "chain2", "-f", "foo", "-b", "baar", "-j", "chain1"},
				{"-t", "table", "-D", "chain", "-f", "foo", "-b", "baar"},
				{"-t", "table2", "-F", "chain"},
				{"-t", "table2", "-X", "chain"},
				{"-t", "table", "-F", "chain2"},
				{"-t", "table", "-X", "chain2"},
				{"-t", "table", "-F", "chain"},
				{"-t", "table", "-X", "chain"},
			},
		},
		{
			"with-jump-istio-prefix", // verify that rules inside ISTIO_* chains are not explicitly deleted
			func(iptables *IptablesRuleBuilder) {
				iptables.AppendRuleV4("ISTIO_TEST", "table", "-f", "foo", "-b", "bar")
				iptables.InsertRuleV4("chain", "table", 1, "-f", "foo", "-b", "bar", "-j", "ISTIO_TEST")
				iptables.AppendRuleV4("chain", "table", "-f", "foo", "-b", "bar")
				iptables.AppendRuleV6("ISTIO_TEST", "table", "-f", "foo", "-b", "baar")
				iptables.InsertRuleV6("chain", "table", 1, "-f", "foo", "-b", "baar", "-j", "ISTIO_TEST")
				iptables.AppendRuleV6("chain", "table", "-f", "foo", "-b", "baar")
			},
			[][]string{
				{"-t", "table", "-D", "chain", "-f", "foo", "-b", "bar"},
				{"-t", "table", "-D", "chain", "-f", "foo", "-b", "bar", "-j", "ISTIO_TEST"},
				{"-t", "table", "-F", "chain"},
				{"-t", "table", "-X", "chain"},
				{"-t", "table", "-F", "ISTIO_TEST"},
				{"-t", "table", "-X", "ISTIO_TEST"},
			},
			[][]string{
				{"-t", "table", "-D", "chain", "-f", "foo", "-b", "baar"},
				{"-t", "table", "-D", "chain", "-f", "foo", "-b", "baar", "-j", "ISTIO_TEST"},
				{"-t", "table", "-F", "chain"},
				{"-t", "table", "-X", "chain"},
				{"-t", "table", "-F", "ISTIO_TEST"},
				{"-t", "table", "-X", "ISTIO_TEST"},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			iptables := NewIptablesRuleBuilder(builderConfig)
			tt.setupFunc(iptables)
			actual := iptables.BuildCleanupV4()
			if !reflect.DeepEqual(actual, tt.expectedv4) {
				t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actual, tt.expectedv4)
			}
			actual = iptables.BuildCleanupV6()
			if !reflect.DeepEqual(actual, tt.expectedv6) {
				t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actual, tt.expectedv6)
			}
		})
	}
}

func TestStateFromRestoreFormat(t *testing.T) {
	builderConfig := &config.Config{}
	for _, tt := range []struct {
		name      string
		setupFunc func(iptables *IptablesRuleBuilder)
		expected  map[string]map[string][]string
	}{
		{
			"default",
			func(iptables *IptablesRuleBuilder) {
				iptables.InsertRuleV4("chain", "nat", 2, "-f", "foo", "-b", "bar")
				iptables.AppendRuleV4("chain", "filter", "-f", "foo", "-b", "baaz")
				iptables.AppendRuleV4("chain", "mangle", "-f", "fooo", "-b", "baz")
				iptables.AppendRuleV4("chain", "raw", "-f", "foo", "-b", "baar")
				iptables.AppendRuleV4("POSTROUTING", "nat", "-f", "foo", "-b", "bar", "-j", "ISTIO_TEST")
				iptables.AppendRuleV4("ISTIO_TEST", "nat", "-f", "foo", "-b", "bar")
				iptables.AppendRuleV4("chain", "filter", "-f", "foo", "-b", "bar")
			},
			map[string]map[string][]string{
				"filter": {
					"chain": {
						"-A chain -f foo -b baaz",
						"-A chain -f foo -b bar",
					},
				},
				"mangle": {
					"chain": {
						"-A chain -f fooo -b baz",
					},
				},
				"nat": {
					"ISTIO_TEST": {
						"-A ISTIO_TEST -f foo -b bar",
					},
					"POSTROUTING": {
						"-A POSTROUTING -f foo -b bar -j ISTIO_TEST",
					},
					"chain": {
						"-I chain 2 -f foo -b bar",
					},
				},
				"raw": {
					"chain": {
						"-A chain -f foo -b baar",
					},
				},
			},
		},
		{
			"empty",
			func(iptables *IptablesRuleBuilder) {
			},
			map[string]map[string][]string{
				"filter": {},
				"mangle": {},
				"nat":    {},
				"raw":    {},
			},
		},
		{
			"with-non-built-in-tables",
			func(iptables *IptablesRuleBuilder) {
				iptables.InsertRuleV4("chain", "nat", 2, "-f", "foo", "-b", "bar")
				iptables.AppendRuleV4("chain2", "filter", "-f", "foo", "-b", "baz")
				iptables.AppendRuleV4("chain2", "does-not-exist", "-f", "foo", "-b", "baz")
			},
			map[string]map[string][]string{
				"filter": {
					"chain2": {
						"-A chain2 -f foo -b baz",
					},
				},
				"mangle": {},
				"nat": {
					"chain": {
						"-I chain 2 -f foo -b bar",
					},
				},
				"raw": {},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			iptables := NewIptablesRuleBuilder(builderConfig)
			tt.setupFunc(iptables)
			actual := iptables.GetStateFromSave(iptables.BuildV4Restore())
			if !reflect.DeepEqual(actual, tt.expected) {
				t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actual, tt.expected)
			}
		})
	}
}
