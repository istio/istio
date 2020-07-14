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

package processing

import (
	"sync"
	"testing"

	"github.com/gogo/protobuf/types"
	. "github.com/onsi/gomega"

	"istio.io/api/mesh/v1alpha1"
	"istio.io/pkg/log"

	"istio.io/istio/galley/pkg/config/mesh"
	"istio.io/istio/galley/pkg/config/scope"
	"istio.io/istio/galley/pkg/config/source/kube/inmemory"
	"istio.io/istio/galley/pkg/config/source/kube/rt"
	"istio.io/istio/galley/pkg/config/testing/basicmeta"
	"istio.io/istio/galley/pkg/config/testing/fixtures"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collections"
)

func init() {
	scope.Processing.SetOutputLevel(log.DebugLevel)
}

func TestRuntime_Startup_NoMeshConfig(t *testing.T) {
	g := NewGomegaWithT(t)

	f := initFixture()
	f.rt.Start()
	defer f.rt.Stop()

	coll := basicmeta.K8SCollection1
	r := &resource.Instance{
		Metadata: resource.Metadata{},
		Message:  &types.Empty{},
	}
	f.src.Get(coll.Name()).Set(r)

	g.Consistently(f.p.acc.Events).Should(HaveLen(0))
	g.Consistently(f.p.HasStarted).Should(BeFalse())
}

func TestRuntime_Startup_MeshConfig_Arrives_No_Resources(t *testing.T) {
	g := NewGomegaWithT(t)

	f := initFixture()
	f.rt.Start()
	defer f.rt.Stop()

	f.meshsrc.Set(mesh.DefaultMeshConfig())

	g.Eventually(f.p.acc.Events).Should(HaveLen(3))
	fixtures.ExpectEventsEventually(t, &f.p.acc,
		event.FullSyncFor(basicmeta.K8SCollection1),
		event.FullSyncFor(collections.IstioMeshV1Alpha1MeshConfig),
		event.AddFor(collections.IstioMeshV1Alpha1MeshConfig, meshConfigEntry(mesh.DefaultMeshConfig())))
	g.Eventually(f.p.HasStarted).Should(BeTrue())
}

func TestRuntime_Startup_MeshConfig_Arrives(t *testing.T) {
	g := NewGomegaWithT(t)

	f := initFixture()
	f.rt.Start()
	defer f.rt.Stop()

	coll := basicmeta.K8SCollection1
	r := &resource.Instance{
		Metadata: resource.Metadata{},
		Message:  &types.Empty{},
	}
	f.src.Get(coll.Name()).Set(r)

	f.meshsrc.Set(mesh.DefaultMeshConfig())
	g.Eventually(f.p.acc.Events).Should(HaveLen(4))
	fixtures.ExpectEventsEventually(t, &f.p.acc,
		event.AddFor(basicmeta.K8SCollection1, r),
		event.FullSyncFor(basicmeta.K8SCollection1),
		event.FullSyncFor(collections.IstioMeshV1Alpha1MeshConfig),
		event.AddFor(collections.IstioMeshV1Alpha1MeshConfig, meshConfigEntry(mesh.DefaultMeshConfig())),
	)

	g.Eventually(f.p.HasStarted).Should(BeTrue())
}

func TestRuntime_Startup_Stop(t *testing.T) {
	g := NewGomegaWithT(t)

	f := initFixture()
	f.rt.Start()

	coll := basicmeta.K8SCollection1
	r := &resource.Instance{
		Metadata: resource.Metadata{},
		Message:  &types.Empty{},
	}
	f.src.Get(coll.Name()).Set(r)

	f.meshsrc.Set(mesh.DefaultMeshConfig())

	g.Eventually(f.p.acc.Events).Should(HaveLen(4))
	g.Eventually(f.p.HasStarted).Should(BeTrue())
	f.rt.Stop()
	g.Eventually(f.p.HasStarted).Should(BeFalse())
}

func TestRuntime_Start_Start_Stop(t *testing.T) {
	g := NewGomegaWithT(t)

	f := initFixture()
	f.rt.Start()
	f.rt.Start() // Double start

	coll := basicmeta.K8SCollection1
	r := &resource.Instance{
		Metadata: resource.Metadata{},
		Message:  &types.Empty{},
	}
	f.src.Get(coll.Name()).Set(r)

	f.meshsrc.Set(mesh.DefaultMeshConfig())
	g.Eventually(f.p.acc.Events).Should(HaveLen(4))
	g.Eventually(f.p.HasStarted).Should(BeTrue())
	f.rt.Stop()
	g.Eventually(f.p.HasStarted).Should(BeFalse())
}

