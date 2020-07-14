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

package status

import (
	"sync"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/source/kube/rt"
	"istio.io/istio/galley/pkg/config/testing/basicmeta"
	"istio.io/istio/galley/pkg/config/testing/data"
	"istio.io/istio/pkg/config/resource"
)

func TestState_SetLastKnown_NoEntry(t *testing.T) {
	g := NewGomegaWithT(t)

	s := newState()
	s.applyMessages(NewMessageSet()) // start reconciliation
	s.setObserved(basicmeta.K8SCollection1.Name(), data.EntryN1I1V1.Metadata.FullName, data.EntryN1I1V1.Metadata.Version, "foo")

	g.Expect(s.hasWork()).To(BeTrue())

	st, ok := s.dequeueWork()
	g.Expect(ok).To(BeTrue())
	g.Expect(st.key.col).To(Equal(basicmeta.K8SCollection1.Name()))
	g.Expect(st.key.res).To(Equal(data.EntryN1I1V1.Metadata.FullName))
	g.Expect(st.observedStatus).To(Equal("foo"))
	g.Expect(st.observedVersion).To(Equal(data.EntryN1I1V1.Metadata.Version))
	g.Expect(st.desiredStatus).To(BeNil())
	g.Expect(st.desiredStatusVersion).To(Equal(resource.Version("")))

	g.Expect(s.hasWork()).To(BeFalse())
}

func TestState_SetLastKnown_NoReconciliation(t *testing.T) {
	g := NewGomegaWithT(t)

	s := newState()
	s.setObserved(basicmeta.K8SCollection1.Name(), data.EntryN1I1V1.Metadata.FullName, data.EntryN1I1V1.Metadata.Version, "foo")

	g.Expect(s.hasWork()).To(BeFalse())
}

func TestState_SetLastKnown_TwoEntries(t *testing.T) {
	g := NewGomegaWithT(t)

	s := newState()
	s.applyMessages(NewMessageSet()) // start reconciliation
	s.setObserved(basicmeta.K8SCollection1.Name(), data.EntryN1I1V1.Metadata.FullName, data.EntryN1I1V1.Metadata.Version, "foo")
	s.setObserved(basicmeta.Collection2.Name(), data.EntryN2I2V1.Metadata.FullName, data.EntryN2I2V1.Metadata.Version, "bar")

	g.Expect(s.hasWork()).To(BeTrue())
	st, ok := s.dequeueWork()
	g.Expect(ok).To(BeTrue())
	g.Expect(st.key.col).To(Equal(basicmeta.K8SCollection1.Name()))
	g.Expect(st.key.res).To(Equal(data.EntryN1I1V1.Metadata.FullName))
	g.Expect(st.observedStatus).To(Equal("foo"))
	g.Expect(st.observedVersion).To(Equal(data.EntryN1I1V1.Metadata.Version))
	g.Expect(st.desiredStatus).To(BeNil())
	g.Expect(st.desiredStatusVersion).To(Equal(resource.Version("")))

	g.Expect(s.hasWork()).To(BeTrue())
	st, ok = s.dequeueWork()
	g.Expect(ok).To(BeTrue())
	g.Expect(st.key.col).To(Equal(basicmeta.Collection2.Name()))
	g.Expect(st.key.res).To(Equal(data.EntryN2I2V1.Metadata.FullName))
	g.Expect(st.observedStatus).To(Equal("bar"))
	g.Expect(st.observedVersion).To(Equal(data.EntryN2I2V1.Metadata.Version))
	g.Expect(st.desiredStatus).To(BeNil())
	g.Expect(st.desiredStatusVersion).To(Equal(resource.Version("")))

	g.Expect(s.hasWork()).To(BeFalse())
}

