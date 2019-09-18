// Copyright 2019 Istio Authors
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

package analysis

import (
	"istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/scope"
)

// Analyzer is an interface for analyzing configuration.
type Analyzer interface {
	Metadata() Metadata
	Analyze(c Context)
}

// Metadata represents metadata for an analyzer
type Metadata struct {
	name            string
	collectionsUsed map[collection.Name]bool
}

// NewMetadata creates a new Metadata object
func NewMetadata(name string, collectionsUsed collection.Names) Metadata {
	cu := make(map[collection.Name]bool)
	for _, c := range collectionsUsed {
		cu[c] = true
	}
	return Metadata{
		name:            name,
		collectionsUsed: cu,
	}
}

// Name of the analyzer this metadata is for
func (m Metadata) Name() string {
	return m.name
}

// CollectionsUsed registers the collections accessed by the analyzer this metadata is for
func (m Metadata) CollectionsUsed() map[collection.Name]bool {
	return m.collectionsUsed
}

type combinedAnalyzers struct {
	metadata  Metadata
	analyzers []Analyzer
}

// Combine multiple analyzers into a single one.
// For collections used, use the union of the component analyzers
func Combine(name string, analyzers ...Analyzer) Analyzer {
	return &combinedAnalyzers{
		metadata:  NewMetadata(name, combineCollectionsUsed(analyzers)),
		analyzers: analyzers,
	}
}

// Metadata implements Analyzer
func (c *combinedAnalyzers) Metadata() Metadata {
	return c.metadata
}

// Analyze implements Analyzer
func (c *combinedAnalyzers) Analyze(ctx Context) {
	for _, a := range c.analyzers {
		scope.Analysis.Debugf("Started analyzer %q...", a.Metadata().Name())
		if ctx.Canceled() {
			scope.Analysis.Debugf("Analyzer %q has been cancelled...", c.Metadata().Name())
			return
		}
		a.Analyze(ctx)
		scope.Analysis.Debugf("Completed analyzer %q...", a.Metadata().Name())
	}
}

func combineCollectionsUsed(analyzers []Analyzer) collection.Names {
	collectionSet := make(map[collection.Name]bool)
	for _, a := range analyzers {
		for c := range a.Metadata().CollectionsUsed() {
			collectionSet[c] = true
		}
	}

	rc := make([]collection.Name, len(collectionSet))
	for k := range collectionSet {
		rc = append(rc, k)
	}

	return rc
}