func TestRuntime_Start_Stop_Stop(t *testing.T) {
	g := NewGomegaWithT(t)

	f := initFixture()
	f.rt.Start()

	coll := basicmeta.K8SCollection1
	r := &resource.Instance{
		Metadata: resource.Metadata{},
		Message:  &types.Empty{},
	}
	f.src.Get(coll.Name()).Set(r)

	f.meshsrc.Set(mesh.DefaultMeshConfig())

	g.Eventually(f.p.acc.Events).Should(HaveLen(4))
	g.Eventually(f.p.HasStarted).Should(BeTrue())
	f.rt.Stop()
	f.rt.Stop()
	g.Eventually(f.p.HasStarted).Should(BeFalse())
}

func TestRuntime_MeshConfig_Causing_Restart(t *testing.T) {
	g := NewGomegaWithT(t)

	f := initFixture()
	f.rt.Start()
	defer f.rt.Stop()

	coll := basicmeta.K8SCollection1
	r := &resource.Instance{
		Metadata: resource.Metadata{},
		Message:  &types.Empty{},
	}
	f.src.Get(coll.Name()).Set(r)

	f.meshsrc.Set(mesh.DefaultMeshConfig())
	fixtures.ExpectEventsEventually(t, &f.p.acc,
		event.AddFor(collections.IstioMeshV1Alpha1MeshConfig, &resource.Instance{
			Metadata: resource.Metadata{
				FullName: mesh.MeshConfigResourceName,
				Schema:   collections.IstioMeshV1Alpha1MeshConfig.Resource(),
			},
			Message: mesh.DefaultMeshConfig(),
			Origin: &rt.Origin{
				Collection: collections.IstioMeshV1Alpha1MeshConfig.Name(),
				Kind:       "MeshConfig",
				FullName:   resource.NewFullName("istio-system", "meshconfig"),
			},
		}),
		event.FullSyncFor(collections.IstioMeshV1Alpha1MeshConfig),
		event.AddFor(coll, r),
		event.FullSyncFor(coll),
	)

	oldSessionID := f.rt.currentSessionID()

	f.p.acc.Clear()

	mcfg := mesh.DefaultMeshConfig()
	mcfg.IngressClass = "ing"

	f.meshsrc.Set(mcfg)
	g.Eventually(f.rt.currentSessionID).Should(Equal(oldSessionID + 1))
	g.Eventually(f.p.acc.Events).Should(HaveLen(4))
}

func TestRuntime_Event_Before_Start(t *testing.T) {
	g := NewGomegaWithT(t)

	f := initFixture()

	coll := basicmeta.K8SCollection1
	r := &resource.Instance{
		Metadata: resource.Metadata{},
		Message:  &types.Empty{},
	}
	f.src.Start()
	f.src.Get(coll.Name()).Set(r)

	g.Consistently(f.p.acc.Events).Should(HaveLen(0))
}

func TestRuntime_Stop_WhileStarting(t *testing.T) {
	g := NewGomegaWithT(t)

	f := newFixture()
	f.meshsrc = nil
	f.src = nil
	f.init()

	// Wait until mockSrc.Start is called, but block it from completing.
	f.mockSrc.blockStart()

	f.rt.Start()

	g.Eventually(f.rt.currentSessionState).Should(Equal(starting))
	g.Eventually(f.mockSrc.hasStarted).Should(BeTrue())

	go f.rt.Stop()

	g.Eventually(f.rt.currentSessionState).Should(Equal(terminating))

	// release Start call. Things should cleanup and release.
	f.mockSrc.releaseStart()

	// Once Stop returns, both started and stopped should be
	g.Eventually(f.mockSrc.hasStopped).Should(BeTrue())
}

func TestRuntime_Reset_WhileStarting(t *testing.T) {
	g := NewGomegaWithT(t)

	f := newFixture()
	f.meshsrc = nil
	f.src = nil
	f.init()

	// Wait until mockSrc.Start is called, but block it from completing.
	f.mockSrc.blockStart()

	f.rt.Start()

	g.Eventually(f.rt.currentSessionState).Should(Equal(starting))
	g.Eventually(f.mockSrc.hasStarted).Should(BeTrue())

	oldSessionID := f.rt.currentSessionID()

	f.mockSrc.h.Handle(event.Event{Kind: event.Reset})

	f.mockSrc.releaseStart()

	g.Eventually(f.rt.currentSessionID).Should(Equal(oldSessionID + 1))

	g.Eventually(f.rt.currentSessionState).Should(Equal(buffering))
	g.Consistently(f.p.acc.Events).Should(BeEmpty())

	f.rt.Stop()
}

func TestRuntime_MeshEvent_WhileBuffering(t *testing.T) {
	g := NewGomegaWithT(t)

	f := newFixture()
	f.meshsrc = nil
	f.src = nil
	f.init()

	f.rt.Start()
	g.Eventually(f.rt.currentSessionState).Should(Equal(buffering))

	f.mockSrc.h.Handle(event.DeleteFor(collections.IstioMeshV1Alpha1MeshConfig, mesh.MeshConfigResourceName, "vxx"))

	g.Consistently(f.rt.currentSessionState).Should(Equal(buffering))

	f.mockSrc.h.Handle(event.FullSyncFor(collections.IstioMeshV1Alpha1MeshConfig))

	g.Eventually(f.rt.currentSessionState).Should(Equal(processing))

	f.rt.Stop()
}

