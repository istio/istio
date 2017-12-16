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

func (v *VarietyDestinations) write(b *bytes.Buffer, indent int, debugInfo *tableDebugInfo) {
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

		sets.write(b, indent + 1, debugInfo)
	}
}

func (v *VarietyDestinations) String() string {
	var b bytes.Buffer
	v.write(&b, 0, nil)
	return b.String()
}

func (d *DestinationSet) write(b *bytes.Buffer, indent int, debugInfo *tableDebugInfo) {
	idnt := strings.Repeat("  ", indent)

	for i, e := range d.entries {
		fmt.Fprintf(b, "%s[#%d] ", idnt, i)

		if debugInfo != nil {
			fmt.Fprintf(b, "%s", debugInfo.handlerNames[e.Handler])
		} else {
			fmt.Fprintf(b, "%v",e.Handler)
		}
		fmt.Fprintln(b, " {H}")

		indent++
		idnt := strings.Repeat("  ", indent)
		for i, input := range e.Inputs {
			fmt.Fprintf(b, "%s[#%d]", idnt, i)
			fmt.Fprintln(b)
			input.write(b, indent + 1, debugInfo)
		}
	}
}

func (d *DestinationSet) String() string {
	var b bytes.Buffer
	d.write(&b, 0, nil)
	return b.String()
}

func (i *InputSet) write(b *bytes.Buffer, indent int, debugInfo *tableDebugInfo) {
	idnt := strings.Repeat("  ", indent)

	fmt.Fprintf(b, "%sConditional: ", idnt)
	if i.Condition != nil {
		if debugInfo != nil {
			fmt.Fprint(b, debugInfo.matchConditions[i.Condition])
		} else {
			fmt.Fprint(b, "...")
		}
	} else {
		fmt.Fprint(b, "<NONE>")
	}
	fmt.Fprintln(b)

	for i, bld := range i.Builders {
		fmt.Fprintf(b, "%s[#%d]", idnt, i)
		if debugInfo != nil {
			fmt.Fprintf(b, " %s {I}", debugInfo.instanceNames[bld])
		}
		fmt.Fprintln(b)
	}
}

func (i *InputSet) String() string {
	var b bytes.Buffer
	i.write(&b, 0, nil)
	return b.String()
}