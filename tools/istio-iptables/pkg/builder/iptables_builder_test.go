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
	"reflect"
	"strings"
	"testing"

	"istio.io/istio/tools/istio-iptables/pkg/config"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
	iptableslog "istio.io/istio/tools/istio-iptables/pkg/log"
)

// getMapFromRestore function takes a string in iptables-restore format and returns a map of the tables, chains, and rules.
// Note that if this function is used to parse iptables-save output, the rules may have changed since they were first applied
// as rules do not necessarily undergo a round-trip through the kernel in the same form.
// Therefore, these rules should not be used for any critical checks.
func getMapFromRestore(data string) map[string]map[string][]string {
	lines := strings.Split(data, "\n")
	result := make(map[string]map[string][]string)

	table := ""
	chain := ""
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, "#") || line == "COMMIT" {
			continue
		}
		// Found table
		if strings.HasPrefix(line, "*") {
			chain = ""
			table = strings.TrimSpace(line[1:])
			_, ok := result[table]
			if !ok {
				result[table] = make(map[string][]string)
			}
			continue
		}
		// Found chain, setup an empty list for the chain if it is an ISTIO one
		if strings.HasPrefix(line, ":") {
			if !strings.HasPrefix(line, ":ISTIO") {
				continue
			}
			chain := strings.Split(line, " ")[0][1:]
			_, ok := result[table][chain]
			if !ok {
				result[table][chain] = []string{}
			}
			continue
		}

		rule := strings.Split(line, " ")
		ruleChain := ""
		if chain == "" {
			// Obtain chain from the rule in case we are parsing iptables-restore builder input
			for i, item := range rule {
				if (item == "--append" || item == "-A" || item == "--insert" || item == "-I") && i+1 < len(rule) {
					ruleChain = rule[i+1]
				}
			}
		} else {
			ruleChain = chain
		}
		if ruleChain == "" {
			continue
		}
		_, ok := result[table][ruleChain]
		if !ok {
			result[table][ruleChain] = []string{}
		}
		result[table][ruleChain] = append(result[table][ruleChain], line)
	}
	return result
}

// TODO(abhide): Add more testcases once BuildV6Restore() are implemented
func TestBuildV6Restore(t *testing.T) {
	iptables := NewIptablesRuleBuilder(nil)
	expected := ""
	actual := iptables.BuildV6Restore()
	if expected != actual {
		t.Errorf("Output didn't match: Got: %s, Expected: %s", actual, expected)
	}
}

// TODO(abhide): Add more testcases once BuildV4Restore() are implemented
func TestBuildV4Restore(t *testing.T) {
	iptables := NewIptablesRuleBuilder(nil)
	expected := ""
	actual := iptables.BuildV4Restore()
	if expected != actual {
		t.Errorf("Output didn't match: Got: %s, Expected: %s", actual, expected)
	}
}

func TestBuildV4InsertSingleRule(t *testing.T) {
	iptables := NewIptablesRuleBuilder(nil)
	iptables.InsertRuleV4(iptableslog.UndefinedCommand, "chain", "table", 2, "-f", "foo", "-b", "bar")
	if err := len(iptables.rules.rulesv6) != 0; err {
		t.Errorf("Expected rulesV6 to be empty; but got %#v", iptables.rules.rulesv6)
	}
	actual := iptables.BuildV4()
	expected := [][]string{
		{"-t", "table", "-N", "chain"},
		{"-t", "table", "-I", "chain", "2", "-f", "foo", "-b", "bar"},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actual, expected)
	}
	// V6 rules should be empty and return an empty slice
	actual = iptables.BuildV6()
	if !reflect.DeepEqual(actual, [][]string{}) {
		t.Errorf("Expected V6 rules to be empty; but instead got Actual: %#v", actual)
	}
}

