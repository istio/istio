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
	mesh "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	tb "istio.io/istio/pilot/pkg/trustbundle"
	"istio.io/istio/pkg/util/gogo"
)

// PcdsGenerator generates ECDS configuration.
type PcdsGenerator struct {
	Server      *DiscoveryServer
	TrustBundle *tb.TrustBundle
}

var _ model.XdsResourceGenerator = &PcdsGenerator{}

func pcdsNeedsPush(req *model.PushRequest) bool {
	if req == nil {
		return true
	}

	if !req.Full {
		// PCDS only handles full push
		return false
	}

	// If none set, we will always push
	if len(req.ConfigsUpdated) == 0 {
		return true
	}
	// TODO: This needs to be optimized
	return true
}

// Generate returns ProxyConfig protobuf containing TrustBundle for given proxy
func (e *PcdsGenerator) Generate(proxy *model.Proxy, push *model.PushContext, w *model.WatchedResource, req *model.PushRequest) (model.Resources, error) {
	if !pcdsNeedsPush(req) {
		return nil, nil
	}
	if e.TrustBundle == nil {
		return nil, nil
	}
	pc := &mesh.ProxyConfig{
		CaCertificatesPem: e.TrustBundle.GetTrustBundle(),
	}
	return model.Resources{gogo.MessageToAny(pc)}, nil
}
