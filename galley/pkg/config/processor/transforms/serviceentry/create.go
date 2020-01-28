// Copyright 2019 Istio Authors
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

package serviceentry

import (
	"istio.io/istio/galley/pkg/config/processing"
	xformer "istio.io/istio/galley/pkg/config/processing/transformer"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

// GetProviders creates transformer providers for Synthetic Service entries
func GetProviders() xformer.Providers {
	inputs := collection.NewSchemasBuilder().
		MustAdd(collections.K8SCoreV1Endpoints).
		MustAdd(collections.K8SCoreV1Nodes).
		MustAdd(collections.K8SCoreV1Pods).
		MustAdd(collections.K8SCoreV1Services).
		Build()

	outputs := collection.NewSchemasBuilder().
		MustAdd(collections.IstioNetworkingV1Alpha3SyntheticServiceentries).
		Build()

	createFn := func(o processing.ProcessorOptions) event.Transformer {
		return &serviceEntryTransformer{
			inputs:  inputs,
			outputs: outputs,
			options: o,
		}
	}
	return []xformer.Provider{xformer.NewProvider(inputs, outputs, createFn)}
}