func TestBuildV4RestoreInsertSingleRule(t *testing.T) {
	iptables := NewIptablesRuleBuilder(nil)
	iptables.InsertRuleV4(iptableslog.UndefinedCommand, "chain", "table", 2, "-f", "foo", "-b", "bar")
	if err := len(iptables.rules.rulesv6) != 0; err {
		t.Errorf("Expected rulesV6 to be empty; but got %#v", iptables.rules.rulesv6)
	}
	actual := getMapFromRestore(iptables.BuildV4Restore())
	expected := map[string]map[string][]string{
		"table": {
			"chain": {
				"-I chain 2 -f foo -b bar",
			},
		},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actual, expected)
	}

	actual = getMapFromRestore(iptables.BuildV6Restore())
	if !reflect.DeepEqual(actual, map[string]map[string][]string{}) {
		t.Errorf("Expected V6 rules to be empty; but instead got Actual: %#v", actual)
	}
}

func TestBuildV4AppendSingleRule(t *testing.T) {
	iptables := NewIptablesRuleBuilder(nil)
	iptables.AppendRuleV4(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "bar")
	if err := len(iptables.rules.rulesv6) != 0; err {
		t.Errorf("Expected rulesV6 to be empty; but got %#v", iptables.rules.rulesv6)
	}
	actual := iptables.BuildV4()
	expected := [][]string{
		{"-t", "table", "-N", "chain"},
		{"-t", "table", "-A", "chain", "-f", "foo", "-b", "bar"},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actual, expected)
	}
	// V6 rules should be empty and return an empty slice
	actual = iptables.BuildV6()
	if !reflect.DeepEqual(actual, [][]string{}) {
		t.Errorf("Expected V6 rules to be empty; but instead got Actual: %#v", actual)
	}
}

func TestBuildV4RestoreAppendSingleRule(t *testing.T) {
	iptables := NewIptablesRuleBuilder(nil)
	iptables.AppendRuleV4(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "bar")
	if err := len(iptables.rules.rulesv6) != 0; err {
		t.Errorf("Expected rulesV6 to be empty; but got %#v", iptables.rules.rulesv6)
	}
	actual := getMapFromRestore(iptables.BuildV4Restore())
	expected := map[string]map[string][]string{
		"table": {
			"chain": {
				"-A chain -f foo -b bar",
			},
		},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actual, expected)
	}
	// V6 rules should be empty and return an empty slice
	actual = getMapFromRestore(iptables.BuildV6Restore())
	if !reflect.DeepEqual(actual, map[string]map[string][]string{}) {
		t.Errorf("Expected V6 rules to be empty; but instead got Actual: %#v", actual)
	}
}

func TestBuildV4AppendMultipleRules(t *testing.T) {
	iptables := NewIptablesRuleBuilder(nil)
	iptables.AppendRuleV4(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "bar")
	iptables.AppendRuleV4(iptableslog.UndefinedCommand, "chain", "table", "-f", "fu", "-b", "bar")
	iptables.AppendRuleV4(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "baz")
	if err := len(iptables.rules.rulesv6) != 0; err {
		t.Errorf("Expected rulesV6 to be empty; but got %#v", iptables.rules.rulesv6)
	}
	actual := iptables.BuildV4()
	expected := [][]string{
		{"-t", "table", "-N", "chain"},
		{"-t", "table", "-A", "chain", "-f", "foo", "-b", "bar"},
		{"-t", "table", "-A", "chain", "-f", "fu", "-b", "bar"},
		{"-t", "table", "-A", "chain", "-f", "foo", "-b", "baz"},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actual, expected)
	}
	// V6 rules should be empty and return an empty slice
	actual = iptables.BuildV6()
	if !reflect.DeepEqual(actual, [][]string{}) {
		t.Errorf("Expected V6 rules to be empty; but instead got Actual: %#v", actual)
	}
}

func TestBuildV4RestoreAppendMultipleRules(t *testing.T) {
	iptables := NewIptablesRuleBuilder(nil)
	iptables.AppendRuleV4(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "bar")
	iptables.AppendRuleV4(iptableslog.UndefinedCommand, "chain", "table", "-f", "fu", "-b", "bar")
	iptables.AppendRuleV4(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "baz")
	if err := len(iptables.rules.rulesv6) != 0; err {
		t.Errorf("Expected rulesV6 to be empty; but got %#v", iptables.rules.rulesv6)
	}
	actual := getMapFromRestore(iptables.BuildV4Restore())
	expected := map[string]map[string][]string{
		"table": {
			"chain": {
				"-A chain -f foo -b bar",
				"-A chain -f fu -b bar",
				"-A chain -f foo -b baz",
			},
		},
	}

	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actual, expected)
	}
	// V6 rules should be empty and return an empty slice
	actual = getMapFromRestore(iptables.BuildV6Restore())
	if !reflect.DeepEqual(actual, map[string]map[string][]string{}) {
		t.Errorf("Expected V6 rules to be empty; but instead got Actual: %#v", actual)
	}
}

