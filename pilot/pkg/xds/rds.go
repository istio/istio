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

type RdsGenerator struct {
	Server *DiscoveryServer
}

var _ model.XdsResourceGenerator = &RdsGenerator{}

var rdsPushMaps = map[resource.GroupVersionKind]struct{}{
	gvk.VirtualService:  {},
	gvk.ServiceEntry:    {},
	gvk.DestinationRule: {},
	gvk.Sidecar:         {},
	gvk.Gateway:         {},
	gvk.EnvoyFilter:     {},
}

func rdsNeedsPush(updates model.XdsUpdates) bool {
	// If none set, we will always push
	if len(updates) == 0 {
		return true
	}
	for config := range updates {
		if _, f := rdsPushMaps[config.Kind]; f {
			return true
		}
	}
	return false
}

func (c RdsGenerator) Generate(proxy *model.Proxy, push *model.PushContext, w *model.WatchedResource, updates model.XdsUpdates) model.Resources {
	if !rdsNeedsPush(updates) {
		return nil
	}
	rawRoutes := c.Server.ConfigGenerator.BuildHTTPRoutes(proxy, push, w.ResourceNames)
	resources := model.Resources{}
	for _, c := range rawRoutes {
		resources = append(resources, util.MessageToAny(c))
	}
	return resources
}
