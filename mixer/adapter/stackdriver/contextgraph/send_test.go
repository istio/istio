// Copyright 2017 Istio Authors
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

package contextgraph

import (
	"context"
	"testing"
	"time"

	gax "github.com/googleapis/gax-go"
	contextgraphpb "google.golang.org/genproto/googleapis/cloud/contextgraph/v1alpha1"

	env "istio.io/istio/mixer/pkg/adapter/test"
)

type mockBatchClient struct {
	ABCalled    int
	NumEntities int
	NumEdges    int
}

func (m *mockBatchClient) fakeAssertBatch(
	ctx context.Context,
	req *contextgraphpb.AssertBatchRequest,
	opts ...gax.CallOption) (*contextgraphpb.AssertBatchResponse, error) {
	m.ABCalled++
	m.NumEntities += len(req.EntityPresentAssertions)
	m.NumEdges += len(req.RelationshipPresentAssertions)
	return nil, nil
}

func TestSendBatch(t *testing.T) {
	const (
		numEntities = 20000
		numEdges    = 20000
	)

	m := &mockBatchClient{
		ABCalled:    0,
		NumEntities: 0,
		NumEdges:    0,
	}
	h := &handler{
		env:         env.NewEnv(t),
		assertBatch: m.fakeAssertBatch,
	}

	entities := make([]entity, 0)
	edges := make([]edge, 0)

	for ii := 0; ii < numEntities; ii++ {
		entities = append(entities, entity{
			containerFullName: "asdf",
			typeName:          "asdf",
			fullName:          "asdf",
			location:          "asdf",
			shortNames:        [4]string{"asdf", "", "", ""},
		})
	}

	for ii := 0; ii < numEdges; ii++ {
		edges = append(edges, edge{
			sourceFullName:      "asdf",
			destinationFullName: "asdf",
			typeName:            "asdf",
		})
	}

	h.send(
		context.Background(),
		time.Unix(1337, 0),
		entities,
		edges,
	)

	if m.ABCalled != 4 {
		t.Fatalf("AssertBatch expected to be called 4 times. Called %v times", m.ABCalled)
	}
	if m.NumEntities != numEntities {
		t.Fatalf("%v entities expected to be sent, got %v:", numEntities, m.NumEntities)
	}
	if m.NumEdges != numEdges {
		t.Fatalf("%v edges expected to be sent, got %v:", numEdges, m.NumEdges)
	}
}
