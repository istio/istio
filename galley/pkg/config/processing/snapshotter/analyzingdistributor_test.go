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
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/pkg/mcp/snapshot"
)

type updaterMock struct{}

// Update implements StatusUpdater
func (u *updaterMock) Update(messages diag.Messages) {}

type analyzerMock struct {
	analyzeCalls []*Snapshot
}

// Analyze implements Analyzer
func (a *analyzerMock) Analyze(c analysis.Context) {
	ctx := *c.(*context)

	a.analyzeCalls = append(a.analyzeCalls, ctx.sn)
}

// Name implements Analyzer
func (a *analyzerMock) Name() string {
	return ""
}

func TestAnalyzeAndDistributeSnapshots(t *testing.T) {
	g := NewGomegaWithT(t)

	u := &updaterMock{}
	a := &analyzerMock{}
	d := NewInMemoryDistributor()
	ad := NewAnalyzingDistributor(u, a, d)

	sDefault := getTestSnapshot("a", "b")
	sSynthetic := getTestSnapshot("c")
	sOther := getTestSnapshot("a", "d")

	ad.Distribute(syntheticSnapshotGroup, sSynthetic)
	ad.Distribute(defaultSnapshotGroup, sDefault)
	ad.Distribute("other", sOther)

	// Assert we sent every received snapshot to the distributor
	g.Eventually(func() snapshot.Snapshot { return d.GetSnapshot(syntheticSnapshotGroup) }).Should(Equal(sSynthetic))
	g.Eventually(func() snapshot.Snapshot { return d.GetSnapshot(defaultSnapshotGroup) }).Should(Equal(sDefault))
	g.Eventually(func() snapshot.Snapshot { return d.GetSnapshot("other") }).Should(Equal(sOther))

	// Assert we triggered only once analysis, with the expected combination of snapshots
	sCombined := getTestSnapshot("a", "b", "c")
	g.Eventually(func() []*Snapshot { return a.analyzeCalls }).Should(ConsistOf(sCombined))
}

func getTestSnapshot(names ...string) *Snapshot {
	c := make([]*collection.Instance, 0)
	for _, name := range names {
		c = append(c, collection.New(collection.NewName(name)))
	}
	return &Snapshot{
		set: collection.NewSetFromCollections(c),
	}
}
