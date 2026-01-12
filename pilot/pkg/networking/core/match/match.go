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

package match

import (
	xds "github.com/cncf/xds/go/xds/core/v3"
	matcher "github.com/cncf/xds/go/xds/type/matcher/v3"
	network "github.com/envoyproxy/go-control-plane/envoy/extensions/matching/common_inputs/network/v3"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pkg/log"
)

var (
	DestinationPort = &xds.TypedExtensionConfig{
		Name:        "port",
		TypedConfig: protoconv.MessageToAny(&network.DestinationPortInput{}),
	}
	DestinationIP = &xds.TypedExtensionConfig{
		Name:        "ip",
		TypedConfig: protoconv.MessageToAny(&network.DestinationIPInput{}),
	}
	SourceIP = &xds.TypedExtensionConfig{
		Name:        "source-ip",
		TypedConfig: protoconv.MessageToAny(&network.SourceIPInput{}),
	}
	SNI = &xds.TypedExtensionConfig{
		Name:        "sni",
		TypedConfig: protoconv.MessageToAny(&network.ServerNameInput{}),
	}
	ApplicationProtocolInput = &xds.TypedExtensionConfig{
		Name:        "application-protocol",
		TypedConfig: protoconv.MessageToAny(&network.ApplicationProtocolInput{}),
	}
	TransportProtocolInput = &xds.TypedExtensionConfig{
		Name:        "transport-protocol",
		TypedConfig: protoconv.MessageToAny(&network.TransportProtocolInput{}),
	}
	AuthorityFilterStateInput = &xds.TypedExtensionConfig{
		Name: "authority-filter-state",
		TypedConfig: protoconv.MessageToAny(&network.FilterStateInput{
			Key: filters.AuthorityFilterStateKey,
		}),
	}
	RequestSourceFilterStateInput = &xds.TypedExtensionConfig{
		Name: "request-source-filter-state",
		TypedConfig: protoconv.MessageToAny(&network.FilterStateInput{
			Key: filters.RequestSourceFilterStateKey,
		}),
	}
)

type Mapper struct {
	*matcher.Matcher
	Map map[string]*matcher.Matcher_OnMatch
}

func newMapper(input *xds.TypedExtensionConfig) Mapper {
	m := map[string]*matcher.Matcher_OnMatch{}
	match := &matcher.Matcher{
		MatcherType: &matcher.Matcher_MatcherTree_{
			MatcherTree: &matcher.Matcher_MatcherTree{
				Input: input,
				TreeType: &matcher.Matcher_MatcherTree_ExactMatchMap{
					ExactMatchMap: &matcher.Matcher_MatcherTree_MatchMap{
						Map: m,
					},
				},
			},
		},
		OnNoMatch: nil,
	}
	return Mapper{Matcher: match, Map: m}
}

func NewDestinationIP() Mapper {
	return newMapper(DestinationIP)
}

func NewSourceIP() Mapper {
	return newMapper(SourceIP)
}

func NewDestinationPort() Mapper {
	return newMapper(DestinationPort)
}

func NewRequestSource() Mapper {
	return newMapper(RequestSourceFilterStateInput)
}

type ProtocolMatch struct {
	TCP, HTTP *matcher.Matcher_OnMatch
}

func NewAppProtocol(pm ProtocolMatch) *matcher.Matcher {
	m := newMapper(ApplicationProtocolInput)
	m.Map["'h2c'"] = pm.HTTP
	m.Map["'http/1.1'"] = pm.HTTP
	if features.HTTP10 {
		m.Map["'http/1.0'"] = pm.HTTP
	}
	m.OnNoMatch = pm.TCP
	return m.Matcher
}

func ToChain(name string) *matcher.Matcher_OnMatch {
	return &matcher.Matcher_OnMatch{
		OnMatch: &matcher.Matcher_OnMatch_Action{
			Action: &xds.TypedExtensionConfig{
				Name:        name,
				TypedConfig: protoconv.MessageToAny(&wrappers.StringValue{Value: name}),
			},
		},
	}
}

