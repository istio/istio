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

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
)

// hboneConnectWaitEligible reports whether this application cluster can use CONNECT wait.
// Only EDS services without application TLS or PROXY protocol origination are eligible.
func (cb *ClusterBuilder) hboneConnectWaitEligible(c *cluster.Cluster, service *model.Service, policy *networking.TrafficPolicy) bool {
	// Traffic policy can change the cluster type independently of service resolution.
	if !cb.hboneConnectWait || c.GetType() != cluster.Cluster_EDS {
		return false
	}
	if service == nil || service.Resolution != model.ClientSideLB || policy.GetProxyProtocol() != nil {
		return false
	}
	switch policy.GetTls().GetMode() {
	case networking.ClientTLSSettings_DISABLE, networking.ClientTLSSettings_ISTIO_MUTUAL:
		return true
	default:
		return false
	}
}

// Required on both application clusters using the adapter transport and their HBONE
// origination clusters. The pool signals CONNECT readiness from tunneling callbacks
// and handles permanent rejections for HTTP and TCP application consumers. Shared
// HBONE listeners and clusters pass callers without CONNECT-wait state through to the stock pool.
func applyHBONEConnectWaitPool(c *cluster.Cluster) {
	// The router resolves this config for every upstream request attempt. Use a
	// direct, empty Any to avoid converting a TypedStruct on each attempt.
	c.UpstreamConfig = &core.TypedExtensionConfig{
		Name:        util.HBONEConnectWaitPool,
		TypedConfig: &anypb.Any{TypeUrl: util.HBONEConnectWaitPoolTypeURL},
	}
}
