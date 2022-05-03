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

package param

import (
	"text/template"
	"text/template/parse"

	"istio.io/istio/pkg/util/sets"
)

// Template that has been parsed to search for template parameters.
type Template struct {
	*template.Template
	params sets.Set
}

// Parse the given template to find the set of template parameters used.
func Parse(t *template.Template) *Template {
	return &Template{
		Template: t,
		params:   getParams(t.Root),
	}
}

// Params returns the set of parameters that were found in this template.
func (t Template) Params() sets.Set {
	return t.params.Copy()
}

// Contains returns true if the given parameter is used by this Template.
func (t Template) Contains(p string) bool {
	return t.params.Contains(p)
}

// ContainsWellKnown returns true if the given well-known parameter is used by
// this Template.
func (t Template) ContainsWellKnown(p WellKnown) bool {
	return t.Contains(p.String())
}

// MissingParams checks the provided params against the parameters used in this Template.
// Returns the set of template parameters not defined in params.
func (t Template) MissingParams(params Params) sets.Set {
	out := sets.New()
	for needed := range t.params {
		if !params.Contains(needed) {
			out.Insert(needed)
		}
	}
	return out
}

func getParams(n parse.Node) sets.Set {
	out := sets.New()
	switch n.Type() {
	case parse.NodeField:
		// Only look at the first identifier. For example, if the action
		// is {{.To.Config.Service}}, this will return "To" as the parameter.
		// That's all we need when looking for well-known params.
		firstIdentifier := n.(*parse.FieldNode).Ident[0]
		out.Insert(firstIdentifier)
	case parse.NodeIf:
		out.Merge(getParams(n.(*parse.IfNode).Pipe))
	case parse.NodeWith:
		out.Merge(getParams(n.(*parse.WithNode).Pipe))
	case parse.NodeRange:
		out.Merge(getParams(n.(*parse.RangeNode).Pipe))
	case parse.NodeAction:
		out.Merge(getParams(n.(*parse.ActionNode).Pipe))
	case parse.NodeCommand:
		for _, arg := range n.(*parse.CommandNode).Args {
			out.Merge(getParams(arg))
		}
	case parse.NodePipe:
		for _, c := range n.(*parse.PipeNode).Cmds {
			out.Merge(getParams(c))
		}
	case parse.NodeList:
		for _, next := range n.(*parse.ListNode).Nodes {
			out.Merge(getParams(next))
		}
	}
	return out
}
