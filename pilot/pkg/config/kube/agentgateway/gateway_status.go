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
	"fmt"
	"net"
	"net/netip"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/api/annotation"
	istio "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/kube/gatewaycommon"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

func extractGatewayServices(domainSuffix string, kgw *gatewayv1.Gateway, info gatewaycommon.ClassInfo) ([]string, *condition) {
	if gatewaycommon.IsManaged(&kgw.Spec) {
		name := model.GetOrDefault(kgw.Annotations[annotation.GatewayNameOverride.Name], gatewaycommon.GetDefaultName(kgw.Name, &kgw.Spec, info.DisableNameSuffix))
		return []string{fmt.Sprintf("%s.%s.svc.%v", name, kgw.Namespace, domainSuffix)}, nil
	}
	gatewayServices := []string{}
	skippedAddresses := []string{}
	for _, addr := range kgw.Spec.Addresses {
		if addr.Type != nil && *addr.Type != gatewayv1.HostnameAddressType {
			// We only support HostnameAddressType. Keep track of invalid ones so we can report in status.
			skippedAddresses = append(skippedAddresses, addr.Value)
			continue
		}
		// TODO: For now we are using Addresses. There has been some discussion of allowing inline
		// parameters on the class field like a URL, in which case we will probably just use that. See
		// https://github.com/kubernetes-sigs/gateway-api/pull/614
		fqdn := addr.Value
		if !strings.Contains(fqdn, ".") {
			// Short name, expand it
			fqdn = fmt.Sprintf("%s.%s.svc.%s", fqdn, kgw.Namespace, domainSuffix)
		}
		gatewayServices = append(gatewayServices, fqdn)
	}
	if len(skippedAddresses) > 0 {
		// Give error but return services, this is a soft failure
		return gatewayServices, &condition{
			status: metav1.ConditionFalse,
			error: &ConfigError{
				Reason:  InvalidAddress,
				Message: fmt.Sprintf("only Hostname is supported, ignoring %v", skippedAddresses),
			},
		}
	}
	if _, f := kgw.Annotations[annotation.NetworkingServiceType.Name]; f {
		// Give error but return services, this is a soft failure
		// Remove entirely in 1.20
		return gatewayServices, &condition{
			status: metav1.ConditionFalse,
			error: &ConfigError{
				Reason:  DeprecateFieldUsage,
				Message: fmt.Sprintf("annotation %v is deprecated, use Spec.Infrastructure.Routeability", annotation.NetworkingServiceType.Name),
			},
		}
	}
	return gatewayServices, nil
}

