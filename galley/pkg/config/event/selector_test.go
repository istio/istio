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

package event_test

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/testing/data"
	"istio.io/istio/galley/pkg/config/testing/fixtures"
)

func TestSelector_Basic(t *testing.T) {
	g := NewGomegaWithT(t)

	s := event.NewSelector()
	acc := &fixtures.Accumulator{}
	s.Select(data.Collection1, acc)
	s.Handle(data.Event1Col1AddItem1)

	g.Expect(acc.Events()).To(HaveLen(1))
}

func TestSelector_NoMatch(t *testing.T) {
	g := NewGomegaWithT(t)

	s := event.NewSelector()
	acc := &fixtures.Accumulator{}
	s.Select(data.Collection2, acc)
	s.Handle(data.Event1Col1AddItem1)

	g.Expect(acc.Events()).To(HaveLen(0))
}

func TestSelector_MultiDispatch(t *testing.T) {
	g := NewGomegaWithT(t)

	s := event.NewSelector()
	acc1 := &fixtures.Accumulator{}
	acc2 := &fixtures.Accumulator{}
	s.Select(data.Collection1, acc1)
	s.Select(data.Collection1, acc2)
	s.Handle(data.Event1Col1AddItem1)

	g.Expect(acc1.Events()).To(HaveLen(1))
	g.Expect(acc2.Events()).To(HaveLen(1))
}
