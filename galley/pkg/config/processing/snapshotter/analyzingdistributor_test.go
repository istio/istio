// Copyright Istio Authors
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
	"testing"
	"time"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	coll "istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/source/kube/rt"
	"istio.io/istio/galley/pkg/config/testing/basicmeta"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	resource2 "istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/config/schema/snapshots"
	"istio.io/istio/pkg/mcp/snapshot"
)

type updaterMock struct {
	m        sync.RWMutex
	messages diag.Messages

	waitCh      chan struct{}
	waitTimeout time.Duration
}

// Update implements StatusUpdater
func (u *updaterMock) Update(messages diag.Messages) {
	u.m.Lock()
	defer u.m.Unlock()
	u.messages = messages

	if u.waitCh != nil {
		close(u.waitCh)
		u.waitCh = nil
	}
}

func (u *updaterMock) getMessages() diag.Messages {
	u.m.Lock()
	ms := u.messages
	if ms != nil {
		u.m.Unlock()
		return ms
	}

	if u.waitCh == nil {
		u.waitCh = make(chan struct{})
	}
	ch := u.waitCh
	u.m.Unlock()

	select {
	case <-time.After(u.waitTimeout):
		return nil
	case <-ch:
		u.m.RLock()
		m := u.messages
		u.m.RUnlock()
		return m
	}
}

type analyzerMock struct {
	m                  sync.RWMutex
	analyzeCalls       []*Snapshot
	collectionToAccess collection.Name
	resourcesToReport  []*resource.Instance
}

// Analyze implements Analyzer
func (a *analyzerMock) Analyze(c analysis.Context) {
	a.m.Lock()
	defer a.m.Unlock()

	ctx := *c.(*context)

	c.Exists(a.collectionToAccess, resource.NewFullName("", ""))

	for _, r := range a.resourcesToReport {
		c.Report(a.collectionToAccess, msg.NewInternalError(r, ""))
	}

	a.analyzeCalls = append(a.analyzeCalls, ctx.sn)
}

// Name implements Analyzer
func (a *analyzerMock) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:   "",
		Inputs: collection.Names{},
	}
}

func (a *analyzerMock) getAnalyzeCalls() []*Snapshot {
	a.m.RLock()
	defer a.m.RUnlock()

	out := make([]*Snapshot, len(a.analyzeCalls))
	copy(out, a.analyzeCalls)
	return out
}