func reportGatewayStatus(
	r *gatewaycommon.GatewayContext,
	obj *gatewayv1.Gateway,
	gs *gatewayv1.GatewayStatus,
	classInfo gatewaycommon.ClassInfo,
	gatewayServices []string,
	servers []*istio.Server,
	listenerSetCount int,
	gatewayErr *ConfigError,
) {
	// TODO: we lose address if servers is empty due to an error
	internal, internalIP, external, pending, warnings, allUsable := r.ResolveGatewayInstances(obj.Namespace, gatewayServices, servers)

	// Setup initial conditions to the success state. If we encounter errors, we will update this.
	// We have two status
	// Accepted: is the configuration valid. We only have errors in listeners, and the status is not supposed to
	// be tied to listeners, so this is always accepted
	// Programmed: is the data plane "ready" (note: eventually consistent)
	gatewayConditions := map[string]*condition{
		string(gatewayv1.GatewayConditionAccepted): {
			reason:  string(gatewayv1.GatewayReasonAccepted),
			message: "Resource accepted",
		},
		string(gatewayv1.GatewayConditionProgrammed): {
			reason:  string(gatewayv1.GatewayReasonProgrammed),
			message: "Resource programmed",
		},
	}
	if gatewayErr != nil {
		gatewayConditions[string(gatewayv1.GatewayConditionAccepted)].error = gatewayErr
	}

	// Not defined in upstream API
	const AttachedListenerSets = "AttachedListenerSets"
	if obj.Spec.AllowedListeners != nil {
		gatewayConditions[AttachedListenerSets] = &condition{
			reason:  "ListenersAttached",
			message: "At least one ListenerSet is attached",
		}
		if !features.EnableAlphaGatewayAPI {
			gatewayConditions[AttachedListenerSets].error = &ConfigError{
				Reason: "Unsupported",
				Message: fmt.Sprintf("AllowedListeners is configured, but ListenerSets are not enabled (set %v=true)",
					features.EnableAlphaGatewayAPIName),
			}
		} else if listenerSetCount == 0 {
			gatewayConditions[AttachedListenerSets].error = &ConfigError{
				Reason:  "NoListenersAttached",
				Message: "AllowedListeners is configured, but no ListenerSets are attached",
			}
		}
	}

	setProgrammedCondition(gatewayConditions, internal, gatewayServices, warnings, allUsable)

	addressesToReport := external
	if len(addressesToReport) == 0 {
		wantAddressType := classInfo.AddressType
		if override, ok := obj.Annotations[addressTypeOverride]; ok {
			wantAddressType = gatewayv1.AddressType(override)
		}
		// There are no external addresses, so report the internal ones
		// This can be IP, Hostname, or both (indicated by empty wantAddressType)
		if wantAddressType != gatewayv1.HostnameAddressType {
			addressesToReport = internalIP
		}
		if wantAddressType != gatewayv1.IPAddressType {
			for _, hostport := range internal {
				svchost, _, _ := net.SplitHostPort(hostport)
				if !slices.Contains(pending, svchost) && !slices.Contains(addressesToReport, svchost) {
					addressesToReport = append(addressesToReport, svchost)
				}
			}
		}
	}
	// Do not report an address until we are ready. But once we are ready, never remove the address.
	if len(addressesToReport) > 0 {
		gs.Addresses = make([]gatewayv1.GatewayStatusAddress, 0, len(addressesToReport))
		for _, addr := range addressesToReport {
			var addrType gatewayv1.AddressType
			if _, err := netip.ParseAddr(addr); err == nil {
				addrType = gatewayv1.IPAddressType
			} else {
				addrType = gatewayv1.HostnameAddressType
			}
			gs.Addresses = append(gs.Addresses, gatewayv1.GatewayStatusAddress{
				Value: addr,
				Type:  &addrType,
			})
		}
	}
	// Prune listeners that have been removed
	haveListeners := getListenerNames(&obj.Spec)
	listeners := make([]gatewayv1.ListenerStatus, 0, len(gs.Listeners))
	for _, l := range gs.Listeners {
		if haveListeners.Contains(l.Name) {
			haveListeners.Delete(l.Name)
			listeners = append(listeners, l)
		}
	}
	gs.Listeners = listeners
	gs.Conditions = setConditions(obj.Generation, gs.Conditions, gatewayConditions)
}

func setProgrammedCondition(gatewayConditions map[string]*condition, internal []string, gatewayServices []string, warnings []string, allUsable bool) {
	if len(internal) > 0 {
		msg := fmt.Sprintf("Resource programmed, assigned to service(s) %s", humanReadableJoin(internal))
		gatewayConditions[string(gatewayv1.GatewayConditionProgrammed)].message = msg
	}

	if len(gatewayServices) == 0 {
		gatewayConditions[string(gatewayv1.GatewayConditionProgrammed)].error = &ConfigError{
			Reason:  InvalidAddress,
			Message: "Failed to assign to any requested addresses",
		}
	} else if len(warnings) > 0 {
		var msg string
		var reason string
		if len(internal) != 0 {
			msg = fmt.Sprintf("Assigned to service(s) %s, but failed to assign to all requested addresses: %s",
				humanReadableJoin(internal), strings.Join(warnings, "; "))
		} else {
			msg = fmt.Sprintf("Failed to assign to any requested addresses: %s", strings.Join(warnings, "; "))
		}
		if allUsable {
			reason = string(gatewayv1.GatewayReasonAddressNotAssigned)
		} else {
			reason = string(gatewayv1.GatewayReasonAddressNotUsable)
		}
		gatewayConditions[string(gatewayv1.GatewayConditionProgrammed)].error = &ConfigError{
			// TODO: this only checks Service ready, we should also check Deployment ready?
			Reason:  reason,
			Message: msg,
		}
	}
}