func TestBuildV4InsertMultipleRules(t *testing.T) {
	iptables := NewIptablesRuleBuilder(nil)
	iptables.InsertRuleV4(iptableslog.UndefinedCommand, "chain", "table", 1, "-f", "foo", "-b", "bar")
	iptables.InsertRuleV4(iptableslog.UndefinedCommand, "chain", "table", 2, "-f", "foo", "-b", "baaz")
	iptables.InsertRuleV4(iptableslog.UndefinedCommand, "chain", "table", 3, "-f", "foo", "-b", "baz")
	if err := len(iptables.rules.rulesv6) != 0; err {
		t.Errorf("Expected rulesV6 to be empty; but got %#v", iptables.rules.rulesv6)
	}
	actual := iptables.BuildV4()
	expected := [][]string{
		{"-t", "table", "-N", "chain"},
		{"-t", "table", "-I", "chain", "1", "-f", "foo", "-b", "bar"},
		{"-t", "table", "-I", "chain", "2", "-f", "foo", "-b", "baaz"},
		{"-t", "table", "-I", "chain", "3", "-f", "foo", "-b", "baz"},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actual, expected)
	}
	// V6 rules should be empty and return an empty slice
	actual = iptables.BuildV6()
	if !reflect.DeepEqual(actual, [][]string{}) {
		t.Errorf("Expected V6 rules to be empty; but instead got Actual: %#v", actual)
	}
}

func TestBuildV4RestoreInsertMultipleRules(t *testing.T) {
	iptables := NewIptablesRuleBuilder(nil)
	iptables.InsertRuleV4(iptableslog.UndefinedCommand, "chain", "table", 1, "-f", "foo", "-b", "bar")
	iptables.InsertRuleV4(iptableslog.UndefinedCommand, "chain", "table", 2, "-f", "foo", "-b", "baaz")
	iptables.InsertRuleV4(iptableslog.UndefinedCommand, "chain", "table", 3, "-f", "foo", "-b", "baz")
	if err := len(iptables.rules.rulesv6) != 0; err {
		t.Errorf("Expected rulesV6 to be empty; but got %#v", iptables.rules.rulesv6)
	}
	actual := getMapFromRestore(iptables.BuildV4Restore())
	expected := map[string]map[string][]string{
		"table": {
			"chain": {
				"-I chain 1 -f foo -b bar",
				"-I chain 2 -f foo -b baaz",
				"-I chain 3 -f foo -b baz",
			},
		},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actual, expected)
	}
	// V6 rules should be empty and return an empty slice
	actual = getMapFromRestore(iptables.BuildV6Restore())
	if !reflect.DeepEqual(actual, map[string]map[string][]string{}) {
		t.Errorf("Expected V6 rules to be empty; but instead got Actual: %#v", actual)
	}
}

func TestBuildV4AppendInsertMultipleRules(t *testing.T) {
	iptables := NewIptablesRuleBuilder(nil)
	iptables.AppendRuleV4(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "bar")
	iptables.InsertRuleV4(iptableslog.UndefinedCommand, "chain", "table", 2, "-f", "foo", "-b", "bar")
	iptables.AppendRuleV4(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "baz")
	if err := len(iptables.rules.rulesv6) != 0; err {
		t.Errorf("Expected rulesV6 to be empty; but got %#v", iptables.rules.rulesv6)
	}
	actual := iptables.BuildV4()
	expected := [][]string{
		{"-t", "table", "-N", "chain"},
		{"-t", "table", "-A", "chain", "-f", "foo", "-b", "bar"},
		{"-t", "table", "-I", "chain", "2", "-f", "foo", "-b", "bar"},
		{"-t", "table", "-A", "chain", "-f", "foo", "-b", "baz"},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actual, expected)
	}
	// V6 rules should be empty and return an empty slice
	actual = iptables.BuildV6()
	if !reflect.DeepEqual(actual, [][]string{}) {
		t.Errorf("Expected V6 rules to be empty; but instead got Actual: %#v", actual)
	}
}

