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
	"istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/util/sets"
)

type RdsGenerator struct {
	ConfigGenerator core.ConfigGenerator
}

var _ model.XdsResourceGenerator = &RdsGenerator{}

// Map of all configs that do not impact RDS
var skippedRdsConfigs = sets.New[kind.Kind](
	kind.WorkloadEntry,
	kind.WorkloadGroup,
	kind.AuthorizationPolicy,
	kind.RequestAuthentication,
	kind.PeerAuthentication,
	kind.Secret,
	kind.WasmPlugin,
	kind.Telemetry,
	kind.ProxyConfig,
	kind.DNSName,
)

func rdsNeedsPush(req *model.PushRequest, proxy *model.Proxy) bool {
	if res, ok := xdsNeedsPush(req, proxy); ok {
		return res
	}
	if !req.Full {
		return false
	}
	for config := range req.ConfigsUpdated {
		if !skippedRdsConfigs.Contains(config.Kind) {
			return true
		}
	}
	return false
}

func (c RdsGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	if !rdsNeedsPush(req, proxy) {
		return nil, model.DefaultXdsLogDetails, nil
	}
	resources, logDetails := c.ConfigGenerator.BuildHTTPRoutes(proxy, req, w.ResourceNames.UnsortedList())
	return resources, logDetails, nil
}
