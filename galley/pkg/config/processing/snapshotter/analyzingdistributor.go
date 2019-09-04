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

package snapshotter

import (
	"sync"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/resource"
)

// AnalyzingDistributor is an snapshotter.Distributor implementation that will perform analysis on a snapshot before
// publishing. It will update the CRD status with the analysis results.
type AnalyzingDistributor struct {
	updater     StatusUpdater
	analyzer    analysis.Analyzer
	distributor Distributor

	analysisMu     sync.Mutex
	cancelAnalysis chan struct{}

	snapshotsMu   sync.RWMutex
	lastSnapshots map[string]*Snapshot
}

var _ Distributor = &AnalyzingDistributor{}

const defaultSnapshotGroup = "default"
const syntheticSnapshotGroup = "syntheticServiceEntry"

// NewAnalyzingDistributor returns a new instance of AnalyzingDistributor.
func NewAnalyzingDistributor(u StatusUpdater, a analysis.Analyzer, d Distributor) *AnalyzingDistributor {
	return &AnalyzingDistributor{
		updater:       u,
		analyzer:      a,
		distributor:   d,
		lastSnapshots: make(map[string]*Snapshot),
	}
}

// Distribute implements snapshotter.Distributor
func (d *AnalyzingDistributor) Distribute(name string, s *Snapshot) {
	// Keep the most recent snapshot for each snapshot group we care about for analysis so we can combine them
	// For analysis, we want default and synthetic, and we can safely combine them since they are disjoint.
	if name == defaultSnapshotGroup || name == syntheticSnapshotGroup {
		d.snapshotsMu.Lock()
		d.lastSnapshots[name] = s
		d.snapshotsMu.Unlock()
	}

	// Make use of the fact that "default" is the main snapshot group, currently. Once/if this changes, we will need to
	// redesign this approach.
	// We use "default" to trigger analysis, since it has a debounce strategy that means it won't trigger constantly.
	if name != defaultSnapshotGroup {
		d.distributor.Distribute(name, s)
		return
	}

	d.analysisMu.Lock()
	defer d.analysisMu.Unlock()

	// Cancel the previous analysis session, if it is still working.
	if d.cancelAnalysis != nil {
		close(d.cancelAnalysis)
		d.cancelAnalysis = nil
	}

	// start a new analysis session
	cancelAnalysis := make(chan struct{})
	d.cancelAnalysis = cancelAnalysis
	go d.analyzeAndDistribute(cancelAnalysis, s)

}

func (d *AnalyzingDistributor) analyzeAndDistribute(cancelCh chan struct{}, s *Snapshot) {
	// For analysis, we use a combined snapshot
	ctx := &context{
		sn:       d.getCombinedSnapshot(),
		cancelCh: cancelCh,
	}

	d.analyzer.Analyze(ctx)

	if !ctx.Canceled() {
		d.updater.Update(ctx.messages)
	}

	// Execution only reaches this point for default snapshot group
	d.distributor.Distribute(defaultSnapshotGroup, s)
}

// getCombinedSnapshot creates a new snapshot from the last snapshots of each snapshot group
// Important assumption: the collections in each snapshot don't overlap.
func (d *AnalyzingDistributor) getCombinedSnapshot() *Snapshot {
	var collections []*collection.Instance

	d.snapshotsMu.RLock()
	defer d.snapshotsMu.RUnlock()

	for _, s := range d.lastSnapshots {
		for _, n := range s.set.Names() {
			// Note that we don't clone the collections, so this combined snapshot is effectively a view into the component snapshots
			collections = append(collections, s.set.Collection(n))
		}
	}

	return &Snapshot{set: collection.NewSetFromCollections(collections)}
}

type context struct {
	sn       *Snapshot
	cancelCh chan struct{}
	messages diag.Messages
}

var _ analysis.Context = &context{}

// Report implements analysis.Context
func (c *context) Report(coll collection.Name, m diag.Message) {
	c.messages.Add(m)
}

// Find implements analysis.Context
func (c *context) Find(cpl collection.Name, name resource.Name) *resource.Entry {
	return c.sn.Find(cpl, name)
}

// ForEach implements analysis.Context
func (c *context) ForEach(col collection.Name, fn analysis.IteratorFn) {
	c.sn.ForEach(col, fn)
}

// Canceled implements analysis.Context
func (c *context) Canceled() bool {
	select {
	case <-c.cancelCh:
		return true
	default:
		return false
	}
}
