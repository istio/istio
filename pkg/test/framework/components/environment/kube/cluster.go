//  Copyright Istio Authors
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

package kube

import (
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/kube"
)

var _ resource.Cluster = Cluster{}

// Cluster for a Kubernetes cluster. Provides access via a kube.Accessor.
type Cluster struct {
	*kube.Accessor
	filename string
}

func (c Cluster) String() string {
	return c.filename
}

// ClusterOrDefault gets the given cluster as a kube Cluster if available. Otherwise
// defaults to the first Cluster in the Environment.
func ClusterOrDefault(c resource.Cluster, e resource.Environment) Cluster {
	if c == nil {
		return e.(*Environment).KubeClusters[0]
	}
	return c.(Cluster)
}
