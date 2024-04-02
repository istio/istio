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
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
)

type Waypoint struct {
	krt.Named

	Addresses []netip.Addr
}

func fetchWaypoint(ctx krt.HandlerContext, Waypoints krt.Collection[Waypoint], Namespaces krt.Collection[*v1.Namespace], o metav1.ObjectMeta) *Waypoint {
	// namespace to be used when the annotation doesn't include a namespace
	fallbackNamespace := o.Namespace
	// try fetching the waypoint defined on the object itself
	wp, isNone := getUseWaypoint(o, fallbackNamespace)
	if isNone {
		// we've got a local override here opting out of waypoint
		return nil
	}
	if wp != nil {
		// plausible the object has a waypoint defined but that waypoint's underlying gateway is not ready, in this case we'd return nil here even if
		// the namespace-defined waypoint is ready and would not be nil... is this OK or should we handle that? Could lead to odd behavior when
		// o was reliant on the namespace waypoint and then get's a use-waypoint annotation added before that gateway is ready.
		// goes from having a waypoint to having no waypoint and then eventually gets a waypoint back
		return krt.FetchOne[Waypoint](ctx, Waypoints, krt.FilterKey(wp.ResourceName()))
	}

	// try fetching the namespace-defined waypoint
	namespace := ptr.OrEmpty[*v1.Namespace](krt.FetchOne[*v1.Namespace](ctx, Namespaces, krt.FilterKey(o.Namespace)))
	// this probably should never be nil. How would o exist in a namespace we know nothing about? maybe edge case of starting the controller or ns delete?
	if namespace != nil {
		// toss isNone, we don't need to know /why/ we got nil
		wpNamespace, _ := getUseWaypoint(namespace.ObjectMeta, fallbackNamespace)
		if wpNamespace != nil {
			return krt.FetchOne[Waypoint](ctx, Waypoints, krt.FilterKey(wpNamespace.ResourceName()))
		}
	}

	// neither o nor it's namespace has a use-waypoint annotation
	return nil
}

// getUseWaypoint takes objectMeta and a defaultNamespace
// it looks for the istio.io/use-waypoint annotation and parses it
// if there is no namespace provided in the annotation the default namespace will be used
// defaultNamespace avoids the need to infer when object meta from a namespace was given
func getUseWaypoint(meta metav1.ObjectMeta, defaultNamespace string) (named *krt.Named, isNone bool) {
	if annotationValue, ok := meta.Annotations[constants.AmbientUseWaypoint]; ok {
		if annotationValue == "#none" || annotationValue == "~" {
			return nil, true
		}
		namespacedName := strings.Split(annotationValue, "/")
		switch len(namespacedName) {
		case 1:
			return &krt.Named{
				Name:      namespacedName[0],
				Namespace: defaultNamespace,
			}, false
		case 2:
			return &krt.Named{
				Name:      namespacedName[1],
				Namespace: namespacedName[0],
			}, false
		default:
			// malformed annotation error
			log.Errorf("%s/%s, has a malformed %s annotation, value found: %s", meta.GetNamespace(), meta.GetName(), constants.AmbientUseWaypoint, annotationValue)
			return nil, false
		}

	}
	return nil, false
}

func (w Waypoint) ResourceName() string {
	return w.GetNamespace() + "/" + w.GetName()
}

func WaypointsCollection(Gateways krt.Collection[*v1beta1.Gateway]) krt.Collection[Waypoint] {
	return krt.NewCollection(Gateways, func(ctx krt.HandlerContext, gateway *v1beta1.Gateway) *Waypoint {
		if len(gateway.Status.Addresses) == 0 {
			// gateway.Status.Addresses should only be populated once the Waypoint's deployment has at least 1 ready pod, it should never be removed after going ready
			// ignore Kubernetes Gateways which aren't waypoints
			return nil
		}
		return &Waypoint{
			Named:     krt.NewNamed(gateway),
			Addresses: getGatewayAddrs(gateway),
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
