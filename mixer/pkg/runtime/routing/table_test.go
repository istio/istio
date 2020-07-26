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

package routing

import (
	"testing"

	tpb "istio.io/api/mixer/adapter/model/v1beta1"
	"istio.io/istio/mixer/pkg/runtime/testing/data"
	"istio.io/pkg/attribute"
)

var tableTests = []struct {
	// Name of the test
	Name string

	// Configs is the collection of all runtime configs to use.
	Configs []string

	// V is the variety for the query.
	V tpb.TemplateVariety

	// Ns is the namespace for the query.
	Ns string

	// ExpectedTable is the expected destination set output.
	E string
}{
	{
		Name:    "basic",
		Configs: []string{data.HandlerACheck1, data.InstanceCheck1, data.RuleCheck1},
		V:       tpb.TEMPLATE_VARIETY_CHECK,
		Ns:      "istio-system",
		E: `
[#0] hcheck1.acheck.istio-system {H}
  [#0]
    Condition: <NONE>
    [#0] icheck1.tcheck.istio-system {I}`,
	},
	{
		Name:    "no-rules-for-variety",
		Configs: []string{data.HandlerACheck1, data.InstanceCheck1, data.RuleCheck1},
		V:       tpb.TEMPLATE_VARIETY_REPORT,
		Ns:      "istio-system",
		E:       ``,
	},
	{
		Name:    "no-rules-for-namespace-returns-the-default-set",
		Configs: []string{data.HandlerACheck1, data.InstanceCheck1, data.RuleCheck1},
		V:       tpb.TEMPLATE_VARIETY_CHECK,
		Ns:      "ns2",
		E: `
[#0] hcheck1.acheck.istio-system {H}
  [#0]
    Condition: <NONE>
    [#0] icheck1.tcheck.istio-system {I}`,
	},
}

func TestTable_GetDestinations(t *testing.T) {
	for _, tst := range tableTests {

		t.Run(tst.Name, func(tt *testing.T) {
			serviceConfig := data.ServiceConfig
			table, snapshot := buildTable(serviceConfig, tst.Configs, true)

			destinations := table.GetDestinations(tst.V, tst.Ns)
			actual := destinations.string(table)
			if normalize(actual) != normalize(tst.E) {
				tt.Logf("Snapshot:\n%v", snapshot)
				tt.Fatalf("\n%v\n!=\n%v\n", actual, tst.E)
			}
		})
	}
}

func TestEmpty(t *testing.T) {
	table := Empty()

	if table.id != -1 {
		t.Fatal("id should be -1")
	}

	if table.ID() != table.id {
		t.Fatal("ID() method should return the id of the table.")
	}

	if table.entries != nil {
		t.Fatal("table's entries should be nil")
	}

	destinations := table.GetDestinations(tpb.TEMPLATE_VARIETY_CHECK, "istio-system")
	if destinations.Count() > 0 {
		t.Fatal("destination count should be zero")
	}
}

func TestDestination_MaxInstances(t *testing.T) {
	var tests = []struct {
		configs []string
		max     int
	}{
		{
			configs: []string{data.HandlerACheck1, data.InstanceCheck1, data.RuleCheck1},
			max:     1,
		},
		{
			configs: []string{data.HandlerACheck1, data.InstanceCheck1, data.RuleCheck1, data.RuleCheck2WithInstance1AndHandler},
			max:     2,
		},
	}

	for _, tst := range tests {
		table, _ := buildTable(
			data.ServiceConfig, tst.configs, true)

		destinations := table.GetDestinations(tpb.TEMPLATE_VARIETY_CHECK, "istio-system")
		entries := destinations.Entries()
		if len(entries) != 1 {
			t.Fatal("There should be one entry")
		}
		e := entries[0]
		if e.MaxInstances() != tst.max {
			t.Logf("Table:\n%v", table)
			t.Fatalf("Max Instances mismatch: %d != %d", e.maxInstances, tst.max)
		}
	}
}

func TestInputs_Matches(t *testing.T) {
	table, _ := buildTable(
		data.ServiceConfig, []string{data.HandlerACheck1, data.InstanceCheck1, data.RuleCheck1WithMatchClause}, true)

	destinations := table.GetDestinations(tpb.TEMPLATE_VARIETY_CHECK, "istio-system")
	entries := destinations.Entries()
	if len(entries) != 1 {
		t.Fatal("There should be one entry")
	}
	e := entries[0]
	if len(e.InstanceGroups) != 1 {
		t.Fatal("There should be 1 InstanceGroup")
	}
	i := e.InstanceGroups[0]

	// Value is not in bag. This should match to false due to error in resolution.
	bag := attribute.GetMutableBagForTesting(map[string]interface{}{})
	if i.Matches(bag) {
		t.Fatal("The group shouldn't have matched")
	}

	// Value is in the bag, but does match the condition.
	bag = attribute.GetMutableBagForTesting(map[string]interface{}{
		"destination.name": "barfoo",
	})
	if i.Matches(bag) {
		t.Fatal("The group shouldn't have matched")
	}

	// Value is in the bag, and matches the condition
	bag = attribute.GetMutableBagForTesting(map[string]interface{}{
		"destination.name": "foobar",
	})
	if !i.Matches(bag) {
		t.Fatal("The group should have matched")
	}
}

func TestInputs_Matches_NoCondition(t *testing.T) {
	table, _ := buildTable(
		data.ServiceConfig, []string{data.HandlerACheck1, data.InstanceCheck1, data.RuleCheck1}, true)

	destinations := table.GetDestinations(tpb.TEMPLATE_VARIETY_CHECK, "istio-system")
	entries := destinations.Entries()
	if len(entries) != 1 {
		t.Fatal("There should be one entry")
	}
	e := entries[0]
	if len(e.InstanceGroups) != 1 {
		t.Fatal("There should be 1 InstanceGroup")
	}
	i := e.InstanceGroups[0]

	// There no condition. Should simply return true.
	bag := attribute.GetMutableBagForTesting(map[string]interface{}{})
	if !i.Matches(bag) {
		t.Fatal("The group should have matched")
	}
}
