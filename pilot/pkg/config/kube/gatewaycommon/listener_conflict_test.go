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
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pkg/ptr"
)

func hostname(h string) *gatewayv1.Hostname {
	v := gatewayv1.Hostname(h)
	return &v
}

func conflictsFor(
	gw *gatewayv1.Gateway,
	attached []*gatewayv1.ListenerSet,
	ls *gatewayv1.ListenerSet,
) map[gatewayv1.SectionName]gatewayv1.ListenerConditionReason {
	return GatewayListenerConflicts{Conflicts: ComputeGatewayListenerConflicts(gw, attached)}.ConflictsFor(ls)
}

func conflictsForGateway(gw *gatewayv1.Gateway, attached []*gatewayv1.ListenerSet) map[gatewayv1.SectionName]gatewayv1.ListenerConditionReason {
	return GatewayListenerConflicts{Conflicts: ComputeGatewayListenerConflicts(gw, attached)}.ConflictsForGateway(gw)
}

func TestComputeGatewayListenerConflictsGatewaySelfConflict(t *testing.T) {
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw", Namespace: "infra"},
		Spec: gatewayv1.GatewaySpec{
			Listeners: []gatewayv1.Listener{
				{Name: "first", Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("shared.com")},
				{Name: "second", Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("shared.com")},
			},
		},
	}
	conflicts := conflictsForGateway(gw, nil)
	if _, ok := conflicts["first"]; ok {
		t.Fatalf("first gateway listener should win: %+v", conflicts)
	}
	if got := conflicts["second"]; got != gatewayv1.ListenerReasonHostnameConflict {
		t.Fatalf("second gateway listener conflict: got %q", got)
	}
}

// TestComputeGatewayListenerConflictsOnlyWinningListenersBlock verifies that a Gateway listener
// dropped by an intra-Gateway conflict does not remain in the accepted set and therefore cannot
// block a later ListenerSet listener that only conflicts with the dropped listener's hostname.
//
// Setup: Gateway listener "winner" (foo.com:80) beats Gateway listener "dropped" (foo.com:80).
// ListenerSet listener "other" (bar.com:80) is distinct from the surviving winner and must not
// be marked conflicted. ListenerSet listener "same" (foo.com:80) conflicts with the winner and
// must be marked conflicted.
//
// Note: under HTTP-like distinctness, a listener cannot conflict with a dropped Gateway
// listener without also conflicting with the Gateway listener that beat it on the same port;
// dropped listeners are same-port losers, so any candidate that would have lost to them would
// also lose to the winner. Only winners occupy slots.
func TestComputeGatewayListenerConflictsOnlyWinningListenersBlock(t *testing.T) {
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw", Namespace: "infra"},
		Spec: gatewayv1.GatewaySpec{
			Listeners: []gatewayv1.Listener{
				{Name: "winner", Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("foo.com")},
				{Name: "dropped", Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("foo.com")},
			},
		},
	}
	ls := &gatewayv1.ListenerSet{
		ObjectMeta: metav1.ObjectMeta{Name: "ls", Namespace: "infra", CreationTimestamp: metav1.NewTime(time.Unix(1, 0))},
		Spec: gatewayv1.ListenerSetSpec{
			Listeners: []gatewayv1.ListenerEntry{
				{Name: "other", Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("bar.com")},
				{Name: "same", Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("foo.com")},
			},
		},
	}

	gwConflicts := conflictsForGateway(gw, []*gatewayv1.ListenerSet{ls})
	if got := gwConflicts["dropped"]; got != gatewayv1.ListenerReasonHostnameConflict {
		t.Fatalf("dropped gateway listener: got %q want %q", got, gatewayv1.ListenerReasonHostnameConflict)
	}

	lsConflicts := conflictsFor(gw, []*gatewayv1.ListenerSet{ls}, ls)
	if _, ok := lsConflicts["other"]; ok {
		t.Fatalf("listener distinct from surviving winner should be accepted: %+v", lsConflicts)
	}
	if got := lsConflicts["same"]; got != gatewayv1.ListenerReasonHostnameConflict {
		t.Fatalf("listener conflicting with surviving winner: got %q", got)
	}
}

