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

package flow

import (
	"reflect"
	"testing"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/galley/pkg/runtime/resource"
)

func TestSnapshotter_Snapshot(t *testing.T) {
	ev1 := &fakeView{
		typeURL:    emptyInfo.TypeURL,
		generation: 1,
		items: []*mcp.Envelope{
			{
				Metadata: &mcp.Metadata{
					Name:    "foo",
					Version: "v1",
				},
			},
			{
				Metadata: &mcp.Metadata{
					Name:    "bar",
					Version: "v2",
				},
			},
		},
	}
	ev2 := &fakeView{
		typeURL:    emptyInfo.TypeURL,
		generation: 2,
		items: []*mcp.Envelope{
			{
				Metadata: &mcp.Metadata{
					Name:    "zoo",
					Version: "v3",
				},
			},
			{
				Metadata: &mcp.Metadata{
					Name:    "zar",
					Version: "v4",
				},
			},
		},
	}
	sv1 := &fakeView{
		typeURL:    structInfo.TypeURL,
		generation: 3,
		items: []*mcp.Envelope{
			{
				Metadata: &mcp.Metadata{
					Name:    "doo",
					Version: "v5",
				},
			},
			{
				Metadata: &mcp.Metadata{
					Name:    "dar",
					Version: "v6",
				},
			},
		}}

	views := []View{ev1, ev2, sv1}
	s := newSnapshotter("sn", views)

	sn := s.snapshot()

	var expected []*mcp.Envelope
	expected = append(expected, ev1.items...)
	expected = append(expected, ev2.items...)

	envs := sn.Resources(emptyInfo.TypeURL.String())
	if !reflect.DeepEqual(expected, envs) {
		t.Fatalf("Mismatch: got:%v, wanted:%v", envs, expected)
	}

	expected = []*mcp.Envelope{}
	expected = append(expected, sv1.items...)

	envs = sn.Resources(structInfo.TypeURL.String())
	if !reflect.DeepEqual(expected, envs) {
		t.Fatalf("Mismatch: got:%v, wanted:%v", envs, expected)
	}
}

type fakeView struct {
	name       string
	typeURL    resource.TypeURL
	generation int64
	items      []*mcp.Envelope
}

var _ View = &fakeView{}

func (f *fakeView) Name() string {
	return f.name
}

func (f *fakeView) String() string {
	return ""
}

func (f *fakeView) Type() resource.TypeURL {
	return f.typeURL
}

func (f *fakeView) Generation() int64 {
	return f.generation
}

func (f *fakeView) Get() []*mcp.Envelope {
	return f.items
}