func ToMatcher(match *matcher.Matcher) *matcher.Matcher_OnMatch {
	return &matcher.Matcher_OnMatch{
		OnMatch: &matcher.Matcher_OnMatch_Matcher{
			Matcher: match,
		},
	}
}

// BuildMatcher cleans the entire match tree to avoid empty maps and returns a viable top-level matcher.
// Note: this mutates the internal mappers/matchers that make up the tree.
func (m Mapper) BuildMatcher() *matcher.Matcher {
	root := m
	for len(root.Map) == 0 {
		// the top level matcher is empty; if its fallback goes to a matcher, return that
		// TODO is there a way we can just say "always go to action"?
		if fallback := root.GetOnNoMatch(); fallback != nil {
			if replacement, ok := mapperFromMatch(fallback.GetMatcher()); ok {
				root = replacement
				continue
			}
		}
		// no fallback or fallback isn't a mapper
		log.Warnf("could not repair invalid matcher; empty map at root matcher does not have a map fallback")
		return nil
	}
	q := []*matcher.Matcher_OnMatch{m.OnNoMatch}
	for _, onMatch := range root.Map {
		q = append(q, onMatch)
	}

	// fix the matchers, add child mappers OnMatch to the queue
	for len(q) > 0 {
		head := q[0]
		q = q[1:]
		q = append(q, fixEmptyOnMatchMap(head)...)
	}
	return root.Matcher
}

// if the onMatch sends to an empty mapper, make the onMatch send directly to the onNoMatch of that empty mapper
// returns mapper if it doesn't need to be fixed, or can't be fixed
func fixEmptyOnMatchMap(onMatch *matcher.Matcher_OnMatch) []*matcher.Matcher_OnMatch {
	if onMatch == nil {
		return nil
	}
	innerMatcher := onMatch.GetMatcher()
	if innerMatcher == nil {
		// this already just performs an Action
		return nil
	}
	innerMapper, ok := mapperFromMatch(innerMatcher)
	if !ok {
		// this isn't a mapper or action, not supported by this func
		return nil
	}
	if len(innerMapper.Map) > 0 {
		return innerMapper.allOnMatches()
	}

	if fallback := innerMapper.GetOnNoMatch(); fallback != nil {
		// change from: onMatch -> map (empty with fallback) to onMatch -> fallback
		// that fallback may be an empty map, so we re-queue onMatch in case it still needs fixing
		onMatch.OnMatch = fallback.OnMatch
		return []*matcher.Matcher_OnMatch{onMatch} // the inner mapper is gone
	}

	// envoy will nack this eventually
	log.Warnf("empty mapper %v with no fallback", innerMapper.Matcher)
	return innerMapper.allOnMatches()
}

func (m Mapper) allOnMatches() []*matcher.Matcher_OnMatch {
	var out []*matcher.Matcher_OnMatch
	out = append(out, m.OnNoMatch)
	if m.Map == nil {
		return out
	}
	for _, match := range m.Map {
		out = append(out, match)
	}
	return out
}

func mapperFromMatch(mmatcher *matcher.Matcher) (Mapper, bool) {
	if mmatcher == nil {
		return Mapper{}, false
	}
	switch m := mmatcher.MatcherType.(type) {
	case *matcher.Matcher_MatcherTree_:
		var mmap *matcher.Matcher_MatcherTree_MatchMap
		switch t := m.MatcherTree.TreeType.(type) {
		case *matcher.Matcher_MatcherTree_PrefixMatchMap:
			mmap = t.PrefixMatchMap
		case *matcher.Matcher_MatcherTree_ExactMatchMap:
			mmap = t.ExactMatchMap
		default:
			return Mapper{}, false
		}
		return Mapper{Matcher: mmatcher, Map: mmap.Map}, true
	}
	return Mapper{}, false
}
