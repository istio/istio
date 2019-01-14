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
	"istio.io/istio/galley/pkg/runtime/processing"
	"istio.io/istio/pkg/log"
)

var scope = log.RegisterScope("processor", "Galley data processing flow", 0)

// AddProcessor attaches Ingress processing components to the given Graph builder.
func AddProcessor(cfg *Config, b *processing.GraphBuilder) {
	localCfg := *cfg

	// Collection for collecting gateways
	addGatewayPipeline(b)
	addVirtualServicePipeline(&localCfg, b)
}

func addGatewayPipeline(b *processing.GraphBuilder) {
	p := processing.NewStoredProjection(metadata.Gateway.TypeURL)
	g := &gwConverter{
		 p: p,
	}
	b.AddHandler(metadata.IngressSpec.TypeURL, g)
	b.AddProjection(p)
}

func addVirtualServicePipeline(cfg *Config, b *processing.GraphBuilder) {
	v := &vsConverter{
		config: cfg,
	}
	b.AddHandler(metadata.IngressSpec.TypeURL, v)
	b.AddProjection(v)
}
