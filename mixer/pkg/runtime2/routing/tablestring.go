// Copyright 2017 Istio Authors
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
	"sort"
	"strings"

	"istio.io/api/mixer/v1/template"
	"istio.io/istio/mixer/pkg/template"
)

func (t *Table) String() string {
	var b bytes.Buffer

	fmt.Fprintln(&b, "[Routing Table]")
	fmt.Fprintf(&b, "ID: %d", t.id)
	fmt.Fprintln(&b)

	fmt.Fprintf(&b, "Identity Attr: %s", t.identityAttribute)
	fmt.Fprintln(&b)

	keys := make([]int, 0, len(t.entries))
	for k := range t.entries {
		keys = append(keys, int(k))
	}
	sort.Ints(keys)

	for i, k := range keys {
		key := istio_mixer_v1_template.TemplateVariety(k)
		entry := t.entries[key]

		fmt.Fprintf(&b, "[#%d] %v {V}", i, key)
		fmt.Fprintln(&b)

		entry.write(&b, 1, t.debugInfo)
	}

	return b.String()
}

func (v *namespaceTable) write(b *bytes.Buffer, indent int, debugInfo *tableDebugInfo) {
	idnt := strings.Repeat("  ", indent)

	keys := make([]string, 0, len(v.entries))
	for k := range v.entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for i, k := range keys {
		sets := v.entries[k]

		fmt.Fprintf(b, "%s[#%d] %s {NS}", idnt, i, k)
		fmt.Fprintln(b)

		sets.write(b, indent+1, debugInfo)
	}
}

func (v *namespaceTable) String() string {
	var b bytes.Buffer
	v.write(&b, 0, nil)
	return b.String()
}

func (e *handlerEntries) write(b *bytes.Buffer, indent int, debugInfo *tableDebugInfo) {
	idnt := strings.Repeat("  ", indent)

	for i, entry := range e.entries {
		fmt.Fprintf(b, "%s[#%d] ", idnt, i)

		if debugInfo != nil {
			fmt.Fprintf(b, "%s", debugInfo.handlerEntries[entry.ID])
		} else {
			fmt.Fprintf(b, "%v", entry.Handler)
		}
		fmt.Fprintln(b, " {H}")

		indent++
		idnt := strings.Repeat("  ", indent)

		inputs := entry.Inputs
		if debugInfo != nil {
			// sort entries by condition string for stable ordering. This helps with tests.
			s := inputSetSorter{
				inputs:    inputs,
				debugInfo: debugInfo,
			}
			sort.Stable(&s)
		}

		for i, input := range inputs {
			fmt.Fprintf(b, "%s[#%d]", idnt, i)
			fmt.Fprintln(b)
			input.write(b, indent+1, debugInfo)
		}
	}
}
func (e *handlerEntries) String() string {
	var b bytes.Buffer
	e.write(&b, 0, nil)
	return b.String()
}

func (s *InputSet) write(b *bytes.Buffer, indent int, debugInfo *tableDebugInfo) {
	idnt := strings.Repeat("  ", indent)

	fmt.Fprintf(b, "%sConditional: ", idnt)
	if s.Condition != nil {
		if debugInfo != nil {
			fmt.Fprint(b, debugInfo.inputSets[s.ID].match)
		} else {
			fmt.Fprint(b, "...")
		}
	} else {
		fmt.Fprint(b, "<NONE>")
	}
	fmt.Fprintln(b)

	if debugInfo != nil {
		sorter := builderSorter{
			setId:     s.ID,
			builders:  s.Builders,
			debugInfo: debugInfo,
		}
		sort.Stable(&sorter)
		for j := range s.Builders {
			fmt.Fprintf(b, "%s[#%d]", idnt, j)
			if debugInfo != nil {
				fmt.Fprintf(b, " %s {I}", debugInfo.inputSets[s.ID].instanceNames[j])
			}
			fmt.Fprintln(b)
		}
	} else {
		for i, bld := range s.Builders {
			fmt.Fprintf(b, "%s[#%d] %v", idnt, i, bld)
			fmt.Fprintln(b)
		}
	}
}

func (s *InputSet) String() string {
	var b bytes.Buffer
	s.write(&b, 0, nil)
	return b.String()
}

type inputSetSorter struct {
	inputs    []*InputSet
	debugInfo *tableDebugInfo
}

// Len is part of sort.Interface.
func (s *inputSetSorter) Len() int {
	return len(s.inputs)
}

// Swap is part of sort.Interface.
func (s *inputSetSorter) Swap(i, j int) {
	s.inputs[i], s.inputs[j] = s.inputs[j], s.inputs[i]
}

// Less is part of sort.Interface.
func (s *inputSetSorter) Less(i, j int) bool {
	if s.inputs[j].Condition == nil {
		return false
	}
	if s.inputs[i].Condition == nil {
		return true
	}
	return s.debugInfo.inputSets[s.inputs[i].ID].match < s.debugInfo.inputSets[s.inputs[j].ID].match
}

type builderSorter struct {
	setId     uint32
	builders  []template.InstanceBuilderFn
	debugInfo *tableDebugInfo
}

// Len is part of sort.Interface.
func (s *builderSorter) Len() int {
	return len(s.builders)
}

// Swap is part of sort.Interface.
func (s *builderSorter) Swap(i, j int) {
	s.builders[i], s.builders[j] = s.builders[j], s.builders[i]
	s.debugInfo.inputSets[s.setId].instanceNames[i], s.debugInfo.inputSets[s.setId].instanceNames[j] =
		s.debugInfo.inputSets[s.setId].instanceNames[j], s.debugInfo.inputSets[s.setId].instanceNames[i]
}

// Less is part of sort.Interface.
func (s *builderSorter) Less(i, j int) bool {
	return s.debugInfo.inputSets[s.setId].instanceNames[i] < s.debugInfo.inputSets[s.setId].instanceNames[j]
}