func TestBuildV4RestoreAppendInsertMultipleRules(t *testing.T) {
	iptables := NewIptablesRuleBuilder(nil)
	iptables.AppendRuleV4(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "bar")
	iptables.InsertRuleV4(iptableslog.UndefinedCommand, "chain", "table", 2, "-f", "foo", "-b", "bar")
	iptables.AppendRuleV4(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "baz")
	if err := len(iptables.rules.rulesv6) != 0; err {
		t.Errorf("Expected rulesV6 to be empty; but got %#v", iptables.rules.rulesv6)
	}
	actual := getMapFromRestore(iptables.BuildV4Restore())
	expected := map[string]map[string][]string{
		"table": {
			"chain": {
				"-A chain -f foo -b bar",
				"-I chain 2 -f foo -b bar",
				"-A chain -f foo -b baz",
			},
		},
	}

	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actual, expected)
	}
	// V6 rules should be empty and return an empty slice
	actual = getMapFromRestore(iptables.BuildV6Restore())
	if !reflect.DeepEqual(actual, map[string]map[string][]string{}) {
		t.Errorf("Expected V6 rules to be empty; but instead got Actual: %#v", actual)
	}
}

var IPv6Config = &config.Config{
	EnableIPv6: true,
}

func TestBuildV6InsertSingleRule(t *testing.T) {
	iptables := NewIptablesRuleBuilder(IPv6Config)
	iptables.InsertRuleV6(iptableslog.UndefinedCommand, "chain", "table", 2, "-f", "foo", "-b", "bar")
	if err := len(iptables.rules.rulesv4) != 0; err {
		t.Errorf("Expected rulesV4 to be empty; but got %#v", iptables.rules.rulesv4)
	}
	actual := iptables.BuildV6()
	expected := [][]string{
		{"-t", "table", "-N", "chain"},
		{"-t", "table", "-I", "chain", "2", "-f", "foo", "-b", "bar"},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actual, expected)
	}
	// V4 rules should be empty and return an empty slice
	actual = iptables.BuildV4()
	if !reflect.DeepEqual(actual, [][]string{}) {
		t.Errorf("Expected V4 rules to be empty; but instead got Actual: %#v", actual)
	}
}

func TestBuildV6RestoreInsertSingleRule(t *testing.T) {
	iptables := NewIptablesRuleBuilder(IPv6Config)
	iptables.InsertRuleV6(iptableslog.UndefinedCommand, "chain", "table", 2, "-f", "foo", "-b", "bar")
	if err := len(iptables.rules.rulesv4) != 0; err {
		t.Errorf("Expected rulesV4 to be empty; but got %#v", iptables.rules.rulesv4)
	}
	actual := getMapFromRestore(iptables.BuildV6Restore())
	expected := map[string]map[string][]string{
		"table": {
			"chain": {
				"-I chain 2 -f foo -b bar",
			},
		},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actual, expected)
	}

	actual = getMapFromRestore(iptables.BuildV4Restore())
	if !reflect.DeepEqual(actual, map[string]map[string][]string{}) {
		t.Errorf("Expected V4 rules to be empty; but instead got Actual: %#v", actual)
	}
}

func TestBuildV6AppendSingleRule(t *testing.T) {
	iptables := NewIptablesRuleBuilder(IPv6Config)
	iptables.AppendRuleV6(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "bar")
	if err := len(iptables.rules.rulesv4) != 0; err {
		t.Errorf("Expected rulesV4 to be empty; but got %#v", iptables.rules.rulesv4)
	}
	actual := iptables.BuildV6()
	expected := [][]string{
		{"-t", "table", "-N", "chain"},
		{"-t", "table", "-A", "chain", "-f", "foo", "-b", "bar"},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actual, expected)
	}
	// V4 rules should be empty and return an empty slice
	actual = iptables.BuildV4()
	if !reflect.DeepEqual(actual, [][]string{}) {
		t.Errorf("Expected V4 rules to be empty; but instead got Actual: %#v", actual)
	}
}

