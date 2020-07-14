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

package fixtures_test

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/testing/data"
	"istio.io/istio/galley/pkg/config/testing/fixtures"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/resource"
)

func TestNoVersions(t *testing.T) {
	g := NewGomegaWithT(t)

	events := []event.Event{data.Event1Col1AddItem1}
	g.Expect(events[0].Resource.Metadata.Version).NotTo(Equal(resource.Version("")))
	events = fixtures.NoVersions(events)
	g.Expect(events[0].Resource.Metadata.Version).To(Equal(resource.Version("")))
}

func TestNoFullSync(t *testing.T) {
	g := NewGomegaWithT(t)

	events := []event.Event{data.Event1Col1AddItem1, data.Event1Col1Synced, data.Event2Col1AddItem2}
	events = fixtures.NoFullSync(events)

	expected := []event.Event{data.Event1Col1AddItem1, data.Event2Col1AddItem2}
	g.Expect(events).To(Equal(expected))
}

func TestSort(t *testing.T) {
	g := NewGomegaWithT(t)

	events := []event.Event{data.Event2Col1AddItem2, data.Event1Col1Synced, data.Event1Col1AddItem1}
	events = fixtures.Sort(events)

	expected := []event.Event{data.Event1Col1AddItem1, data.Event2Col1AddItem2, data.Event1Col1Synced}
	g.Expect(events).To(Equal(expected))
}

func TestChain(t *testing.T) {
	g := NewGomegaWithT(t)

	events := []event.Event{data.Event2Col1AddItem2, data.Event1Col1Synced, data.Event1Col1AddItem1}
	fn := fixtures.Chain(fixtures.Sort, fixtures.NoFullSync, fixtures.NoVersions)
	events = fn(events)

	expected := []event.Event{
		data.Event1Col1AddItem1.Clone(),
		data.Event2Col1AddItem2.Clone(),
	}
	expected[0].Resource.Metadata.Version = resource.Version("")
	expected[1].Resource.Metadata.Version = resource.Version("")

	g.Expect(events).To(Equal(expected))
}
