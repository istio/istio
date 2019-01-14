//  Copyright 2019 Istio Authors
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

package processing

import (
	"reflect"
	"testing"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/galley/pkg/runtime/resource"
)

func TestGraph_Basic(t *testing.T) {
	b := NewGraphBuilder()

	v := NewStoredProjection(emptyInfo.TypeURL)
	b.AddProjection(v)

	var received *resource.Event
	fh := HandlerFromFn(func(e resource.Event) {
		received = &e
		v.Set(e.Entry.ID.FullName, toMcpResourceOrPanic(e.Entry))
	})

	b.AddHandler(emptyInfo.TypeURL, fh)

	var changedType resource.TypeURL
	l := ListenerFromFn(func(t resource.TypeURL) {
		changedType = t
	})
	b.AddListener(l)

	p := b.Build()

	p.Handle(addRes1V1())
	if received == nil || !reflect.DeepEqual(*received, addRes1V1()) {
		t.Fatal("Expected event is not received")
	}

	if !reflect.DeepEqual(changedType, emptyInfo.TypeURL) {
		t.Fatalf("Expected projection change event not found: %v", changedType)
	}

	sn := p.Snapshot([]resource.TypeURL{emptyInfo.TypeURL})
	res := sn.Resources(emptyInfo.TypeURL.String())
	expected := []*mcp.Resource{toMcpResourceOrPanic(res1V1())}
	if !reflect.DeepEqual(res, expected) {
		t.Fatalf("Mismatch: got:%v, wanted:%v", res, expected)
	}
}

func TestGraph_SnapshotUnknown(t *testing.T) {
	b := NewGraphBuilder()

	v := NewStoredProjection(emptyInfo.TypeURL)
	b.AddProjection(v)

	fh := HandlerFromFn(func(e resource.Event) {
		v.Set(e.Entry.ID.FullName, toMcpResourceOrPanic(e.Entry))
	})

	b.AddHandler(emptyInfo.TypeURL, fh)

	p := b.Build()

	p.Handle(addRes1V1())

	sn := p.Snapshot([]resource.TypeURL{structInfo.TypeURL})
	res := sn.Resources(emptyInfo.TypeURL.String())
	var expected []*mcp.Resource
	if !reflect.DeepEqual(res, expected) {
		t.Fatalf("Mismatch: got:%v, wanted:%v", res, expected)
	}
}
