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
	"io"
	"sort"
	"strings"

	"istio.io/api/mixer/v1/template"
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

func (e *HandlerEntries) write(w io.Writer, indent int, debugInfo *tableDebugInfo) {
	idnt := strings.Repeat("  ", indent)

	for i, entry := range e.entries {
		fmt.Fprintf(w, "%s[#%d] ", idnt, i)

		if debugInfo != nil {
			fmt.Fprintf(w, "%s", debugInfo.handlerNamesByID[entry.ID])
		} else {
			fmt.Fprintf(w, "%v", entry.Handler)
		}
		fmt.Fprintln(w, " {H}")

		indent++
		idnt := strings.Repeat("  ", indent)

		inputs := entry.Inputs
		for i, input := range inputs {
			fmt.Fprintf(w, "%s[#%d]", idnt, i)
			fmt.Fprintln(w)
			input.write(w, indent+1, debugInfo)
		}
	}
}
func (e *HandlerEntries) String() string {
	var b bytes.Buffer
	e.write(&b, 0, nil)
	return b.String()
}

func (s *InputSet) write(w io.Writer, indent int, debugInfo *tableDebugInfo) {
	idnt := strings.Repeat("  ", indent)

	fmt.Fprintf(w, "%sConditional: ", idnt)
	if s.Condition != nil {
		if debugInfo != nil {
			fmt.Fprint(w, debugInfo.matchesByID[s.ID])
		} else {
			fmt.Fprint(w, "...")
		}
	} else {
		fmt.Fprint(w, "<NONE>")
	}
	fmt.Fprintln(w)

	if debugInfo != nil {
		for j := range s.Builders {
			fmt.Fprintf(w, "%s[#%d]", idnt, j)
			if debugInfo != nil {
				fmt.Fprintf(w, " %s {I}", debugInfo.instanceNamesByID[s.ID][j])
			}
			fmt.Fprintln(w)
		}
	} else {
		for i, bld := range s.Builders {
			fmt.Fprintf(w, "%s[#%d] %v", idnt, i, bld)
			fmt.Fprintln(w)
		}
	}
}

func (s *InputSet) String() string {
	var b bytes.Buffer
	s.write(&b, 0, nil)
	return b.String()
}