func TestComputeGatewayListenerConflictsImplicitPort(t *testing.T) {
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw", Namespace: "infra"},
		Spec: gatewayv1.GatewaySpec{
			Listeners: []gatewayv1.Listener{
				{Name: "gw", Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("foo.com")},
			},
		},
	}
	ls := &gatewayv1.ListenerSet{
		ObjectMeta: metav1.ObjectMeta{Name: "ls", Namespace: "infra", CreationTimestamp: metav1.NewTime(time.Unix(1, 0))},
		Spec: gatewayv1.ListenerSetSpec{
			Listeners: []gatewayv1.ListenerEntry{
				{Name: "implicit", Port: 0, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("foo.com")},
				{Name: "distinct", Port: 0, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("bar.com")},
			},
		},
	}

	conflicts := conflictsFor(gw, []*gatewayv1.ListenerSet{ls}, ls)
	if got := conflicts["implicit"]; got != gatewayv1.ListenerReasonHostnameConflict {
		t.Fatalf("implicit port HTTP listener on :80/foo.com: got %q want %q", got, gatewayv1.ListenerReasonHostnameConflict)
	}
	if _, ok := conflicts["distinct"]; ok {
		t.Fatalf("distinct hostname should not conflict: %+v", conflicts)
	}
}

func TestComputeGatewayListenerConflictsHostnameConflict(t *testing.T) {
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw", Namespace: "infra"},
		Spec: gatewayv1.GatewaySpec{
			Listeners: []gatewayv1.Listener{
				{Name: "gateway-listener", Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("gateway-listener.com")},
				{
					Name:     "hostname-conflict-with-gateway-listener",
					Port:     80,
					Protocol: gatewayv1.HTTPProtocolType,
					Hostname: hostname("hostname-conflict-with-gateway-listener.com"),
				},
			},
		},
	}
	ls1 := &gatewayv1.ListenerSet{
		ObjectMeta: metav1.ObjectMeta{Name: "ls1", Namespace: "infra", CreationTimestamp: metav1.NewTime(time.Unix(1, 0))},
		Spec: gatewayv1.ListenerSetSpec{
			Listeners: []gatewayv1.ListenerEntry{
				{Name: "listener-set-1-listener", Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("listener-set-1-listener.com")},
				{
					Name:     "hostname-conflict-with-gateway-listener",
					Port:     80,
					Protocol: gatewayv1.HTTPProtocolType,
					Hostname: hostname("hostname-conflict-with-gateway-listener.com"),
				},
				{
					Name:     "hostname-conflict-with-listener-set-listener",
					Port:     80,
					Protocol: gatewayv1.HTTPProtocolType,
					Hostname: hostname("hostname-conflict-with-listener-set-listener.com"),
				},
			},
		},
	}
	ls2 := &gatewayv1.ListenerSet{
		ObjectMeta: metav1.ObjectMeta{Name: "ls2", Namespace: "infra", CreationTimestamp: metav1.NewTime(time.Unix(2, 0))},
		Spec: gatewayv1.ListenerSetSpec{
			Listeners: []gatewayv1.ListenerEntry{
				{
					Name:     "hostname-conflict-with-gateway-listener",
					Port:     80,
					Protocol: gatewayv1.HTTPProtocolType,
					Hostname: hostname("hostname-conflict-with-gateway-listener.com"),
				},
			},
		},
	}
	ls3 := &gatewayv1.ListenerSet{
		ObjectMeta: metav1.ObjectMeta{Name: "ls3", Namespace: "infra", CreationTimestamp: metav1.NewTime(time.Unix(3, 0))},
		Spec: gatewayv1.ListenerSetSpec{
			Listeners: []gatewayv1.ListenerEntry{
				{Name: "listener-set-2-listener", Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("listener-set-2-listener.com")},
				{
					Name:     "hostname-conflict-with-listener-set-listener",
					Port:     80,
					Protocol: gatewayv1.HTTPProtocolType,
					Hostname: hostname("hostname-conflict-with-listener-set-listener.com"),
				},
			},
		},
	}
	ls4 := &gatewayv1.ListenerSet{
		ObjectMeta: metav1.ObjectMeta{Name: "ls4", Namespace: "infra", CreationTimestamp: metav1.NewTime(time.Unix(4, 0))},
		Spec: gatewayv1.ListenerSetSpec{
			Listeners: []gatewayv1.ListenerEntry{
				{
					Name:     "hostname-conflict-with-listener-set-listener",
					Port:     80,
					Protocol: gatewayv1.HTTPProtocolType,
					Hostname: hostname("hostname-conflict-with-listener-set-listener.com"),
				},
			},
		},
	}

	conflicts := conflictsFor(gw, []*gatewayv1.ListenerSet{ls1, ls2, ls3, ls4}, ls1)
	if got, want := conflicts["hostname-conflict-with-gateway-listener"], gatewayv1.ListenerReasonHostnameConflict; got != want {
		t.Fatalf("ls1 gateway conflict: got %q want %q", got, want)
	}
	if _, ok := conflicts["listener-set-1-listener"]; ok {
		t.Fatalf("ls1 valid listener should not conflict")
	}
	if _, ok := conflicts["hostname-conflict-with-listener-set-listener"]; ok {
		t.Fatalf("ls1 winning listener should not conflict")
	}

	conflicts = conflictsFor(gw, []*gatewayv1.ListenerSet{ls1, ls2, ls3, ls4}, ls2)
	if len(conflicts) != 1 || conflicts["hostname-conflict-with-gateway-listener"] != gatewayv1.ListenerReasonHostnameConflict {
		t.Fatalf("ls2 conflicts: %+v", conflicts)
	}

	conflicts = conflictsFor(gw, []*gatewayv1.ListenerSet{ls1, ls2, ls3, ls4}, ls3)
	if got := conflicts["hostname-conflict-with-listener-set-listener"]; got != gatewayv1.ListenerReasonHostnameConflict {
		t.Fatalf("ls3 sibling conflict: got %q", got)
	}

	conflicts = conflictsFor(gw, []*gatewayv1.ListenerSet{ls1, ls2, ls3, ls4}, ls4)
	if len(conflicts) != 1 || conflicts["hostname-conflict-with-listener-set-listener"] != gatewayv1.ListenerReasonHostnameConflict {
		t.Fatalf("ls4 conflicts: %+v", conflicts)
	}
}

