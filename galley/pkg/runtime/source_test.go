// Copyright 2018 Istio Authors
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

package runtime

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gogo/protobuf/types"

	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/galley/pkg/testing/events"
	"istio.io/istio/galley/pkg/testing/resources"
)

func TestInMemory_Start_Empty(t *testing.T) {
	i := NewInMemorySource()
	ch := make(chan resource.Event, 1024)
	err := i.Start(events.ChannelHandler(ch))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	actual := captureChannelOutput(t, ch, 1)
	expected := strings.TrimSpace(`
[Event](FullSync)
`)
	if actual != expected {
		t.Fatalf("Channel mismatch:\nActual:\n%v\nExpected:\n%v\n", actual, expected)
	}
}

func TestInMemory_Start_WithItem(t *testing.T) {
	i := NewInMemorySource()
	fn := resource.FullNameFromNamespaceAndName("n1", "f1")
	i.Set(resource.Key{Collection: resources.EmptyInfo.Collection, FullName: fn}, resource.Metadata{}, &types.Empty{})

	ch := make(chan resource.Event, 1024)
	if err := i.Start(events.ChannelHandler(ch)); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	actual := captureChannelOutput(t, ch, 2)
	expected := strings.TrimSpace(`
[Event](Added: [VKey](empty:n1/f1 @v1))
[Event](FullSync)
`)
	if actual != expected {
		t.Fatalf("Channel mismatch:\nActual:\n%v\nExpected:\n%v\n", actual, expected)
	}
}

func TestInMemory_Start_DoubleStart(t *testing.T) {
	i := NewInMemorySource()
	ch := make(chan resource.Event, 1024)
	_ = i.Start(events.ChannelHandler(ch))
	if err := i.Start(events.ChannelHandler(ch)); err == nil {
		t.Fatal("should have returned error")
	}
}

func TestInMemory_Start_DoubleStop(t *testing.T) {
	i := NewInMemorySource()
	ch := make(chan resource.Event, 1024)
	_ = i.Start(events.ChannelHandler(ch))
	i.Stop()
	// should not panic
	i.Stop()
}

func TestInMemory_Set(t *testing.T) {
	i := NewInMemorySource()
	ch := make(chan resource.Event, 1024)
	if err := i.Start(events.ChannelHandler(ch)); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// One Register one update
	fn := resource.FullNameFromNamespaceAndName("n1", "f1")
	i.Set(resource.Key{Collection: resources.EmptyInfo.Collection, FullName: fn}, resource.Metadata{}, &types.Empty{})
	i.Set(resource.Key{Collection: resources.EmptyInfo.Collection, FullName: fn}, resource.Metadata{}, &types.Empty{})

	actual := captureChannelOutput(t, ch, 3)
	expected := strings.TrimSpace(`
[Event](FullSync)
[Event](Added: [VKey](empty:n1/f1 @v1))
[Event](Updated: [VKey](empty:n1/f1 @v2))
`)
	if actual != expected {
		t.Fatalf("Channel mismatch:\nActual:\n%v\nExpected:\n%v\n", actual, expected)
	}
}

func TestInMemory_Delete(t *testing.T) {
	i := NewInMemorySource()
	ch := make(chan resource.Event, 1024)
	if err := i.Start(events.ChannelHandler(ch)); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	fn := resource.FullNameFromNamespaceAndName("n1", "f1")
	i.Set(resource.Key{Collection: resources.EmptyInfo.Collection, FullName: fn}, resource.Metadata{}, &types.Empty{})
	// Two deletes
	i.Delete(resource.Key{Collection: resources.EmptyInfo.Collection, FullName: fn})
	i.Delete(resource.Key{Collection: resources.EmptyInfo.Collection, FullName: fn})

	actual := captureChannelOutput(t, ch, 3)
	expected := strings.TrimSpace(`
[Event](FullSync)
[Event](Added: [VKey](empty:n1/f1 @v1))
[Event](Deleted: [VKey](empty:n1/f1 @v2))
`)
	if actual != expected {
		t.Fatalf("Channel mismatch:\nActual:\n%v\nExpected:\n%v\n", actual, expected)
	}
}

func TestInMemory_Get(t *testing.T) {
	fn := resource.FullNameFromNamespaceAndName("n1", "f1")

	i := NewInMemorySource()
	i.Set(resource.Key{Collection: resources.EmptyInfo.Collection, FullName: fn}, resource.Metadata{}, &types.Empty{})

	r, _ := i.Get(resource.Key{Collection: resources.EmptyInfo.Collection, FullName: fn})
	if r.IsEmpty() {
		t.Fatal("Get should have been non empty")
	}

	fn2 := resource.FullNameFromNamespaceAndName("", "fn2")
	r, _ = i.Get(resource.Key{Collection: resources.EmptyInfo.Collection, FullName: fn2})
	if !r.IsEmpty() {
		t.Fatalf("Get should have been empty: %v", r)
	}
}

func captureChannelOutput(t *testing.T, ch chan resource.Event, count int) string {
	t.Helper()

	result := ""
	for i := 0; i < count; i++ {
		e := <-ch

		switch e.Kind {
		case resource.Added, resource.Updated:
			if e.Entry.Item == nil {
				t.Fatalf("Invalid event received: event should have item: %v", e)
			}

		case resource.Deleted, resource.FullSync:
			if e.Entry.Item != nil {
				t.Fatalf("Invalid event received: event should *not* have item: %v", e)
			}

		default:
			t.Fatalf("Unrecognized event type: %v, event: %v", e.Kind, e)
		}

		result += fmt.Sprintf("%v\n", e)
	}

	result = strings.TrimSpace(result)

	return result
}
