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
	"testing"

	"istio.io/istio/tools/istio-iptables/pkg/constants"
)

// TODO(abhide): Add more testcases once BuildV6Restore() are implemented
func TestBuildV6Restore(t *testing.T) {
	iptables := NewIptablesBuilder()
	expected := ""
	actual := iptables.BuildV6Restore()
	if expected != actual {
		t.Errorf("Output didn't match: Got: %s, Expected: %s", actual, expected)
	}
}

// TODO(abhide): Add more testcases once BuildV4Restore() are implemented
func TestBuildV4Restore(t *testing.T) {
	iptables := NewIptablesBuilder()
	expected := ""
	actual := iptables.BuildV4Restore()
	if expected != actual {
		t.Errorf("Output didn't match: Got: %s, Expected: %s", actual, expected)
	}
}

func TestBuildV4InsertSingleRule(t *testing.T) {
	iptables := NewIptablesBuilder()
	iptables.InsertRuleV4("chain", "table", 2, "-f", "foo", "-b", "bar")
	if err := len(iptables.rules.rulesv6) != 0; err {
		t.Errorf("Expected rulesV6 to be empty; but got %#v", iptables.rules.rulesv6)
	}
	actual := iptables.BuildV4()
	expected := [][]string{
		{"iptables", "-t", "table", "-N", "chain"},
		{"iptables", "-t", "table", "-I", "chain", "2", "-f", "foo", "-b", "bar"},
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

func TestBuildV4AppendSingleRule(t *testing.T) {
	iptables := NewIptablesBuilder()
	iptables.AppendRuleV4("chain", "table", "-f", "foo", "-b", "bar")
	if err := len(iptables.rules.rulesv6) != 0; err {
		t.Errorf("Expected rulesV6 to be empty; but got %#v", iptables.rules.rulesv6)
	}
	actual := iptables.BuildV4()
	expected := [][]string{
		{"iptables", "-t", "table", "-N", "chain"},
		{"iptables", "-t", "table", "-A", "chain", "-f", "foo", "-b", "bar"},
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

func TestBuildV4AppendMultipleRules(t *testing.T) {
	iptables := NewIptablesBuilder()
	iptables.AppendRuleV4("chain", "table", "-f", "foo", "-b", "bar")
	iptables.AppendRuleV4("chain", "table", "-f", "fu", "-b", "bar")
	iptables.AppendRuleV4("chain", "table", "-f", "foo", "-b", "baz")
	if err := len(iptables.rules.rulesv6) != 0; err {
		t.Errorf("Expected rulesV6 to be empty; but got %#v", iptables.rules.rulesv6)
	}
	actual := iptables.BuildV4()
	expected := [][]string{
		{"iptables", "-t", "table", "-N", "chain"},
		{"iptables", "-t", "table", "-A", "chain", "-f", "foo", "-b", "bar"},
		{"iptables", "-t", "table", "-A", "chain", "-f", "fu", "-b", "bar"},
		{"iptables", "-t", "table", "-A", "chain", "-f", "foo", "-b", "baz"},
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

func TestBuildV4InsertMultipleRules(t *testing.T) {
	iptables := NewIptablesBuilder()
	iptables.InsertRuleV4("chain", "table", 1, "-f", "foo", "-b", "bar")
	iptables.InsertRuleV4("chain", "table", 2, "-f", "foo", "-b", "baaz")
	iptables.InsertRuleV4("chain", "table", 3, "-f", "foo", "-b", "baz")
	if err := len(iptables.rules.rulesv6) != 0; err {
		t.Errorf("Expected rulesV6 to be empty; but got %#v", iptables.rules.rulesv6)
	}
	actual := iptables.BuildV4()
	expected := [][]string{
		{"iptables", "-t", "table", "-N", "chain"},
		{"iptables", "-t", "table", "-I", "chain", "1", "-f", "foo", "-b", "bar"},
		{"iptables", "-t", "table", "-I", "chain", "2", "-f", "foo", "-b", "baaz"},
		{"iptables", "-t", "table", "-I", "chain", "3", "-f", "foo", "-b", "baz"},
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

func TestBuildV4AppendInsertMultipleRules(t *testing.T) {
	iptables := NewIptablesBuilder()
	iptables.AppendRuleV4("chain", "table", "-f", "foo", "-b", "bar")
	iptables.InsertRuleV4("chain", "table", 2, "-f", "foo", "-b", "bar")
	iptables.AppendRuleV4("chain", "table", "-f", "foo", "-b", "baz")
	if err := len(iptables.rules.rulesv6) != 0; err {
		t.Errorf("Expected rulesV6 to be empty; but got %#v", iptables.rules.rulesv6)
	}
	actual := iptables.BuildV4()
	expected := [][]string{
		{"iptables", "-t", "table", "-N", "chain"},
		{"iptables", "-t", "table", "-A", "chain", "-f", "foo", "-b", "bar"},
		{"iptables", "-t", "table", "-I", "chain", "2", "-f", "foo", "-b", "bar"},
		{"iptables", "-t", "table", "-A", "chain", "-f", "foo", "-b", "baz"},
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

func TestBuildV6InsertSingleRule(t *testing.T) {
	iptables := NewIptablesBuilder()
	iptables.InsertRuleV6("chain", "table", 2, "-f", "foo", "-b", "bar")
	if err := len(iptables.rules.rulesv4) != 0; err {
		t.Errorf("Expected rulesV4 to be empty; but got %#v", iptables.rules.rulesv4)
	}
	actual := iptables.BuildV6()
	expected := [][]string{
		{"ip6tables", "-t", "table", "-N", "chain"},
		{"ip6tables", "-t", "table", "-I", "chain", "2", "-f", "foo", "-b", "bar"},
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

func TestBuildV6AppendSingleRule(t *testing.T) {
	iptables := NewIptablesBuilder()
	iptables.AppendRuleV6("chain", "table", "-f", "foo", "-b", "bar")
	if err := len(iptables.rules.rulesv4) != 0; err {
		t.Errorf("Expected rulesV6 to be empty; but got %#v", iptables.rules.rulesv6)
	}
	actual := iptables.BuildV6()
	expected := [][]string{
		{"ip6tables", "-t", "table", "-N", "chain"},
		{"ip6tables", "-t", "table", "-A", "chain", "-f", "foo", "-b", "bar"},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actual, expected)
	}
	// V6 rules should be empty and return an empty slice
	actual = iptables.BuildV4()
	if !reflect.DeepEqual(actual, [][]string{}) {
		t.Errorf("Expected V6 rules to be empty; but instead got Actual: %#v", actual)
	}
}

func TestBuildV6AppendMultipleRules(t *testing.T) {
	iptables := NewIptablesBuilder()
	iptables.AppendRuleV6("chain", "table", "-f", "foo", "-b", "bar")
	iptables.AppendRuleV6("chain", "table", "-f", "fu", "-b", "bar")
	iptables.AppendRuleV6("chain", "table", "-f", "foo", "-b", "baz")
	if err := len(iptables.rules.rulesv4) != 0; err {
		t.Errorf("Expected rulesV6 to be empty; but got %#v", iptables.rules.rulesv6)
	}
	actual := iptables.BuildV6()
	expected := [][]string{
		{"ip6tables", "-t", "table", "-N", "chain"},
		{"ip6tables", "-t", "table", "-A", "chain", "-f", "foo", "-b", "bar"},
		{"ip6tables", "-t", "table", "-A", "chain", "-f", "fu", "-b", "bar"},
		{"ip6tables", "-t", "table", "-A", "chain", "-f", "foo", "-b", "baz"},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actual, expected)
	}
	// V6 rules should be empty and return an empty slice
	actual = iptables.BuildV4()
	if !reflect.DeepEqual(actual, [][]string{}) {
		t.Errorf("Expected V6 rules to be empty; but instead got Actual: %#v", actual)
	}
}

func TestBuildV6InsertMultipleRules(t *testing.T) {
	iptables := NewIptablesBuilder()
	iptables.InsertRuleV6("chain", "table", 1, "-f", "foo", "-b", "bar")
	iptables.InsertRuleV6("chain", "table", 2, "-f", "foo", "-b", "baaz")
	iptables.InsertRuleV6("chain", "table", 3, "-f", "foo", "-b", "baz")
	if err := len(iptables.rules.rulesv4) != 0; err {
		t.Errorf("Expected rulesV4 to be empty; but got %#v", iptables.rules.rulesv4)
	}
	actual := iptables.BuildV6()
	expected := [][]string{
		{"ip6tables", "-t", "table", "-N", "chain"},
		{"ip6tables", "-t", "table", "-I", "chain", "1", "-f", "foo", "-b", "bar"},
		{"ip6tables", "-t", "table", "-I", "chain", "2", "-f", "foo", "-b", "baaz"},
		{"ip6tables", "-t", "table", "-I", "chain", "3", "-f", "foo", "-b", "baz"},
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

func TestBuildV6InsertAppendMultipleRules(t *testing.T) {
	iptables := NewIptablesBuilder()
	iptables.AppendRuleV6("chain", "table", "-f", "foo", "-b", "bar")
	iptables.InsertRuleV6("chain", "table", 2, "-f", "foo", "-b", "bar")
	iptables.InsertRuleV6("chain", "table", 1, "-f", "foo", "-b", "bar")
	if err := len(iptables.rules.rulesv4) != 0; err {
		t.Errorf("Expected rulesV4 to be empty; but got %#v", iptables.rules.rulesv4)
	}
	actual := iptables.BuildV6()
	expected := [][]string{
		{"ip6tables", "-t", "table", "-N", "chain"},
		{"ip6tables", "-t", "table", "-A", "chain", "-f", "foo", "-b", "bar"},
		{"ip6tables", "-t", "table", "-I", "chain", "2", "-f", "foo", "-b", "bar"},
		{"ip6tables", "-t", "table", "-I", "chain", "1", "-f", "foo", "-b", "bar"},
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

func TestBuildV4V6MultipleRulesWithNewChain(t *testing.T) {
	iptables := NewIptablesBuilder()
	iptables.AppendRuleV4("chain", "table", "-f", "foo", "-b", "bar")
	iptables.InsertRuleV4("chain", "table", 2, "-f", "foo", "-b", "bar")
	iptables.AppendRuleV4("chain", "table", "-f", "foo", "-b", "baz")
	iptables.AppendRuleV6("chain", "table", "-f", "foo", "-b", "bar")
	iptables.InsertRuleV6("chain", "table", 2, "-f", "foo", "-b", "bar")
	iptables.InsertRuleV6("chain", "table", 1, "-f", "foo", "-b", "bar")
	iptables.AppendRuleV4(constants.PREROUTING, constants.NAT, "-f", "foo", "-b", "bar")
	actualV4 := iptables.BuildV4()
	actualV6 := iptables.BuildV6()
	expectedV6 := [][]string{
		{"ip6tables", "-t", "table", "-N", "chain"},
		{"ip6tables", "-t", "table", "-A", "chain", "-f", "foo", "-b", "bar"},
		{"ip6tables", "-t", "table", "-I", "chain", "2", "-f", "foo", "-b", "bar"},
		{"ip6tables", "-t", "table", "-I", "chain", "1", "-f", "foo", "-b", "bar"},
	}
	expectedV4 := [][]string{
		{"iptables", "-t", "table", "-N", "chain"},
		{"iptables", "-t", "table", "-A", "chain", "-f", "foo", "-b", "bar"},
		{"iptables", "-t", "table", "-I", "chain", "2", "-f", "foo", "-b", "bar"},
		{"iptables", "-t", "table", "-A", "chain", "-f", "foo", "-b", "baz"},
		{"iptables", "-t", "nat", "-A", "PREROUTING", "-f", "foo", "-b", "bar"},
	}
	if !reflect.DeepEqual(actualV4, expectedV4) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actualV4, expectedV4)
	}
	if !reflect.DeepEqual(actualV6, expectedV6) {
		t.Errorf("Actual and expected output mismatch; but instead got Actual: %#v ; Expected: %#v", actualV6, expectedV6)
	}
}
