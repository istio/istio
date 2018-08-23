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

// Package servicegraph defines the core model for the servicegraph service.
package servicegraph

import (
	"encoding/json"
	"errors"
	"io"
)

type (
	// d3Graph is a graph representation for JSON serialization to be
	// consumed easily by the D3.js library.
	d3Graph struct {
		Nodes []d3Node `json:"nodes"`
		Links []d3Link `json:"links"`
	}

	d3Node struct {
		Name string `json:"name"`
	}

	d3Link struct {
		Source int        `json:"source"`
		Target int        `json:"target"`
		Labels Attributes `json:"labels"`
	}
)

func indexOf(nodes []d3Node, name string) (int, error) {
	for i, v := range nodes {
		if v.Name == name {
			return i, nil
		}
	}
	return 0, errors.New("invalid graph")
}

// GenerateD3JSON converts the standard Dynamic graph to d3Graph, then
// serializes to JSON.
func GenerateD3JSON(w io.Writer, g *Dynamic) error {
	graph := d3Graph{
		Nodes: make([]d3Node, 0, len(g.Nodes)),
		Links: make([]d3Link, 0, len(g.Edges)),
	}
	for k := range g.Nodes {
		n := d3Node{
			Name: k,
		}
		graph.Nodes = append(graph.Nodes, n)
	}
	for _, v := range g.Edges {
		s, err := indexOf(graph.Nodes, v.Source)
		if err != nil {
			return err
		}
		t, err := indexOf(graph.Nodes, v.Target)
		if err != nil {
			return err
		}
		l := d3Link{
			Source: s,
			Target: t,
			Labels: v.Labels,
		}
		graph.Links = append(graph.Links, l)
	}
	return json.NewEncoder(w).Encode(graph)
}
