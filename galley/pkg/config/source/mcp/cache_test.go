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
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/proto"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/api/networking/v1alpha3"

	"istio.io/istio/galley/pkg/config/testing/fixtures"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	resource2 "istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/mcp/sink"
)

var (
	testCollection = collection.Builder{
		Name: "ns/mycol",
		Resource: resource2.Builder{
			Kind:         "Empty",
			Plural:       "empties",
			ProtoPackage: "github.com/gogo/protobuf/types",
			Proto:        "google.protobuf.Empty",
		}.MustBuild(),
	}.MustBuild()

	eventTime = time.Now().UTC()
	fullSync  = output{
		kind: event.FullSync,
	}
)

func TestWrongCollectionShouldFail(t *testing.T) {
	c := newCacheHelper()
	c.applyExpectError(t, change{
		collection: "ns/wrong",
	})
}

func TestEventsBeforeStart(t *testing.T) {
	c := newCacheHelper()

	// Add r1
	c.applyExpectNoError(t, change{
		input: []input{
			{
				name: "ns/r1",
				body: &v1alpha3.Gateway{},
			},
		}})
	c.expectNoEvents(t)

	// Add r2, Update r1
	c.applyExpectNoError(t, change{
		input: []input{
			{
				name: "ns/r1",
				body: &v1alpha3.Gateway{},
			},
			{
				name: "ns/r2",
				body: &v1alpha3.Gateway{},
			},
		}})
	c.expectNoEvents(t)

	// Delete r2
	c.applyExpectNoError(t, change{
		input: []input{
			{
				name: "ns/r1",
				body: &v1alpha3.Gateway{},
			},
		}})
	c.expectNoEvents(t)

	c.Start()
	defer c.Stop()

	c.expect(t,
		output{
			kind: event.Added,
			name: "ns/r1",
			body: &v1alpha3.Gateway{},
		},
		fullSync)
}

func TestStartBeforeEvents(t *testing.T) {
	c := newCacheHelper()
	c.Start()
	defer c.Stop()

	c.expectNoEvents(t)
}

func TestEmptyUpdateShouldSendFullSync(t *testing.T) {
	c := newCacheHelper()
	c.Start()
	defer c.Stop()

	c.applyExpectNoError(t, change{})
	c.expect(t, fullSync)
}

func TestIncrementalUpdateBeforeFullShouldFail(t *testing.T) {
	c := newCacheHelper()
	c.Start()
	defer c.Stop()

	c.applyExpectError(t, change{incremental: true})
}

func TestIncremental(t *testing.T) {
	c := newCacheHelper()

	// Start it and apply an initial empty update.
	c.Start()
	defer c.Stop()
	c.applyExpectNoError(t, change{})
	c.expect(t, fullSync)

	// Add r1, r2
	c.applyExpectNoError(t, change{
		incremental: true,
		input: inputList{
			{
				name: "ns/r1",
				body: &v1alpha3.Gateway{},
			},
			{
				name: "ns/r2",
				body: &v1alpha3.Gateway{},
			},
		},
	})
	c.expect(t,
		output{
			kind: event.Added,
			name: "ns/r1",
			body: &v1alpha3.Gateway{},
		},
		output{
			kind: event.Added,
			name: "ns/r2",
			body: &v1alpha3.Gateway{},
		})

	// Delete r1, Update r2, delete r3
	c.applyExpectNoError(t, change{
		incremental: true,
		removed:     []string{"ns/r1"},
		input: inputList{
			{
				name: "ns/r2",
				body: &v1alpha3.Gateway{},
			},
			{
				name: "ns/r3",
				body: &v1alpha3.Gateway{},
			},
		},
	})
	c.expect(t,
		output{
			kind: event.Deleted,
			name: "ns/r1",
			body: &v1alpha3.Gateway{},
		},
		output{
			kind: event.Updated,
			name: "ns/r2",
			body: &v1alpha3.Gateway{},
		},
		output{
			kind: event.Added,
			name: "ns/r3",
			body: &v1alpha3.Gateway{},
		})
}

func TestIncrementalDeleteMissingResourceShouldFail(t *testing.T) {
	c := newCacheHelper()

	// Start it and apply an initial empty update.
	c.Start()
	defer c.Stop()
	c.applyExpectNoError(t, change{})
	c.expect(t, fullSync)

	// Delete non-existent resource.
	c.applyExpectError(t, change{
		incremental: true,
		removed:     []string{"ns/r1"},
	})
}

