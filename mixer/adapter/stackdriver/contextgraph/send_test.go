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

package contextgraph

import (
	"context"
	"fmt"
	"testing"
	"time"

	"istio.io/istio/mixer/pkg/adapter"

	"github.com/googleapis/gax-go"

	contextgraphpb "istio.io/istio/mixer/adapter/stackdriver/internal/google.golang.org/genproto/googleapis/cloud/contextgraph/v1alpha1"
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

func BenchmarkSendBatch(b *testing.B) {

	m := &mockBatchClient{
		ABCalled:    0,
		NumEntities: 0,
		NumEdges:    0,
	}
	for _, size := range []int{2, 200, 20000} {
		h, entities, edges := setupSendBatch(env.NewEnv(nil), m.fakeAssertBatch, size)

		b.Run(fmt.Sprintf("Size%d", size), func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				_ = h.send(
					context.Background(),
					time.Unix(1337, 0),
					entities,
					edges,
				)
			}
		})
	}
}

func TestSendBatch(t *testing.T) {
	testSize := 21000
	m := &mockBatchClient{
		ABCalled:    0,
		NumEntities: 0,
		NumEdges:    0,
	}
	h, entities, edges := setupSendBatch(env.NewEnv(t), m.fakeAssertBatch, testSize)

	h.send(
		context.Background(),
		time.Unix(1337, 0),
		entities,
		edges,
	)

	if m.ABCalled != 4 {
		t.Fatalf("AssertBatch expected to be called 4 times. Called %v times", m.ABCalled)
	}
	if m.NumEntities != testSize {
		t.Fatalf("%v entities expected to be sent, got %v:", testSize, m.NumEntities)
	}
	if m.NumEdges != testSize {
		t.Fatalf("%v edges expected to be sent, got %v:", testSize, m.NumEdges)
	}
}

func setupSendBatch(env adapter.Env, assert assertBatchFn, size int) (*handler, []entity, []edge) {

	h := &handler{
		env:         env,
		assertBatch: assert,
	}

	entities := make([]entity, 0)
	edges := make([]edge, 0)

	for ii := 0; ii < size; ii++ {
		entities = append(entities, entity{
			containerFullName: "asdf",
			typeName:          "asdf",
			fullName:          "asdf",
			location:          "asdf",
			shortNames:        [4]string{"asdf", "", "", ""},
		})
	}

	for ii := 0; ii < size; ii++ {
		edges = append(edges, edge{
			sourceFullName:      "asdf",
			destinationFullName: "asdf",
			typeName:            "asdf",
		})
	}
	return h, entities, edges
}
