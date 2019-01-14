//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package flow

import (
	"fmt"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/galley/pkg/runtime/resource"
)

// ToEnvelopeFn converts an interface{} to an MCP envelope.
type ToEnvelopeFn func(iface interface{}) (*mcp.Resource, error)

// TableView is a View implementation, backed by a table.
type TableView struct {
	typeURL resource.TypeURL
	xform   ToEnvelopeFn
	table   *Table
	listener ViewListener
}

var _ View = &TableView{}
var _ tableChangeListener = &TableView{}

// NewTableView returns a TableView backed by the given Table instance.
func NewTableView(typeURL resource.TypeURL, c *Table, xform ToEnvelopeFn) *TableView {
	if xform == nil {
		xform = func(iface interface{}) (*mcp.Resource, error) {
			e, ok := iface.(*mcp.Resource)
			if !ok {
				return nil, fmt.Errorf("unable to convert object to envelope: %v", iface)
			}
			return e, nil
		}
	}

	v :=  &TableView{
		xform:   xform,
		typeURL: typeURL,
		table:   c,
	}

	c.setTableChangeListener(v)

	return v
}

// Type implements View
func (v *TableView) Type() resource.TypeURL {
	return v.typeURL
}

// Generation implements View
func (v *TableView) Generation() int64 {
	return v.table.Generation()
}

// Get implements View
func (v *TableView) Get() []*mcp.Resource {
	result := make([]*mcp.Resource, 0, v.table.Count())

	v.table.ForEachItem(func(iface interface{}) {
		e, err := v.xform(iface)
		if err != nil {
			scope.Errorf("TableView: %v", err)
			return
		}
		result = append(result, e)
	})

	return result
}

func (v *TableView) SetViewListener(l ViewListener) {
	v.listener = l
}

func (v *TableView) tableChanged(t *Table) {
	if v.listener != nil  {
		v.listener.Changed(v)
	}
}