func TestBuildV6RestoreAppendSingleRule(t *testing.T) {
	iptables := NewIptablesRuleBuilder(IPv6Config)
	iptables.AppendRuleV6(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "bar")
	if err := len(iptables.rules.rulesv4) != 0; err {
		t.Errorf("Expected rulesV4 to be empty; but got %#v", iptables.rules.rulesv4)
	}
	actual := getMapFromRestore(iptables.BuildV6Restore())
	expected := map[string]map[string][]string{
		"table": {
			"chain": {
				"-A chain -f foo -b bar",
			},
		},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actual, expected)
	}
	// V4 rules should be empty and return an empty slice
	actual = getMapFromRestore(iptables.BuildV4Restore())
	if !reflect.DeepEqual(actual, map[string]map[string][]string{}) {
		t.Errorf("Expected V4 rules to be empty; but instead got Actual: %#v", actual)
	}
}

func TestBuildV6AppendMultipleRules(t *testing.T) {
	iptables := NewIptablesRuleBuilder(IPv6Config)
	iptables.AppendRuleV6(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "bar")
	iptables.AppendRuleV6(iptableslog.UndefinedCommand, "chain", "table", "-f", "fu", "-b", "bar")
	iptables.AppendRuleV6(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "baz")
	if err := len(iptables.rules.rulesv4) != 0; err {
		t.Errorf("Expected rulesV4 to be empty; but got %#v", iptables.rules.rulesv4)
	}
	actual := iptables.BuildV6()
	expected := [][]string{
		{"-t", "table", "-N", "chain"},
		{"-t", "table", "-A", "chain", "-f", "foo", "-b", "bar"},
		{"-t", "table", "-A", "chain", "-f", "fu", "-b", "bar"},
		{"-t", "table", "-A", "chain", "-f", "foo", "-b", "baz"},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actual, expected)
	}
	// V4 rules should be empty and return an empty slice
	actual = iptables.BuildV4()
	if !reflect.DeepEqual(actual, [][]string{}) {
		t.Errorf("Expected V4 rules to be empty; but instead got Actual: %#v", actual)
	}
}

func TestBuildV6RestoreAppendMultipleRules(t *testing.T) {
	iptables := NewIptablesRuleBuilder(IPv6Config)
	iptables.AppendRuleV6(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "bar")
	iptables.AppendRuleV6(iptableslog.UndefinedCommand, "chain", "table", "-f", "fu", "-b", "bar")
	iptables.AppendRuleV6(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "baz")
	if err := len(iptables.rules.rulesv4) != 0; err {
		t.Errorf("Expected rulesV4 to be empty; but got %#v", iptables.rules.rulesv4)
	}
	actual := getMapFromRestore(iptables.BuildV6Restore())
	expected := map[string]map[string][]string{
		"table": {
			"chain": {
				"-A chain -f foo -b bar",
				"-A chain -f fu -b bar",
				"-A chain -f foo -b baz",
			},
		},
	}

	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actual, expected)
	}
	// V4 rules should be empty and return an empty slice
	actual = getMapFromRestore(iptables.BuildV4Restore())
	if !reflect.DeepEqual(actual, map[string]map[string][]string{}) {
		t.Errorf("Expected V4 rules to be empty; but instead got Actual: %#v", actual)
	}
}

func TestBuildV6InsertMultipleRules(t *testing.T) {
	iptables := NewIptablesRuleBuilder(IPv6Config)
	iptables.InsertRuleV6(iptableslog.UndefinedCommand, "chain", "table", 1, "-f", "foo", "-b", "bar")
	iptables.InsertRuleV6(iptableslog.UndefinedCommand, "chain", "table", 2, "-f", "foo", "-b", "baaz")
	iptables.InsertRuleV6(iptableslog.UndefinedCommand, "chain", "table", 3, "-f", "foo", "-b", "baz")
	if err := len(iptables.rules.rulesv4) != 0; err {
		t.Errorf("Expected rulesV4 to be empty; but got %#v", iptables.rules.rulesv4)
	}
	actual := iptables.BuildV6()
	expected := [][]string{
		{"-t", "table", "-N", "chain"},
		{"-t", "table", "-I", "chain", "1", "-f", "foo", "-b", "bar"},
		{"-t", "table", "-I", "chain", "2", "-f", "foo", "-b", "baaz"},
		{"-t", "table", "-I", "chain", "3", "-f", "foo", "-b", "baz"},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actual, expected)
	}
	// V4 rules should be empty and return an empty slice
	actual = iptables.BuildV4()
	if !reflect.DeepEqual(actual, [][]string{}) {
		t.Errorf("Expected V4 rules to be empty; but instead got Actual: %#v", actual)
	}
}

