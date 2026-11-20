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
	"fmt"
	"net/netip"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

var cidrs = []netip.Prefix{netip.MustParsePrefix("10.96.0.0/16"), netip.MustParsePrefix("fd00:10:96::/112")}

func ip(value string, r gatewayv1.GatewayAddressRoutabilityType) gatewayv1.GatewaySpecAddress {
	t := gatewayv1.IPAddressType
	return gatewayv1.GatewaySpecAddress{Type: &t, Value: value, Routability: r}
}

func hostAddr(value string, r gatewayv1.GatewayAddressRoutabilityType) gatewayv1.GatewaySpecAddress {
	t := gatewayv1.HostnameAddressType
	return gatewayv1.GatewaySpecAddress{Type: &t, Value: value, Routability: r}
}

func TestIsManaged(t *testing.T) {
	test.SetForTest(t, &features.EnableGatewayAPIManualDeployment, true)
	ipType, hostType := gatewayv1.IPAddressType, gatewayv1.HostnameAddressType
	cases := []struct {
		name  string
		addrs []gatewayv1.GatewaySpecAddress
		want  bool
	}{
		{"none", nil, true},
		{"one ip", []gatewayv1.GatewaySpecAddress{ip("1.2.3.4", "")}, true},
		{"nil type", []gatewayv1.GatewaySpecAddress{{Value: "1.2.3.4"}}, true},
		{"many ips", []gatewayv1.GatewaySpecAddress{ip("", ""), ip("", gatewayv1.GatewayAddressRoutabilityCluster)}, true},
		{"hostname", []gatewayv1.GatewaySpecAddress{hostAddr("svc", "")}, false},
		{"many hostnames", []gatewayv1.GatewaySpecAddress{hostAddr("a", ""), hostAddr("b", "")}, false},
		{"mixed", []gatewayv1.GatewaySpecAddress{{Type: &ipType, Value: "1.2.3.4"}, {Type: &hostType, Value: "svc"}}, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, IsManaged(&gatewayv1.GatewaySpec{Addresses: tt.addrs}), tt.want)
		})
	}
	test.SetForTest(t, &features.EnableGatewayAPIManualDeployment, false)
	assert.Equal(t, IsManaged(&gatewayv1.GatewaySpec{Addresses: []gatewayv1.GatewaySpecAddress{hostAddr("svc", "")}}), true)
}

