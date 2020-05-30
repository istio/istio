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

// Package contextgraph adapter for Stackdriver Context API.
package contextgraph

import (
	"context"
	"fmt"
	"time"

	gax "github.com/googleapis/gax-go"
	"google.golang.org/api/option"

	"istio.io/istio/mixer/adapter/stackdriver/config"
	"istio.io/istio/mixer/adapter/stackdriver/helper"
	contextgraph "istio.io/istio/mixer/adapter/stackdriver/internal/cloud.google.com/go/contextgraph/apiv1alpha1"
	contextgraphpb "istio.io/istio/mixer/adapter/stackdriver/internal/google.golang.org/genproto/googleapis/cloud/contextgraph/v1alpha1"
	"istio.io/istio/mixer/pkg/adapter"
	edgepb "istio.io/istio/mixer/template/edge"
)

type newClientFn func(context.Context, ...option.ClientOption) (*contextgraph.Client, error)
type assertBatchFn func(context.Context, *contextgraphpb.AssertBatchRequest, ...gax.CallOption) (*contextgraphpb.AssertBatchResponse, error)

type (
	builder struct {
		projectID string
		zone      string
		cluster   string
		mg        helper.MetadataGenerator
		newClient newClientFn
		cfg       *config.Params
	}
	handler struct {
		client         *contextgraph.Client
		env            adapter.Env
		projectID      string
		meshUID        string
		zone, cluster  string
		entityCache    *entityCache
		edgeCache      *edgeCache
		traffics       chan trafficAssertion
		entitiesToSend []entity
		edgesToSend    []edge
		sendTick       *time.Ticker
		quit           chan int
		assertBatch    assertBatchFn
	}
)

// ensure types implement the requisite interfaces
var _ edgepb.HandlerBuilder = &builder{}
var _ edgepb.Handler = &handler{}

///////////////// Configuration-time Methods ///////////////

// adapter.HandlerBuilder#Build
func (b *builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {
	opts := helper.ToOpts(b.cfg, env.Logger())

	env.Logger().Debugf("Proj, zone, cluster, opts: %s,%s,%s,%s",
		b.projectID, b.zone, b.cluster, opts)

	meshUID := b.cfg.MeshUid
	if len(meshUID) == 0 {
		// must be a fallback here
		meshUID = fmt.Sprintf("%s/%s/%s", b.projectID, b.zone, b.cluster)
	}

	h := &handler{
		env:            env,
		projectID:      b.projectID,
		meshUID:        meshUID,
		zone:           b.zone,
		cluster:        b.cluster,
		entityCache:    newEntityCache(env.Logger()),
		edgeCache:      newEdgeCache(env.Logger()),
		traffics:       make(chan trafficAssertion),
		entitiesToSend: make([]entity, 0),
		edgesToSend:    make([]edge, 0),
		sendTick:       time.NewTicker(30 * time.Second),
		quit:           make(chan int),
	}

	var err error
	h.client, err = b.newClient(ctx, opts...)
	if err != nil {
		return nil, err
	}
	h.assertBatch = h.client.AssertBatch

	env.ScheduleDaemon(func() { h.cacheAndSend(ctx) })

	return h, nil
}

// adapter.HandlerBuilder#SetAdapterConfig
func (b *builder) SetAdapterConfig(cfg adapter.Config) {
	b.cfg = cfg.(*config.Params)
	b.projectID = b.cfg.ProjectId
	md := b.mg.GenerateMetadata()
	if b.projectID == "" {
		b.projectID = md.ProjectID
	}
	b.zone = md.Location
	b.cluster = md.ClusterName
}

// adapter.HandlerBuilder#Validate
func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	if b.projectID == "" {
		ce = ce.Appendf("project_id", "Project ID not provided and could not be determined")
	}
	return
}

// edgepb.HandlerBuilder#SetEdgeTypes
func (b *builder) SetEdgeTypes(types map[string]*edgepb.Type) {
}

////////////////// Request-time Methods //////////////////////////

// edgepb.Handler#HandleEdge
func (h *handler) HandleEdge(ctx context.Context, insts []*edgepb.Instance) error {
	for _, i := range insts {
		source := workloadInstance{
			h.meshUID,
			h.projectID,
			h.projectID,
			h.zone,
			h.cluster,
			i.SourceUid,
			i.SourceOwner,
			i.SourceWorkloadName,
			i.SourceWorkloadNamespace,
		}
		destination := workloadInstance{
			h.meshUID,
			h.projectID,
			h.projectID,
			h.zone,
			h.cluster,
			i.DestinationUid,
			i.DestinationOwner,
			i.DestinationWorkloadName,
			i.DestinationWorkloadNamespace,
		}
		destinationService := service{
			meshUID:      h.meshUID,
			namespace:    i.DestinationServiceNamespace,
			name:         i.DestinationServiceName,
			istioProject: h.projectID,
		}
		h.traffics <- trafficAssertion{
			source:             source,
			destination:        destination,
			contextProtocol:    i.ContextProtocol,
			apiProtocol:        i.ApiProtocol,
			destinationService: destinationService,
			timestamp:          i.Timestamp,
		}
	}
	return nil
}

// adapter.Handler#Close
func (h *handler) Close() error {
	h.quit <- 0
	h.client.Close()
	return nil
}

////////////////// Bootstrap //////////////////////////

// NewBuilder returns a builder implementing the edge.HandlerBuilder interface.
func NewBuilder(mg helper.MetadataGenerator) edgepb.HandlerBuilder {
	return &builder{
		mg:        mg,
		newClient: contextgraph.NewClient,
	}
}
