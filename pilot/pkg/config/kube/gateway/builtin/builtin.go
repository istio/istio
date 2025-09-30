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

package builtin

import (
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config/constants"
)

var BuiltinClasses = GetBuiltinClasses()

func GetBuiltinClasses() map[gateway.ObjectName]gateway.GatewayController {
	res := map[gateway.ObjectName]gateway.GatewayController{
		gateway.ObjectName(features.GatewayAPIDefaultGatewayClass): gateway.GatewayController(features.ManagedGatewayController),
	}

	if features.MultiNetworkGatewayAPI {
		res[constants.RemoteGatewayClassName] = constants.UnmanagedGatewayController
	}

	if features.EnableAmbientWaypoints {
		res[constants.WaypointGatewayClassName] = constants.ManagedGatewayMeshController
	}

	// N.B Ambient e/w gateways are just fancy waypoints, but we want a different
	// GatewayClass for better UX
	if features.EnableAmbientMultiNetwork {
		res[constants.EastWestGatewayClassName] = constants.ManagedGatewayEastWestController
	}
	return res
}