func TestComputeGatewayListenerConflictsProtocolConflict(t *testing.T) {
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw", Namespace: "infra"},
		Spec: gatewayv1.GatewaySpec{
			Listeners: []gatewayv1.Listener{
				{Name: "gateway-listener", Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("gateway-listener.com")},
				{
					Name:     "protocol-conflict-with-gateway-listener",
					Port:     80,
					Protocol: gatewayv1.HTTPProtocolType,
					Hostname: hostname("protocol-conflict-with-gateway-listener.com"),
				},
			},
		},
	}
	ls1 := &gatewayv1.ListenerSet{
		ObjectMeta: metav1.ObjectMeta{Name: "ls1", Namespace: "infra", CreationTimestamp: metav1.NewTime(time.Unix(1, 0))},
		Spec: gatewayv1.ListenerSetSpec{
			Listeners: []gatewayv1.ListenerEntry{
				{Name: "listener-set-1-listener", Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("listener-set-1-listener.com")},
				{Name: "protocol-conflict-with-gateway-listener", Port: 80, Protocol: gatewayv1.TCPProtocolType},
				{
					Name:     "protocol-conflict-with-listener-set-listener",
					Port:     80,
					Protocol: gatewayv1.HTTPProtocolType,
					Hostname: hostname("protocol-conflict-with-listener-set-listener.com"),
				},
			},
		},
	}
	ls2 := &gatewayv1.ListenerSet{
		ObjectMeta: metav1.ObjectMeta{Name: "ls2", Namespace: "infra", CreationTimestamp: metav1.NewTime(time.Unix(2, 0))},
		Spec: gatewayv1.ListenerSetSpec{
			Listeners: []gatewayv1.ListenerEntry{
				{Name: "protocol-conflict-with-gateway-listener", Port: 80, Protocol: gatewayv1.TCPProtocolType},
			},
		},
	}
	ls3 := &gatewayv1.ListenerSet{
		ObjectMeta: metav1.ObjectMeta{Name: "ls3", Namespace: "infra", CreationTimestamp: metav1.NewTime(time.Unix(3, 0))},
		Spec: gatewayv1.ListenerSetSpec{
			Listeners: []gatewayv1.ListenerEntry{
				{Name: "listener-set-2-listener", Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("listener-set-2-listener.com")},
				{Name: "protocol-conflict-with-listener-set-listener", Port: 80, Protocol: gatewayv1.TCPProtocolType},
			},
		},
	}
	ls4 := &gatewayv1.ListenerSet{
		ObjectMeta: metav1.ObjectMeta{Name: "ls4", Namespace: "infra", CreationTimestamp: metav1.NewTime(time.Unix(4, 0))},
		Spec: gatewayv1.ListenerSetSpec{
			Listeners: []gatewayv1.ListenerEntry{
				{Name: "protocol-conflict-with-listener-set-listener", Port: 80, Protocol: gatewayv1.TCPProtocolType},
			},
		},
	}

	conflicts := conflictsFor(gw, []*gatewayv1.ListenerSet{ls1, ls2, ls3, ls4}, ls1)
	if got := conflicts["protocol-conflict-with-gateway-listener"]; got != gatewayv1.ListenerReasonProtocolConflict {
		t.Fatalf("ls1 gateway protocol conflict: got %q", got)
	}
	if _, ok := conflicts["protocol-conflict-with-listener-set-listener"]; ok {
		t.Fatalf("ls1 winning listener should not conflict: %+v", conflicts)
	}

	conflicts = conflictsFor(gw, []*gatewayv1.ListenerSet{ls1, ls2, ls3, ls4}, ls3)
	if got := conflicts["protocol-conflict-with-listener-set-listener"]; got != gatewayv1.ListenerReasonProtocolConflict {
		t.Fatalf("ls3 sibling protocol conflict: got %q", got)
	}
}

