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
	"istio.io/istio/galley/pkg/config/testing/fixtures"
)

func TestBasicSingleSource(t *testing.T) {
	g := NewGomegaWithT(t)

	s1 := &fixtures.Source{}

	ps := newPrecedenceSource([]event.Source{s1})

	h := &fixtures.Accumulator{}
	ps.Dispatch(h)

	ps.Start()
	defer ps.Stop()

	e1 := createTestEvent(event.Added, createTestResource("ns", "resource1", "v1"))
	e2 := createTestEvent(event.FullSync, nil)

	s1.Handle(e1)
	s1.Handle(e2)
	g.Expect(h.Events()).To(Equal([]event.Event{e1, e2}))
}

func TestWaitAndCombineFullSync(t *testing.T) {
	g := NewGomegaWithT(t)

	s1 := &fixtures.Source{}
	s2 := &fixtures.Source{}

	ps := newPrecedenceSource([]event.Source{s1, s2})

	h := &fixtures.Accumulator{}
	ps.Dispatch(h)

	ps.Start()
	defer ps.Stop()

	e := createTestEvent(event.FullSync, nil)

	s1.Handle(e)
	g.Expect(h.Events()).To(BeEmpty())

	s2.Handle(e)
	g.Expect(h.Events()).To(Equal([]event.Event{e}))
}

func TestPrecedence(t *testing.T) {
	g := NewGomegaWithT(t)

	s1 := &fixtures.Source{}
	s2 := &fixtures.Source{}
	s3 := &fixtures.Source{}

	ps := newPrecedenceSource([]event.Source{s1, s2, s3})

	h := &fixtures.Accumulator{}
	ps.Dispatch(h)

	ps.Start()
	defer ps.Stop()

	e1 := createTestEvent(event.Added, createTestResource("ns", "resource1", "v1"))
	e2 := createTestEvent(event.Added, createTestResource("ns", "resource1", "v2"))

	s2.Handle(e1)
	g.Expect(h.Events()).To(Equal([]event.Event{e1}))

	// For a lower precedence source, e2 should get ignored
	s1.Handle(e2)
	g.Expect(h.Events()).To(Equal([]event.Event{e1}))

	// For a higher precedence source, e2 should get handled
	s3.Handle(e2)
	g.Expect(h.Events()).To(Equal([]event.Event{e1, e2}))
}
