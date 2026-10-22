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

package core

import (
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"

	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/util/protoconv"
)

// Apply only after the service has opted into the shim transport. The adapter
// supports HTTP and TCP consumers and passes through ordinary transport failures.
func (cb *ClusterBuilder) applyHBONERejectionHandling(c *cluster.Cluster) {
	if !cb.hboneRejectionHandling || c.UpstreamConfig != nil {
		return
	}
	c.UpstreamConfig = &core.TypedExtensionConfig{
		Name:        util.HBONERejectionPool,
		TypedConfig: protoconv.TypedStructWithFields(util.HBONERejectionPoolType, map[string]any{}),
	}
}
