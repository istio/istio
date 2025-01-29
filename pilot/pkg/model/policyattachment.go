// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package model

import (
	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/label"
	"istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/ptr"
)

// this can be any type from istio/api that uses these types of selectors
type TargetablePolicy interface {
	GetTargetRef() *v1beta1.PolicyTargetReference
	GetTargetRefs() []*v1beta1.PolicyTargetReference
	GetSelector() *v1beta1.WorkloadSelector
}

// WorkloadPolicyMatcher performs policy selection either using targetRef or label selectors.
// Label selection uses the workload labels.
// TargetRef selection uses either the workload's namespace + the gateway name based on labels,
// or the Services the workload is a part of.
type WorkloadPolicyMatcher struct {
	WorkloadNamespace string
	WorkloadLabels    labels.Instance
	IsWaypoint        bool
	Services          []ServiceInfoForPolicyMatcher
	RootNamespace     string
}

type ServiceInfoForPolicyMatcher struct {
	Name      string
	Namespace string
	Registry  provider.ID
}

func PolicyMatcherFor(workloadNamespace string, labels labels.Instance, isWaypoint bool) WorkloadPolicyMatcher {
	return WorkloadPolicyMatcher{
		WorkloadNamespace: workloadNamespace,
		WorkloadLabels:    labels,
		IsWaypoint:        isWaypoint,
	}
}

func PolicyMatcherForProxy(proxy *Proxy) WorkloadPolicyMatcher {
	return WorkloadPolicyMatcher{
		WorkloadNamespace: proxy.ConfigNamespace,
		WorkloadLabels:    proxy.Labels,
		IsWaypoint:        proxy.IsWaypointProxy(),
	}
}

func (p WorkloadPolicyMatcher) WithRootNamespace(rns string) WorkloadPolicyMatcher {
	p.RootNamespace = rns
	return p
}

func (p WorkloadPolicyMatcher) WithService(service *Service) WorkloadPolicyMatcher {
	if service == nil {
		return p
	}

	p.Services = append(p.Services, ServiceInfoForPolicyMatcher{
		Name:      ptr.NonEmptyOrDefault(service.Attributes.ObjectName, service.Attributes.Name),
		Namespace: service.Attributes.Namespace,
		Registry:  service.Attributes.ServiceRegistry,
	})
	return p
}

// WithServices marks multiple services as part of the selection criteria. This is used when we want to
// find **all** policies attached to a specific proxy instance, rather than scoped to a specific service.
// This is useful when using ECDS, for example, where we might have:
// * Each unique service creates a listener, and applies a policy selected by `WithService` pointing to ECDS
// * All policies are found, by `WithServices`, and returned in ECDS.
func (p WorkloadPolicyMatcher) WithServices(services []*Service) WorkloadPolicyMatcher {
	for _, svc := range services {
		p = p.WithService(svc)
	}
	return p
}

// workloadGatewayName returns the name of the gateway for which a workload is an instance.
// This is based on the gateway.networking.k8s.io/gateway-name label.
func workloadGatewayName(l labels.Instance) (string, bool) {
	gwName, exists := l[label.IoK8sNetworkingGatewayGatewayName.Name]
	return gwName, exists
}

func (p WorkloadPolicyMatcher) isSelected(policy TargetablePolicy) bool {
	selector := policy.GetSelector()
	return selector == nil || labels.Instance(selector.GetMatchLabels()).SubsetOf(p.WorkloadLabels)
}

// GetTargetRefs returns the list of targetRefs, taking into account the legacy targetRef
func GetTargetRefs(p TargetablePolicy) []*v1beta1.PolicyTargetReference {
	targetRefs := p.GetTargetRefs()
	if len(targetRefs) == 0 && p.GetTargetRef() != nil {
		targetRefs = []*v1beta1.PolicyTargetReference{p.GetTargetRef()}
	}
	return targetRefs
}

func (p WorkloadPolicyMatcher) ShouldAttachPolicy(kind config.GroupVersionKind,
	policyName types.NamespacedName,
	policy TargetablePolicy,
) bool {
	gatewayName, isGatewayAPI := workloadGatewayName(p.WorkloadLabels)
	targetRefs := GetTargetRefs(policy)

	// non-gateway: use selector
	if !isGatewayAPI {
		// if targetRef is specified, ignore the policy altogether
		if len(targetRefs) > 0 {
			return false
		}
		return p.isSelected(policy)
	}

	// gateway with no targetRefs: (sometimes) fallback to selector
	if len(targetRefs) == 0 {
		// gateways require the feature flag for selector-based policy
		// waypoints never use selector
		if p.IsWaypoint || !features.EnableSelectorBasedK8sGatewayPolicy {
			log.Debugf("Ignoring workload-scoped %s/%s %s for gateway %s.%s because it has no targetRef",
				kind.Group, kind.Kind, policyName, gatewayName, p.WorkloadNamespace)
			return false
		}
		return p.isSelected(policy)
	}

	for _, targetRef := range targetRefs {
		target := targetRef.GetName()

		// Service attached
		if p.IsWaypoint && matchesGroupKind(targetRef, gvk.Service) {
			for _, svc := range p.Services {
				if target == svc.Name &&
					policyName.Namespace == svc.Namespace &&
					svc.Registry == provider.Kubernetes {
					return true
				}
			}
		}

		// ServiceEntry attached
		if p.IsWaypoint && matchesGroupKind(targetRef, gvk.ServiceEntry) {
			for _, svc := range p.Services {
				if target == svc.Name &&
					policyName.Namespace == svc.Namespace &&
					svc.Registry == provider.External {
					return true
				}
			}
		}

		// Is p.IsWaypoint good enough or do we specifically need to check that it is an istio-waypoint?
		if policyName.Namespace == p.RootNamespace &&
			p.IsWaypoint &&
			matchesGroupKind(targetRef, gvk.GatewayClass) &&
			targetRef.GetName() == constants.WaypointGatewayClassName {
			return true
		}

		// Namespace does not match
		if p.WorkloadNamespace != policyName.Namespace {
			// Policy is not in the same namespace
			continue
		}
		if !(targetRef.GetNamespace() == "" || targetRef.GetNamespace() == p.WorkloadNamespace) {
			// Policy references a different namespace (which is unsupported; it will never match anything)
			continue
		}

		// Gateway attached
		if matchesGroupKind(targetRef, gvk.KubernetesGateway) && target == gatewayName {
			return true
		}
	}

	return false
}

func matchesGroupKind(targetRef *v1beta1.PolicyTargetReference, gk config.GroupVersionKind) bool {
	return config.CanonicalGroup(targetRef.GetGroup()) == gk.CanonicalGroup() && targetRef.GetKind() == gk.Kind
}
