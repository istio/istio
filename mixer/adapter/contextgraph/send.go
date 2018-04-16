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
	"fmt"

	"github.com/golang/protobuf/ptypes/timestamp"
	contextgraphpb "google.golang.org/genproto/googleapis/cloud/contextgraph/v1alpha1"
)

func (h *handler) cacheAndSend() {
	for {
		select {
		case w := <-h.workloads:
			if h.wCache.Check(w) {
				h.wToSend = append(h.wToSend, w)
				h.tCache.Invalid(w)
			}
		case t := <-h.traffics:
			if h.tCache.Check(t) {
				h.tToSend = append(h.tToSend, t)
			}
		case <-h.sendTick.C:
			h.send()
		case <-h.quit:
			return
		}
	}
}

func (h *handler) send() {
	if (len(h.wToSend) == 0) && (len(h.tToSend) == 0) {
		return
	}

	req := &contextgraphpb.AssertBatchRequest{
		EntityPresentAssertions:       make([]*contextgraphpb.EntityPresentAssertion, 0),
		RelationshipPresentAssertions: make([]*contextgraphpb.RelationshipPresentAssertion, 0),
	}

	for _, wl := range h.wToSend {
		fullName := fmt.Sprintf("%s/%s", h.workloadContainer, wl.id)
		asst := &contextgraphpb.EntityPresentAssertion{
			Timestamp: &timestamp.Timestamp{
				Seconds: wl.seen.Unix(),
			},
			Entity: &contextgraphpb.Entity{
				ContainerFullName: h.gcpContainer,
				TypeName:          "io.istio.Workload",
				Location:          "global",
				FullName:          fullName,
				ShortNames:        []string{wl.id},
			},
		}
		req.EntityPresentAssertions = append(req.EntityPresentAssertions, asst)

		var ownName string
		switch wl.wlType {
		case "pods":
			ownName = fmt.Sprintf("%s/k8s/namespaces/%s/pods/%s", h.clusterContainer, wl.namespace, wl.name)
		case "replicationcontrollers":
			ownName = fmt.Sprintf("%s/k8s/namespaces/%s/replicationcontrollers/%s", h.clusterContainer, wl.namespace, wl.name)
		case "cronjobs":
			ownName = fmt.Sprintf("%s/k8s/namespaces/%s/batch/cronjobs/%s", h.clusterContainer, wl.namespace, wl.name)
		case "jobs":
			ownName = fmt.Sprintf("%s/k8s/namespaces/%s/batch/jobs/%s", h.clusterContainer, wl.namespace, wl.name)
		case "daemonsets":
			ownName = fmt.Sprintf("%s/k8s/namespaces/%s/%s/daemonsets/%s", h.clusterContainer, wl.namespace, wl.group, wl.name)
		case "deployments":
			ownName = fmt.Sprintf("%s/k8s/namespaces/%s/%s/deployments/%s", h.clusterContainer, wl.namespace, wl.group, wl.name)
		case "replicasets":
			ownName = fmt.Sprintf("%s/k8s/namespaces/%s/%s/replicasets/%s", h.clusterContainer, wl.namespace, wl.group, wl.name)
		case "statefulsets":
			ownName = fmt.Sprintf("%s/k8s/namespaces/%s/apps/statefulsets/%s", h.clusterContainer, wl.namespace, wl.name)
		default:
			// Unknown owner type, don't add relationship
			h.env.Logger().Warningf("Unknown type of workload: %s", wl.wlType)
			continue
		}

		relAsst := &contextgraphpb.RelationshipPresentAssertion{
			Timestamp: &timestamp.Timestamp{
				Seconds: wl.seen.Unix(),
			},
			Relationship: &contextgraphpb.Relationship{
				TypeName:       "google.cloud.contextgraph.Membership",
				SourceFullName: fullName,
				TargetFullName: ownName,
			},
			OwningEntity: contextgraphpb.RelationshipPresentAssertion_SOURCE,
		}
		req.RelationshipPresentAssertions = append(req.RelationshipPresentAssertions, relAsst)
	}

	for _, t := range h.tToSend {
		var typeName string
		switch t.protocol {
		case "http":
			typeName = "google.cloud.contextgraph.Communication.Http"
		case "tcp":
			typeName = "google.cloud.contextgraph.Communication.Tcp"
		case "https":
			typeName = "google.cloud.contextgraph.Communication.Https"
		case "grpc":
			typeName = "google.cloud.contextgraph.Communication.Grpc"
		default:
			h.env.Logger().Warningf("Unknown type of protocol: %s", t.protocol)
			continue
		}
		fullSource := fmt.Sprintf("%s/%s", h.workloadContainer, t.sourceId)
		fullDest := fmt.Sprintf("%s/%s", h.workloadContainer, t.destId)
		relAsst := &contextgraphpb.RelationshipPresentAssertion{
			Timestamp: &timestamp.Timestamp{
				Seconds: t.seen.Unix(),
			},
			Relationship: &contextgraphpb.Relationship{
				TypeName:       typeName,
				SourceFullName: fullSource,
				TargetFullName: fullDest,
			},
			OwningEntity: contextgraphpb.RelationshipPresentAssertion_SOURCE,
		}
		req.RelationshipPresentAssertions = append(req.RelationshipPresentAssertions, relAsst)
	}

	h.env.Logger().Debugf("Context api request: %s", req)
	if _, err := h.client.AssertBatch(h.ctx, req); err != nil {
		h.env.Logger().Errorf("Request failed: %s\n", err)
		return
		// s, _ := status.FromError(err)
		// h.env.Logger().Errorf("STATUS: %s\n", s.Proto().Details[0].Value)
	}
	h.wToSend = make([]workload, 0)
	h.tToSend = make([]traffic, 0)
}
