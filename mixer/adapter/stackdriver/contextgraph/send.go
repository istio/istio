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
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/timestamp"

	contextgraphpb "istio.io/istio/mixer/adapter/stackdriver/internal/google.golang.org/genproto/googleapis/cloud/contextgraph/v1alpha1"
	"istio.io/istio/pkg/mcp/status"
)

// Maximum payload size allowed for calling this API is 600KB. Set to
// 550 to be safe.
const maxReq = 550 * 1024

func (h *handler) cacheAndSend(ctx context.Context) {
	epoch := 0
	var lastFlush time.Time
	for {
		select {
		case t := <-h.traffics:
			entities, edges := t.Reify(h.env.Logger())
			for _, entity := range entities {
				if h.entityCache.AssertAndCheck(entity, epoch) {
					h.entitiesToSend = append(h.entitiesToSend, entity)
					h.edgeCache.Invalidate(entity.fullName)
				}
			}
			for _, edge := range edges {
				if h.edgeCache.AssertAndCheck(edge, epoch) {
					h.edgesToSend = append(h.edgesToSend, edge)
				}
			}
		case t := <-h.sendTick.C:
			if t.After(lastFlush.Add(9 * time.Minute)) {
				h.entitiesToSend = append(h.entitiesToSend, h.entityCache.Flush(epoch)...)
				h.edgesToSend = append(h.edgesToSend, h.edgeCache.Flush(epoch)...)
				lastFlush = t
			}
			if err := h.send(ctx, t, h.entitiesToSend, h.edgesToSend); err != nil {
				_ = h.env.Logger().Errorf("sending context graph batch failed: %v", err)
				// TODO: Invalidate these entities and edges so we try again on the next tick?
			}
			epoch++
			// TODO: Consider using [:0] to preserve existing cap?
			h.entitiesToSend = nil
			h.edgesToSend = nil
		case <-h.quit:
			return
		}
	}
}

func (e entity) ToProto() *contextgraphpb.Entity {
	epb := &contextgraphpb.Entity{
		ContainerFullName: e.containerFullName,
		TypeName:          e.typeName,
		Location:          e.location,
		FullName:          e.fullName,
	}
	for _, name := range e.shortNames {
		if name != "" {
			epb.ShortNames = append(epb.ShortNames, name)
		}
	}
	return epb
}

func (e edge) ToProto() *contextgraphpb.Relationship {
	return &contextgraphpb.Relationship{
		TypeName:       e.typeName,
		SourceFullName: e.sourceFullName,
		TargetFullName: e.destinationFullName,
	}
}

// splitBySize helps find the largest BatchRequest that is smaller than the max request size
func splitBySizeEntity(msg *contextgraphpb.AssertBatchRequest) int {
	curSize := 0
	curIndex := 0
	for ; curIndex < len(msg.EntityPresentAssertions); curIndex++ {
		curSize += proto.Size(msg.EntityPresentAssertions[curIndex])
		if curSize >= maxReq {
			break
		}
	}
	return curIndex
}

func splitBySizeRelationship(msg *contextgraphpb.AssertBatchRequest) int {
	curSize := 0
	curIndex := 0
	for ; curIndex < len(msg.RelationshipPresentAssertions); curIndex++ {
		curSize += proto.Size(msg.RelationshipPresentAssertions[curIndex])
		if curSize >= maxReq {
			break
		}
	}
	return curIndex
}

func (h *handler) send(ctx context.Context, t time.Time, entitiesToSend []entity, edgesToSend []edge) error {
	if (len(entitiesToSend) == 0) && (len(edgesToSend) == 0) {
		h.env.Logger().Debugf("Nothing to send this tick")
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req := &contextgraphpb.AssertBatchRequest{
		EntityPresentAssertions:       make([]*contextgraphpb.EntityPresentAssertion, 0),
		RelationshipPresentAssertions: make([]*contextgraphpb.RelationshipPresentAssertion, 0),
	}

	for _, entity := range entitiesToSend {
		asst := &contextgraphpb.EntityPresentAssertion{
			Timestamp: &timestamp.Timestamp{
				Seconds: t.Unix(),
			},
			Entity: entity.ToProto(),
		}
		req.EntityPresentAssertions = append(req.EntityPresentAssertions, asst)
	}

	for _, edge := range edgesToSend {
		relAsst := &contextgraphpb.RelationshipPresentAssertion{
			Timestamp: &timestamp.Timestamp{
				Seconds: t.Unix(),
			},
			Relationship: edge.ToProto(),
			OwningEntity: contextgraphpb.RelationshipPresentAssertion_SOURCE,
		}
		req.RelationshipPresentAssertions = append(req.RelationshipPresentAssertions, relAsst)
	}

	h.env.Logger().Debugf("Context api request size: %v", proto.Size(req))
	h.env.Logger().Debugf("Sending %v entities and %v relationships",
		len(req.EntityPresentAssertions), len(req.RelationshipPresentAssertions))

	if proto.Size(req) < maxReq {
		if err := h.call(ctx, req); err != nil {
			return err
		}
	} else {
		entReq := &contextgraphpb.AssertBatchRequest{
			EntityPresentAssertions:       req.EntityPresentAssertions,
			RelationshipPresentAssertions: nil,
		}
		relReq := &contextgraphpb.AssertBatchRequest{
			EntityPresentAssertions:       nil,
			RelationshipPresentAssertions: req.RelationshipPresentAssertions,
		}

		for len(entReq.EntityPresentAssertions) > 0 {
			split := splitBySizeEntity(entReq)
			allRequests := entReq.EntityPresentAssertions
			entReq.EntityPresentAssertions = allRequests[:split]
			h.env.Logger().Debugf("Batch request size: %v", proto.Size(entReq))
			if err := h.call(ctx, entReq); err != nil {
				return err
			}
			entReq.EntityPresentAssertions = allRequests[split:]
		}

		for len(relReq.RelationshipPresentAssertions) > 0 {
			split := splitBySizeRelationship(relReq)
			allRequests := relReq.RelationshipPresentAssertions
			relReq.RelationshipPresentAssertions = allRequests[:split]
			h.env.Logger().Debugf("Batch request size: %v", proto.Size(relReq))
			if err := h.call(ctx, relReq); err != nil {
				return err
			}
			relReq.RelationshipPresentAssertions = allRequests[split:]
		}
	}
	return nil
}

func (h *handler) call(ctx context.Context, req *contextgraphpb.AssertBatchRequest) error {
	h.env.Logger().Debugf("Sending Context Graph AssertBatch with %d entities, and %d relationships",
		len(req.EntityPresentAssertions), len(req.RelationshipPresentAssertions))
	if _, err := h.assertBatch(ctx, req); err != nil {
		s, _ := status.FromError(err)
		if d := s.Proto().Details; len(d) > 0 {
			// Log the debug message, if present.
			_ = h.env.Logger().Errorf("STATUS: %s\n", d[0].Value)
		}
		return err
	}
	return nil
}
