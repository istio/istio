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

// nolint: gocritic
package ambient

import (
	"net/netip"

	"sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
)

type Waypoint struct {
	krt.Named

	ForServiceAccount string
	Addresses         []netip.Addr
}

func (w Waypoint) ResourceName() string {
	return w.GetNamespace() + "/" + w.GetName()
}

func WaypointsCollection(Gateways krt.Collection[*v1beta1.Gateway]) krt.Collection[Waypoint] {
	return krt.NewCollection(Gateways, func(ctx krt.HandlerContext, gateway *v1beta1.Gateway) *Waypoint {
		if gateway.Spec.GatewayClassName != constants.WaypointGatewayClassName {
			// Not a gateway
			return nil
		}
		if len(gateway.Status.Addresses) == 0 {
			// gateway.Status.Addresses should only be populated once the Waypoint's deployment has at least 1 ready pod, it should never be removed after going ready
			// ignore Kubernetes Gateways which aren't waypoints
			return nil
		}
		sa := gateway.Annotations[constants.WaypointServiceAccount]
		return &Waypoint{
			Named:             krt.NewNamed(gateway),
			ForServiceAccount: sa,
			Addresses:         getGatewayAddrs(gateway),
		}
	}, krt.WithName("Waypoints"))
}

func getGatewayAddrs(gw *v1beta1.Gateway) []netip.Addr {
	// Currently, we only look at one address. Probably this should be made more robust
	ip, err := netip.ParseAddr(gw.Status.Addresses[0].Value)
	if err == nil {
		return []netip.Addr{ip}
	}
	log.Errorf("Unable to parse IP address in status of %v/%v/%v", gvk.KubernetesGateway, gw.Namespace, gw.Name)
	return nil
}
