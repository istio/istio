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

// Implements GEP-5093 (Gateway address routability) for managed Gateways.
// The same per-entry decision (EvaluateAddressEntries) drives both what we provision on the
// Service (PlanService) and what we report in status (ResolveAddresses).

import (
	"fmt"
	"net/netip"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
)

// RoutabilitySentinel is reserved for conformance. It carries no routability guarantee.
const RoutabilitySentinel gatewayv1.GatewayAddressRoutabilityType = "testing.x-k8s.io/sentinel"

type entryKind int

const (
	// entryDefault is satisfied by default provisioning (LoadBalancer, or ClusterIP for ClusterIP Services).
	entryDefault entryKind = iota
	// entryClusterAuto is a Cluster entry without a value; satisfied by the Service ClusterIP(s).
	entryClusterAuto
	// entryClusterStatic is a Cluster entry with a value in a ServiceCIDR; becomes a Service clusterIP.
	entryClusterStatic
	entryUnsatisfied
)

// AddressEntry is the decision for a single spec.addresses entry.
type AddressEntry struct {
	Kind        entryKind
	Routability gatewayv1.GatewayAddressRoutabilityType
	Value       string
	// Addr is the parsed Value of a static Cluster entry.
	Addr netip.Addr
	// Reason and Message are set when Kind is entryUnsatisfied.
	Reason  string
	Message string
}

func (e AddressEntry) satisfiable() bool { return e.Kind != entryUnsatisfied }

func (e AddressEntry) describe() string {
	return fmt.Sprintf("routability=%q value=%q: %s", e.Routability, e.Value, e.Message)
}

// EvaluateAddressEntries decides, for each entry, whether it can be satisfied.
// An empty cidrs means ServiceCIDR information is unavailable, so static Cluster values are unusable.
func EvaluateAddressEntries(addrs []gatewayv1.GatewaySpecAddress, cidrs []netip.Prefix) []AddressEntry {
	out := make([]AddressEntry, len(addrs))
	clusterFamilies := map[bool]bool{} // keyed by Is4
	defaultSeen := false
	for i, a := range addrs {
		e := AddressEntry{Routability: a.Routability, Value: a.Value}
		unsatisfied := func(reason gatewayv1.GatewayConditionReason, msg string) {
			e.Kind, e.Reason, e.Message = entryUnsatisfied, string(reason), msg
		}
		switch {
		case a.Type != nil && *a.Type != gatewayv1.IPAddressType:
			unsatisfied(gatewayv1.GatewayReasonAddressNotAssigned, fmt.Sprintf("address type %q is not supported", *a.Type))
		case a.Routability == "" || a.Routability == RoutabilitySentinel:
			if defaultSeen {
				// A single Service can only back one default provisioned entry.
				unsatisfied(gatewayv1.GatewayReasonAddressNotAssigned, "only one entry without Cluster routability can be assigned")
			} else {
				defaultSeen = true
				e.Kind = entryDefault
			}
		case a.Routability != gatewayv1.GatewayAddressRoutabilityCluster:
			unsatisfied(gatewayv1.GatewayReasonAddressNotAssigned, "routability is not supported")
		case a.Value == "":
			e.Kind = entryClusterAuto
		default:
			addr, err := netip.ParseAddr(a.Value)
			switch {
			case err != nil || !inPrefixes(addr, cidrs):
				unsatisfied(gatewayv1.GatewayReasonAddressNotUsable, "address is not in a known ServiceCIDR")
			case clusterFamilies[addr.Is4()]:
				unsatisfied(gatewayv1.GatewayReasonAddressNotAssigned, "a Service has at most one ClusterIP per IP family")
			default:
				clusterFamilies[addr.Is4()] = true
				e.Kind, e.Addr = entryClusterStatic, addr
			}
		}
		out[i] = e
	}
	return out
}

func inPrefixes(a netip.Addr, cidrs []netip.Prefix) bool {
	for _, c := range cidrs {
		if c.Contains(a) {
			return true
		}
	}
	return false
}

// ServicePlan is what to provision for a managed Gateway.
type ServicePlan struct {
	// Deploy is false when spec.addresses is set but no entry can be satisfied.
	Deploy      bool
	ServiceType corev1.ServiceType
	// LoadBalancerIP comes only from a default entry, never a Cluster entry.
	LoadBalancerIP string
	ClusterIPs     []string
}

