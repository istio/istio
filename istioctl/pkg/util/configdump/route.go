// Copyright 2018 Istio Authors
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

package configdump

import (
	"fmt"
	"sort"

	adminapi "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"
	proto "github.com/gogo/protobuf/types"
)

// GetDynamicRouteDump retrieves a route dump with just dynamic active routes in it
func (w *Wrapper) GetDynamicRouteDump(stripVersions bool) (*adminapi.RoutesConfigDump, error) {
	routeDump, err := w.GetRouteConfigDump()
	if err != nil {
		return nil, err
	}
	drc := routeDump.GetDynamicRouteConfigs()
	sort.Slice(drc, func(i, j int) bool {
		return drc[i].RouteConfig.Name < drc[j].RouteConfig.Name
	})
	if stripVersions {
		for i := range drc {
			drc[i].VersionInfo = ""
			drc[i].LastUpdated = nil
		}
	}
	return &adminapi.RoutesConfigDump{DynamicRouteConfigs: drc}, nil
}

// GetRouteConfigDump retrieves the route config dump from the ConfigDump
func (w *Wrapper) GetRouteConfigDump() (*adminapi.RoutesConfigDump, error) {
	if w.Configs == nil {
		return nil, fmt.Errorf("config dump has no route dump")
	}
	routeDumpAny, ok := w.Configs["routes"]
	if !ok {
		return nil, fmt.Errorf("config dump has no route dump")
	}
	routeDump := &adminapi.RoutesConfigDump{}
	err := proto.UnmarshalAny(&routeDumpAny, routeDump)
	if err != nil {
		return nil, err
	}
	return routeDump, nil
}
