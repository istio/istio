// Copyright 2018 Istio Authors
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

package config

import (
	"fmt"
	"io"
	"sort"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/pool"
	"istio.io/istio/mixer/pkg/template"
)

// String writes out contents of a snapshot in a stable way. Useful for quickly writing out contents in a string for
// comparison testing.
func (s *Snapshot) String() string {
	b := pool.GetBuffer()
	fmt.Fprintf(b, "ID: %d", s.ID)
	fmt.Fprintln(b)

	names := make([]string, 0, 20)
	for t := range s.Templates {
		names = append(names, t)
	}
	sort.Strings(names)

	fmt.Fprintln(b, "Templates:")
	writeTemplates(b, s.Templates)

	fmt.Fprintln(b, "Adapters:")
	writeAdapters(b, s.Adapters)

	fmt.Fprintln(b, "Handlers:")
	writeHandlers(b, s.HandlersLegacy)

	fmt.Fprintln(b, "Instances:")
	writeInstances(b, s.InstancesLegacy)

	fmt.Fprintln(b, "Rules:")
	writeRules(b, s.RulesLegacy)

	if len(s.AdapterMetadatas) != 0 {
		fmt.Fprintln(b, "AdapterMetadata:")
		writeAdapterMetadatas(b, s.AdapterMetadatas)

	}

	if len(s.TemplateMetadatas) != 0 {
		fmt.Fprintln(b, "TemplateMetadata:")
		writeTemplateMetadatas(b, s.TemplateMetadatas)
	}

	fmt.Fprintf(b, "%v", s.Attributes)

	str := b.String()
	pool.PutBuffer(b)
	return str
}

func writeTemplates(w io.Writer, templates map[string]*template.Info) {
	i := 0
	names := make([]string, len(templates))
	for n := range templates {
		names[i] = n
		i++
	}
	sort.Strings(names)

	for _, n := range names {
		fmt.Fprintf(w, "  Name: %s", n)
		fmt.Fprintln(w)
	}
}

func writeAdapters(w io.Writer, adapters map[string]*adapter.Info) {
	i := 0
	names := make([]string, len(adapters))
	for n := range adapters {
		names[i] = n
		i++
	}
	sort.Strings(names)

	for _, n := range names {
		fmt.Fprintf(w, "  Name: %s", n)
		fmt.Fprintln(w)
	}
}

func writeHandlers(w io.Writer, handlers map[string]*HandlerLegacy) {
	i := 0
	names := make([]string, len(handlers))
	for n := range handlers {
		names[i] = n
		i++
	}
	sort.Strings(names)

	for _, n := range names {
		h := handlers[n]
		fmt.Fprintf(w, "  Name:    %s", h.Name)
		fmt.Fprintln(w)

		fmt.Fprintf(w, "  Adapter: %s", h.Adapter.Name)
		fmt.Fprintln(w)

		fmt.Fprintf(w, "  Params:  %+v", h.Params)
		fmt.Fprintln(w)
	}
}

func writeInstances(w io.Writer, instances map[string]*InstanceLegacy) {
	i := 0
	names := make([]string, len(instances))
	for n := range instances {
		names[i] = n
		i++
	}
	sort.Strings(names)

	for _, n := range names {
		h := instances[n]
		fmt.Fprintf(w, "  Name:     %s", h.Name)
		fmt.Fprintln(w)

		fmt.Fprintf(w, "  Template: %s", h.Template.Name)
		fmt.Fprintln(w)

		fmt.Fprintf(w, "  Params:   %+v", h.Params)
		fmt.Fprintln(w)
	}
}

func writeRules(w io.Writer, rules []*RuleLegacy) {
	names := make([]string, len(rules))
	m := make(map[string]*RuleLegacy, len(rules))
	for i, r := range rules {
		names[i] = r.Name
		m[r.Name] = r
	}
	sort.Strings(names)

	for _, n := range names {
		r := m[n]

		fmt.Fprintf(w, "  Name:      %s", r.Name)
		fmt.Fprintln(w)

		fmt.Fprintf(w, "  Namespace: %s", r.Namespace)
		fmt.Fprintln(w)

		fmt.Fprintf(w, "  Match:   %+v", r.Match)
		fmt.Fprintln(w)

		fmt.Fprintf(w, "  ResourceType: %v", r.ResourceType)
		fmt.Fprintln(w)

		fmt.Fprintln(w, "  Actions:")
		writeActions(w, r.Actions)
	}
}

func writeAdapterMetadatas(w io.Writer, adapters map[string]*AdapterMetadata) {
	names := make([]string, 0, len(adapters))
	for k := range adapters {
		names = append(names, k)
	}
	sort.Strings(names)

	for _, n := range names {
		a := adapters[n]

		fmt.Fprintf(w, "  Name:      %s", a.Name)
		fmt.Fprintln(w)

		fmt.Fprintf(w, "  Templates: %s", a.SupportedTemplates)
		fmt.Fprintln(w)
	}
}

func writeTemplateMetadatas(w io.Writer, templates map[string]*TemplateMetadata) {
	names := make([]string, 0, len(templates))
	for k := range templates {
		names = append(names, k)
	}
	sort.Strings(names)

	for _, n := range names {
		a := templates[n]

		fmt.Fprintf(w, "  Name:      %s", a.Name)
		fmt.Fprintln(w)
	}
}

func writeActions(w io.Writer, actions []*ActionLegacy) {
	// write actions without sorting. This should be acceptable, as the action order within an order is
	// based on the order on the original content. This is stricter than simple-equality, but should be good enough
	// for testing purposes.
	for _, a := range actions {
		fmt.Fprintf(w, "    Handler: %s", a.Handler.Name)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "    Instances:")

		for _, instance := range a.Instances {
			fmt.Fprintf(w, "      Name: %s", instance.Name)
			fmt.Fprintln(w)
		}
	}
}