func TestPlanService(t *testing.T) {
	cluster := gatewayv1.GatewayAddressRoutabilityCluster
	lb, cip := corev1.ServiceTypeLoadBalancer, corev1.ServiceTypeClusterIP
	cases := []struct {
		name     string
		addrs    []gatewayv1.GatewaySpecAddress
		class    corev1.ServiceType // class default; LoadBalancer when empty
		override *string            // service-type annotation; nil when unset
		want     ServicePlan
	}{
		{name: "no addresses", want: ServicePlan{Deploy: true, ServiceType: lb}},
		{name: "no addresses with override", override: ptr.Of("NodePort"), want: ServicePlan{Deploy: true, ServiceType: corev1.ServiceTypeNodePort}},
		{name: "empty override is kept, as before routability", override: ptr.Of(""), want: ServicePlan{Deploy: true, ServiceType: ""}},
		{
			name: "static default", addrs: []gatewayv1.GatewaySpecAddress{ip("1.2.3.4", "")},
			want: ServicePlan{Deploy: true, ServiceType: lb, LoadBalancerIP: "1.2.3.4"},
		},
		{
			name: "default entry with override", addrs: []gatewayv1.GatewaySpecAddress{ip("", "")}, override: ptr.Of("ClusterIP"),
			want: ServicePlan{Deploy: true, ServiceType: cip},
		},
		{name: "cluster only", addrs: []gatewayv1.GatewaySpecAddress{ip("", cluster)}, want: ServicePlan{Deploy: true, ServiceType: cip}},
		{
			name: "cluster only with override", addrs: []gatewayv1.GatewaySpecAddress{ip("", cluster)}, override: ptr.Of("LoadBalancer"),
			want: ServicePlan{Deploy: true, ServiceType: lb},
		},
		{
			name: "default and cluster", addrs: []gatewayv1.GatewaySpecAddress{ip("", ""), ip("", cluster)},
			want: ServicePlan{Deploy: true, ServiceType: lb},
		},
		{
			name: "ClusterIP class with default entry", addrs: []gatewayv1.GatewaySpecAddress{ip("", "")}, class: cip,
			want: ServicePlan{Deploy: true, ServiceType: cip},
		},
		{
			name: "ClusterIP class with default and cluster", addrs: []gatewayv1.GatewaySpecAddress{ip("", ""), ip("", cluster)}, class: cip,
			want: ServicePlan{Deploy: true, ServiceType: cip},
		},
		{
			name:  "sentinel with value, static cluster",
			addrs: []gatewayv1.GatewaySpecAddress{ip("1.2.3.4", RoutabilitySentinel), ip("10.96.0.5", cluster)},
			want:  ServicePlan{Deploy: true, ServiceType: lb, LoadBalancerIP: "1.2.3.4", ClusterIPs: []string{"10.96.0.5"}},
		},
		{
			name: "static cluster never becomes loadBalancerIP", addrs: []gatewayv1.GatewaySpecAddress{ip("10.96.0.5", cluster)},
			want: ServicePlan{Deploy: true, ServiceType: cip, ClusterIPs: []string{"10.96.0.5"}},
		},
		{
			name: "dual stack static", addrs: []gatewayv1.GatewaySpecAddress{ip("10.96.0.5", cluster), ip("fd00:10:96::5", cluster)},
			want: ServicePlan{Deploy: true, ServiceType: cip, ClusterIPs: []string{"10.96.0.5", "fd00:10:96::5"}},
		},
		{
			name:  "nothing satisfiable",
			addrs: []gatewayv1.GatewaySpecAddress{ip("192.0.2.20", cluster), ip("", "testing.x-k8s.io/unsupported")},
			want:  ServicePlan{Deploy: false, ServiceType: cip},
		},
		{
			name:     "override does not force a deploy when nothing is satisfiable",
			addrs:    []gatewayv1.GatewaySpecAddress{ip("", "testing.x-k8s.io/unsupported")},
			override: ptr.Of("LoadBalancer"),
			want:     ServicePlan{Deploy: false, ServiceType: lb},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			class := tt.class
			if class == "" {
				class = lb
			}
			assert.Equal(t, PlanService(EvaluateAddressEntries(tt.addrs, cidrs), class, tt.override), tt.want)
		})
	}
}

type wantAddr struct {
	value, routability string
}

