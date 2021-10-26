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
	data2 "istio.io/istio/pkg/config/legacy/testing/data"
	fixtures2 "istio.io/istio/pkg/config/legacy/testing/fixtures"

	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/resource"
)

func TestNoVersions(t *testing.T) {
	g := NewWithT(t)

	events := []event.Event{data2.Event1Col1AddItem1}
	g.Expect(events[0].Resource.Metadata.Version).NotTo(Equal(resource.Version("")))
	events = fixtures2.NoVersions(events)
	g.Expect(events[0].Resource.Metadata.Version).To(Equal(resource.Version("")))
}

func TestNoFullSync(t *testing.T) {
	g := NewWithT(t)

	events := []event.Event{data2.Event1Col1AddItem1, data2.Event1Col1Synced, data2.Event2Col1AddItem2}
	events = fixtures2.NoFullSync(events)

	expected := []event.Event{data2.Event1Col1AddItem1, data2.Event2Col1AddItem2}
	g.Expect(events).To(Equal(expected))
}

func TestSort(t *testing.T) {
	g := NewWithT(t)

	events := []event.Event{data2.Event2Col1AddItem2, data2.Event1Col1Synced, data2.Event1Col1AddItem1}
	events = fixtures2.Sort(events)

	expected := []event.Event{data2.Event1Col1AddItem1, data2.Event2Col1AddItem2, data2.Event1Col1Synced}
	g.Expect(events).To(Equal(expected))
}

func TestChain(t *testing.T) {
	g := NewWithT(t)

	events := []event.Event{data2.Event2Col1AddItem2, data2.Event1Col1Synced, data2.Event1Col1AddItem1}
	fn := fixtures2.Chain(fixtures2.Sort, fixtures2.NoFullSync, fixtures2.NoVersions)
	events = fn(events)

	expected := []event.Event{
		data2.Event1Col1AddItem1.Clone(),
		data2.Event2Col1AddItem2.Clone(),
	}
	expected[0].Resource.Metadata.Version = resource.Version("")
	expected[1].Resource.Metadata.Version = resource.Version("")

	g.Expect(events).To(Equal(expected))
}
