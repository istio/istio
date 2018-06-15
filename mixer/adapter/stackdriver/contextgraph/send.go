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
	"time"

	"github.com/golang/protobuf/ptypes/timestamp"
	contextgraphpb "google.golang.org/genproto/googleapis/cloud/contextgraph/v1alpha1"
	"google.golang.org/grpc/status"
)

func (h *handler) cacheAndSend() {
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
			h.send(t)
			epoch++
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

func (h *handler) send(t time.Time) {
	if (len(h.entitiesToSend) == 0) && (len(h.edgesToSend) == 0) {
		h.env.Logger().Debugf("Nothing to send this tick")
		return
	}

	req := &contextgraphpb.AssertBatchRequest{
		EntityPresentAssertions:       make([]*contextgraphpb.EntityPresentAssertion, 0),
		RelationshipPresentAssertions: make([]*contextgraphpb.RelationshipPresentAssertion, 0),
	}

	for _, entity := range h.entitiesToSend {
		asst := &contextgraphpb.EntityPresentAssertion{
			Timestamp: &timestamp.Timestamp{
				Seconds: t.Unix(),
			},
			Entity: entity.ToProto(),
		}
		req.EntityPresentAssertions = append(req.EntityPresentAssertions, asst)
	}

	for _, edge := range h.edgesToSend {
		relAsst := &contextgraphpb.RelationshipPresentAssertion{
			Timestamp: &timestamp.Timestamp{
				Seconds: t.Unix(),
			},
			Relationship: edge.ToProto(),
			OwningEntity: contextgraphpb.RelationshipPresentAssertion_SOURCE,
		}
		req.RelationshipPresentAssertions = append(req.RelationshipPresentAssertions, relAsst)
	}

	// TODO: Batch requests if there are too many entities and edges in one request.

	h.env.Logger().Debugf("Context api request: %s", req)
	if _, err := h.assertBatch(h.ctx, req); err != nil {
		h.env.Logger().Errorf("Request failed: %s\n", err)
		s, _ := status.FromError(err)
		h.env.Logger().Errorf("STATUS: %s\n", s.Proto().Details[0].Value)
		return
	}

	// TODO: Reslice to save the old cap()?
	h.entitiesToSend = nil
	h.edgesToSend = nil
}