func TestResolveAddresses(t *testing.T) {
	cluster := gatewayv1.GatewayAddressRoutabilityCluster
	assigned := string(gatewayv1.GatewayReasonAddressesAssigned)
	partial := string(gatewayv1.GatewayReasonAddressesPartiallyAssigned)
	notAssigned := string(gatewayv1.GatewayReasonAddressesNotAssigned)
	dual := []string{"10.96.0.5", "fd00:10:96::5"}
	cases := []struct {
		name       string
		in         AddressInputs
		addrs      []wantAddr
		cond       string // reason; empty means the condition is omitted
		condStatus metav1.ConditionStatus
		programmed string
	}{
		{
			name:       "empty spec, external",
			in:         AddressInputs{Default: []string{"203.0.113.10"}, External: []string{"203.0.113.10"}, ClusterIPs: dual},
			addrs:      []wantAddr{{"203.0.113.10", ""}},
			cond:       assigned,
			condStatus: metav1.ConditionTrue,
		},
		{
			name:       "empty spec, cluster ip and hostname",
			in:         AddressInputs{Default: []string{"10.96.0.5", "svc.ns.svc.cluster.local"}, ClusterIPs: dual},
			addrs:      []wantAddr{{"10.96.0.5", "Cluster"}, {"svc.ns.svc.cluster.local", ""}},
			cond:       assigned,
			condStatus: metav1.ConditionTrue,
		},
		{
			name: "empty spec, nothing yet",
			in:   AddressInputs{},
		},
		{
			name:       "default value, LB assigned it",
			in:         AddressInputs{Spec: []gatewayv1.GatewaySpecAddress{ip("1.2.3.4", "")}, External: []string{"1.2.3.4"}, ClusterIPs: dual},
			addrs:      []wantAddr{{"1.2.3.4", ""}},
			cond:       assigned,
			condStatus: metav1.ConditionTrue,
		},
		{
			name:       "default value, LB assigned a different address",
			in:         AddressInputs{Spec: []gatewayv1.GatewaySpecAddress{ip("1.2.3.4", "")}, External: []string{"5.6.7.8"}, ClusterIPs: dual},
			cond:       notAssigned,
			condStatus: metav1.ConditionFalse,
			programmed: string(gatewayv1.GatewayReasonAddressNotUsable),
		},
		{
			name: "default value, LB pending",
			in:   AddressInputs{Spec: []gatewayv1.GatewaySpecAddress{ip("1.2.3.4", "")}, ClusterIPs: dual, LBPending: true},
		},
		{
			name:       "default value, ClusterIP Service does not have it",
			in:         AddressInputs{Spec: []gatewayv1.GatewaySpecAddress{ip("1.2.3.4", "")}, ClusterIPs: dual},
			cond:       notAssigned,
			condStatus: metav1.ConditionFalse,
			programmed: string(gatewayv1.GatewayReasonAddressNotUsable),
		},
		{
			name:       "default value plus cluster, only the default value mismatches",
			in:         AddressInputs{Spec: []gatewayv1.GatewaySpecAddress{ip("1.2.3.4", ""), ip("", cluster)}, External: []string{"5.6.7.8"}, ClusterIPs: dual},
			addrs:      []wantAddr{{"10.96.0.5", "Cluster"}, {"fd00:10:96::5", "Cluster"}},
			cond:       partial,
			condStatus: metav1.ConditionFalse,
		},
		{
			name:       "cluster, dual stack",
			in:         AddressInputs{Spec: []gatewayv1.GatewaySpecAddress{ip("", cluster)}, ClusterIPs: dual},
			addrs:      []wantAddr{{"10.96.0.5", "Cluster"}, {"fd00:10:96::5", "Cluster"}},
			cond:       assigned,
			condStatus: metav1.ConditionTrue,
		},
		{
			name: "cluster, service not ready",
			in:   AddressInputs{Spec: []gatewayv1.GatewaySpecAddress{ip("", cluster)}},
		},
		{
			name:       "only one default entry can be assigned",
			in:         AddressInputs{Spec: []gatewayv1.GatewaySpecAddress{ip("", ""), ip("", RoutabilitySentinel)}, External: []string{"203.0.113.10"}, ClusterIPs: dual},
			addrs:      []wantAddr{{"203.0.113.10", ""}},
			cond:       partial,
			condStatus: metav1.ConditionFalse,
		},
		{
			name:       "second same-family static cluster is unsatisfied",
			in:         AddressInputs{Spec: []gatewayv1.GatewaySpecAddress{ip("10.96.0.5", cluster), ip("10.96.0.6", cluster)}, CIDRs: cidrs, ClusterIPs: dual},
			addrs:      []wantAddr{{"10.96.0.5", "Cluster"}},
			cond:       partial,
			condStatus: metav1.ConditionFalse,
		},
		{
			name:       "sentinel external",
			in:         AddressInputs{Spec: []gatewayv1.GatewaySpecAddress{ip("", RoutabilitySentinel)}, External: []string{"203.0.113.10"}, ClusterIPs: dual},
			addrs:      []wantAddr{{"203.0.113.10", string(RoutabilitySentinel)}},
			cond:       assigned,
			condStatus: metav1.ConditionTrue,
		},
		{
			name:       "empty entry on ClusterIP service is tagged Cluster",
			in:         AddressInputs{Spec: []gatewayv1.GatewaySpecAddress{ip("", "")}, ClusterIPs: dual},
			addrs:      []wantAddr{{"10.96.0.5", "Cluster"}, {"fd00:10:96::5", "Cluster"}},
			cond:       assigned,
			condStatus: metav1.ConditionTrue,
		},
		{
			name:       "sentinel entry on ClusterIP service keeps sentinel",
			in:         AddressInputs{Spec: []gatewayv1.GatewaySpecAddress{ip("", RoutabilitySentinel)}, ClusterIPs: []string{"10.96.0.5"}},
			addrs:      []wantAddr{{"10.96.0.5", string(RoutabilitySentinel)}},
			cond:       assigned,
			condStatus: metav1.ConditionTrue,
		},
		{
			name:  "pending LB does not borrow the ClusterIP held by another entry; condition omitted",
			in:    AddressInputs{Spec: []gatewayv1.GatewaySpecAddress{ip("", ""), ip("", cluster)}, ClusterIPs: []string{"10.96.0.5"}, LBPending: true},
			addrs: []wantAddr{{"10.96.0.5", "Cluster"}},
		},
		{
			name:       "default and cluster on ClusterIP service: default has nothing left",
			in:         AddressInputs{Spec: []gatewayv1.GatewaySpecAddress{ip("", ""), ip("", cluster)}, ClusterIPs: []string{"10.96.0.5"}},
			addrs:      []wantAddr{{"10.96.0.5", "Cluster"}},
			cond:       partial,
			condStatus: metav1.ConditionFalse,
		},
		{
			name:       "static cluster in cidr reports exactly that value, not the sibling family",
			in:         AddressInputs{Spec: []gatewayv1.GatewaySpecAddress{ip("10.96.0.5", cluster)}, CIDRs: cidrs, ClusterIPs: dual},
			addrs:      []wantAddr{{"10.96.0.5", "Cluster"}},
			cond:       assigned,
			condStatus: metav1.ConditionTrue,
		},
		{
			name: "static cluster waits for the Service to have it",
			in:   AddressInputs{Spec: []gatewayv1.GatewaySpecAddress{ip("10.96.0.5", cluster)}, CIDRs: cidrs, ClusterIPs: []string{"10.96.0.9"}},
		},
		{
			name:       "static cluster plus valueless cluster get distinct addresses",
			in:         AddressInputs{Spec: []gatewayv1.GatewaySpecAddress{ip("", cluster), ip("10.96.0.5", cluster)}, CIDRs: cidrs, ClusterIPs: dual},
			addrs:      []wantAddr{{"fd00:10:96::5", "Cluster"}, {"10.96.0.5", "Cluster"}},
			cond:       assigned,
			condStatus: metav1.ConditionTrue,
		},
		{
			name:       "static cluster out of cidr alone",
			in:         AddressInputs{Spec: []gatewayv1.GatewaySpecAddress{ip("192.0.2.20", cluster)}, CIDRs: cidrs, ClusterIPs: dual},
			cond:       notAssigned,
			condStatus: metav1.ConditionFalse,
			programmed: string(gatewayv1.GatewayReasonAddressNotUsable),
		},
		{
			name:       "static cluster without cidr info alone",
			in:         AddressInputs{Spec: []gatewayv1.GatewaySpecAddress{ip("10.96.0.5", cluster)}, ClusterIPs: dual},
			cond:       notAssigned,
			condStatus: metav1.ConditionFalse,
			programmed: string(gatewayv1.GatewayReasonAddressNotUsable),
		},
		{
			name:       "unsupported routability alone",
			in:         AddressInputs{Spec: []gatewayv1.GatewaySpecAddress{ip("", "testing.x-k8s.io/unsupported")}, External: []string{"203.0.113.10"}, ClusterIPs: dual},
			cond:       notAssigned,
			condStatus: metav1.ConditionFalse,
			programmed: string(gatewayv1.GatewayReasonAddressNotAssigned),
		},
		{
			name:       "unusable static plus unsupported prefix reports NotAssigned",
			in:         AddressInputs{Spec: []gatewayv1.GatewaySpecAddress{ip("192.0.2.20", cluster), ip("", "example.com/CorpWan")}, CIDRs: cidrs},
			cond:       notAssigned,
			condStatus: metav1.ConditionFalse,
			programmed: string(gatewayv1.GatewayReasonAddressNotAssigned),
		},
		{
			name: "partial: unusable static plus default",
			in: AddressInputs{
				Spec: []gatewayv1.GatewaySpecAddress{ip("192.0.2.20", cluster), ip("", "")}, CIDRs: cidrs,
				External: []string{"203.0.113.10"}, ClusterIPs: dual,
			},
			addrs:      []wantAddr{{"203.0.113.10", ""}},
			cond:       partial,
			condStatus: metav1.ConditionFalse,
		},
		{
			name: "partial but the default is still pending: omitted until it is assigned",
			in: AddressInputs{
				Spec: []gatewayv1.GatewaySpecAddress{ip("192.0.2.20", cluster), ip("", "")}, CIDRs: cidrs,
				LBPending: true, ClusterIPs: dual,
			},
		},
		{
			name:       "hostname class still reports explicit ip entries as ips",
			in:         AddressInputs{Spec: []gatewayv1.GatewaySpecAddress{ip("", "")}, Default: []string{"svc.ns.svc"}, External: []string{"203.0.113.10"}},
			addrs:      []wantAddr{{"203.0.113.10", ""}},
			cond:       assigned,
			condStatus: metav1.ConditionTrue,
		},
		{
			name:       "mixed with default entry first",
			in:         AddressInputs{Spec: []gatewayv1.GatewaySpecAddress{ip("", ""), ip("", cluster)}, External: []string{"203.0.113.10"}, ClusterIPs: dual},
			addrs:      []wantAddr{{"203.0.113.10", ""}, {"10.96.0.5", "Cluster"}, {"fd00:10:96::5", "Cluster"}},
			cond:       assigned,
			condStatus: metav1.ConditionTrue,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveAddresses(tt.in)
			var addrs []wantAddr
			seen := map[string]bool{}
			for _, a := range got.Addresses {
				assert.Equal(t, a.Routability != nil, true)
				assert.Equal(t, seen[a.Value], false) // distinct
				seen[a.Value] = true
				addrs = append(addrs, wantAddr{a.Value, string(*a.Routability)})
			}
			assert.Equal(t, fmt.Sprint(addrs), fmt.Sprint(tt.addrs))
			if tt.cond == "" {
				assert.Equal(t, got.Condition == nil, true)
			} else {
				assert.Equal(t, got.Condition.Reason, tt.cond)
				assert.Equal(t, got.Condition.Status, tt.condStatus)
			}
			assert.Equal(t, got.ProgrammedReason, tt.programmed)
		})
	}
}

