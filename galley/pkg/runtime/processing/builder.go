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

package processing

import (
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/pkg/mcp/snapshot"
)

// Pipeline handles incoming events and creates a snapshot
type Pipeline interface {
	Handler
	Snapshotter
}

type pipeline struct {
	handler     Handler
	snapshotter Snapshotter
}

var _ Pipeline = &pipeline{}

// Handle implements Handler
func (p *pipeline) Handle(e resource.Event) bool {
	return p.handler.Handle(e)
}

// Snapshot implements Pipeline
func (p *pipeline) Snapshot() snapshot.Snapshot {
	return p.snapshotter.Snapshot()
}

// PipelineBuilder builds a new pipeline
type PipelineBuilder struct {
	views   []View
	builder *DispatcherBuilder
}

// NewPipelineBuilder returns a new PipelineBuilder
func NewPipelineBuilder() *PipelineBuilder {
	return &PipelineBuilder{
		builder: NewDispatcherBuilder(),
	}
}

// AddHandler adds a new handler for the given resource type URL
func (b *PipelineBuilder) AddHandler(t resource.TypeURL, h Handler) {
	b.builder.Add(t, h)
}

// AddView adds a new view
func (b *PipelineBuilder) AddView(v View) {
	b.views = append(b.views, v)
}

// Build creates and returns a pipeline
func (b *PipelineBuilder) Build() Pipeline {
	handler := b.builder.Build()
	snapshotter := NewSnapshotter(b.views)
	return &pipeline{
		handler:     handler,
		snapshotter: snapshotter,
	}
}
