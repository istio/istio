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

package inmemory

import (
	"istio.io/istio/galley/pkg/model/component"
	"istio.io/istio/galley/pkg/model/distributor"
	"istio.io/istio/pkg/log"
)

var scope = log.RegisterScope("inmemory", "inmemory config distributor/provider", 0)

type Distributor struct {
	Bundles map[component.InstanceId]distributor.Bundle
}

var _ distributor.Interface = &Distributor{}

func NewDistributor() *Distributor {
	return &Distributor{
		Bundles: make(map[component.InstanceId]distributor.Bundle),
	}
}

func (d *Distributor) Initialize() error {
	scope.Infof("Initializing the in-memory distributor")

	return nil
}

func (d *Distributor) Start() {
	scope.Infof("Starting the in-memory distributor")
}

func (d *Distributor) Distribute(b distributor.Bundle) {
	scope.Infof("Distributing bundle: %v", b)

	if scope.DebugEnabled() {
		scope.Debugf("[%v/Manifest] %v", b.Destination(), b.GenerateManifest())
		for i, f := range b.GenerateFragments() {
			scope.Debugf("[%v/Fragment/%d] %v", b.Destination(), i, f)
		}
	}

	d.Bundles[b.Destination()] = b
}

func (d *Distributor) Shutdown() {
	scope.Infof("Shutting down the in-memory distributor")
}
