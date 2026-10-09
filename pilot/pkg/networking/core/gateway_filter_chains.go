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
	"bytes"
	"sort"

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	"google.golang.org/protobuf/proto"
)

// sortFilterChains puts a gateway listener's filter chains into a deterministic order.
//
// Chains are appended while pilot iterates a map (see mutableopts in
// buildGatewayListeners), so two pilot replicas can emit the same set of chains in a
// different order. Order is part of the listener proto and Envoy hashes the proto to
// decide whether an update is required, so that difference was reported as a config
// change and drove the in-place filter chain update path on every reconnect that landed
// on another replica. That path rebuilds and drains filter chains, and when the drain
// expires the connections bound to them are closed with response code detail
// "filter_chain_is_being_removed".
//
// Ordering rules:
//   - a chain with an empty FilterChainMatch matches every connection, and matching is
//     first-match-wins, so such catch-all chains must stay last;
//   - the other chains are ordered by the deterministic serialization of their match,
//     which is a pure function of the match content;
//   - the deterministic serialization of the whole chain is used as a tie-break so that
//     the order is total, and therefore identical in every process.
func sortFilterChains(l *listener.Listener) {
	if l == nil || len(l.FilterChains) < 2 {
		return
	}
	type chainKey struct {
		match []byte
		chain []byte
	}
	keys := make([]chainKey, len(l.FilterChains))
	opts := proto.MarshalOptions{Deterministic: true}
	for i, fc := range l.FilterChains {
		if fc == nil {
			continue
		}
		if m := fc.GetFilterChainMatch(); m != nil {
			if b, err := opts.Marshal(m); err == nil {
				keys[i].match = b
			}
		}
		if b, err := opts.Marshal(fc); err == nil {
			keys[i].chain = b
		}
	}

	order := make([]int, len(l.FilterChains))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		ka, kb := keys[order[a]], keys[order[b]]
		if (len(ka.match) == 0) != (len(kb.match) == 0) {
			// keep catch-all chains last
			return len(kb.match) == 0
		}
		if c := bytes.Compare(ka.match, kb.match); c != 0 {
			return c < 0
		}
		return bytes.Compare(ka.chain, kb.chain) < 0
	})

	sorted := make([]*listener.FilterChain, len(l.FilterChains))
	for i, o := range order {
		sorted[i] = l.FilterChains[o]
	}
	l.FilterChains = sorted
}