func TestState_SetLastKnown_ExistingEntry(t *testing.T) {
	g := NewGomegaWithT(t)

	s := newState()
	s.applyMessages(NewMessageSet()) // start reconciliation
	s.setObserved(basicmeta.K8SCollection1.Name(), data.EntryN1I1V1.Metadata.FullName, data.EntryN1I1V1.Metadata.Version, "foo")
	s.setObserved(basicmeta.K8SCollection1.Name(), data.EntryN1I1V2.Metadata.FullName, data.EntryN1I1V2.Metadata.Version, "bar")

	g.Expect(s.hasWork()).To(BeTrue())

	st, ok := s.dequeueWork()
	g.Expect(ok).To(BeTrue())
	g.Expect(st.key.col).To(Equal(basicmeta.K8SCollection1.Name()))
	g.Expect(st.key.res).To(Equal(data.EntryN1I1V2.Metadata.FullName))
	g.Expect(st.observedStatus).To(Equal("bar"))
	g.Expect(st.observedVersion).To(Equal(data.EntryN1I1V2.Metadata.Version))
	g.Expect(st.desiredStatus).To(BeNil())
	g.Expect(st.desiredStatusVersion).To(Equal(resource.Version("")))

	g.Expect(s.hasWork()).To(BeFalse())
}

func TestState_ClearLastKnown_NoEntry(t *testing.T) {
	g := NewGomegaWithT(t)

	s := newState()
	s.applyMessages(NewMessageSet()) // start reconciliation

	g.Expect(s.hasWork()).To(BeFalse())

	s.setObserved(basicmeta.K8SCollection1.Name(), data.EntryN1I1V2.Metadata.FullName, data.EntryN1I1V2.Metadata.Version, nil)

	g.Expect(s.hasWork()).To(BeFalse())
}

func TestState_ClearLastKnown_ExistingEntry(t *testing.T) {
	g := NewGomegaWithT(t)

	s := newState()
	s.applyMessages(NewMessageSet()) // start reconciliation
	s.setObserved(basicmeta.K8SCollection1.Name(), data.EntryN1I1V1.Metadata.FullName, data.EntryN1I1V1.Metadata.Version, "foo")
	s.setObserved(basicmeta.K8SCollection1.Name(), data.EntryN1I1V2.Metadata.FullName, data.EntryN1I1V2.Metadata.Version, nil)

	// Even though we reverted to the original state before dequeueing, it is still in the queue. Let the dequeueWork()
	// call deal with this.
	g.Expect(s.hasWork()).To(BeTrue())

	// Add work for another collection, so that dequeueWork would complete.
	s.setObserved(basicmeta.Collection2.Name(), data.EntryN1I1V2.Metadata.FullName, data.EntryN1I1V2.Metadata.Version, "zzz")

	// Simulate removal of work.
	_, ok := s.dequeueWork()
	g.Expect(ok).To(BeTrue())

	s.setObserved(basicmeta.K8SCollection1.Name(), data.EntryN1I1V1.Metadata.FullName, data.EntryN1I1V1.Metadata.Version, nil)

	g.Expect(s.hasWork()).To(BeFalse())
}

func TestState_Quiesce_PendingWork(t *testing.T) {
	g := NewGomegaWithT(t)

	s := newState()
	s.applyMessages(NewMessageSet()) // start reconciliation
	s.setObserved(basicmeta.K8SCollection1.Name(), data.EntryN1I1V1.Metadata.FullName, data.EntryN1I1V1.Metadata.Version, "foo")
	s.setObserved(basicmeta.K8SCollection1.Name(), data.EntryN1I1V2.Metadata.FullName, data.EntryN1I1V2.Metadata.Version, "bar")

	s.quiesceWork()

	_, ok := s.dequeueWork()
	g.Expect(ok).To(BeFalse())
}

func TestState_Quiesce_WaitingForWork(t *testing.T) {
	g := NewGomegaWithT(t)

	s := newState()

	var ok bool
	var wg1, wg2 sync.WaitGroup
	wg1.Add(1)
	wg2.Add(1)
	go func() {
		wg1.Done()
		_, ok = s.dequeueWork()
		wg2.Done()
	}()

	wg1.Wait()
	time.Sleep(time.Second)
	s.quiesceWork()

	wg2.Wait()
	g.Expect(ok).To(BeFalse())
}