// reportUnsupportedListenerSet reports a status message for a ListenerSet that is not supported
func reportUnsupportedListenerSet(class string, status *gatewayv1.ListenerSetStatus, obj *gatewayv1.ListenerSet) {
	gatewayConditions := map[string]*condition{
		string(gatewayv1.GatewayConditionAccepted): {
			reason: string(gatewayv1.GatewayReasonAccepted),
			error: &ConfigError{
				Reason:  string(gatewayv1.ListenerSetReasonNotAllowed),
				Message: fmt.Sprintf("The %q GatewayClass does not support ListenerSet", class),
			},
		},
		string(gatewayv1.GatewayConditionProgrammed): {
			reason: string(gatewayv1.GatewayReasonProgrammed),
			error: &ConfigError{
				Reason:  string(gatewayv1.ListenerSetReasonNotAllowed),
				Message: fmt.Sprintf("The %q GatewayClass does not support ListenerSet", class),
			},
		},
	}
	status.Listeners = nil
	status.Conditions = setConditions(obj.Generation, status.Conditions, gatewayConditions)
}

// reportNotAllowedListenerSet reports a status message for a ListenerSet that is not allowed to be selected
func reportNotAllowedListenerSet(status *gatewayv1.ListenerSetStatus, obj *gatewayv1.ListenerSet) {
	gatewayConditions := map[string]*condition{
		string(gatewayv1.GatewayConditionAccepted): {
			reason: string(gatewayv1.GatewayReasonAccepted),
			error: &ConfigError{
				Reason:  string(gatewayv1.ListenerSetReasonNotAllowed),
				Message: "The parent Gateway does not allow this reference; check the 'spec.allowedRoutes'",
			},
		},
		string(gatewayv1.GatewayConditionProgrammed): {
			reason: string(gatewayv1.GatewayReasonProgrammed),
			error: &ConfigError{
				Reason:  string(gatewayv1.ListenerSetReasonNotAllowed),
				Message: "The parent Gateway does not allow this reference; check the 'spec.allowedRoutes'",
			},
		},
	}
	status.Listeners = nil
	status.Conditions = setConditions(obj.Generation, status.Conditions, gatewayConditions)
}

func humanReadableJoin(ss []string) string {
	switch len(ss) {
	case 0:
		return ""
	case 1:
		return ss[0]
	case 2:
		return ss[0] + " and " + ss[1]
	default:
		return strings.Join(ss[:len(ss)-1], ", ") + ", and " + ss[len(ss)-1]
	}
}

func getListenerNames(spec *gatewayv1.GatewaySpec) sets.Set[gatewayv1.SectionName] {
	res := sets.New[gatewayv1.SectionName]()
	for _, l := range spec.Listeners {
		res.Insert(l.Name)
	}
	return res
}

func toRouteKind(g config.GroupVersionKind) gatewayv1.RouteGroupKind {
	return gatewayv1.RouteGroupKind{Group: (*gatewayv1.Group)(&g.Group), Kind: gatewayv1.Kind(g.Kind)}
}

// routeGroupKindEqual checks if two RouteGroupKinds are equal
func routeGroupKindEqual(rgk1, rgk2 gatewayv1.RouteGroupKind) bool {
	return rgk1.Kind == rgk2.Kind && getGroup(rgk1) == getGroup(rgk2)
}

func getGroup(rgk gatewayv1.RouteGroupKind) gatewayv1.Group {
	return ptr.OrDefault(rgk.Group, gatewayv1.GroupName)
}