func TestRuntime_MeshEvent_WhileRunning(t *testing.T) {
	g := NewGomegaWithT(t)

	f := initFixture()
	f.rt.Start()
	defer f.rt.Stop()

	f.meshsrc.Set(mesh.DefaultMeshConfig())
	fixtures.ExpectEventsEventually(t, &f.p.acc,
		event.FullSyncFor(basicmeta.K8SCollection1),
		event.FullSyncFor(collections.IstioMeshV1Alpha1MeshConfig),
		event.AddFor(collections.IstioMeshV1Alpha1MeshConfig, meshConfigEntry(mesh.DefaultMeshConfig())),
	)

	oldSessionID := f.rt.currentSessionID()
	f.p.acc.Clear()

	// Send a mesh event out-of-band
	f.mockSrc.h.Handle(event.DeleteFor(collections.IstioMeshV1Alpha1MeshConfig, mesh.MeshConfigResourceName, "vxx"))

	g.Eventually(f.rt.currentSessionID).Should(Equal(oldSessionID + 1))
	fixtures.ExpectEventsEventually(t, &f.p.acc,
		event.FullSyncFor(basicmeta.K8SCollection1),
		event.FullSyncFor(collections.IstioMeshV1Alpha1MeshConfig),
		event.AddFor(collections.IstioMeshV1Alpha1MeshConfig, meshConfigEntry(mesh.DefaultMeshConfig())))

	g.Eventually(f.p.HasStarted).Should(BeTrue())
}

type fixture struct {
	meshsrc *mesh.InMemorySource
	src     *inmemory.KubeSource
	mockSrc *testSource
	p       *testProcessor
	rt      *Runtime
}

func newFixture() *fixture {
	p := &testProcessor{}
	f := &fixture{
		meshsrc: mesh.NewInmemoryMeshCfg(),
		src:     inmemory.NewKubeSource(basicmeta.MustGet().KubeCollections()),
		mockSrc: &testSource{},
		p:       p,
	}

	f.mockSrc.startCalled = sync.NewCond(&f.mockSrc.mu)
	return f
}

func initFixture() *fixture {
	f := newFixture()
	f.init()
	return f
}

func (f *fixture) init() {
	var srcs []event.Source

	if f.meshsrc != nil {
		srcs = append(srcs, f.meshsrc)
	}
	if f.src != nil {
		srcs = append(srcs, f.src)
	}
	if f.mockSrc != nil {
		srcs = append(srcs, f.mockSrc)
	}

	o := RuntimeOptions{
		DomainSuffix:      "local.svc",
		Source:            event.CombineSources(srcs...),
		ProcessorProvider: func(_ ProcessorOptions) event.Processor { return f.p },
	}

	f.rt = NewRuntime(o)
}

type testSource struct {
	mu          sync.Mutex
	h           event.Handler
	startCalled *sync.Cond
	startWG     sync.WaitGroup
	started     bool
	stopped     bool
}

var _ event.Source = &testSource{}

func (s *testSource) Dispatch(handler event.Handler) {
	s.h = event.CombineHandlers(s.h, handler)
}

func (s *testSource) Start() {
	s.mu.Lock()
	s.startCalled.Broadcast()
	s.started = true
	s.mu.Unlock()
	s.startWG.Wait()
}

func (s *testSource) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
}

func (s *testSource) blockStart() {
	s.startWG.Add(1)
}

func (s *testSource) releaseStart() {
	s.startWG.Done()
}

func (s *testSource) hasStarted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started
}

func (s *testSource) hasStopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

type testProcessor struct {
	acc     fixtures.Accumulator
	started bool
}

func (t *testProcessor) Handle(e event.Event) {
	t.acc.Handle(e)
}

func (t *testProcessor) Start() {
	t.started = true
}

func (t *testProcessor) Stop() {
	t.started = false
}

func (t *testProcessor) HasStarted() bool {
	return t.started
}

func meshConfigEntry(m *v1alpha1.MeshConfig) *resource.Instance { // nolint:interfacer
	return &resource.Instance{
		Metadata: resource.Metadata{
			FullName: resource.NewFullName("istio-system", "meshconfig"),
			Schema:   collections.IstioMeshV1Alpha1MeshConfig.Resource(),
		},
		Message: m,
		Origin: &rt.Origin{
			Collection: collections.IstioMeshV1Alpha1MeshConfig.Name(),
			Kind:       "MeshConfig",
			FullName:   resource.NewFullName("istio-system", "meshconfig"),
		},
	}
}
