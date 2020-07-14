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

package fixtures

import (
	"testing"

	"github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/testing/basicmeta"
	"istio.io/istio/pkg/config/event"
)

func TestSource(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	s := &Source{}

	s.Start()
	g.Expect(s.running).To(gomega.BeTrue())

	s.Stop()
	g.Expect(s.running).To(gomega.BeFalse())
}

func TestSource_Dispatch(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	a := &Accumulator{}

	s := &Source{}
	s.Dispatch(a)
	s.Start()

	g.Expect(s.Handlers).To(gomega.Equal(a))
}

func TestSource_Handle(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	s := &Source{}

	a := &Accumulator{}
	s.Dispatch(a)

	s.Start()

	e := event.Event{
		Kind:     event.Added,
		Source:   basicmeta.K8SCollection1,
		Resource: nil,
	}
	s.Handle(e)

	g.Expect(a.Events()).To(gomega.Equal([]event.Event{e}))
}
