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

package identity

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/galley/pkg/runtime/processing"
	"istio.io/istio/galley/pkg/runtime/resource"
)

var schema *resource.Schema
var emptyInfo resource.Info
var structInfo resource.Info

func init() {
	b := resource.NewSchemaBuilder()
	emptyInfo = b.Register("type.googleapis.com/google.protobuf.Empty")
	structInfo = b.Register("type.googleapis.com/google.protobuf.Struct")
	schema = b.Build()
}

func addRes1V1() resource.Event {
	return resource.Event{
		Kind: resource.Added,
		Entry: resource.Entry{
			ID: resource.VersionedKey{
				Version: "v1",
				Key: resource.Key{
					TypeURL:  emptyInfo.TypeURL,
					FullName: resource.FullNameFromNamespaceAndName("ns1", "res1"),
				},
			},
			Metadata: resource.Metadata{
				CreateTime: time.Unix(1, 1),
			},
			Item: &types.Empty{},
		},
	}
}

func addRes2V1() resource.Event {
	return resource.Event{
		Kind: resource.Added,
		Entry: resource.Entry{
			ID: resource.VersionedKey{
				Version: "v1",
				Key: resource.Key{
					TypeURL:  emptyInfo.TypeURL,
					FullName: resource.FullNameFromNamespaceAndName("ns1", "res2"),
				},
			},
			Metadata: resource.Metadata{
				CreateTime: time.Unix(2, 1),
			},
			Item: &types.Empty{},
		},
	}
}

func updateRes1V2() resource.Event {
	return resource.Event{
		Kind: resource.Updated,
		Entry: resource.Entry{
			ID: resource.VersionedKey{
				Version: "v2",
				Key: resource.Key{
					TypeURL:  emptyInfo.TypeURL,
					FullName: resource.FullNameFromNamespaceAndName("ns1", "res1"),
				},
			},
			Item: &types.Empty{},
		},
	}
}

func delete1() resource.Event {
	return resource.Event{
		Kind: resource.Deleted,
		Entry: resource.Entry{
			ID: resource.VersionedKey{
				Version: "v1",
				Key: resource.Key{
					TypeURL:  emptyInfo.TypeURL,
					FullName: resource.FullNameFromNamespaceAndName("ns1", "res1"),
				},
			},
		},
	}
}

func TestPipeline_Changes(t *testing.T) {
	b := processing.NewGraphBuilder()
	AddProcessor(emptyInfo.TypeURL, b)

	var changed bool
	l := processing.ListenerFromFn(func(_ resource.TypeURL) {
		changed = true
	})
	b.AddListener(l)

	p := b.Build()

	p.Handle(addRes1V1())
	if !changed {
		t.Fatal("Expected a change")
	}
	changed = false

	p.Handle(addRes1V1())
	if changed {
		t.Fatal("Not expected a change")
	}

	p.Handle(updateRes1V2())
	if !changed {
		t.Fatal("Expected a change")
	}
	changed = false

	p.Handle(updateRes1V2())
	if changed {
		t.Fatal("Not expected a change")
	}

	sn := p.Snapshot([]resource.TypeURL{emptyInfo.TypeURL})
	envs := sn.Resources(emptyInfo.TypeURL.String())

	if len(envs) != 1 {
		t.Fatalf("Unexpected items: %v", envs)
	}

	ts, _ := types.TimestampProto(time.Time{})

	expected := &mcp.Resource{
		Metadata: &mcp.Metadata{
			Name:       "ns1/res1",
			Version:    "v2",
			CreateTime: ts,
		},
		Body: &types.Any{
			TypeUrl: "type.googleapis.com/google.protobuf.Empty",
		},
	}

	if !expected.Equal(envs[0]) {
		t.Fatalf("mismatch:\n   got:'%+v',\nwanted:'%+v'", envs[0], expected)
	}
}

func TestPipeline_AddTwo(t *testing.T) {
	b := processing.NewGraphBuilder()
	AddProcessor(emptyInfo.TypeURL, b)

	var changed bool
	l := processing.ListenerFromFn(func(_ resource.TypeURL) {
		changed = true
	})
	b.AddListener(l)

	p := b.Build()

	p.Handle(addRes1V1())
	if !changed {
		t.Fatal("Expected a change")
	}
	changed = false

	p.Handle(addRes2V1())
	if !changed {
		t.Fatal("Expected a change")
	}
	changed = false

	sn := p.Snapshot([]resource.TypeURL{emptyInfo.TypeURL})
	envs := sn.Resources(emptyInfo.TypeURL.String())

	if len(envs) != 2 {
		t.Fatalf("Unexpected items: %v", envs)
	}

	sort.Slice(envs, func(i, j int) bool {
		return strings.Compare(envs[i].Metadata.Name, envs[j].Metadata.Name) < 0
	})

	ts1, _ := types.TimestampProto(time.Unix(1, 1))
	ts2, _ := types.TimestampProto(time.Unix(2, 1))

	expected := []*mcp.Resource{
		{
			Metadata: &mcp.Metadata{
				Name:       "ns1/res1",
				Version:    "v1",
				CreateTime: ts1,
			},
			Body: &types.Any{
				TypeUrl: "type.googleapis.com/google.protobuf.Empty",
			},
		},
		{
			Metadata: &mcp.Metadata{
				Name:       "ns1/res2",
				Version:    "v1",
				CreateTime: ts2,
			},
			Body: &types.Any{
				TypeUrl: "type.googleapis.com/google.protobuf.Empty",
			},
		},
	}

	if len(expected) != len(envs) || !expected[0].Equal(envs[0]) || !expected[1].Equal(envs[1]) {
		t.Fatalf("mismatch:\n   got:'%+v',\nwanted:'%+v'", envs, expected)
	}
}

func TestPipeline_AddRemove(t *testing.T) {
	b := processing.NewGraphBuilder()
	AddProcessor(emptyInfo.TypeURL, b)

	var changed bool
	l := processing.ListenerFromFn(func(_ resource.TypeURL) {
		changed = true
	})
	b.AddListener(l)

	p := b.Build()

	p.Handle(addRes1V1())
	if !changed {
		t.Fatal("Expected a change")
	}
	changed = false

	p.Handle(delete1())
	if !changed {
		t.Fatal("Expected a change")
	}
	changed = false

	sn := p.Snapshot([]resource.TypeURL{emptyInfo.TypeURL})
	envs := sn.Resources(emptyInfo.TypeURL.String())

	if len(envs) != 0 {
		t.Fatalf("Unexpected items: %v", envs)
	}
}
