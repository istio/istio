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

package core

import (
	"testing"

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	"google.golang.org/protobuf/proto"
)

func chainForSNI(names ...string) *listener.FilterChain {
	return &listener.FilterChain{
		FilterChainMatch: &listener.FilterChainMatch{ServerNames: names},
	}
}

func catchAllChain() *listener.FilterChain {
	return &listener.FilterChain{FilterChainMatch: &listener.FilterChainMatch{}}
}

func serverNamesOf(l *listener.Listener) [][]string {
	out := make([][]string, 0, len(l.FilterChains))
	for _, fc := range l.FilterChains {
		out = append(out, fc.GetFilterChainMatch().GetServerNames())
	}
	return out
}

func marshalListener(t *testing.T, l *listener.Listener) []byte {
	t.Helper()
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(l)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// TestSortFilterChainsIsOrderIndependent is the regression test for the listener hash
// churn seen when two pilot replicas built the same set of gateway filter chains in a
// different order.
func TestSortFilterChainsIsOrderIndependent(t *testing.T) {
	a := &listener.Listener{Name: "0.0.0.0_443", FilterChains: []*listener.FilterChain{
		chainForSNI("cj502.test"), chainForSNI("cj502b.test"), chainForSNI("aaa.test"),
	}}
	b := &listener.Listener{Name: "0.0.0.0_443", FilterChains: []*listener.FilterChain{
		chainForSNI("aaa.test"), chainForSNI("cj502.test"), chainForSNI("cj502b.test"),
	}}
	sortFilterChains(a)
	sortFilterChains(b)
	if got, want := string(marshalListener(t, a)), string(marshalListener(t, b)); got != want {
		t.Fatalf("listeners still differ after sorting:\n a=%v\n b=%v", serverNamesOf(a), serverNamesOf(b))
	}
}

// TestSortFilterChainsKeepsCatchAllLast makes sure the deterministic sort cannot change
// matching semantics: a chain with an empty match matches everything, and Envoy uses
// first-match-wins, so it has to stay at the end.
func TestSortFilterChainsKeepsCatchAllLast(t *testing.T) {
	l := &listener.Listener{Name: "0.0.0.0_443", FilterChains: []*listener.FilterChain{
		catchAllChain(), chainForSNI("zzz.test"), chainForSNI("aaa.test"),
	}}
	sortFilterChains(l)
	got := serverNamesOf(l)
	if len(got) != 3 {
		t.Fatalf("unexpected chain count %d", len(got))
	}
	if len(got[2]) != 0 {
		t.Fatalf("catch-all chain is not last: %v", got)
	}
	if got[0][0] != "aaa.test" || got[1][0] != "zzz.test" {
		t.Fatalf("specific chains not sorted: %v", got)
	}
}

// TestSortFilterChainsNoopForSingleChain keeps the common non-TLS gateway listener (one
// chain) untouched.
func TestSortFilterChainsNoopForSingleChain(t *testing.T) {
	only := chainForSNI()
	l := &listener.Listener{Name: "0.0.0.0_80", FilterChains: []*listener.FilterChain{only}}
	sortFilterChains(l)
	if l.FilterChains[0] != only {
		t.Fatal("chain was replaced")
	}
	// must not panic
	sortFilterChains(nil)
	sortFilterChains(&listener.Listener{})
}
