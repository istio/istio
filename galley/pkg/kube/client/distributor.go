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

package client

import (
	"gopkg.in/yaml.v2"
	"istio.io/istio/galley/pkg/api/distrib"
	"istio.io/istio/galley/pkg/kube"
	"istio.io/istio/galley/pkg/runtime"
	"istio.io/istio/pkg/log"
)

type Distributor struct {
	k kube.Kube
}

var _ runtime.Distributor = &Distributor{}

func NewDistributor(k kube.Kube) *Distributor {
	return &Distributor{
		k: k,
	}
}

func (d *Distributor) Initialize() error {
	return nil
}

func (d *Distributor) Start() {
	log.Info("Start")

}

func (d *Distributor) Shutdown() {

}

func (d *Distributor) Dispatch(config *distrib.MixerConfig) error {
	b, _ := yaml.Marshal(config)
	log.Infof("Dispatch: %v", string(b))
	return nil
}
