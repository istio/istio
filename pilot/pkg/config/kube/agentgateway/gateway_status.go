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
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

func extractGatewayServices(domainSuffix string, kgw *gatewayv1.Gateway, info gatewaycommon.ClassInfo) ([]string, *Condition) {
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
		return gatewayServices, &Condition{
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
		return gatewayServices, &Condition{
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
	validListeners int,
) {
	// TODO: we lose address if servers is empty due to an error
	internal, internalIP, external, pending, warnings, allUsable := r.ResolveGatewayInstances(obj.Namespace, gatewayServices, servers)

	// Setup initial conditions to the success state. If we encounter errors, we will update this.
	// We have two status
	// Accepted: is the configuration valid. This reflects the validity of the Gateway's listeners:
	// we report the ListenersNotValid reason whenever any listener is invalid, and only reject the
	// Gateway (Accepted=False) when none of its listeners are valid.
	// Programmed: is the data plane "ready" (note: eventually consistent)
	gatewayConditions := map[string]*Condition{
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
	} else if totalListeners := len(obj.Spec.Listeners); totalListeners > 0 && validListeners < totalListeners {
		// One or more listeners are invalid. A Gateway stays Accepted=True as long
		// as at least one of its listeners is valid; it is only rejected when none are valid. In both
		// cases we surface the ListenersNotValid reason.
		accepted := gatewayConditions[string(gatewayv1.GatewayConditionAccepted)]
		if validListeners == 0 {
			accepted.error = &ConfigError{
				Reason:  string(gatewayv1.GatewayReasonListenersNotValid),
				Message: "None of the Gateway's listeners are valid",
			}
		} else {
			accepted.reason = string(gatewayv1.GatewayReasonListenersNotValid)
			accepted.message = "One or more of the Gateway's listeners are invalid"
		}
	}

	gs.AttachedListenerSets = ptr.Of(int32(listenerSetCount))
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

func setProgrammedCondition(gatewayConditions map[string]*Condition, internal []string, gatewayServices []string, warnings []string, allUsable bool) {
	programmed := gatewayConditions[string(gatewayv1.GatewayConditionProgrammed)]
	mapped := map[string]*gatewaycommon.ListenerStatusCondition{
		string(gatewayv1.GatewayConditionProgrammed): agentgatewayListenerStatusCondition(programmed),
	}
	gatewaycommon.SetProgrammedCondition(mapped, internal, gatewayServices, warnings, allUsable)
	applyAgentgatewayConditionFromListenerStatus(programmed, mapped[string(gatewayv1.GatewayConditionProgrammed)])
}

func agentgatewayListenerStatusCondition(c *Condition) *gatewaycommon.ListenerStatusCondition {
	if c == nil {
		return &gatewaycommon.ListenerStatusCondition{}
	}
	out := &gatewaycommon.ListenerStatusCondition{
		Reason:  c.reason,
		Message: c.message,
		Status:  c.status,
		SetOnce: c.setOnce,
	}
	if c.error != nil {
		out.Error = &gatewaycommon.ListenerStatusConfigError{Reason: c.error.Reason, Message: c.error.Message}
	}
	return out
}

func applyAgentgatewayConditionFromListenerStatus(c *Condition, u *gatewaycommon.ListenerStatusCondition) {
	if c == nil || u == nil {
		return
	}
	c.reason = u.Reason
	c.message = u.Message
	c.status = u.Status
	c.setOnce = u.SetOnce
	if u.Error != nil {
		c.error = &ConfigError{Reason: u.Error.Reason, Message: u.Error.Message}
	} else {
		c.error = nil
	}
}

func getListenerNames(spec *gatewayv1.GatewaySpec) sets.Set[gatewayv1.SectionName] {
	res := sets.New[gatewayv1.SectionName]()
	for _, l := range spec.Listeners {
		res.Insert(l.Name)
	}
	return res
}
