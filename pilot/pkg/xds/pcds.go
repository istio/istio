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

package xds

import (
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	mesh "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	tb "istio.io/istio/pilot/pkg/trustbundle"
	"istio.io/istio/pilot/pkg/util/protoconv"
)

// PcdsGenerator generates proxy configuration for proxies to consume
type PcdsGenerator struct {
	Server      *DiscoveryServer
	TrustBundle *tb.TrustBundle
}

var _ model.XdsResourceGenerator = &PcdsGenerator{}

func pcdsNeedsPush(req *model.PushRequest) bool {
	if !features.MultiRootMesh {
		return false
	}

	if req == nil {
		return true
	}

	if !req.Full {
		return false
	}

	if len(req.ConfigsUpdated) == 0 {
		// This needs to be better optimized
		return true
	}

	return false
}

// Generate returns ProxyConfig protobuf containing TrustBundle for given proxy
func (e *PcdsGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	if !pcdsNeedsPush(req) {
		return nil, model.DefaultXdsLogDetails, nil
	}
	if e.TrustBundle == nil {
		return nil, model.DefaultXdsLogDetails, nil
	}
	// TODO: For now, only TrustBundle updates are pushed. Eventually, this should push entire Proxy Configuration
	pc := &mesh.ProxyConfig{
		CaCertificatesPem: e.TrustBundle.GetTrustBundle(),
	}
	return model.Resources{&discovery.Resource{Resource: protoconv.MessageToAny(pc)}}, model.DefaultXdsLogDetails, nil
}
