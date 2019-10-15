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
	"istio.io/istio/galley/pkg/config/analysis/msg"
	coll "istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/meta/metadata"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/source/kube/rt"
	"istio.io/istio/galley/pkg/config/testing/data"
	"istio.io/istio/pkg/mcp/snapshot"
)

type updaterMock struct {
	messages diag.Messages
}

// Update implements StatusUpdater
func (u *updaterMock) Update(messages diag.Messages) {
	u.messages = messages
}

type analyzerMock struct {
	analyzeCalls       []*Snapshot
	collectionToAccess collection.Name
	entriesToReport    []*resource.Entry
}

// Analyze implements Analyzer
func (a *analyzerMock) Analyze(c analysis.Context) {
	ctx := *c.(*context)

	a.analyzeCalls = append(a.analyzeCalls, ctx.sn)

	c.Exists(a.collectionToAccess, resource.NewName("", ""))

	for _, r := range a.entriesToReport {
		c.Report(a.collectionToAccess, msg.NewInternalError(r, ""))
	}
}

// Name implements Analyzer
func (a *analyzerMock) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:   "",
		Inputs: collection.Names{},
	}
}

func TestAnalyzeAndDistributeSnapshots(t *testing.T) {
	g := NewGomegaWithT(t)

	u := &updaterMock{}
	a := &analyzerMock{
		collectionToAccess: data.Collection1,
		entriesToReport: []*resource.Entry{
			{
				Origin: &rt.Origin{
					Collection: data.Collection1,
					Name:       resource.NewName("includedNamespace", "r1"),
				},
			},
			{
				Origin: &rt.Origin{
					Collection: data.Collection1,
					Name:       resource.NewName("excludedNamespace", "r2"),
				},
			},
		},
	}
	d := NewInMemoryDistributor()

	var collectionAccessed collection.Name
	cr := func(col collection.Name) {
		collectionAccessed = col
	}

	settings := AnalyzingDistributorSettings{
		StatusUpdater:      u,
		Analyzer:           analysis.Combine("testCombined", a),
		Distributor:        d,
		AnalysisSnapshots:  []string{metadata.Default, metadata.SyntheticServiceEntry},
		TriggerSnapshot:    metadata.Default,
		CollectionReporter: cr,
		AnalysisNamespaces: []string{"includedNamespace"},
	}
	ad := NewAnalyzingDistributor(settings)

	sDefault := getTestSnapshot("a", "b")
	sSynthetic := getTestSnapshot("c")
	sOther := getTestSnapshot("a", "d")

	ad.Distribute(metadata.SyntheticServiceEntry, sSynthetic)
	ad.Distribute(metadata.Default, sDefault)
	ad.Distribute("other", sOther)

	// Assert we sent every received snapshot to the distributor
	g.Eventually(func() snapshot.Snapshot { return d.GetSnapshot(metadata.SyntheticServiceEntry) }).Should(Equal(sSynthetic))
	g.Eventually(func() snapshot.Snapshot { return d.GetSnapshot(metadata.Default) }).Should(Equal(sDefault))
	g.Eventually(func() snapshot.Snapshot { return d.GetSnapshot("other") }).Should(Equal(sOther))

	// Assert we triggered analysis only once, with the expected combination of snapshots
	sCombined := getTestSnapshot("a", "b", "c")
	g.Eventually(func() []*Snapshot { return a.analyzeCalls }).Should(ConsistOf(sCombined))

	// Verify the collection reporter hook was called
	g.Expect(collectionAccessed).To(Equal(a.collectionToAccess))

	// Verify we only reported messages in the AnalysisNamespaces
	g.Expect(u.messages).To(HaveLen(1))
	for _, m := range u.messages {
		g.Expect(m.Origin.Namespace()).To(Equal("includedNamespace"))
	}
}

func getTestSnapshot(names ...string) *Snapshot {
	c := make([]*coll.Instance, 0)
	for _, name := range names {
		c = append(c, coll.New(collection.NewName(name)))
	}
	return &Snapshot{
		set: coll.NewSetFromCollections(c),
	}
}