// PlanService computes the Service configuration. typeOverride is the user's service-type annotation, nil when unset.
// The annotation always wins, even when empty (as before routability, an empty type lets Kubernetes default to ClusterIP).
func PlanService(entries []AddressEntry, classDefault corev1.ServiceType, typeOverride *string) ServicePlan {
	plan := ServicePlan{Deploy: true, ServiceType: classDefault}
	if typeOverride != nil {
		plan.ServiceType = corev1.ServiceType(*typeOverride)
	}
	if len(entries) == 0 {
		return plan
	}
	plan.Deploy = false
	hasDefault := false
	for _, e := range entries {
		switch e.Kind {
		case entryDefault:
			hasDefault = true
			plan.LoadBalancerIP = e.Value
		case entryClusterStatic:
			plan.ClusterIPs = append(plan.ClusterIPs, e.Addr.String())
		}
		plan.Deploy = plan.Deploy || e.satisfiable()
	}
	if !hasDefault && typeOverride == nil {
		plan.ServiceType = corev1.ServiceTypeClusterIP
	}
	return plan
}

// AddressInputs describes the observed state of the Service backing a managed Gateway.
type AddressInputs struct {
	Spec  []gatewayv1.GatewaySpecAddress
	CIDRs []netip.Prefix
	// Default is the address list reported when spec.addresses is empty.
	Default []string
	// External are LoadBalancer addresses.
	External []string
	// ClusterIPs are the local cluster's ClusterIPs of the Service; empty until the Service is ready.
	ClusterIPs []string
	// LBPending is true when the Service is a LoadBalancer without an address yet.
	LBPending bool
}

// AddressesAssigned is the AddressesAssigned condition to publish.
type AddressesAssigned struct {
	Status  metav1.ConditionStatus
	Reason  string
	Message string
}

// AddressResult is the outcome of ResolveAddresses.
type AddressResult struct {
	Addresses []gatewayv1.GatewayStatusAddress
	// Condition is nil while assignment is still pending; the condition must then be omitted.
	Condition *AddressesAssigned
	// ProgrammedReason, when set, forces Programmed=False since no entry can be satisfied.
	ProgrammedReason  string
	ProgrammedMessage string
}

// StatusAddress builds a status address with explicit routability.
func StatusAddress(value string, r gatewayv1.GatewayAddressRoutabilityType) gatewayv1.GatewayStatusAddress {
	t := gatewayv1.IPAddressType
	if _, err := netip.ParseAddr(value); err != nil {
		t = gatewayv1.HostnameAddressType
	}
	return gatewayv1.GatewayStatusAddress{Type: &t, Value: value, Routability: &r}
}

