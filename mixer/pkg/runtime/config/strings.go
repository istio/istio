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

package config

import (
	"fmt"
	"io"
	"sort"

	"istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/pkg/pool"
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

	fmt.Fprintln(b, "TemplatesStatic:")
	writeTemplates(b, s.Templates)

	fmt.Fprintln(b, "AdaptersStatic:")
	writeAdapters(b, s.Adapters)

	fmt.Fprintln(b, "HandlersStatic:")
	writeStaticHandlers(b, s.HandlersStatic)

	fmt.Fprintln(b, "InstancesStatic:")
	writeStaticInstances(b, s.InstancesStatic)

	fmt.Fprintln(b, "Rules:")
	writeRules(b, s.Rules)

	if len(s.AdapterMetadatas) != 0 {
		fmt.Fprintln(b, "AdaptersDynamic:")
		writeAdapterMetadatas(b, s.AdapterMetadatas)

	}

	if len(s.TemplateMetadatas) != 0 {
		fmt.Fprintln(b, "TemplatesDynamic:")
		writeTemplateMetadatas(b, s.TemplateMetadatas)
	}

	if len(s.HandlersDynamic) != 0 {
		fmt.Fprintln(b, "HandlersDynamic:")
		writeDynamicHandlers(b, s.HandlersDynamic)
	}

	if len(s.InstancesDynamic) != 0 {
		fmt.Fprintln(b, "InstancesDynamic:")
		writeDynamicInstances(b, s.InstancesDynamic)
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

func writeStaticHandlers(w io.Writer, handlers map[string]*HandlerStatic) {
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

func writeDynamicHandlers(w io.Writer, handlers map[string]*HandlerDynamic) {
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
	}
}

func writeStaticInstances(w io.Writer, instances map[string]*InstanceStatic) {
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

func writeDynamicInstances(w io.Writer, instances map[string]*InstanceDynamic) {
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

		keys := make([]string, 0, len(h.Params))
		for k := range h.Params {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintln(w, "  Params:")
		for _, k := range keys {
			fmt.Fprintf(w, "  - %s:%v", k, h.Params[k])
			fmt.Fprintln(w)
		}
	}
}

func writeRules(w io.Writer, rules []*Rule) {
	names := make([]string, len(rules))
	m := make(map[string]*Rule, len(rules))
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

		fmt.Fprintln(w, "  ActionsStatic:")
		writeActionsStatic(w, r.ActionsStatic)

		if len(r.ActionsDynamic) != 0 {
			fmt.Fprintln(w, "  ActionsDynamic:")
			writeActionsDynamic(w, r.ActionsDynamic)
		}

		if len(r.RequestHeaderOperations) != 0 {
			fmt.Fprintln(w, "  RequestHeaderOperations:")
			writeRuleOperations(w, r.RequestHeaderOperations)
		}

		if len(r.ResponseHeaderOperations) != 0 {
			fmt.Fprintln(w, "  ResponseHeaderOperations:")
			writeRuleOperations(w, r.ResponseHeaderOperations)
		}
	}
}

func writeRuleOperations(w io.Writer, ops []*v1beta1.Rule_HeaderOperationTemplate) {
	for _, op := range ops {
		fmt.Fprintf(w, "    Name: %q\n", op.Name)
		for _, value := range op.Values {
			fmt.Fprintf(w, "    Value: %q\n", value)
		}
		fmt.Fprintf(w, "    Operation: %v\n", op.Operation)
	}
}

func writeAdapterMetadatas(w io.Writer, adapters map[string]*Adapter) {
	names := make([]string, 0, len(adapters))
	for k := range adapters {
		names = append(names, k)
	}
	sort.Strings(names)

	for _, n := range names {
		a := adapters[n]

		fmt.Fprintf(w, "  Name:      %s", a.Name)
		fmt.Fprintln(w)

		fmt.Fprint(w, "  Templates:")
		for _, tmplName := range a.SupportedTemplates {
			fmt.Fprintln(w)
			fmt.Fprintln(w, "  - ", tmplName.Name)
		}
		fmt.Fprintln(w)
	}
}

func writeTemplateMetadatas(w io.Writer, templates map[string]*Template) {
	names := make([]string, 0, len(templates))
	for k := range templates {
		names = append(names, k)
	}
	sort.Strings(names)

	for _, n := range names {
		a := templates[n]

		fmt.Fprintln(w, "  Resource Name: ", n)
		fmt.Fprintln(w, "    Name: ", a.Name)
		fmt.Fprintln(w, "    InternalPackageDerivedName: ", a.InternalPackageDerivedName)
	}
}

func writeActionsStatic(w io.Writer, actions []*ActionStatic) {
	// write actions without sorting. This should be acceptable, as the action order within an order is
	// based on the order on the original content. This is stricter than simple-equality, but should be good enough
	// for testing purposes.
	for _, a := range actions {
		fmt.Fprintf(w, "    Handler: %s", a.Handler.Name)
		fmt.Fprintln(w)
		if a.Name != "" {
			fmt.Fprintf(w, "    Name: %s\n", a.Name)
		}
		fmt.Fprintln(w, "    Instances:")

		for _, instance := range a.Instances {
			fmt.Fprintf(w, "      Name: %s", instance.Name)
			fmt.Fprintln(w)
		}
	}
}

func writeActionsDynamic(w io.Writer, actions []*ActionDynamic) {
	// write actions without sorting. This should be acceptable, as the action order within an order is
	// based on the order on the original content. This is stricter than simple-equality, but should be good enough
	// for testing purposes.
	for _, a := range actions {
		fmt.Fprintf(w, "    Handler: %s", a.Handler.Name)
		fmt.Fprintln(w)
		if a.Name != "" {
			fmt.Fprintf(w, "    Name: %s\n", a.Name)
		}
		fmt.Fprintln(w, "    Instances:")

		for _, instance := range a.Instances {
			fmt.Fprintf(w, "      Name: %s", instance.Name)
			fmt.Fprintln(w)
		}
	}
}
