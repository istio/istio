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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pilot/pkg/config/kube/gatewaycommon"
	"istio.io/istio/pilot/pkg/serviceregistry/ambient"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
)

// WaypointServiceBinding maps a fronted service to the AGW waypoint Gateway that fronts it.
type WaypointServiceBinding struct {
	// ServiceKey is the NamespacedName of the fronted service
	ServiceKey types.NamespacedName
	// WaypointGateway is the NamespacedName of the waypoint Gateway
	WaypointGateway types.NamespacedName
}

func (w WaypointServiceBinding) ResourceName() string {
	return w.ServiceKey.String()
}

func (w WaypointServiceBinding) Equals(other WaypointServiceBinding) bool {
	return w.ServiceKey == other.ServiceKey && w.WaypointGateway == other.WaypointGateway
}

// BuildWaypointServiceBindings creates a collection mapping services to their AGW waypoint gateways.
// For each k8s Service with a use-waypoint label (or inheriting from namespace) pointing to an
// agentgateway-waypoint class Gateway, a WaypointServiceBinding is created.
func BuildWaypointServiceBindings(
	services krt.Collection[*corev1.Service],
	namespaces krt.Collection[*corev1.Namespace],
	gateways krt.Collection[*gatewayv1.Gateway],
	gatewayClasses krt.Collection[gatewaycommon.GatewayClass],
	opts krt.OptionsBuilder,
) krt.Collection[WaypointServiceBinding] {
	return krt.NewCollection(services, func(ctx krt.HandlerContext, svc *corev1.Service) *WaypointServiceBinding {
		// check if the service or its namespace has the use-waypoint label
		wpRef := resolveUseWaypoint(ctx, svc.ObjectMeta, namespaces)
		if wpRef == nil {
			return nil
		}

		// Check if the referenced gateway exists. Ignore otherwise
		gw := ptr.Flatten(krt.FetchOne(ctx, gateways, krt.FilterKey(wpRef.ResourceName())))
		if gw == nil {
			return nil
		}

		// Check that the gateway has an AGW waypoint class. Ignore otherwise
		class := gatewaycommon.FetchAgentgatewayClass(ctx, gatewayClasses, gw.Spec.GatewayClassName)
		if class == nil || class.Controller != constants.ManagedAgentgatewayWaypointController {
			return nil
		}

		return &WaypointServiceBinding{
			ServiceKey:      types.NamespacedName{Namespace: svc.Namespace, Name: svc.Name},
			WaypointGateway: types.NamespacedName{Namespace: wpRef.Namespace, Name: wpRef.Name},
		}
	}, opts.WithName("WaypointServiceBindings")...)
}

// resolveUseWaypoint looks up the use-waypoint label on a service or its namespace
// and returns the referenced waypoint gateway, if any.
func resolveUseWaypoint(
	ctx krt.HandlerContext,
	meta metav1.ObjectMeta,
	namespaces krt.Collection[*corev1.Namespace],
) *krt.Named {
	// Check object labels first
	// These labels take precedence over namespace labels
	wp, isNone := ambient.GetUseWaypoint(meta, meta.Namespace)
	if isNone {
		return nil
	}
	if wp != nil {
		return wp
	}

	// Fall back to namespace labels
	ns := ptr.Flatten(krt.FetchOne(ctx, namespaces, krt.FilterKey(meta.Namespace)))
	if ns == nil {
		return nil
	}
	wp, _ = ambient.GetUseWaypoint(ns.ObjectMeta, meta.Namespace)
	return wp
}