// ResolveAddresses computes status.addresses and the AddressesAssigned condition.
func ResolveAddresses(in AddressInputs) AddressResult {
	var clusterIPs, externalIPs []netip.Addr
	for _, s := range in.ClusterIPs {
		if a, err := netip.ParseAddr(s); err == nil {
			clusterIPs = append(clusterIPs, a)
		}
	}
	for _, s := range in.External {
		if a, err := netip.ParseAddr(s); err == nil {
			externalIPs = append(externalIPs, a)
		}
	}

	if len(in.Spec) == 0 {
		res := AddressResult{}
		for _, s := range in.Default {
			r := gatewayv1.GatewayAddressRoutabilityDefault
			if a, err := netip.ParseAddr(s); err == nil && containsAddr(clusterIPs, a) {
				r = gatewayv1.GatewayAddressRoutabilityCluster
			}
			res.Addresses = append(res.Addresses, StatusAddress(s, r))
		}
		if len(res.Addresses) > 0 {
			res.Condition = &AddressesAssigned{
				Status: metav1.ConditionTrue, Reason: string(gatewayv1.GatewayReasonAddressesAssigned), Message: "Addresses assigned",
			}
		}
		return res
	}

	entries := EvaluateAddressEntries(in.Spec, in.CIDRs)
	perEntry := make([][]gatewayv1.GatewayStatusAddress, len(entries))
	pending := make([]bool, len(entries))
	claimed := map[netip.Addr]bool{}
	claim := func(i int, a netip.Addr, r gatewayv1.GatewayAddressRoutabilityType) {
		claimed[a] = true
		perEntry[i] = append(perEntry[i], StatusAddress(a.String(), r))
	}
	// A Cluster entry with a value gets exactly that address, so it must claim before valueless entries.
	for i, e := range entries {
		if e.Kind != entryClusterStatic {
			continue
		}
		if containsAddr(clusterIPs, e.Addr) {
			claim(i, e.Addr, gatewayv1.GatewayAddressRoutabilityCluster)
		} else {
			pending[i] = true
		}
	}
	for i, e := range entries {
		if e.Kind != entryClusterAuto {
			continue
		}
		for _, a := range clusterIPs {
			if !claimed[a] {
				claim(i, a, gatewayv1.GatewayAddressRoutabilityCluster)
			}
		}
		pending[i] = len(perEntry[i]) == 0
	}
	for i := range entries {
		e := &entries[i]
		if e.Kind != entryDefault {
			continue
		}
		clusterRoutability := gatewayv1.GatewayAddressRoutabilityCluster
		if e.Routability != "" {
			clusterRoutability = e.Routability
		}
		if e.Value != "" {
			// A requested value matches only that exact address (GEP-5093 status matching).
			a, err := netip.ParseAddr(e.Value)
			switch {
			case err == nil && containsAddr(externalIPs, a):
				claim(i, a, e.Routability)
			case err == nil && !claimed[a] && containsAddr(clusterIPs, a):
				claim(i, a, clusterRoutability)
			case in.LBPending || (len(externalIPs) == 0 && len(clusterIPs) == 0):
				pending[i] = true
			default:
				e.Kind, e.Reason, e.Message = entryUnsatisfied, string(gatewayv1.GatewayReasonAddressNotUsable), "the requested address was not assigned"
			}
			continue
		}
		switch {
		case len(externalIPs) > 0:
			for _, a := range externalIPs {
				claim(i, a, e.Routability)
			}
		case in.LBPending:
			// Do not fall back to a ClusterIP, which may be held by another entry, while the LB is pending.
			pending[i] = true
		default:
			for _, a := range clusterIPs {
				if !claimed[a] {
					claim(i, a, clusterRoutability)
				}
			}
			if len(perEntry[i]) == 0 {
				if len(clusterIPs) > 0 {
					// The Service is ClusterIP only, and every ClusterIP is held by other entries.
					e.Kind, e.Reason, e.Message = entryUnsatisfied, string(gatewayv1.GatewayReasonAddressNotAssigned), "no address is available"
				} else {
					pending[i] = true
				}
			}
		}
	}

	var res AddressResult
	var unsatisfied []string
	satisfiable, anyPending, allNotUsable := 0, false, true
	for i, e := range entries {
		res.Addresses = append(res.Addresses, perEntry[i]...)
		if e.satisfiable() {
			satisfiable++
			anyPending = anyPending || pending[i]
			continue
		}
		unsatisfied = append(unsatisfied, e.describe())
		allNotUsable = allNotUsable && e.Reason == string(gatewayv1.GatewayReasonAddressNotUsable)
	}
	list := strings.Join(unsatisfied, "; ")
	switch {
	case satisfiable == 0:
		res.Condition = &AddressesAssigned{
			Status: metav1.ConditionFalse, Reason: string(gatewayv1.GatewayReasonAddressesNotAssigned),
			Message: "No requested address could be assigned: " + list,
		}
		res.ProgrammedReason = string(gatewayv1.GatewayReasonAddressNotAssigned)
		if allNotUsable {
			res.ProgrammedReason = string(gatewayv1.GatewayReasonAddressNotUsable)
		}
		res.ProgrammedMessage = "Failed to assign to any requested addresses: " + list
	case anyPending:
	case len(unsatisfied) == 0:
		res.Condition = &AddressesAssigned{
			Status: metav1.ConditionTrue, Reason: string(gatewayv1.GatewayReasonAddressesAssigned), Message: "All requested addresses assigned",
		}
	default:
		res.Condition = &AddressesAssigned{
			Status: metav1.ConditionFalse, Reason: string(gatewayv1.GatewayReasonAddressesPartiallyAssigned),
			Message: "Some requested addresses could not be assigned: " + list,
		}
	}
	return res
}

func containsAddr(l []netip.Addr, a netip.Addr) bool {
	for _, x := range l {
		if x == a {
			return true
		}
	}
	return false
}

