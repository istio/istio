//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package runtime

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gogo/protobuf/types"

	"istio.io/istio/galley/pkg/runtime/resource"
)

var emptyInfo resource.Info

func init() {
	schema := resource.NewSchema()
	schema.Register("type.googleapis.com/google.protobuf.Empty", true)
	emptyInfo, _ = schema.Lookup("type.googleapis.com/google.protobuf.Empty")
}

func TestInMemory_Start_Empty(t *testing.T) {
	i := NewInMemorySource()
	ch, err := i.Start()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	actual := logChannelOutput(ch, 1)
	expected := strings.TrimSpace(`
[Event](FullSync: [VKey](: @))
`)
	if actual != expected {
		t.Fatalf("Channel mismatch:\nActual:\n%v\nExpected:\n%v\n", actual, expected)
	}
}

func TestInMemory_Start_WithItem(t *testing.T) {
	i := NewInMemorySource()
	i.Set(resource.Key{TypeURL: emptyInfo.TypeURL, FullName: "n1/f1"}, &types.Empty{})

	ch, err := i.Start()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	actual := logChannelOutput(ch, 2)
	expected := strings.TrimSpace(`
[Event](Added: [VKey](type.googleapis.com/google.protobuf.Empty:n1/f1 @v1))
[Event](FullSync: [VKey](: @))`)
	if actual != expected {
		t.Fatalf("Channel mismatch:\nActual:\n%v\nExpected:\n%v\n", actual, expected)
	}
}

func TestInMemory_Start_DoubleStart(t *testing.T) {
	i := NewInMemorySource()
	_, _ = i.Start()
	_, err := i.Start()
	if err == nil {
		t.Fatal("should have returned error")
	}
}

func TestInMemory_Start_DoubleStop(t *testing.T) {
	i := NewInMemorySource()
	_, _ = i.Start()
	i.Stop()
	// should not panic
	i.Stop()
}

func TestInMemory_Set(t *testing.T) {
	i := NewInMemorySource()
	ch, err := i.Start()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// One Register one update
	i.Set(resource.Key{TypeURL: emptyInfo.TypeURL, FullName: "n1/f1"}, &types.Empty{})
	i.Set(resource.Key{TypeURL: emptyInfo.TypeURL, FullName: "n1/f1"}, &types.Empty{})

	actual := logChannelOutput(ch, 3)
	expected := strings.TrimSpace(`
[Event](FullSync: [VKey](: @))
[Event](Added: [VKey](type.googleapis.com/google.protobuf.Empty:n1/f1 @v1))
[Event](Updated: [VKey](type.googleapis.com/google.protobuf.Empty:n1/f1 @v2))`)
	if actual != expected {
		t.Fatalf("Channel mismatch:\nActual:\n%v\nExpected:\n%v\n", actual, expected)
	}
}

func TestInMemory_Delete(t *testing.T) {
	i := NewInMemorySource()
	ch, err := i.Start()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	i.Set(resource.Key{TypeURL: emptyInfo.TypeURL, FullName: "n1/f1"}, &types.Empty{})
	// Two deletes
	i.Delete(resource.Key{TypeURL: emptyInfo.TypeURL, FullName: "n1/f1"})
	i.Delete(resource.Key{TypeURL: emptyInfo.TypeURL, FullName: "n1/f1"})

	actual := logChannelOutput(ch, 3)
	expected := strings.TrimSpace(`
[Event](FullSync: [VKey](: @))
[Event](Added: [VKey](type.googleapis.com/google.protobuf.Empty:n1/f1 @v1))
[Event](Deleted: [VKey](type.googleapis.com/google.protobuf.Empty:n1/f1 @v2))`)
	if actual != expected {
		t.Fatalf("Channel mismatch:\nActual:\n%v\nExpected:\n%v\n", actual, expected)
	}
}

func TestInMemory_Get(t *testing.T) {
	i := NewInMemorySource()
	i.Set(resource.Key{TypeURL: emptyInfo.TypeURL, FullName: "n1/f1"}, &types.Empty{})

	r, _ := i.Get(resource.Key{TypeURL: emptyInfo.TypeURL, FullName: "n1/f1"})
	if r.IsEmpty() {
		t.Fatal("Get should have been non empty")
	}

	r, _ = i.Get(resource.Key{TypeURL: emptyInfo.TypeURL, FullName: "n1/f2"})
	if !r.IsEmpty() {
		t.Fatalf("Get should have been empty: %v", r)
	}
}

func logChannelOutput(ch chan resource.Event, count int) string {
	result := ""
	for i := 0; i < count; i++ {
		item := <-ch
		result += fmt.Sprintf("%v\n", item)
	}

	result = strings.TrimSpace(result)

	return result
}