func TestBuildV6RestoreInsertMultipleRules(t *testing.T) {
	iptables := NewIptablesRuleBuilder(IPv6Config)
	iptables.InsertRuleV6(iptableslog.UndefinedCommand, "chain", "table", 1, "-f", "foo", "-b", "bar")
	iptables.InsertRuleV6(iptableslog.UndefinedCommand, "chain", "table", 2, "-f", "foo", "-b", "baaz")
	iptables.InsertRuleV6(iptableslog.UndefinedCommand, "chain", "table", 3, "-f", "foo", "-b", "baz")
	if err := len(iptables.rules.rulesv4) != 0; err {
		t.Errorf("Expected rulesV4 to be empty; but got %#v", iptables.rules.rulesv4)
	}
	actual := getMapFromRestore(iptables.BuildV6Restore())
	expected := map[string]map[string][]string{
		"table": {
			"chain": {
				"-I chain 1 -f foo -b bar",
				"-I chain 2 -f foo -b baaz",
				"-I chain 3 -f foo -b baz",
			},
		},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actual, expected)
	}
	// V4 rules should be empty and return an empty slice
	actual = getMapFromRestore(iptables.BuildV4Restore())
	if !reflect.DeepEqual(actual, map[string]map[string][]string{}) {
		t.Errorf("Expected V4 rules to be empty; but instead got Actual: %#v", actual)
	}
}

func TestBuildV6InsertAppendMultipleRules(t *testing.T) {
	iptables := NewIptablesRuleBuilder(IPv6Config)
	iptables.AppendRuleV6(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "bar")
	iptables.InsertRuleV6(iptableslog.UndefinedCommand, "chain", "table", 2, "-f", "foo", "-b", "bar")
	iptables.InsertRuleV6(iptableslog.UndefinedCommand, "chain", "table", 1, "-f", "foo", "-b", "bar")
	if err := len(iptables.rules.rulesv4) != 0; err {
		t.Errorf("Expected rulesV4 to be empty; but got %#v", iptables.rules.rulesv4)
	}
	actual := iptables.BuildV6()
	expected := [][]string{
		{"-t", "table", "-N", "chain"},
		{"-t", "table", "-A", "chain", "-f", "foo", "-b", "bar"},
		{"-t", "table", "-I", "chain", "2", "-f", "foo", "-b", "bar"},
		{"-t", "table", "-I", "chain", "1", "-f", "foo", "-b", "bar"},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actual, expected)
	}
	// V4 rules should be empty and return an empty slice
	actual = iptables.BuildV4()
	if !reflect.DeepEqual(actual, [][]string{}) {
		t.Errorf("Expected V4 rules to be empty; but instead got Actual: %#v", actual)
	}
}

func TestBuildV6RestoreInsertAppendMultipleRules(t *testing.T) {
	iptables := NewIptablesRuleBuilder(IPv6Config)
	iptables.AppendRuleV6(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "bar")
	iptables.InsertRuleV6(iptableslog.UndefinedCommand, "chain", "table", 2, "-f", "foo", "-b", "bar")
	iptables.InsertRuleV6(iptableslog.UndefinedCommand, "chain", "table", 1, "-f", "foo", "-b", "bar")
	if err := len(iptables.rules.rulesv4) != 0; err {
		t.Errorf("Expected rulesV4 to be empty; but got %#v", iptables.rules.rulesv4)
	}
	actual := getMapFromRestore(iptables.BuildV6Restore())
	expected := map[string]map[string][]string{
		"table": {
			"chain": {
				"-A chain -f foo -b bar",
				"-I chain 2 -f foo -b bar",
				"-I chain 1 -f foo -b bar",
			},
		},
	}

	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actual, expected)
	}
	// V4 rules should be empty and return an empty slice
	actual = getMapFromRestore(iptables.BuildV4Restore())
	if !reflect.DeepEqual(actual, map[string]map[string][]string{}) {
		t.Errorf("Expected V4 rules to be empty; but instead got Actual: %#v", actual)
	}
}

