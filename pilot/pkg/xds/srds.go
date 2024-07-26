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
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	alifeatures "istio.io/istio/pkg/ali/features"
	"istio.io/istio/pkg/config/schema/kind"
)

type SrdsGenerator struct {
	Server *DiscoveryServer
}

var _ model.XdsResourceGenerator = &SrdsGenerator{}

// Map of all configs that do not impact SRDS
var skippedSrdsConfigs = map[kind.Kind]struct{}{
	kind.WorkloadEntry: {},
	kind.WorkloadGroup: {},
	// Delete by ingress
	// When the authn and authz filter changes, we should
	// rebuild srds to populate host-scoped and route-scoped filters.
	// gvk.AuthorizationPolicy:   {},
	// End delete by ingress
	kind.RequestAuthentication: {},
	kind.PeerAuthentication:    {},
	kind.Secret:                {},
}

func srdsNeedsPush(req *model.PushRequest) bool {
	if !alifeatures.EnableScopedRDS {
		return false
	}
	if req == nil {
		return true
	}
	if !req.Full {
		// SRDS only handles full push
		return false
	}
	// If none set, we will always push
	if len(req.ConfigsUpdated) == 0 {
		return true
	}
	for config := range req.ConfigsUpdated {
		if _, f := skippedSrdsConfigs[config.Kind]; !f {
			return true
		}
	}
	return false
}

func (s SrdsGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	if !srdsNeedsPush(req) {
		return nil, model.DefaultXdsLogDetails, nil
	}

	scopedRoutes := s.Server.ConfigGenerator.BuildScopedRoutes(proxy, req.Push)
	resources := model.Resources{}
	for _, sr := range scopedRoutes {
		resources = append(resources, &discovery.Resource{
			Name:     sr.Name,
			Resource: protoconv.MessageToAny(sr),
		})
	}
	return resources, model.DefaultXdsLogDetails, nil
}
