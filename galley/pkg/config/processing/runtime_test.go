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

package processing_test

import (
	"testing"

	"github.com/gogo/protobuf/types"
	. "github.com/onsi/gomega"

	"istio.io/pkg/log"

	"istio.io/api/mesh/v1alpha1"

	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/meshcfg"
	"istio.io/istio/galley/pkg/config/processing"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/source/kube/inmemory"
	"istio.io/istio/galley/pkg/config/testing/basicmeta"
	"istio.io/istio/galley/pkg/config/testing/fixtures"
)

var scope = log.RegisterScope("processing", "", 0)

func init() {
	scope.SetOutputLevel(log.DebugLevel)
}

func TestRuntime_Startup_NoMeshConfig(t *testing.T) {
	g := NewGomegaWithT(t)

	f := newFixture()
	f.rt.Start()
	defer f.rt.Stop()

	coll := basicmeta.Collection1
	r := &resource.Entry{
		Metadata: resource.Metadata{},
		Item:     &types.Empty{},
	}
	f.src.Get(coll).Set(r)

	g.Consistently(f.p.acc.Events).Should(HaveLen(0))
	g.Consistently(f.p.HasStarted).Should(BeFalse())
}

func TestRuntime_Startup_MeshConfig_Arrives_No_Resources(t *testing.T) {
	g := NewGomegaWithT(t)

	f := newFixture()
	f.rt.Start()
	defer f.rt.Stop()

	f.meshsrc.Set(meshcfg.Default())

	g.Eventually(f.p.acc.Events).Should(HaveLen(3))
	g.Eventually(f.p.acc.Events).Should(ConsistOf(
		event.FullSyncFor(basicmeta.Collection1),
		event.FullSyncFor(meshcfg.IstioMeshconfig),
		event.AddFor(meshcfg.IstioMeshconfig, meshConfigEntry(meshcfg.Default())),
	))
	g.Eventually(f.p.HasStarted).Should(BeTrue())
}

func TestRuntime_Startup_MeshConfig_Arrives(t *testing.T) {
	g := NewGomegaWithT(t)

	f := newFixture()
	f.rt.Start()
	defer f.rt.Stop()

	coll := basicmeta.Collection1
	r := &resource.Entry{
		Metadata: resource.Metadata{},
		Item:     &types.Empty{},
	}
	f.src.Get(coll).Set(r)

	f.meshsrc.Set(meshcfg.Default())
	g.Eventually(f.p.acc.Events).Should(HaveLen(4))
	g.Eventually(f.p.acc.Events).Should(ConsistOf(
		event.AddFor(basicmeta.Collection1, r),
		event.FullSyncFor(basicmeta.Collection1),
		event.FullSyncFor(meshcfg.IstioMeshconfig),
		event.AddFor(meshcfg.IstioMeshconfig, meshConfigEntry(meshcfg.Default())),
	))

	g.Eventually(f.p.HasStarted).Should(BeTrue())
}

func TestRuntime_Startup_Stop(t *testing.T) {
	g := NewGomegaWithT(t)

	f := newFixture()
	f.rt.Start()

	coll := basicmeta.Collection1
	r := &resource.Entry{
		Metadata: resource.Metadata{},
		Item:     &types.Empty{},
	}
	f.src.Get(coll).Set(r)

	f.meshsrc.Set(meshcfg.Default())

	g.Eventually(f.p.acc.Events).Should(HaveLen(4))
	g.Eventually(f.p.HasStarted).Should(BeTrue())
	f.rt.Stop()
	g.Eventually(f.p.HasStarted).Should(BeFalse())
}

func TestRuntime_Start_Start_Stop(t *testing.T) {
	g := NewGomegaWithT(t)

	f := newFixture()
	f.rt.Start()
	f.rt.Start() // Double start

	coll := basicmeta.Collection1
	r := &resource.Entry{
		Metadata: resource.Metadata{},
		Item:     &types.Empty{},
	}
	f.src.Get(coll).Set(r)

	f.meshsrc.Set(meshcfg.Default())
	g.Eventually(f.p.acc.Events).Should(HaveLen(4))
	g.Eventually(f.p.HasStarted).Should(BeTrue())
	f.rt.Stop()
	g.Eventually(f.p.HasStarted).Should(BeFalse())
}

