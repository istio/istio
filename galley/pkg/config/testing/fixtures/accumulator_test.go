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

	"istio.io/istio/galley/pkg/config/testing/data"
	"istio.io/istio/pkg/config/event"
)

func TestAccumulator(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	a := &Accumulator{}

	a.Handle(data.Event1Col1AddItem1)

	expected := []event.Event{data.Event1Col1AddItem1}
	g.Expect(a.Events()).To(gomega.Equal(expected))

	a.Handle(data.Event2Col1AddItem2)

	expected = []event.Event{data.Event1Col1AddItem1, data.Event2Col1AddItem2}
	g.Expect(a.Events()).To(gomega.Equal(expected))
}

func TestAccumulator_Clear(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	a := &Accumulator{}

	a.Handle(data.Event1Col1AddItem1)
	a.Handle(data.Event2Col1AddItem2)
	a.Clear()

	g.Expect(a.Events()).To(gomega.Equal([]event.Event{}))
}

func TestAccumulator_String(t *testing.T) {
	a := &Accumulator{}

	a.Handle(data.Event1Col1AddItem1)
	a.Handle(data.Event2Col1AddItem2)

	// ensure that it does not crash
	_ = a.String()
}
