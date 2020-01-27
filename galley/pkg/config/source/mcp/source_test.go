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

package mcp

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/mcp/sink"
)

func TestApplyUnknownCollection(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewSource(collection.SchemasFor())
	s.Start()
	defer s.Stop()

	err := s.Apply(&sink.Change{
		Collection: "ns/unknown",
	})
	g.Expect(err).ToNot(BeNil())
}

func TestApply(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewSource(collection.SchemasFor(testCollection))
	s.Start()
	defer s.Stop()

	var events []event.Event
	s.Dispatch(event.HandlerFromFn(func(e event.Event) {
		events = append(events, e)
	}))

	err := s.Apply(&sink.Change{
		Collection: testCollection.Name().String(),
	})
	g.Expect(err).To(BeNil())
	g.Expect(len(events)).To(Equal(1))
	g.Expect(events[0]).To(Equal(event.FullSyncFor(testCollection)))
}
