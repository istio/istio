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
package local

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/testing/data"
	"istio.io/istio/galley/pkg/config/testing/fixtures"
)

func TestBasicSingleSource(t *testing.T) {
	g := NewGomegaWithT(t)

	s1 := &fixtures.Source{}

	psi := precedenceSourceInput{src: s1, cols: collection.Names{data.Collection1}}
	ps := newPrecedenceSource([]precedenceSourceInput{psi})

	h := &fixtures.Accumulator{}
	ps.Dispatch(h)

	ps.Start()
	defer ps.Stop()

	e1 := createTestEvent(t, event.Added, createTestResource(t, "ns", "resource1", "v1"))
	e2 := createTestEvent(t, event.FullSync, nil)

	s1.Handle(e1)
	s1.Handle(e2)
	g.Expect(h.Events()).To(Equal([]event.Event{e1, e2}))
}

func TestWaitAndCombineFullSync(t *testing.T) {
	g := NewGomegaWithT(t)

	s1 := &fixtures.Source{}
	s2 := &fixtures.Source{}

	psi1 := precedenceSourceInput{src: s1, cols: collection.Names{data.Collection1, data.Collection2}}
	psi2 := precedenceSourceInput{src: s2, cols: collection.Names{data.Collection1}}

	ps := newPrecedenceSource([]precedenceSourceInput{psi1, psi2})

	h := &fixtures.Accumulator{}
	ps.Dispatch(h)

	ps.Start()
	defer ps.Stop()

	// For collections in more than one source, wait for all sources before publishing fullsync
	e1 := createTestEvent(t, event.FullSync, nil)

	s1.Handle(e1)
	g.Expect(h.Events()).To(BeEmpty())

	s2.Handle(e1)
	g.Expect(h.Events()).To(Equal([]event.Event{e1}))

	// Collection2 is only in one source, so we shouldn't wait for an event from both sources
	e2 := createTestEvent(t, event.FullSync, nil)
	e2.Source = data.Collection2

	s1.Handle(e2)
	g.Expect(h.Events()).To(Equal([]event.Event{e1, e2}))
}

func TestPrecedence(t *testing.T) {
	g := NewGomegaWithT(t)

	s1 := &fixtures.Source{}
	s2 := &fixtures.Source{}
	s3 := &fixtures.Source{}

	psi1 := precedenceSourceInput{src: s1, cols: collection.Names{data.Collection1}}
	psi2 := precedenceSourceInput{src: s2, cols: collection.Names{data.Collection1}}
	psi3 := precedenceSourceInput{src: s3, cols: collection.Names{data.Collection1}}

	ps := newPrecedenceSource([]precedenceSourceInput{psi1, psi2, psi3})

	h := &fixtures.Accumulator{}
	ps.Dispatch(h)

	ps.Start()
	defer ps.Stop()

	e1 := createTestEvent(t, event.Added, createTestResource(t, "ns", "resource1", "v1"))
	e2 := createTestEvent(t, event.Added, createTestResource(t, "ns", "resource1", "v2"))

	s2.Handle(e1)
	g.Expect(h.Events()).To(Equal([]event.Event{e1}))

	// For a lower precedence source, e2 should get ignored
	s1.Handle(e2)
	g.Expect(h.Events()).To(Equal([]event.Event{e1}))

	// For a higher precedence source, e2 should get handled
	s3.Handle(e2)
	g.Expect(h.Events()).To(Equal([]event.Event{e1, e2}))
}
