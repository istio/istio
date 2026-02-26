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

package gatewaycommon

import (
	corev1 "k8s.io/api/core/v1"
	gateway "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config/constants"
)

// ClassInfo holds information about a gateway class
type ClassInfo struct {
	// Controller name for this class
	Controller string
	// ControllerLabel for this class
	ControllerLabel string
	// Description for this class
	Description string
	// The key in the templates to use for this class
	Templates string

	// DefaultServiceType sets the default service type if one is not explicit set
	DefaultServiceType corev1.ServiceType

	// DisableRouteGeneration, if set, will make it so the controller ignores this class.
	DisableRouteGeneration bool

	// SupportsListenerSet declares whether a given class supports ListenerSet
	SupportsListenerSet bool

	// DisableNameSuffix, if set, will avoid appending -<class> to names
	DisableNameSuffix bool

	// AddressType is the default address type to report
	AddressType gateway.AddressType
}

// ClassInfos contains all known gateway class infos.
var ClassInfos = GetClassInfos()

// BuiltinClasses contains the built-in gateway classes.
var BuiltinClasses = GetBuiltinClasses()

// AgentgatewayClassInfos contains class infos specific to agentgateway.
var AgentgatewayClassInfos = GetAgentgatewayClassInfos()

// AgentgatewayBuiltinClasses contains built-in classes specific to agentgateway.
var AgentgatewayBuiltinClasses = GetAgentgatewayBuiltinClasses()

// GetBuiltinClasses returns the built-in gateway class mappings.
func GetBuiltinClasses() map[gateway.ObjectName]gateway.GatewayController {
	res := map[gateway.ObjectName]gateway.GatewayController{
		gateway.ObjectName(features.GatewayAPIDefaultGatewayClass): gateway.GatewayController(features.ManagedGatewayController),
	}

	if features.MultiNetworkGatewayAPI {
		res[constants.RemoteGatewayClassName] = constants.UnmanagedGatewayController
	}

	if features.EnableAgentgateway {
		res[constants.AgentgatewayClassName] = constants.ManagedAgentgatewayController
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

// GetClassInfos returns the full mapping of gateway controller to ClassInfo.
func GetClassInfos() map[gateway.GatewayController]ClassInfo {
	m := map[gateway.GatewayController]ClassInfo{
		gateway.GatewayController(features.ManagedGatewayController): {
			Controller:          features.ManagedGatewayController,
			Description:         "The default Istio GatewayClass",
			Templates:           "kube-gateway",
			DefaultServiceType:  corev1.ServiceTypeLoadBalancer,
			AddressType:         gateway.HostnameAddressType,
			ControllerLabel:     constants.ManagedGatewayControllerLabel,
			SupportsListenerSet: true,
		},
	}

	if features.MultiNetworkGatewayAPI {
		m[constants.UnmanagedGatewayController] = ClassInfo{
			// This represents a gateway that our control plane cannot discover directly via the API server.
			// We shouldn't generate Istio resources for it. We aren't programming this gateway.
			Controller:             constants.UnmanagedGatewayController,
			Description:            "Remote to this cluster. Does not deploy or affect configuration.",
			DisableRouteGeneration: true,
			AddressType:            gateway.HostnameAddressType,
			SupportsListenerSet:    false,
		}
	}
	if features.EnableAmbientWaypoints {
		m[constants.ManagedGatewayMeshController] = ClassInfo{
			Controller:          constants.ManagedGatewayMeshController,
			Description:         "The default Istio waypoint GatewayClass",
			Templates:           "waypoint",
			DisableNameSuffix:   true,
			DefaultServiceType:  corev1.ServiceTypeClusterIP,
			SupportsListenerSet: false,
			// Report both. Consumers of the gateways can choose which they want.
			// In particular, Istio across different versions consumes different address types, so this retains compat
			AddressType:     "",
			ControllerLabel: constants.ManagedGatewayMeshControllerLabel,
		}
	}
	if features.EnableAgentgateway {
		m[constants.ManagedAgentgatewayController] = ClassInfo{
			Controller:          constants.ManagedAgentgatewayController,
			Description:         "Istio with Agentgateway.",
			Templates:           "agentgateway",
			DefaultServiceType:  corev1.ServiceTypeLoadBalancer,
			AddressType:         gateway.HostnameAddressType,
			ControllerLabel:     constants.ManagedGatewayControllerLabel,
			SupportsListenerSet: true,
		}
	}

	if features.EnableAmbientMultiNetwork {
		m[constants.ManagedGatewayEastWestController] = ClassInfo{
			Controller:         constants.ManagedGatewayEastWestController,
			Description:        "The default GatewayClass for Istio East West Gateways",
			Templates:          "waypoint",
			DisableNameSuffix:  true,
			DefaultServiceType: corev1.ServiceTypeLoadBalancer,
			AddressType:        "",
			ControllerLabel:    constants.ManagedGatewayEastWestControllerLabel,
		}
	}
	return m
}

// GetAgentgatewayClassInfos returns class infos for the agentgateway controller.
func GetAgentgatewayClassInfos() map[gateway.GatewayController]ClassInfo {
	m := map[gateway.GatewayController]ClassInfo{}
	if features.EnableAgentgateway {
		m[constants.ManagedAgentgatewayController] = ClassInfo{
			Controller:          constants.ManagedAgentgatewayController,
			Description:         "Istio with Agentgateway.",
			Templates:           "agentgateway",
			DefaultServiceType:  corev1.ServiceTypeLoadBalancer,
			AddressType:         gateway.HostnameAddressType,
			ControllerLabel:     constants.ManagedGatewayControllerLabel,
			SupportsListenerSet: true,
		}
	}
	return m
}

// GetAgentgatewayBuiltinClasses returns built-in class mappings for the agentgateway controller.
func GetAgentgatewayBuiltinClasses() map[gateway.ObjectName]gateway.GatewayController {
	res := map[gateway.ObjectName]gateway.GatewayController{}
	if features.EnableAgentgateway {
		res[constants.AgentgatewayClassName] = constants.ManagedAgentgatewayController
	}
	return res
}
