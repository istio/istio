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

package xdstest

import (
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
)

// EvaluateListenerFilterPredicates runs through the ListenerFilterChainMatchPredicate logic
// This is exposed for testing only, and should not be used in XDS generation code
func EvaluateListenerFilterPredicates(predicate *listener.ListenerFilterChainMatchPredicate, port int) bool {
	if predicate == nil {
		return true
	}
	switch r := predicate.Rule.(type) {
	case *listener.ListenerFilterChainMatchPredicate_NotMatch:
		return !EvaluateListenerFilterPredicates(r.NotMatch, port)
	case *listener.ListenerFilterChainMatchPredicate_OrMatch:
		matches := false
		for _, r := range r.OrMatch.Rules {
			matches = matches || EvaluateListenerFilterPredicates(r, port)
		}
		return matches
	case *listener.ListenerFilterChainMatchPredicate_DestinationPortRange:
		return int32(port) >= r.DestinationPortRange.GetStart() && int32(port) < r.DestinationPortRange.GetEnd()
	default:
		panic("unsupported predicate")
	}
}