func TestManualAddressesAssigned(t *testing.T) {
	hostType := gatewayv1.HostnameAddressType
	hostEntry := gatewayv1.GatewaySpecAddress{Type: &hostType, Value: "svc"}
	cases := []struct {
		name       string
		addrs      []gatewayv1.GatewaySpecAddress
		internal   []string
		pending    []string
		warnings   []string
		reason     string // empty: condition omitted
		status     metav1.ConditionStatus
		programmed string
	}{
		{name: "pending", addrs: []gatewayv1.GatewaySpecAddress{hostEntry}, internal: []string{"svc:80"}, pending: []string{"svc"}, warnings: []string{"address pending"}},
		{
			name: "hostname with routability is unsatisfied",
			addrs: []gatewayv1.GatewaySpecAddress{
				{Type: &hostType, Value: "svc", Routability: gatewayv1.GatewayAddressRoutabilityCluster},
			},
			reason: "NotAssigned", status: metav1.ConditionFalse, programmed: "AddressNotAssigned",
		},
		{
			name: "one of two hostnames has routability",
			addrs: []gatewayv1.GatewaySpecAddress{
				hostEntry, {Type: &hostType, Value: "other", Routability: gatewayv1.GatewayAddressRoutabilityCluster},
			},
			internal: []string{"svc:80"}, reason: "PartiallyAssigned", status: metav1.ConditionFalse,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, progErr := manualAddressesAssigned(tt.addrs, tt.internal, tt.pending, tt.warnings)
			if tt.reason == "" {
				assert.Equal(t, got == nil, true)
			} else {
				assert.Equal(t, got.Reason, tt.reason)
				assert.Equal(t, got.Status, tt.status)
			}
			if tt.programmed == "" {
				assert.Equal(t, progErr == nil, true)
			} else {
				assert.Equal(t, progErr.Reason, tt.programmed)
			}
		})
	}
}
