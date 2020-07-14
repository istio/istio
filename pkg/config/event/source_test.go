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

package event_test

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/testing/fixtures"
	"istio.io/istio/pkg/config/event"
)

func TestMergeSources_Basic(t *testing.T) {
	g := NewGomegaWithT(t)

	s1 := &fixtures.Source{}
	s2 := &fixtures.Source{}

	s := event.CombineSources(s1, s2)

	h := &fixtures.Accumulator{}
	s.Dispatch(h)

	g.Expect(s1.Handlers).To(Equal(h))
	g.Expect(s2.Handlers).To(Equal(h))

	s.Start()
	g.Expect(s1.Running()).To(BeTrue())
	g.Expect(s2.Running()).To(BeTrue())

	s.Stop()
	g.Expect(s1.Running()).To(BeFalse())
	g.Expect(s2.Running()).To(BeFalse())
}

func TestMergeSources_Composite(t *testing.T) {
	g := NewGomegaWithT(t)

	s1 := &fixtures.Source{}
	s2a := &fixtures.Source{}
	s2b := &fixtures.Source{}
	s2 := event.CombineSources(s2a, s2b)

	s := event.CombineSources(s1, s2)

	h := &fixtures.Accumulator{}
	s.Dispatch(h)

	g.Expect(s1.Handlers).To(Equal(h))
	g.Expect(s2a.Handlers).To(Equal(h))
	g.Expect(s2b.Handlers).To(Equal(h))

	s.Start()
	g.Expect(s1.Running()).To(BeTrue())
	g.Expect(s2a.Running()).To(BeTrue())
	g.Expect(s2b.Running()).To(BeTrue())

	s.Stop()
	g.Expect(s1.Running()).To(BeFalse())
	g.Expect(s2a.Running()).To(BeFalse())
	g.Expect(s2b.Running()).To(BeFalse())
}
