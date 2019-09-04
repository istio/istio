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

	ad.Distribute("syntheticServiceEntry", sSynthetic)
	ad.Distribute("default", sDefault)
	ad.Distribute("other", sOther)

	// Assert we sent every received snapshot to the distributor
	g.Eventually(func() snapshot.Snapshot { return d.GetSnapshot("default") }).Should(Equal(sDefault))
	g.Eventually(func() snapshot.Snapshot { return d.GetSnapshot("syntheticServiceEntry") }).Should(Equal(sSynthetic))
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
