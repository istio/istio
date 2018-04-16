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

	"cloud.google.com/go/contextgraph/apiv1alpha1"
	"google.golang.org/api/option"

	"istio.io/istio/mixer/adapter/contextgraph/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/edge"
)

type (
	builder struct {
		projectID string
		apiKey    string
		saPath    string
		zone      string
		cluster   string
	}
	handler struct {
		client            *contextgraph.Client
		env               adapter.Env
		projectID         string
		gcpContainer      string
		workloadContainer string
		clusterContainer  string
		wCache            workloadCache
		tCache            trafficCache
		workloads         chan workload
		traffics          chan traffic
		wToSend           []workload
		tToSend           []traffic
		sendTick          *time.Ticker
		quit              chan int
		ctx               context.Context
	}
)

// ensure types implement the requisite interfaces
var _ edge.HandlerBuilder = &builder{}
var _ edge.Handler = &handler{}

///////////////// Configuration-time Methods ///////////////

// adapter.HandlerBuilder#Build
func (b *builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {

	h := &handler{
		env:               env,
		projectID:         b.projectID,
		gcpContainer:      fmt.Sprintf("//cloudresourcemanager.googleapis.com/projects/%s", b.projectID),
		workloadContainer: fmt.Sprintf("//istio.io/projects/%s/workloads", b.projectID),
		clusterContainer:  fmt.Sprintf("//container.googleapis.com/projects/%s/zones/%s/clusters/%s", b.projectID, b.zone, b.cluster),
		wCache:            newWorkloadCache(env.Logger()),
		tCache:            newTrafficCache(),
		workloads:         make(chan workload),
		traffics:          make(chan traffic),
		wToSend:           make([]workload, 0),
		tToSend:           make([]traffic, 0),
		sendTick:          time.NewTicker(30 * time.Second),
		quit:              make(chan int),
		ctx:               ctx,
	}

	var opts option.ClientOption
	if b.saPath != "" {
		opts = option.WithCredentialsFile(b.saPath)
	} else if b.apiKey != "" {
		opts = option.WithAPIKey(b.apiKey)
	}
	var err error
	h.client, err = contextgraph.NewClient(ctx, opts)
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

// edge.HandlerBuilder#SetEdgeTypes
func (b *builder) SetEdgeTypes(types map[string]*edge.Type) {
}

////////////////// Request-time Methods //////////////////////////

// edge.Handler#HandleEdge
func (h *handler) HandleEdge(ctx context.Context, insts []*edge.Instance) error {
	for _, i := range insts {
		source := newWorkload(i.SourceOwner, i.SourceWorkloadName, i.SourceWorkloadNamespace, i.Timestamp)
		dest := newWorkload(i.DestinationOwner, i.DestinationWorkloadName, i.DestinationWorkloadNamespace, i.Timestamp)
		//h.env.Logger().Debugf("Connection Reported: %v to %v", source.name, dest.name)
		h.workloads <- source
		h.workloads <- dest

		t := newTraffic(source, dest, i.ContextProtocol, i.ApiProtocol, i.Timestamp)
		h.traffics <- t
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
			edge.TemplateName,
		},
		NewBuilder: func() adapter.HandlerBuilder { return &builder{} },
		DefaultConfig: &config.Params{
			ProjectId: "",
		},
	}
}
