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

// Package transforms contains basic processing building blocks that can be incorporated into bigger/self-contained
// processing pipelines.
package transforms

import (
	"istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/processing"
)

//TODO: doc comments everywhere
type Info struct {
	inputs   collection.Names //TODO: Combine these into a struct?
	outputs  collection.Names
	createFn func(processing.ProcessorOptions) event.Transformer
}

func NewInfo(inputs, outputs collection.Names, createFn func(processing.ProcessorOptions) event.Transformer) *Info {
	return &Info{
		inputs:   inputs,
		outputs:  outputs,
		createFn: createFn,
	}
}

func (i *Info) Inputs() collection.Names {
	return i.inputs
}

func (i *Info) Outputs() collection.Names {
	return i.outputs
}

func (i *Info) Create(o processing.ProcessorOptions) event.Transformer {
	return i.createFn(o)
}

type Infos []*Info

func (t Infos) Create(o processing.ProcessorOptions) []event.Transformer {
	xforms := make([]event.Transformer, 0)
	for _, i := range t {
		xforms = append(xforms, i.Create(o))
	}
	return xforms
}

// TODO: Singleton registry, so that transformer objects register functions to bootstrap themselves in init()
// Still need to trigger the registrations somewhere, though! Maybe as a lazy getter?
// var transformerInfo []*Info
// func Register(fn func(m *schema.Metadata) []Info) {
// 	//TODO
// }
