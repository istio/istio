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

//go:generate $GOPATH/src/istio.io/istio/bin/mixer_codegen.sh -f mixer/adapter/contextgraph/config/config.proto

// Package contextgraph adapter for Stackdriver Context API.
package contextgraph

import (
	"context"
	"fmt"
	"time"

	contextgraph "cloud.google.com/go/contextgraph/apiv1alpha1"
	"google.golang.org/api/option"

	"istio.io/istio/mixer/adapter/contextgraph/config"
	"istio.io/istio/mixer/pkg/adapter"
	edgepb "istio.io/istio/mixer/template/edge"
)

type (
	builder struct {
		projectID string
		endpoint  string
		apiKey    string
		saPath    string
		zone      string
		cluster   string
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
		ctx            context.Context
	}
)

// ensure types implement the requisite interfaces
var _ edgepb.HandlerBuilder = &builder{}
var _ edgepb.Handler = &handler{}

///////////////// Configuration-time Methods ///////////////

// adapter.HandlerBuilder#Build
func (b *builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {

	// TODO: meshUID should come from an attribute when
	// multi-cluster Istio is supported. Currently we assume each
	// cluster is its own mesh.
	h := &handler{
		env:            env,
		projectID:      b.projectID,
		meshUID:        fmt.Sprintf("%s/%s/%s", b.projectID, b.zone, b.cluster),
		zone:           b.zone,
		cluster:        b.cluster,
		entityCache:    newEntityCache(env.Logger()),
		edgeCache:      newEdgeCache(env.Logger()),
		traffics:       make(chan trafficAssertion),
		entitiesToSend: make([]entity, 0),
		edgesToSend:    make([]edge, 0),
		sendTick:       time.NewTicker(30 * time.Second),
		quit:           make(chan int),
		ctx:            ctx,
	}

	var opts []option.ClientOption
	if b.saPath != "" {
		opts = append(opts, option.WithCredentialsFile(b.saPath))
	} else if b.apiKey != "" {
		opts = append(opts, option.WithAPIKey(b.apiKey))
	}
	if b.endpoint != "" {
		opts = append(opts, option.WithEndpoint(b.endpoint))
	}
	var err error
	h.client, err = contextgraph.NewClient(ctx, opts...)
	if err != nil {
		return nil, err
	}

	env.ScheduleDaemon(h.cacheAndSend)

	return h, nil
}

// adapter.HandlerBuilder#SetAdapterConfig
func (b *builder) SetAdapterConfig(cfg adapter.Config) {
	c := cfg.(*config.Params)
	b.projectID = c.ProjectId
	b.zone = c.Zone
	b.cluster = c.Cluster
	b.endpoint = c.Endpoint
	switch x := c.Creds.(type) {
	case *config.Params_AppCredentials:
	case *config.Params_ApiKey:
		b.apiKey = x.ApiKey
	case *config.Params_ServiceAccountPath:
		b.saPath = x.ServiceAccountPath
	}
}

// adapter.HandlerBuilder#Validate
func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	// FIXME Error on empty projid, etc
	return
}

// edgepb.HandlerBuilder#SetEdgeTypes
func (b *builder) SetEdgeTypes(types map[string]*edgepb.Type) {
}

////////////////// Request-time Methods //////////////////////////

// edgepb.Handler#HandleEdge
func (h *handler) HandleEdge(ctx context.Context, insts []*edgepb.Instance) error {
	for _, i := range insts {
		source := workloadInstance{h.meshUID, h.projectID, h.projectID, h.zone, h.cluster, i.SourceUid, i.SourceOwner, i.SourceWorkloadName, i.SourceWorkloadNamespace}
		destination := workloadInstance{h.meshUID, h.projectID, h.projectID, h.zone, h.cluster, i.DestinationUid, i.DestinationOwner, i.DestinationWorkloadName, i.DestinationWorkloadNamespace}
		h.traffics <- trafficAssertion{source, destination, i.ContextProtocol, i.ApiProtocol, i.Timestamp}

		//h.env.Logger().Debugf("Connection Reported: %v to %v", source.name, dest.name)
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

// GetInfo returns the adapter.Info specific to this adapter.
func GetInfo() adapter.Info {
	return adapter.Info{
		Name:        "contextgraph",
		Description: "Sends graph to Stackdriver Context API",
		SupportedTemplates: []string{
			edgepb.TemplateName,
		},
		NewBuilder: func() adapter.HandlerBuilder { return &builder{} },
		DefaultConfig: &config.Params{
			ProjectId: "",
		},
	}
}
