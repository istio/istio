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

package ingress

import (
	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/runtime/flow"
	"istio.io/istio/pkg/log"
)

var scope = log.RegisterScope("pipeline", "Galley processing pipeline", 0)

// AddIngressPipeline attaches Ingress processing components to the given Pipeline builder.
func AddIngressPipeline(cfg *Config, b *flow.PipelineBuilder) {
	localCfg := *cfg

	// Collection for collecting gateways
	addGatewayPipeline(b)
	addVirtualServicePipeline(&localCfg, b)
}

func addGatewayPipeline(b *flow.PipelineBuilder) {

	// table that will store enveloped gateway resources.
	t := flow.NewTable()

	// Accumulator that will do direct transformation of the resource to a Gateway and store.
	a := flow.NewAccumulator(t, toEnvelopedGateway)

	// View for exposing the transformed gateway resources for snapshotting
	v := flow.NewTableView(metadata.Gateway.TypeURL, t, nil)

	// Register the accumulator for listening to ingress changes.
	b.AddHandler(metadata.IngressSpec.TypeURL, a)

	// Expose the gateway view for snapshotting.
	b.AddView(v)
}

func addVirtualServicePipeline(cfg *Config, b *flow.PipelineBuilder) {
	// Create a table to store incoming ingresses
	t := flow.NewEntryTable()

	// Create a view on top of the ingress table that generate the virtual services.
	vsView := &virtualServiceView{
		table:  t,
		config: cfg,
	}

	// Handle incoming ingress resources and accumulate them to the table
	b.AddHandler(metadata.IngressSpec.TypeURL, t)

	// Add the view to expose the Virtual Services.
	b.AddView(vsView)
}
