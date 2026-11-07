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
	"google.golang.org/protobuf/types/known/anypb"

	"istio.io/istio/pilot/pkg/networking/util"
)

// Required on both application clusters using the adapter transport and their HBONE
// origination clusters. The pool signals CONNECT readiness from tunneling callbacks
// and handles permanent rejections for HTTP and TCP application consumers. Shared
// HBONE clusters pass callers without CONNECT-wait state through to the stock pool.
func applyHBONEConnectWaitPool(c *cluster.Cluster) {
	// The router resolves this config for every upstream request attempt. Use a
	// direct, empty Any to avoid converting a TypedStruct on each attempt.
	c.UpstreamConfig = &core.TypedExtensionConfig{
		Name:        util.HBONERejectionPool,
		TypedConfig: &anypb.Any{TypeUrl: util.HBONERejectionPoolType},
	}
}
