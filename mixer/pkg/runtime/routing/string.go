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
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"

	tpb "istio.io/api/mixer/adapter/model/v1beta1"
)

func (t *Table) String() string {
	var b bytes.Buffer

	fmt.Fprintln(&b, "[Routing ExpectedTable]")
	fmt.Fprintf(&b, "ID: %d", t.id)
	fmt.Fprintln(&b)

	// Stable sort order for varieties.
	keys := make([]int, 0, len(t.entries))
	for k := range t.entries {
		keys = append(keys, int(k))
	}
	sort.Ints(keys)

	for i, k := range keys {
		key := tpb.TemplateVariety(k)
		entry := t.entries[key]

		fmt.Fprintf(&b, "[#%d] %v {V}", i, key)
		fmt.Fprintln(&b)

		entry.write(&b, 1, t.debugInfo)
	}

	return b.String()
}

func (d *varietyTable) write(w io.Writer, indent int, debugInfo *tableDebugInfo) {
	idnt := strings.Repeat("  ", indent)

	// Stable sort order based on namespaces.
	keys := make([]string, 0, len(d.entries))
	for k := range d.entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for i, k := range keys {
		sets := d.entries[k]

		fmt.Fprintf(w, "%s[#%d] %s {NS}", idnt, i, k)
		fmt.Fprintln(w)

		sets.write(w, indent+1, debugInfo)
	}
}

func (d *NamespaceTable) write(w io.Writer, indent int, debugInfo *tableDebugInfo) {
	idnt := strings.Repeat("  ", indent)

	entries := d.entries
	// Copy and stable sort the entries by the handler name.
	e := make([]*Destination, len(entries))
	copy(e, entries)
	entries = e

	sort.SliceStable(entries, func(i int, j int) bool {
		return entries[i].HandlerName < entries[j].HandlerName
	})

	for i, entry := range entries {
		fmt.Fprintf(w, "%s[#%d] ", idnt, i)

		if debugInfo != nil {
			fmt.Fprintf(w, "%s ", entry.HandlerName)
		}
		fmt.Fprintln(w, "{H}")

		idnt := strings.Repeat("  ", indent+1)

		inputs := entry.InstanceGroups
		// Copy and stable sort the input sets, based on match clause text.
		if debugInfo != nil {
			i := make([]*InstanceGroup, len(inputs))
			copy(i, inputs)
			inputs = i
			sort.SliceStable(inputs, func(i int, j int) bool {
				iMatch := debugInfo.matchesByID[inputs[i].id]
				jMatch := debugInfo.matchesByID[inputs[j].id]
				return iMatch < jMatch
			})
		}

		for i, input := range inputs {
			fmt.Fprintf(w, "%s[#%d]", idnt, i)
			fmt.Fprintln(w)
			input.write(w, indent+2, debugInfo)
		}
	}
}

func (d *NamespaceTable) string(t *Table) string {
	var b bytes.Buffer
	d.write(&b, 0, t.debugInfo)
	return b.String()
}

func (i *InstanceGroup) write(w io.Writer, indent int, debugInfo *tableDebugInfo) {
	idnt := strings.Repeat("  ", indent)

	fmt.Fprintf(w, "%sCondition: ", idnt)
	if i.Condition != nil {
		if debugInfo != nil {
			fmt.Fprint(w, debugInfo.matchesByID[i.id])
		} else {
			fmt.Fprint(w, "<PRESENT>")
		}
	} else {
		fmt.Fprint(w, "<NONE>")
	}
	fmt.Fprintln(w)

	if debugInfo != nil {
		// Copy and stable sort the input instance names, based on match clause text.
		instanceNames := make([]string, len(i.Builders))
		for j := range i.Builders {
			instanceNames[j] = debugInfo.instanceNamesByID[i.id][j]
		}
		sort.Strings(instanceNames)

		for j, name := range instanceNames {
			fmt.Fprintf(w, "%s[#%d]", idnt, j)
			fmt.Fprintf(w, " %s {I}", name)
			fmt.Fprintln(w)
		}
	} else {
		for i, bld := range i.Builders {
			fmt.Fprintf(w, "%s[#%d] %v {I}", idnt, i, bld)
			fmt.Fprintln(w)
		}
	}
}
