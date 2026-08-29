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

package agentgateway

import (
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pilot/pkg/config/kube/gatewaycommon"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
)

// WaypointServiceBinding maps a fronted service to an AGW waypoint Gateway that fronts it.
type WaypointServiceBinding struct {
	// ServiceKey is the NamespacedName of the fronted service.
	ServiceKey types.NamespacedName
	// WaypointGateway is the NamespacedName of the waypoint Gateway.
	WaypointGateway types.NamespacedName
}

// ResourceName keys on both the service and the gateway so a service fronted by two AGW waypoints
// (primary + canary) produces two distinct entries in the collection.
func (w WaypointServiceBinding) ResourceName() string {
	return w.ServiceKey.String() + "/" + w.WaypointGateway.String()
}

func (w WaypointServiceBinding) Equals(other WaypointServiceBinding) bool {
	return w.ServiceKey == other.ServiceKey && w.WaypointGateway == other.WaypointGateway
}

// BuildWaypointServiceBindings projects the shared ambient ServiceInfo collection down to
// (k8s Service, AGW waypoint Gateway) pairs. Waypoint resolution — use-waypoint labels, namespace
// inheritance, "none" opt-out, weighted-waypoint canary — is delegated to ambient via
// waypointNames. Here we only keep bindings whose gateway exists and is served by the AGW
// waypoint controller. A service split between a primary and a canary produces one binding per
// AGW-class waypoint; non-AGW waypoints in the pair are dropped.
func BuildWaypointServiceBindings(
	services krt.Collection[model.ServiceInfo],
	gateways krt.Collection[*gatewayv1.Gateway],
	gatewayClasses krt.Collection[gatewaycommon.GatewayClass],
	waypointNames ServiceWaypointResolver,
	opts krt.OptionsBuilder,
) krt.Collection[WaypointServiceBinding] {
	return krt.NewManyCollection(services, func(ctx krt.HandlerContext, svc model.ServiceInfo) []WaypointServiceBinding {
		// Only project k8s Services; ServiceEntry bindings are not yet handled by consumers of this collection.
		if svc.Source.Kind != kind.Service {
			return nil
		}
		if waypointNames == nil {
			return nil
		}
		wpNNs := waypointNames(ctx, svc)
		if len(wpNNs) == 0 {
			return nil
		}
		out := make([]WaypointServiceBinding, 0, len(wpNNs))
		for _, wpNN := range wpNNs {
			gw := ptr.Flatten(krt.FetchOne(ctx, gateways, krt.FilterKey(wpNN.String())))
			if gw == nil {
				continue
			}
			class := gatewaycommon.FetchAgentgatewayClass(ctx, gatewayClasses, gw.Spec.GatewayClassName)
			if class == nil || class.Controller != constants.ManagedAgentgatewayWaypointController {
				continue
			}
			out = append(out, WaypointServiceBinding{
				ServiceKey:      svc.NamespacedName(),
				WaypointGateway: wpNN,
			})
		}
		return out
	}, opts.WithName("WaypointServiceBindings")...)
}