func TestListenersDistinct(t *testing.T) {
	wildcard := hostname("*")
	cases := []struct {
		name string
		a, b gatewayv1.Listener
		want bool
	}{
		{
			name: "different hostnames",
			a:    gatewayv1.Listener{Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("a.com")},
			b:    gatewayv1.Listener{Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("b.com")},
			want: true,
		},
		{
			name: "same hostname",
			a:    gatewayv1.Listener{Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("a.com")},
			b:    gatewayv1.Listener{Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("a.com")},
			want: false,
		},
		{
			name: "http vs tcp",
			a:    gatewayv1.Listener{Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("a.com")},
			b:    gatewayv1.Listener{Port: 80, Protocol: gatewayv1.TCPProtocolType},
			want: false,
		},
		{
			name: "nil hostname vs explicit wildcard",
			a:    gatewayv1.Listener{Port: 443, Protocol: gatewayv1.HTTPSProtocolType},
			b:    gatewayv1.Listener{Port: 443, Protocol: gatewayv1.HTTPSProtocolType, Hostname: wildcard},
			want: false,
		},
		{
			name: "nil hostname vs specific hostname",
			a:    gatewayv1.Listener{Port: 443, Protocol: gatewayv1.HTTPSProtocolType},
			b:    gatewayv1.Listener{Port: 443, Protocol: gatewayv1.HTTPSProtocolType, Hostname: hostname("httpbin.example.com")},
			want: true,
		},
		{
			name: "https vs tls different hostnames",
			a:    gatewayv1.Listener{Port: 443, Protocol: gatewayv1.HTTPSProtocolType, Hostname: hostname("a.com")},
			b:    gatewayv1.Listener{Port: 443, Protocol: gatewayv1.TLSProtocolType, Hostname: hostname("b.com")},
			want: true,
		},
		{
			name: "https vs tls same hostname",
			a:    gatewayv1.Listener{Port: 443, Protocol: gatewayv1.HTTPSProtocolType, Hostname: hostname("a.com")},
			b:    gatewayv1.Listener{Port: 443, Protocol: gatewayv1.TLSProtocolType, Hostname: hostname("a.com")},
			want: false,
		},
		{
			name: "tcp vs tcp same port",
			a:    gatewayv1.Listener{Port: 8080, Protocol: gatewayv1.TCPProtocolType},
			b:    gatewayv1.Listener{Port: 8080, Protocol: gatewayv1.TCPProtocolType},
			want: false,
		},
		{
			name: "tcp vs udp same port",
			a:    gatewayv1.Listener{Port: 8080, Protocol: gatewayv1.TCPProtocolType},
			b:    gatewayv1.Listener{Port: 8080, Protocol: gatewayv1.UDPProtocolType},
			want: false,
		},
		{
			name: "different ports",
			a:    gatewayv1.Listener{Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("a.com")},
			b:    gatewayv1.Listener{Port: 81, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("a.com")},
			want: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := listenersDistinct(c.a, c.b); got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestListenerConflictReason(t *testing.T) {
	cases := []struct {
		name string
		a, b gatewayv1.Listener
		want gatewayv1.ListenerConditionReason
	}{
		{
			name: "http vs tcp",
			a:    gatewayv1.Listener{Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("a.com")},
			b:    gatewayv1.Listener{Port: 80, Protocol: gatewayv1.TCPProtocolType},
			want: gatewayv1.ListenerReasonProtocolConflict,
		},
		{
			name: "tcp vs udp",
			a:    gatewayv1.Listener{Port: 8080, Protocol: gatewayv1.TCPProtocolType},
			b:    gatewayv1.Listener{Port: 8080, Protocol: gatewayv1.UDPProtocolType},
			want: gatewayv1.ListenerReasonProtocolConflict,
		},
		{
			name: "http same hostname",
			a:    gatewayv1.Listener{Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("a.com")},
			b:    gatewayv1.Listener{Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("a.com")},
			want: gatewayv1.ListenerReasonHostnameConflict,
		},
		{
			name: "https vs tls same hostname",
			a:    gatewayv1.Listener{Port: 443, Protocol: gatewayv1.HTTPSProtocolType, Hostname: hostname("a.com")},
			b:    gatewayv1.Listener{Port: 443, Protocol: gatewayv1.TLSProtocolType, Hostname: hostname("a.com")},
			want: gatewayv1.ListenerReasonProtocolConflict,
		},
		{
			name: "tcp vs tcp same port",
			a:    gatewayv1.Listener{Port: 8080, Protocol: gatewayv1.TCPProtocolType},
			b:    gatewayv1.Listener{Port: 8080, Protocol: gatewayv1.TCPProtocolType},
			want: gatewayv1.ListenerReasonProtocolConflict,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := listenerConflictReason(c.a, c.b); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestComputeGatewayListenerConflictsIntraSetHostnameConflict(t *testing.T) {
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw", Namespace: "infra"},
		Spec:       gatewayv1.GatewaySpec{},
	}
	ls := &gatewayv1.ListenerSet{
		ObjectMeta: metav1.ObjectMeta{Name: "ls1", Namespace: "infra", CreationTimestamp: metav1.NewTime(time.Unix(1, 0))},
		Spec: gatewayv1.ListenerSetSpec{
			Listeners: []gatewayv1.ListenerEntry{
				{Name: "first", Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("shared.com")},
				{Name: "second", Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("shared.com")},
			},
		},
	}
	conflicts := conflictsFor(gw, []*gatewayv1.ListenerSet{ls}, ls)
	if got := conflicts["second"]; got != gatewayv1.ListenerReasonHostnameConflict {
		t.Fatalf("intra-set conflict: got %q", got)
	}
	if _, ok := conflicts["first"]; ok {
		t.Fatalf("first listener in spec order should win")
	}
}

func TestComputeGatewayListenerConflictsTCPUDPConflict(t *testing.T) {
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw", Namespace: "infra"},
		Spec:       gatewayv1.GatewaySpec{},
	}
	ls1 := &gatewayv1.ListenerSet{
		ObjectMeta: metav1.ObjectMeta{Name: "ls1", Namespace: "infra", CreationTimestamp: metav1.NewTime(time.Unix(1, 0))},
		Spec: gatewayv1.ListenerSetSpec{
			Listeners: []gatewayv1.ListenerEntry{
				{Name: "ls-tcp", Port: 8080, Protocol: gatewayv1.TCPProtocolType},
			},
		},
	}
	ls2 := &gatewayv1.ListenerSet{
		ObjectMeta: metav1.ObjectMeta{Name: "ls2", Namespace: "infra", CreationTimestamp: metav1.NewTime(time.Unix(2, 0))},
		Spec: gatewayv1.ListenerSetSpec{
			Listeners: []gatewayv1.ListenerEntry{
				{Name: "ls-udp", Port: 8080, Protocol: gatewayv1.UDPProtocolType},
			},
		},
	}
	conflicts := conflictsFor(gw, []*gatewayv1.ListenerSet{ls1, ls2}, ls2)
	if got := conflicts["ls-udp"]; got != gatewayv1.ListenerReasonProtocolConflict {
		t.Fatalf("cross-set tcp/udp conflict: got %q want %q", got, gatewayv1.ListenerReasonProtocolConflict)
	}
}

func TestConflictReasonsNilHostnameWildcardConflict(t *testing.T) {
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw", Namespace: "infra"},
		Spec: gatewayv1.GatewaySpec{
			Listeners: []gatewayv1.Listener{
				{Name: "wildcard", Port: 443, Protocol: gatewayv1.HTTPSProtocolType},
			},
		},
	}
	ls := &gatewayv1.ListenerSet{
		ObjectMeta: metav1.ObjectMeta{Name: "ls1", Namespace: "infra", CreationTimestamp: metav1.NewTime(time.Unix(1, 0))},
		Spec: gatewayv1.ListenerSetSpec{
			Listeners: []gatewayv1.ListenerEntry{
				{Name: "also-wildcard", Port: 443, Protocol: gatewayv1.HTTPSProtocolType, Hostname: hostname("*")},
			},
		},
	}
	conflicts := conflictsFor(gw, []*gatewayv1.ListenerSet{ls}, ls)
	if got := conflicts["also-wildcard"]; got != gatewayv1.ListenerReasonHostnameConflict {
		t.Fatalf("expected hostname conflict for nil vs *, got %q", got)
	}
}

func TestEligibleListenerSetsForConflict(t *testing.T) {
	allowed := &gatewayv1.ListenerSet{ObjectMeta: metav1.ObjectMeta{Name: "ok", Namespace: "infra"}}
	denied := &gatewayv1.ListenerSet{ObjectMeta: metav1.ObjectMeta{Name: "no", Namespace: "other"}}
	got := EligibleListenerSetsForConflict([]*gatewayv1.ListenerSet{allowed, denied}, func(ns string) bool {
		return ns == "infra"
	})
	if len(got) != 1 || got[0].Name != "ok" {
		t.Fatalf("eligible sets: %+v", got)
	}
}

func TestConflictReasonsExcludesIneligibleListenerSets(t *testing.T) {
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw", Namespace: "infra"},
		Spec: gatewayv1.GatewaySpec{
			Listeners: []gatewayv1.Listener{
				{Name: "gw-listener", Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("foo.com")},
			},
			AllowedListeners: &gatewayv1.AllowedListeners{
				Namespaces: &gatewayv1.ListenerNamespaces{
					From: ptr.Of(gatewayv1.NamespacesFromSame),
				},
			},
		},
	}
	// Older denied-namespace set would win precedence for bar.com if included in merge.
	denied := &gatewayv1.ListenerSet{
		ObjectMeta: metav1.ObjectMeta{Name: "denied", Namespace: "other", CreationTimestamp: metav1.NewTime(time.Unix(0, 0))},
		Spec: gatewayv1.ListenerSetSpec{
			Listeners: []gatewayv1.ListenerEntry{
				{Name: "denied-listener", Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("bar.com")},
			},
		},
	}
	allowed := &gatewayv1.ListenerSet{
		ObjectMeta: metav1.ObjectMeta{Name: "allowed", Namespace: "infra", CreationTimestamp: metav1.NewTime(time.Unix(1, 0))},
		Spec: gatewayv1.ListenerSetSpec{
			Listeners: []gatewayv1.ListenerEntry{
				{Name: "mine", Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostname("bar.com")},
			},
		},
	}
	eligible := EligibleListenerSetsForConflict([]*gatewayv1.ListenerSet{denied, allowed}, func(ns string) bool {
		return ns == "infra"
	})
	conflicts := conflictsFor(gw, eligible, allowed)
	if len(conflicts) != 0 {
		t.Fatalf("ineligible set must not cause conflict: %+v", conflicts)
	}

	// Including the denied set would incorrectly mark the allowed listener conflicted.
	all := conflictsFor(gw, []*gatewayv1.ListenerSet{denied, allowed}, allowed)
	if got := all["mine"]; got != gatewayv1.ListenerReasonHostnameConflict {
		t.Fatalf("with ineligible set included, expected hostname conflict, got %q", got)
	}
}

func TestListenerSetParentKey(t *testing.T) {
	ls := &gatewayv1.ListenerSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: "user"},
		Spec: gatewayv1.ListenerSetSpec{
			ParentRef: gatewayv1.ParentGatewayReference{
				Name:      "gw",
				Namespace: ptr.Of(gatewayv1.Namespace("infra")),
			},
		},
	}
	ns, name := ListenerSetParentKey(ls)
	if ns != "infra" || name != "gw" {
		t.Fatalf("got %q/%q", ns, name)
	}
}