func TestFullUpdate(t *testing.T) {
	c := newCacheHelper()

	// Start it and apply an initial empty update.
	c.Start()
	c.applyExpectNoError(t, change{})
	c.expect(t, fullSync)

	// Add r1, r2
	c.applyExpectNoError(t, change{
		input: inputList{
			{
				name: "ns/r1",
				body: &v1alpha3.Gateway{},
			},
			{
				name: "ns/r2",
				body: &v1alpha3.Gateway{},
			},
		},
	})
	c.expect(t,
		output{
			kind: event.Added,
			name: "ns/r1",
			body: &v1alpha3.Gateway{},
		},
		output{
			kind: event.Added,
			name: "ns/r2",
			body: &v1alpha3.Gateway{},
		})

	// Delete r1, Update r2, add r3
	c.applyExpectNoError(t, change{
		input: inputList{
			{
				name: "ns/r2",
				body: &v1alpha3.Gateway{},
			},
			{
				name: "ns/r3",
				body: &v1alpha3.Gateway{},
			},
		},
	})
	c.expect(t,
		output{
			kind: event.Deleted,
			name: "ns/r1",
			body: &v1alpha3.Gateway{},
		},
		output{
			kind: event.Updated,
			name: "ns/r2",
			body: &v1alpha3.Gateway{},
		},
		output{
			kind: event.Added,
			name: "ns/r3",
			body: &v1alpha3.Gateway{},
		})
}

type cacheHelper struct {
	*cache

	events []event.Event
}

func newCacheHelper() *cacheHelper {
	c := &cacheHelper{
		cache: newCache(testCollection),
	}
	c.Dispatch(event.HandlerFromFn(func(e event.Event) {
		c.events = append(c.events, e)
	}))
	return c
}

func (c *cacheHelper) applyExpectNoError(t *testing.T, ch change) {
	t.Helper()

	// Clear any previous events.
	c.clearEvents()

	if err := c.apply(ch.toMCP()); err != nil {
		t.Fatal(err)
	}
}

func (c *cacheHelper) applyExpectError(t *testing.T, ch change) {
	t.Helper()

	// Clear any previous events.
	c.clearEvents()

	if err := c.apply(ch.toMCP()); err == nil {
		t.Fatal("expected error")
	}
}

func (c *cacheHelper) expect(t *testing.T, expectedOutput ...output) {
	t.Helper()
	if len(c.events) != len(expectedOutput) {
		t.Fatalf("expected num events %d to be %d", len(c.events), len(expectedOutput))
	}

	// Convert the output to events.
	expectedEvents := outputList(expectedOutput).toEvents()

	sortEvents(c.events)
	sortEvents(expectedEvents)

	for i, actual := range c.events {
		expected := expectedEvents[i]
		fixtures.ExpectEqual(t, actual, expected)
	}
}

func (c *cacheHelper) expectNoEvents(t *testing.T) {
	t.Helper()
	c.expect(t)
}

func (c *cacheHelper) clearEvents() {
	c.events = c.events[:0]
}

func sortEvents(events []event.Event) {
	sort.SliceStable(events, func(i, j int) bool {
		return strings.Compare(resourceName(events[i]), resourceName(events[j])) < 0
	})
}

func resourceName(e event.Event) string {
	if e.Resource == nil {
		return ""
	}
	return e.Resource.Metadata.FullName.String()
}

func protoTime(t time.Time) *types.Timestamp {
	out, _ := types.TimestampProto(t)
	return out
}

type change struct {
	incremental bool
	input       inputList
	removed     []string
	collection  string
}

func (c change) toMCP() *sink.Change {
	col := c.collection
	if col == "" {
		col = testCollection.Name().String()
	}
	return &sink.Change{
		Incremental: c.incremental,
		Collection:  col,
		Objects:     c.input.toObjects(),
		Removed:     c.removed,
	}
}

type inputList []input

func (l inputList) toObjects() []*sink.Object {
	out := make([]*sink.Object, 0, len(l))
	for _, in := range l {
		out = append(out, in.toObject())
	}
	return out
}

type input struct {
	name string
	body proto.Message
}

func (i input) toObject() *sink.Object {
	return &sink.Object{
		Metadata: &mcp.Metadata{
			Name:       i.name,
			CreateTime: protoTime(eventTime),
			Version:    "v1",
		},
		Body: i.body,
	}
}

type outputList []output

func (l outputList) toEvents() []event.Event {
	events := make([]event.Event, 0, len(l))
	for _, o := range l {
		events = append(events, o.toEvent())
	}
	return events
}

type output struct {
	kind event.Kind
	name string
	body proto.Message
}

func (e output) toEvent() event.Event {
	if e.kind == event.FullSync {
		return event.FullSyncFor(testCollection)
	}

	fullName, _ := resource.ParseFullName(e.name)
	return event.Event{
		Kind:   e.kind,
		Source: testCollection,
		Resource: &resource.Instance{
			Metadata: resource.Metadata{
				FullName:   fullName,
				CreateTime: eventTime,
				Version:    "v1",
				Schema:     testCollection.Resource(),
			},
			Message: e.body,
			Origin:  defaultOrigin,
		},
	}
}