// ReportAddresses sets gs.Addresses and returns the AddressesAssigned condition to publish (nil means omit it)
// and, when non-nil, the error that forces Programmed=False.
func ReportAddresses(
	r *GatewayContext,
	obj *gatewayv1.Gateway,
	gs *gatewayv1.GatewayStatus,
	cidrs []netip.Prefix,
	gatewayServices, addressesToReport, internal, external, pending, warnings []string,
) (*AddressesAssigned, *ListenerStatusConfigError) {
	if !IsManaged(&obj.Spec) {
		// Manual deployment: the Gateway points to an existing Service by hostname.
		// Do not report an address until we are ready. But once we are ready, never remove the address.
		if len(addressesToReport) > 0 {
			gs.Addresses = make([]gatewayv1.GatewayStatusAddress, 0, len(addressesToReport))
			for _, addr := range addressesToReport {
				gs.Addresses = append(gs.Addresses, StatusAddress(addr, gatewayv1.GatewayAddressRoutabilityDefault))
			}
		}
		return manualAddressesAssigned(obj.Spec.Addresses, internal, pending, warnings)
	}
	res := ResolveAddresses(AddressInputs{
		Spec:       obj.Spec.Addresses,
		CIDRs:      cidrs,
		Default:    addressesToReport,
		External:   external,
		ClusterIPs: localClusterIPs(r, obj.Namespace, gatewayServices, internal),
		LBPending:  len(pending) > 0,
	})
	// With spec.addresses empty, keep reported addresses once ready. With explicit addresses the status must
	// exactly match what was assigned, so it is always replaced.
	if len(res.Addresses) > 0 || len(obj.Spec.Addresses) > 0 {
		gs.Addresses = res.Addresses
	}
	if res.ProgrammedReason == "" {
		return res.Condition, nil
	}
	return res.Condition, &ListenerStatusConfigError{Reason: res.ProgrammedReason, Message: res.ProgrammedMessage}
}

// manualAddressesAssigned maps the resolution of a manually deployed Gateway onto AddressesAssigned.
// Routability cannot be guaranteed for an existing Service, so hostname entries requesting one are unsatisfied.
func manualAddressesAssigned(addrs []gatewayv1.GatewaySpecAddress, internal, pending, warnings []string) (*AddressesAssigned, *ListenerStatusConfigError) {
	var unsatisfied []string
	satisfiable := 0
	// Unsupported address types keep the existing UnsupportedAddress reason on Programmed.
	onlyHostnames := true
	for _, a := range addrs {
		isHostname := a.Type != nil && *a.Type == gatewayv1.HostnameAddressType
		onlyHostnames = onlyHostnames && isHostname
		if isHostname && a.Routability == "" {
			satisfiable++
		} else {
			unsatisfied = append(unsatisfied, fmt.Sprintf("type=%v routability=%q value=%q", ptr.OrEmpty(a.Type), a.Routability, a.Value))
		}
	}
	details := strings.Join(append(slices.Clone(warnings), unsatisfied...), "; ")
	notAssigned := &AddressesAssigned{
		Status: metav1.ConditionFalse, Reason: string(gatewayv1.GatewayReasonAddressesNotAssigned),
		Message: "No requested address could be assigned: " + details,
	}
	switch {
	case satisfiable == 0:
		if onlyHostnames {
			return notAssigned, &ListenerStatusConfigError{
				Reason: string(gatewayv1.GatewayReasonAddressNotAssigned), Message: "Failed to assign to any requested addresses: " + details,
			}
		}
		return notAssigned, nil
	case len(pending) > 0:
		return nil, nil
	case len(warnings) == 0 && len(unsatisfied) == 0:
		return &AddressesAssigned{
			Status: metav1.ConditionTrue, Reason: string(gatewayv1.GatewayReasonAddressesAssigned), Message: "All requested addresses assigned",
		}, nil
	case len(internal) > 0:
		return &AddressesAssigned{
			Status: metav1.ConditionFalse, Reason: string(gatewayv1.GatewayReasonAddressesPartiallyAssigned),
			Message: "Some requested addresses could not be assigned: " + details,
		}, nil
	}
	return notAssigned, nil
}

// localClusterIPs returns the local cluster's ClusterIPs of the Gateway's Service, once it has instances.
func localClusterIPs(r *GatewayContext, namespace string, gatewayServices, internal []string) []string {
	if len(internal) == 0 {
		return nil
	}
	var ips []string
	for _, g := range gatewayServices {
		if svc := r.GetService(g, namespace); svc != nil {
			for _, ip := range svc.ClusterVIPs.GetAddressesFor(r.Cluster()) {
				if _, err := netip.ParseAddr(ip); err == nil {
					ips = append(ips, ip)
				}
			}
		}
	}
	return ips
}

// ServiceCIDRPrefixes flattens the CIDRs of ServiceCIDR objects, skipping invalid ones.
func ServiceCIDRPrefixes(objs []*networkingv1.ServiceCIDR) []netip.Prefix {
	var out []netip.Prefix
	for _, o := range objs {
		for _, c := range o.Spec.CIDRs {
			if p, err := netip.ParsePrefix(c); err == nil {
				out = append(out, p)
			}
		}
	}
	return out
}
