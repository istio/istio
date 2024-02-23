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

package multicluster

import (
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/kube"
)

type Fake struct {
	handlers []handler
}

func (f *Fake) registerHandler(h handler) {
	f.handlers = append(f.handlers, h)
}

func (f *Fake) Add(id cluster.ID, client kube.Client, stop chan struct{}) {
	for _, handler := range f.handlers {
		handler.clusterAdded(&Cluster{
			ID:            id,
			Client:        client,
			kubeConfigSha: [32]byte{},
			stop:          stop,
		})
	}
}

func (f *Fake) Delete(id cluster.ID) {
	for _, handler := range f.handlers {
		handler.clusterDeleted(id)
	}
}

func NewFakeController() *Fake {
	return &Fake{}
}
