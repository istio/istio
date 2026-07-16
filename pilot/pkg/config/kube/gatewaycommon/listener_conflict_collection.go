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
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
)

// GatewayListenerConflicts holds listener conflict results for one Gateway.
type GatewayListenerConflicts struct {
	Gateway   types.NamespacedName
	Conflicts map[types.NamespacedName]map[gatewayv1.SectionName]gatewayv1.ListenerConditionReason
}

func (g GatewayListenerConflicts) ResourceName() string {
	return g.Gateway.String()
}

func (g GatewayListenerConflicts) Equals(o GatewayListenerConflicts) bool {
	if g.Gateway != o.Gateway {
		return false
	}
	if len(g.Conflicts) != len(o.Conflicts) {
		return false
	}
	for k, v := range g.Conflicts {
		ov, ok := o.Conflicts[k]
		if !ok || len(v) != len(ov) {
			return false
		}
		for lk, lv := range v {
			if ov[lk] != lv {
				return false
			}
		}
	}
	return true
}

// ConflictsForGateway returns conflict reasons for listeners on the given Gateway.
func (g GatewayListenerConflicts) ConflictsForGateway(gw *gatewayv1.Gateway) map[gatewayv1.SectionName]gatewayv1.ListenerConditionReason {
	if gw == nil || g.Conflicts == nil {
		return nil
	}
	return g.Conflicts[types.NamespacedName{Namespace: gw.Namespace, Name: gw.Name}]
}

// ConflictsFor returns conflict reasons for listeners on the given ListenerSet.
func (g GatewayListenerConflicts) ConflictsFor(ls *gatewayv1.ListenerSet) map[gatewayv1.SectionName]gatewayv1.ListenerConditionReason {
	if ls == nil || g.Conflicts == nil {
		return nil
	}
	return g.Conflicts[types.NamespacedName{Namespace: ls.Namespace, Name: ls.Name}]
}

// GatewayListenerConflictCollection computes listener conflicts once per Gateway.
func GatewayListenerConflictCollection(
	gateways krt.Collection[*gatewayv1.Gateway],
	listenerSets krt.Collection[*gatewayv1.ListenerSet],
	namespaces krt.Collection[*corev1.Namespace],
	fetchClass GatewayClassFetcher,
	opts krt.OptionsBuilder,
) krt.Collection[GatewayListenerConflicts] {
	listenerSetsByGateway := krt.NewIndex(listenerSets, "gatewayParent", func(o *gatewayv1.ListenerSet) []types.NamespacedName {
		pns, name := ListenerSetParentKey(o)
		return []types.NamespacedName{{Namespace: pns, Name: name}}
	})

	return krt.NewCollection(gateways, func(ctx krt.HandlerContext, gw *gatewayv1.Gateway) *GatewayListenerConflicts {
		gwNN := config.NamespacedName(gw)
		class := fetchClass(ctx, gw.Spec.GatewayClassName)
		if class == nil {
			return &GatewayListenerConflicts{Gateway: gwNN, Conflicts: map[types.NamespacedName]map[gatewayv1.SectionName]gatewayv1.ListenerConditionReason{}}
		}
		classInfo, ok := ClassInfos[class.Controller]
		if !ok || !classInfo.SupportsListenerSet {
			return &GatewayListenerConflicts{Gateway: gwNN, Conflicts: map[types.NamespacedName]map[gatewayv1.SectionName]gatewayv1.ListenerConditionReason{}}
		}

		attached := listenerSetsByGateway.Fetch(ctx, gwNN)
		eligible := EligibleListenerSetsForConflict(attached, func(ns string) bool {
			return NamespaceAcceptedByAllowListeners(ns, gw, func(s string) *corev1.Namespace {
				return ptr.Flatten(krt.FetchOne(ctx, namespaces, krt.FilterKey(s)))
			})
		})
		return &GatewayListenerConflicts{
			Gateway:   gwNN,
			Conflicts: ComputeGatewayListenerConflicts(gw, eligible),
		}
	}, opts.WithName("GatewayListenerConflicts")...)
}