func TestState_ApplyMessages_New(t *testing.T) {
	g := NewGomegaWithT(t)

	s := newState()

	res := *data.EntryN1I1V1
	res.Origin = &rt.Origin{
		Collection: basicmeta.K8SCollection1.Name(),
		Kind:       "k1",
		FullName:   res.Metadata.FullName,
		Version:    res.Metadata.Version,
	}

	ms := msg.NewInternalError(&res, "t")
	msgs := NewMessageSet()
	msgs.Add(res.Origin.(*rt.Origin), ms)
	s.applyMessages(msgs)

	g.Expect(s.hasWork()).To(BeTrue())
	st, ok := s.dequeueWork()
	g.Expect(ok).To(BeTrue())
	g.Expect(st.key.col).To(Equal(basicmeta.K8SCollection1.Name()))
	g.Expect(st.key.res).To(Equal(res.Metadata.FullName))
	g.Expect(st.observedStatus).To(BeNil())
	g.Expect(st.observedVersion).To(Equal(resource.Version("")))
	g.Expect(st.desiredStatus).To(Equal(toStatusValue(diag.Messages{ms})))
	g.Expect(st.desiredStatusVersion).To(Equal(res.Metadata.Version))
}

func TestState_ApplyMessages_AgainstExistingUnappliedState(t *testing.T) {
	g := NewGomegaWithT(t)

	s := newState()
	s.applyMessages(NewMessageSet()) // start reconciliation
	s.setObserved(basicmeta.K8SCollection1.Name(), data.EntryN1I1V1.Metadata.FullName, data.EntryN1I1V2.Metadata.Version, "foo")

	_, ok := s.dequeueWork()
	g.Expect(ok).To(BeTrue())

	res := *data.EntryN1I1V1
	res.Origin = &rt.Origin{
		Collection: basicmeta.K8SCollection1.Name(),
		Kind:       "k1",
		FullName:   res.Metadata.FullName,
		Version:    res.Metadata.Version,
	}

	ms := msg.NewInternalError(&res, "t")

	msgs := NewMessageSet()
	msgs.Add(res.Origin.(*rt.Origin), ms)
	s.applyMessages(msgs)

	g.Expect(s.hasWork()).To(BeTrue())
	st, ok := s.dequeueWork()
	g.Expect(ok).To(BeTrue())
	g.Expect(st.key.col).To(Equal(basicmeta.K8SCollection1.Name()))
	g.Expect(st.key.res).To(Equal(res.Metadata.FullName))
	g.Expect(st.observedStatus).To(Equal("foo"))
	g.Expect(st.observedVersion).To(Equal(data.EntryN1I1V2.Metadata.Version))
	g.Expect(st.desiredStatus).To(Equal(toStatusValue(diag.Messages{ms})))
	g.Expect(st.desiredStatusVersion).To(Equal(res.Metadata.Version))
}

func TestState_ClearMessages_AgainstAppliedState(t *testing.T) {
	g := NewGomegaWithT(t)

	s := newState()
	s.applyMessages(NewMessageSet()) // start reconciliation
	s.setObserved(basicmeta.K8SCollection1.Name(), data.EntryN1I1V1.Metadata.FullName, data.EntryN1I1V1.Metadata.Version, "foo")

	_, ok := s.dequeueWork()
	g.Expect(ok).To(BeTrue())

	s.applyMessages(NewMessageSet())

	g.Expect(s.hasWork()).To(BeTrue())
	st, ok := s.dequeueWork()
	g.Expect(ok).To(BeTrue())
	g.Expect(st.key.col).To(Equal(basicmeta.K8SCollection1.Name()))
	g.Expect(st.key.res).To(Equal(data.EntryN1I1V1.Metadata.FullName))
	g.Expect(st.observedStatus).To(Equal("foo"))
	g.Expect(st.observedVersion).To(Equal(data.EntryN1I1V1.Metadata.Version))
	g.Expect(st.desiredStatus).To(BeNil())
	g.Expect(st.desiredStatusVersion).To(Equal(resource.Version("")))
}

func TestState_ClearMessages_AgainstAppliedEmptyState(t *testing.T) {
	g := NewGomegaWithT(t)

	s := newState()
	s.applyMessages(NewMessageSet()) // start reconciliation
	s.setObserved(basicmeta.K8SCollection1.Name(), data.EntryN1I1V1.Metadata.FullName, data.EntryN1I1V1.Metadata.Version, "foo")

	_, ok := s.dequeueWork()
	g.Expect(ok).To(BeTrue())

	s.setObserved(basicmeta.K8SCollection1.Name(), data.EntryN1I1V1.Metadata.FullName, data.EntryN1I1V1.Metadata.Version, nil)

	s.applyMessages(NewMessageSet())

	g.Expect(s.hasWork()).To(BeFalse())
}