func TestBuildV4V6MultipleRulesWithNewChain(t *testing.T) {
	iptables := NewIptablesRuleBuilder(IPv6Config)
	iptables.AppendRuleV4(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "bar")
	iptables.InsertRuleV4(iptableslog.UndefinedCommand, "chain", "table", 2, "-f", "foo", "-b", "bar")
	iptables.AppendRuleV4(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "baz")
	iptables.AppendRuleV6(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "bar")
	iptables.InsertRuleV6(iptableslog.UndefinedCommand, "chain", "table", 2, "-f", "foo", "-b", "bar")
	iptables.InsertRuleV6(iptableslog.UndefinedCommand, "chain", "table", 1, "-f", "foo", "-b", "bar")
	iptables.AppendRuleV4(iptableslog.UndefinedCommand, constants.PREROUTING, constants.NAT, "-f", "foo", "-b", "bar")
	actualV4 := iptables.BuildV4()
	actualV6 := iptables.BuildV6()
	expectedV6 := [][]string{
		{"-t", "table", "-N", "chain"},
		{"-t", "table", "-A", "chain", "-f", "foo", "-b", "bar"},
		{"-t", "table", "-I", "chain", "2", "-f", "foo", "-b", "bar"},
		{"-t", "table", "-I", "chain", "1", "-f", "foo", "-b", "bar"},
	}
	expectedV4 := [][]string{
		{"-t", "table", "-N", "chain"},
		{"-t", "table", "-A", "chain", "-f", "foo", "-b", "bar"},
		{"-t", "table", "-I", "chain", "2", "-f", "foo", "-b", "bar"},
		{"-t", "table", "-A", "chain", "-f", "foo", "-b", "baz"},
		{"-t", "nat", "-A", "PREROUTING", "-f", "foo", "-b", "bar"},
	}
	if !reflect.DeepEqual(actualV4, expectedV4) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actualV4, expectedV4)
	}
	if !reflect.DeepEqual(actualV6, expectedV6) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actualV6, expectedV6)
	}
}

func TestBuildV4V6RestoreMultipleRulesWithNewChain(t *testing.T) {
	iptables := NewIptablesRuleBuilder(IPv6Config)
	iptables.AppendRuleV4(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "bar")
	iptables.InsertRuleV4(iptableslog.UndefinedCommand, "chain", "table", 2, "-f", "foo", "-b", "bar")
	iptables.AppendRuleV4(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "baz")
	iptables.AppendRuleV6(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "bar")
	iptables.InsertRuleV6(iptableslog.UndefinedCommand, "chain", "table", 2, "-f", "foo", "-b", "bar")
	iptables.InsertRuleV6(iptableslog.UndefinedCommand, "chain", "table", 1, "-f", "foo", "-b", "bar")
	iptables.AppendRuleV4(iptableslog.UndefinedCommand, constants.PREROUTING, constants.NAT, "-f", "foo", "-b", "bar")
	actualV4 := getMapFromRestore(iptables.BuildV4Restore())
	actualV6 := getMapFromRestore(iptables.BuildV6Restore())
	expectedV6 := map[string]map[string][]string{
		"table": {
			"chain": {
				"-A chain -f foo -b bar",
				"-I chain 2 -f foo -b bar",
				"-I chain 1 -f foo -b bar",
			},
		},
	}
	expectedV4 := map[string]map[string][]string{
		"table": {
			"chain": {
				"-A chain -f foo -b bar",
				"-I chain 2 -f foo -b bar",
				"-A chain -f foo -b baz",
			},
		},
		"nat": {
			"PREROUTING": {
				"-A PREROUTING -f foo -b bar",
			},
		},
	}
	if !reflect.DeepEqual(actualV4, expectedV4) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actualV4, expectedV4)
	}
	if !reflect.DeepEqual(actualV6, expectedV6) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actualV6, expectedV6)
	}
}
