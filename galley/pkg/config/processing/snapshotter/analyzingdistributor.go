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
}

var _ Distributor = &AnalyzingDistributor{}

// StatusUpdater updates resource statuses, based on the given diagnostic messages.
type StatusUpdater interface {
	Update(messages diag.Messages)
}

// InMemoryStatusUpdater is an in-memory implementation of StatusUpdater
type InMemoryStatusUpdater struct {
	mu     sync.RWMutex
	m      diag.Messages
	waitCh chan struct{}
}

var _ StatusUpdater = &InMemoryStatusUpdater{}

// NewAnalyzingDistributor returns a new instance of AnalyzingDistributor.
func NewAnalyzingDistributor(u StatusUpdater, a analysis.Analyzer, d Distributor) *AnalyzingDistributor {
	return &AnalyzingDistributor{
		updater:     u,
		analyzer:    a,
		distributor: d,
	}
}

// Distribute implements snapshotter.Distributor
func (d *AnalyzingDistributor) Distribute(name string, s *Snapshot) {
	// Make use of the fact that "default" is the main snapshot group, currently. Once/if this changes, we will need to
	// redesign this approach.

	if name != "default" {
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
	ctx := &context{
		sn:       s,
		cancelCh: cancelCh,
	}

	d.analyzer.Analyze(ctx)

	if !ctx.Canceled() {
		d.updater.Update(ctx.messages)
	}

	d.distributor.Distribute("default", s)
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

// Update implements StatusUpdater
func (u *InMemoryStatusUpdater) Update(m diag.Messages) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.m = m
	if u.waitCh != nil {
		close(u.waitCh)
	}
}

// Get returns the current set of captured diag.Messages
func (u *InMemoryStatusUpdater) Get() diag.Messages {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.m
}

// WaitForReport blocks until a report is available. Returns true if a report is available, false if cancelCh was closed.
func (u *InMemoryStatusUpdater) WaitForReport(cancelCh chan struct{}) bool {
	u.mu.Lock()
	if u.m != nil {
		return true
	}

	if u.waitCh == nil {
		u.waitCh = make(chan struct{})
	}
	ch := u.waitCh
	u.mu.Unlock()
	select {
	case <-cancelCh:
		return false
	case <-ch:
		return true
	}
}
