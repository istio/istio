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

	"istio.io/istio/tools/istio-iptables/pkg/config"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
	iptableslog "istio.io/istio/tools/istio-iptables/pkg/log"
)

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

func TestBuildV6AppendSingleRule(t *testing.T) {
	iptables := NewIptablesRuleBuilder(IPv6Config)
	iptables.AppendRuleV6(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "bar")
	if err := len(iptables.rules.rulesv4) != 0; err {
		t.Errorf("Expected rulesV6 to be empty; but got %#v", iptables.rules.rulesv6)
	}
	actual := iptables.BuildV6()
	expected := [][]string{
		{"-t", "table", "-N", "chain"},
		{"-t", "table", "-A", "chain", "-f", "foo", "-b", "bar"},
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
	iptables := NewIptablesRuleBuilder(IPv6Config)
	iptables.AppendRuleV6(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "bar")
	iptables.AppendRuleV6(iptableslog.UndefinedCommand, "chain", "table", "-f", "fu", "-b", "bar")
	iptables.AppendRuleV6(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "baz")
	if err := len(iptables.rules.rulesv4) != 0; err {
		t.Errorf("Expected rulesV6 to be empty; but got %#v", iptables.rules.rulesv6)
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
	// V6 rules should be empty and return an empty slice
	actual = iptables.BuildV4()
	if !reflect.DeepEqual(actual, [][]string{}) {
		t.Errorf("Expected V6 rules to be empty; but instead got Actual: %#v", actual)
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

func TestCheckRulesV4V6(t *testing.T) {
	iptables := NewIptablesRuleBuilder(IPv6Config)
	iptables.InsertRuleV4(iptableslog.UndefinedCommand, "chain", "table", 2, "-f", "foo", "-b", "bar")
	iptables.AppendRuleV4(iptableslog.UndefinedCommand, "chain2", "table2", "-f", "foo", "-b", "baz")
	iptables.AppendRuleV4(iptableslog.UndefinedCommand, "chain2", "table", "-f", "foo", "-b", "baz", "-j", "chain")
	iptables.InsertRuleV6(iptableslog.UndefinedCommand, "chain", "table", 3, "-f", "foo", "-b", "baar")
	iptables.AppendRuleV6(iptableslog.UndefinedCommand, "chain2", "table2", "-f", "foo", "-b", "baaz")
	iptables.AppendRuleV6(iptableslog.UndefinedCommand, "chain2", "table", "-f", "foo", "-b", "baaz", "-j", "chain")

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

func TestReverseRulesV4V6(t *testing.T) {
	for _, tt := range []struct {
		name       string
		setupFunc  func(iptables *IptablesRuleBuilder)
		expectedv4 [][]string
		expectedv6 [][]string
	}{
		{
			"no-jumps",
			func(iptables *IptablesRuleBuilder) {
				iptables.InsertRuleV4(iptableslog.UndefinedCommand, "chain", "table", 2, "-f", "foo", "-b", "bar")
				iptables.AppendRuleV4(iptableslog.UndefinedCommand, "chain2", "table2", "-f", "foo", "-b", "baz")
				iptables.InsertRuleV6(iptableslog.UndefinedCommand, "chain", "table", 3, "-f", "foo", "-b", "baar")
				iptables.AppendRuleV6(iptableslog.UndefinedCommand, "chain2", "table2", "-f", "foo", "-b", "baaz")
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
				iptables.InsertRuleV4(iptableslog.UndefinedCommand, "chain", "table", 2, "-f", "foo", "-b", "bar")
				iptables.AppendRuleV4(iptableslog.UndefinedCommand, "chain2", "table", "-f", "foo", "-b", "bar", "-j", "chain1")
				iptables.InsertRuleV4(iptableslog.UndefinedCommand, "chain2", "table", 1, "-f", "foo", "-b", "baz")
				iptables.AppendRuleV4(iptableslog.UndefinedCommand, "chain", "table2", "-f", "foo", "-b", "bar")
				iptables.InsertRuleV6(iptableslog.UndefinedCommand, "chain", "table", 2, "-f", "foo", "-b", "baar")
				iptables.AppendRuleV6(iptableslog.UndefinedCommand, "chain2", "table", "-f", "foo", "-b", "baar", "-j", "chain1")
				iptables.InsertRuleV6(iptableslog.UndefinedCommand, "chain2", "table", 1, "-f", "foo", "-b", "baaz")
				iptables.AppendRuleV6(iptableslog.UndefinedCommand, "chain", "table2", "-f", "foo", "-b", "baar")
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
				iptables.AppendRuleV4(iptableslog.UndefinedCommand, "ISTIO_TEST", "table", "-f", "foo", "-b", "bar")
				iptables.InsertRuleV4(iptableslog.UndefinedCommand, "chain", "table", 1, "-f", "foo", "-b", "bar", "-j", "ISTIO_TEST")
				iptables.AppendRuleV4(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "bar")
				iptables.AppendRuleV6(iptableslog.UndefinedCommand, "ISTIO_TEST", "table", "-f", "foo", "-b", "baar")
				iptables.InsertRuleV6(iptableslog.UndefinedCommand, "chain", "table", 1, "-f", "foo", "-b", "baar", "-j", "ISTIO_TEST")
				iptables.AppendRuleV6(iptableslog.UndefinedCommand, "chain", "table", "-f", "foo", "-b", "baar")
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
			iptables := NewIptablesRuleBuilder(IPv6Config)
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