func TestRuntime_Start_Stop_Stop(t *testing.T) {
	g := NewGomegaWithT(t)

	f := newFixture()
	f.rt.Start()

	coll := basicmeta.Collection1
	r := &resource.Entry{
		Metadata: resource.Metadata{},
		Item:     &types.Empty{},
	}
	f.src.Get(coll).Set(r)

	f.meshsrc.Set(meshcfg.Default())

	g.Eventually(f.p.acc.Events).Should(HaveLen(4))
	g.Eventually(f.p.HasStarted).Should(BeTrue())
	f.rt.Stop()
	f.rt.Stop()
	g.Eventually(f.p.HasStarted).Should(BeFalse())
}

func TestRuntime_MeshConfig_Causing_Restart(t *testing.T) {
	g := NewGomegaWithT(t)

	f := newFixture()
	f.rt.Start()
	defer f.rt.Stop()

	coll := basicmeta.Collection1
	r := &resource.Entry{
		Metadata: resource.Metadata{},
		Item:     &types.Empty{},
	}
	f.src.Get(coll).Set(r)

	f.meshsrc.Set(meshcfg.Default())
	g.Eventually(f.p.acc.Events).Should(ConsistOf(
		event.AddFor(meshcfg.IstioMeshconfig, &resource.Entry{
			Metadata: resource.Metadata{
				Name: meshcfg.ResourceName,
			},
			Item: meshcfg.Default(),
		}),
		event.FullSyncFor(meshcfg.IstioMeshconfig),
		event.AddFor(coll, r),
		event.FullSyncFor(coll),
	))

	oldSessionID := f.rt.CurrentSessionID()

	f.p.acc.Clear()

	mcfg := meshcfg.Default()
	mcfg.IngressClass = "ing"

	f.meshsrc.Set(mcfg)
	g.Eventually(f.p.acc.Events).Should(HaveLen(4))

	g.Eventually(f.rt.CurrentSessionID).Should(Equal(oldSessionID + 1))
}

func TestRuntime_Event_Before_Start(t *testing.T) {
	g := NewGomegaWithT(t)

	f := newFixture()

	coll := basicmeta.Collection1
	r := &resource.Entry{
		Metadata: resource.Metadata{},
		Item:     &types.Empty{},
	}
	f.src.Start()
	f.src.Get(coll).Set(r)

	g.Consistently(f.p.acc.Events).Should(HaveLen(0))
}

type fixture struct {
	meshsrc *meshcfg.InMemorySource
	src     *inmemory.KubeSource
	p       *testProcessor
	rt      *processing.Runtime
}

func newFixture() *fixture {
	f := &fixture{
		meshsrc: meshcfg.NewInmemory(),
		src:     inmemory.NewKubeSource(basicmeta.MustGet().KubeSource().Resources()),
		p:       &testProcessor{},
	}

	o := processing.RuntimeOptions{
		DomainSuffix: "local.svc",
		Sources:      []event.Source{f.meshsrc, f.src},
		Processor:    f.p,
	}

	f.rt = processing.NewRuntime(o)
	return f
}

type testProcessor struct {
	acc     fixtures.Accumulator
	started bool
}

func (t *testProcessor) Handle(e event.Event) {
	t.acc.Handle(e)
}

func (t *testProcessor) Start(_ interface{}) {
	t.started = true
}

func (t *testProcessor) Stop() {
	t.started = false
}

func (t *testProcessor) HasStarted() bool {
	return t.started
}

func meshConfigEntry(m *v1alpha1.MeshConfig) *resource.Entry { // nolint:interfacer
	return &resource.Entry{
		Metadata: resource.Metadata{
			Name: resource.NewName("istio-system", "meshconfig"),
		},
		Item: m,
	}
}
