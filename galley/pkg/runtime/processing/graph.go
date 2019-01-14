//  Copyright 2019 Istio Authors
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

package processing

import (
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/pkg/mcp/snapshot"
)

// Graph handles incoming events and facilitates creation of snapshots.
type Graph interface {
	Handler

	Snapshot(urls []resource.TypeURL) snapshot.Snapshot
}

type graph struct {
	handler     Handler
	snapshotter *snapshotter
}

var _ Graph = &graph{}

// Handle implements Handler
func (g *graph) Handle(e resource.Event) {
	g.handler.Handle(e)
}

// Snapshot implements Graph
func (g *graph) Snapshot(urls []resource.TypeURL) snapshot.Snapshot {
	return g.snapshotter.snapshot(urls)
}

// GraphBuilder builds a new graph
type GraphBuilder struct {
	projections []Projection
	dispatcher  *DispatcherBuilder
	listeners   []Listener
}

// NewGraphBuilder returns a new GraphBuilder
func NewGraphBuilder() *GraphBuilder {
	return &GraphBuilder{
		dispatcher: NewDispatcherBuilder(),
	}
}

// AddHandler adds a new handler for the given resource type URL
func (b *GraphBuilder) AddHandler(t resource.TypeURL, h Handler) {
	b.dispatcher.Add(t, h)
}

// AddProjection adds a new projection
func (b *GraphBuilder) AddProjection(p Projection) {
	b.projections = append(b.projections, p)
}

// AddListener adds a new listener
func (b *GraphBuilder) AddListener(l Listener) {
	b.listeners = append(b.listeners, l)
}

// Build creates and returns a graph
func (b *GraphBuilder) Build() Graph {
	handler := b.dispatcher.Build()

	// TODO: Should we keep a reference to notifier?
	n := newNotifier(b.listeners, b.projections)
	_ = n

	snapshotter := newSnapshotter(b.projections)

	p := &graph{
		handler:     handler,
		snapshotter: snapshotter,
	}

	return p
}