func TestAnalyzeAndDistributeSnapshots(t *testing.T) {
	g := NewGomegaWithT(t)

	u := &updaterMock{}
	a := &analyzerMock{
		collectionToAccess: basicmeta.K8SCollection1.Name(),
		resourcesToReport: []*resource.Instance{
			{
				Origin: &rt.Origin{
					Collection: basicmeta.K8SCollection1.Name(),
					FullName:   resource.NewFullName("includedNamespace", "r1"),
				},
			},
			{
				Origin: &rt.Origin{
					Collection: basicmeta.K8SCollection1.Name(),
					FullName:   resource.NewFullName("excludedNamespace", "r2"),
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
		AnalysisSnapshots:  []string{snapshots.Default},
		TriggerSnapshot:    snapshots.Default,
		CollectionReporter: cr,
		AnalysisNamespaces: []resource.Namespace{"includedNamespace"},
	}
	ad := NewAnalyzingDistributor(settings)

	schemaA := newSchema("a")
	schemaB := newSchema("b")
	schemaD := newSchema("d")

	sDefault := getTestSnapshot(schemaA, schemaB)
	sOther := getTestSnapshot(schemaA, schemaD)

	ad.Distribute(snapshots.Default, sDefault)
	ad.Distribute("other", sOther)

	// Assert we sent every received snapshot to the distributor
	g.Eventually(func() snapshot.Snapshot { return d.GetSnapshot(snapshots.Default) }).Should(Equal(sDefault))
	g.Eventually(func() snapshot.Snapshot { return d.GetSnapshot("other") }).Should(Equal(sOther))

	// Assert we triggered analysis only once, with the expected combination of snapshots
	sCombined := getTestSnapshot(schemaA, schemaB)
	g.Eventually(a.getAnalyzeCalls).Should(ConsistOf(sCombined))

	// Verify the collection reporter hook was called
	g.Expect(collectionAccessed).To(Equal(a.collectionToAccess))

	// Verify we only reported messages in the AnalysisNamespaces
	g.Eventually(u.getMessages()).Should(HaveLen(1))
	for _, m := range u.getMessages() {
		g.Expect(m.Resource.Origin.Namespace()).To(Equal(resource.Namespace("includedNamespace")))
	}
}

func TestAnalyzeNamespaceMessageHasNoResource(t *testing.T) {
	g := NewGomegaWithT(t)

	u := &updaterMock{waitTimeout: 1 * time.Second}
	a := &analyzerMock{
		collectionToAccess: basicmeta.K8SCollection1.Name(),
		resourcesToReport: []*resource.Instance{
			nil,
		},
	}
	d := NewInMemoryDistributor()

	settings := AnalyzingDistributorSettings{
		StatusUpdater:      u,
		Analyzer:           analysis.Combine("testCombined", a),
		Distributor:        d,
		AnalysisSnapshots:  []string{snapshots.Default},
		TriggerSnapshot:    snapshots.Default,
		CollectionReporter: nil,
		AnalysisNamespaces: []resource.Namespace{"includedNamespace"},
	}
	ad := NewAnalyzingDistributor(settings)

	sDefault := getTestSnapshot()

	ad.Distribute(snapshots.Default, sDefault)
	g.Eventually(a.getAnalyzeCalls).Should(Not(BeEmpty()))
	g.Eventually(u.getMessages()).Should(HaveLen(1))
}

func TestAnalyzeNamespaceMessageHasOriginWithNoNamespace(t *testing.T) {
	g := NewGomegaWithT(t)

	u := &updaterMock{waitTimeout: 1 * time.Second}
	a := &analyzerMock{
		collectionToAccess: basicmeta.K8SCollection1.Name(),
		resourcesToReport: []*resource.Instance{
			{
				Origin: fakeOrigin{
					friendlyName: "myFriendlyName",
					// explicitly set namespace to the empty string
					namespace: "",
				},
			},
		},
	}
	d := NewInMemoryDistributor()

	settings := AnalyzingDistributorSettings{
		StatusUpdater:      u,
		Analyzer:           analysis.Combine("testCombined", a),
		Distributor:        d,
		AnalysisSnapshots:  []string{snapshots.Default},
		TriggerSnapshot:    snapshots.Default,
		CollectionReporter: nil,
		AnalysisNamespaces: []resource.Namespace{"includedNamespace"},
	}
	ad := NewAnalyzingDistributor(settings)

	sDefault := getTestSnapshot()

	ad.Distribute(snapshots.Default, sDefault)
	g.Eventually(a.getAnalyzeCalls).Should(Not(BeEmpty()))
	g.Eventually(u.getMessages()).Should(HaveLen(1))
}

func TestAnalyzeSortsMessages(t *testing.T) {
	g := NewGomegaWithT(t)

	u := &updaterMock{waitTimeout: 1 * time.Second}
	r1 := &resource.Instance{
		Origin: &rt.Origin{
			Collection: basicmeta.K8SCollection1.Name(),
			FullName:   resource.NewFullName("includedNamespace", "r2"),
		},
	}
	r2 := &resource.Instance{
		Origin: &rt.Origin{
			Collection: basicmeta.K8SCollection1.Name(),
			FullName:   resource.NewFullName("includedNamespace", "r1"),
		},
	}

	a := &analyzerMock{
		collectionToAccess: basicmeta.K8SCollection1.Name(),
		resourcesToReport:  []*resource.Instance{r1, r2},
	}
	d := NewInMemoryDistributor()

	settings := AnalyzingDistributorSettings{
		StatusUpdater:      u,
		Analyzer:           analysis.Combine("testCombined", a),
		Distributor:        d,
		AnalysisSnapshots:  []string{snapshots.Default},
		TriggerSnapshot:    snapshots.Default,
		CollectionReporter: nil,
		AnalysisNamespaces: []resource.Namespace{"includedNamespace"},
	}
	ad := NewAnalyzingDistributor(settings)

	sDefault := getTestSnapshot()

	ad.Distribute(snapshots.Default, sDefault)

	g.Eventually(a.getAnalyzeCalls).Should(ConsistOf(sDefault))

	g.Eventually(u.getMessages()).Should(HaveLen(2))
	g.Eventually(u.getMessages()[0].Resource).Should(Equal(r2))
	g.Eventually(u.getMessages()[1].Resource).Should(Equal(r1))
}

func TestAnalyzeSuppressesMessages(t *testing.T) {
	g := NewGomegaWithT(t)

	u := &updaterMock{waitTimeout: 1 * time.Second}
	r1 := &resource.Instance{
		Origin: &rt.Origin{
			Collection: basicmeta.K8SCollection1.Name(),
			FullName:   resource.NewFullName("includedNamespace", "r2"),
			Kind:       "Kind1",
		},
	}
	r2 := &resource.Instance{
		Origin: &rt.Origin{
			Collection: basicmeta.K8SCollection1.Name(),
			FullName:   resource.NewFullName("includedNamespace", "r1"),
			Kind:       "Kind1",
		},
	}

	a := &analyzerMock{
		collectionToAccess: basicmeta.K8SCollection1.Name(),
		resourcesToReport:  []*resource.Instance{r1, r2},
	}
	s := AnalysisSuppression{
		Code:         "IST0001", // InternalError, reported by analyzerMock
		ResourceName: "Kind1 r2.includedNamespace",
	}
	d := NewInMemoryDistributor()

	settings := AnalyzingDistributorSettings{
		StatusUpdater:      u,
		Analyzer:           analysis.Combine("testCombined", a),
		Distributor:        d,
		AnalysisSnapshots:  []string{snapshots.Default},
		TriggerSnapshot:    snapshots.Default,
		CollectionReporter: nil,
		AnalysisNamespaces: []resource.Namespace{"includedNamespace"},
		Suppressions:       []AnalysisSuppression{s},
	}
	ad := NewAnalyzingDistributor(settings)

	sDefault := getTestSnapshot()

	ad.Distribute(snapshots.Default, sDefault)

	g.Eventually(a.getAnalyzeCalls).Should(ConsistOf(sDefault))

	g.Eventually(u.getMessages()).Should(HaveLen(1))
	g.Eventually(u.getMessages()[0].Resource).Should(Equal(r2))
}

func TestAnalyzeSuppressesMessagesWithWildcards(t *testing.T) {
	g := NewGomegaWithT(t)

	u := &updaterMock{waitTimeout: 1 * time.Second}
	// r1 and r2 have the same prefix, but r3 does not
	r1 := &resource.Instance{
		Origin: &rt.Origin{
			Collection: basicmeta.K8SCollection1.Name(),
			FullName:   resource.NewFullName("includedNamespace", "r2"),
			Kind:       "Kind1",
		},
	}
	r2 := &resource.Instance{
		Origin: &rt.Origin{
			Collection: basicmeta.K8SCollection1.Name(),
			FullName:   resource.NewFullName("includedNamespace", "r1"),
			Kind:       "Kind1",
		},
	}
	r3 := &resource.Instance{
		Origin: &rt.Origin{
			Collection: basicmeta.K8SCollection1.Name(),
			FullName:   resource.NewFullName("includedNamespace", "x1"),
			Kind:       "Kind1",
		},
	}
	a := &analyzerMock{
		collectionToAccess: basicmeta.K8SCollection1.Name(),
		resourcesToReport:  []*resource.Instance{r1, r2, r3},
	}
	s := AnalysisSuppression{
		Code:         "IST0001",                    // InternalError, reported by analyzerMock
		ResourceName: "Kind1 r*.includedNamespace", // should catch r1/r2 but not x1
	}
	d := NewInMemoryDistributor()

	settings := AnalyzingDistributorSettings{
		StatusUpdater:      u,
		Analyzer:           analysis.Combine("testCombined", a),
		Distributor:        d,
		AnalysisSnapshots:  []string{snapshots.Default},
		TriggerSnapshot:    snapshots.Default,
		CollectionReporter: nil,
		AnalysisNamespaces: []resource.Namespace{"includedNamespace"},
		Suppressions:       []AnalysisSuppression{s},
	}
	ad := NewAnalyzingDistributor(settings)

	sDefault := getTestSnapshot()

	ad.Distribute(snapshots.Default, sDefault)

	g.Eventually(a.getAnalyzeCalls).Should(ConsistOf(sDefault))

	g.Eventually(u.getMessages()).Should(HaveLen(1))
	g.Eventually(u.getMessages()[0].Resource).Should(Equal(r3))
}

func TestAnalyzeSuppressesMessagesWhenResourceIsAnnotated(t *testing.T) {
	// AnalyzerMock always throws IST0001.
	tests := map[string]struct {
		annotations  map[string]string
		wantSuppress bool
	}{
		"basic match": {
			annotations: map[string]string{
				"galley.istio.io/analyze-suppress": "IST0001",
			},
			wantSuppress: true,
		},
		"non-matching code": {
			annotations: map[string]string{
				"galley.istio.io/analyze-suppress": "IST0234",
			},
			wantSuppress: false,
		},
		"code matches inside list of codes": {
			annotations: map[string]string{
				"galley.istio.io/analyze-suppress": "IST123,IST0101,IST0001,BEEF1",
			},
			wantSuppress: true,
		},
		"invalid suppression format does not suppress": {
			annotations: map[string]string{
				"galley.istio.io/analyze-suppress": ",,some text, in here1!,",
			},
			wantSuppress: false,
		},
		"wrong annotation does not suppress": {
			annotations: map[string]string{
				"galley.istio.io/plz-suppress": "IST0001",
			},
			wantSuppress: false,
		},
		"wildcard matches": {
			annotations: map[string]string{
				"galley.istio.io/analyze-suppress": "*",
			},
			wantSuppress: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			g := NewGomegaWithT(t)
			u := &updaterMock{waitTimeout: 1 * time.Second}
			r := &resource.Instance{
				Metadata: resource.Metadata{
					Annotations: tc.annotations,
				},
				Origin: &rt.Origin{
					Collection: basicmeta.K8SCollection1.Name(),
					Kind:       "foobar",
					FullName:   resource.NewFullName("includedNamespace", "r1"),
				},
			}
			a := &analyzerMock{
				collectionToAccess: basicmeta.K8SCollection1.Name(),
				resourcesToReport:  []*resource.Instance{r},
			}
			d := NewInMemoryDistributor()
			settings := AnalyzingDistributorSettings{
				StatusUpdater:      u,
				Analyzer:           analysis.Combine("testCombined", a),
				Distributor:        d,
				AnalysisSnapshots:  []string{snapshots.Default},
				TriggerSnapshot:    snapshots.Default,
				CollectionReporter: nil,
				AnalysisNamespaces: []resource.Namespace{"includedNamespace"},
			}
			ad := NewAnalyzingDistributor(settings)
			sDefault := getTestSnapshot()

			ad.Distribute(snapshots.Default, sDefault)

			g.Eventually(a.getAnalyzeCalls).Should(ConsistOf(sDefault))
			if tc.wantSuppress {
				g.Eventually(u.getMessages()).Should(HaveLen(0))
			} else {
				g.Eventually(u.getMessages()).Should(HaveLen(1))
				g.Eventually(u.getMessages()[0].Resource).Should(Equal(r))
			}
		})
	}

}

func getTestSnapshot(schemas ...collection.Schema) *Snapshot {
	c := make([]*coll.Instance, 0)
	for _, s := range schemas {
		c = append(c, coll.New(s))
	}
	return &Snapshot{
		set: coll.NewSetFromCollections(c),
	}
}

func newSchema(name string) collection.Schema {
	return collection.Builder{
		Name: name,
		Resource: resource2.Builder{
			Kind:         name,
			Plural:       name + "s",
			ProtoPackage: "github.com/gogo/protobuf/types",
			Proto:        "google.protobuf.Empty",
		}.MustBuild(),
	}.MustBuild()
}

var _ resource.Origin = fakeOrigin{}
var _ resource.Reference = fakeReference{}

type fakeOrigin struct {
	namespace    resource.Namespace
	friendlyName string
	reference    fakeReference
}

func (f fakeOrigin) Namespace() resource.Namespace { return f.namespace }
func (f fakeOrigin) FriendlyName() string          { return f.friendlyName }
func (f fakeOrigin) Reference() resource.Reference { return f.reference }

type fakeReference struct {
	name string
}

func (r fakeReference) String() string {
	return r.name
}
