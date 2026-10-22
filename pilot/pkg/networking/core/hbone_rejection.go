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

// Required on application clusters using the shim transport. The adapter handles
// permanent CONNECT rejections for HTTP and TCP consumers, and passes through
// ordinary transport failures, including failures of non-HBONE transport matches.
func applyHBONERejectionHandling(c *cluster.Cluster) {
	c.UpstreamConfig = &core.TypedExtensionConfig{
		Name:        util.HBONERejectionPool,
		TypedConfig: protoconv.TypedStructWithFields(util.HBONERejectionPoolType, map[string]any{}),
	}
}
