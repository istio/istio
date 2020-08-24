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
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/resource"
)

type LdsGenerator struct {
	Server *DiscoveryServer
}

var _ model.XdsResourceGenerator = &LdsGenerator{}

var ldsPushMaps = map[resource.GroupVersionKind]struct{}{
	gvk.VirtualService:        {},
	gvk.ServiceEntry:          {},
	gvk.Gateway:               {},
	gvk.Sidecar:               {},
	gvk.RequestAuthentication: {},
	gvk.EnvoyFilter:           {},
	gvk.PeerAuthentication:    {},
}

func ldsNeedsPush(updates model.XdsUpdates) bool {
	// If none set, we will always push
	if len(updates) == 0 {
		return true
	}
	for config := range updates {
		if _, f := ldsPushMaps[config.Kind]; f {
			return true
		}
	}
	return false
}

func (l LdsGenerator) Generate(proxy *model.Proxy, push *model.PushContext, w *model.WatchedResource, updates model.XdsUpdates) model.Resources {
	if !ldsNeedsPush(updates) {
		return nil
	}
	listeners := l.Server.ConfigGenerator.BuildListeners(proxy, push)
	resources := model.Resources{}
	for _, c := range listeners {
		resources = append(resources, util.MessageToAny(c))
	}
	return resources
}
