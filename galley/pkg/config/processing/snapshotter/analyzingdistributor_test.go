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
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/schema/collection"
	resource2 "istio.io/istio/galley/pkg/config/schema/resource"
	"istio.io/istio/galley/pkg/config/schema/snapshots"
	"istio.io/istio/galley/pkg/config/source/kube/rt"
	"istio.io/istio/galley/pkg/config/testing/basicmeta"
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
	resourcesToReport  []*resource.Instance
}

// Analyze implements Analyzer
func (a *analyzerMock) Analyze(c analysis.Context) {
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
		AnalysisSnapshots:  []string{snapshots.Default, snapshots.SyntheticServiceEntry},
		TriggerSnapshot:    snapshots.Default,
		CollectionReporter: cr,
		AnalysisNamespaces: []resource.Namespace{"includedNamespace"},
	}
	ad := NewAnalyzingDistributor(settings)

	schemaA := newSchema("a")
	schemaB := newSchema("b")
	schemaC := newSchema("c")
	schemaD := newSchema("d")

	sDefault := getTestSnapshot(schemaA, schemaB)
	sSynthetic := getTestSnapshot(schemaC)
	sOther := getTestSnapshot(schemaA, schemaD)

	ad.Distribute(snapshots.SyntheticServiceEntry, sSynthetic)
	ad.Distribute(snapshots.Default, sDefault)
	ad.Distribute("other", sOther)

	// Assert we sent every received snapshot to the distributor
	g.Eventually(func() snapshot.Snapshot { return d.GetSnapshot(snapshots.SyntheticServiceEntry) }).Should(Equal(sSynthetic))
	g.Eventually(func() snapshot.Snapshot { return d.GetSnapshot(snapshots.Default) }).Should(Equal(sDefault))
	g.Eventually(func() snapshot.Snapshot { return d.GetSnapshot("other") }).Should(Equal(sOther))

	// Assert we triggered analysis only once, with the expected combination of snapshots
	sCombined := getTestSnapshot(schemaA, schemaB, schemaC)
	g.Eventually(func() []*Snapshot { return a.analyzeCalls }).Should(ConsistOf(sCombined))

	// Verify the collection reporter hook was called
	g.Expect(collectionAccessed).To(Equal(a.collectionToAccess))

	// Verify we only reported messages in the AnalysisNamespaces
	g.Expect(u.messages).To(HaveLen(1))
	for _, m := range u.messages {
		g.Expect(m.Origin.Namespace()).To(Equal(resource.Namespace("includedNamespace")))
	}
}

func TestAnalyzeNamespaceMessageHasNoOrigin(t *testing.T) {
	g := NewGomegaWithT(t)

	u := &updaterMock{}
	a := &analyzerMock{
		collectionToAccess: basicmeta.K8SCollection1.Name(),
		resourcesToReport: []*resource.Instance{
			{},
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
	g.Eventually(func() []*Snapshot { return a.analyzeCalls }).Should(Not(BeEmpty()))
	g.Expect(u.messages).To(HaveLen(1))
}

func TestAnalyzeNamespaceMessageHasOriginWithNoNamespace(t *testing.T) {
	g := NewGomegaWithT(t)

	u := &updaterMock{}
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
	g.Eventually(func() []*Snapshot { return a.analyzeCalls }).Should(Not(BeEmpty()))
	g.Expect(u.messages).To(HaveLen(1))
}

func TestAnalyzeSortsMessages(t *testing.T) {
	g := NewGomegaWithT(t)

	u := &updaterMock{}
	o1 := &rt.Origin{
		Collection: basicmeta.K8SCollection1.Name(),
		FullName:   resource.NewFullName("includedNamespace", "r2"),
	}
	o2 := &rt.Origin{
		Collection: basicmeta.K8SCollection1.Name(),
		FullName:   resource.NewFullName("includedNamespace", "r1"),
	}
	a := &analyzerMock{
		collectionToAccess: basicmeta.K8SCollection1.Name(),
		resourcesToReport: []*resource.Instance{
			{Origin: o1},
			{Origin: o2},
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

	g.Eventually(func() []*Snapshot { return a.analyzeCalls }).Should(ConsistOf(sDefault))
	g.Expect(u.messages).To(HaveLen(2))
	g.Expect(u.messages[0].Origin).To(Equal(o2))
	g.Expect(u.messages[1].Origin).To(Equal(o1))
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

type fakeOrigin struct {
	namespace    resource.Namespace
	friendlyName string
}

func (f fakeOrigin) Namespace() resource.Namespace { return f.namespace }
func (f fakeOrigin) FriendlyName() string          { return f.friendlyName }
