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

package model

import (
	"strings"

	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/config/host"
)

// hostTrie resolves a Sidecar egress host ("namespace/dnsName") to its candidate services without scanning the
// whole mesh. It is hostname-first: services are indexed by reversed DNS labels ("a.foo.com" under com -> foo ->
// a) and each terminal node carries a byNamespace map, so the namespace is the leaf key rather than the top key.
// This lets an exact FQDN resolve to a single node (also reachable in O(1) via byHostname), a leading-label
// wildcard ("*.foo.com") resolve to the subtree at its suffix node, and the match-all "*" resolve to the whole
// trie; the requested namespace (concrete or the wildcard "*") is applied at each node. Because the namespace
// lives at the leaf, a service is stored exactly once (no per-namespace duplication) and a node keeps every
// service terminating there, so unlike HostnameAndNamespace the candidate set is not lossy.
type hostTrie struct {
	root *hostTrieNode
	// byHostname maps a full hostname to its terminal node for O(1) exact and enumerate-namespaces lookups,
	// avoiding a label walk. The wildcard paths descend root.children instead.
	byHostname map[host.Name]*hostTrieNode
	// order records each service's index in the creation-time-sorted build slice so callers can restore that
	// deterministic order after a map-based dedup (map iteration and collect() walk order are random).
	order map[*Service]int
}

type hostTrieNode struct {
	children map[string]*hostTrieNode
	// byNamespace holds the services terminating at this hostname, keyed by their namespace.
	byNamespace map[string][]*Service
}

// newHostTrie builds the trie over the primary hostnames of services, once per PushContext.
// services must be creation-time sorted; their index in that slice is recorded in order.
func newHostTrie(services []*Service) *hostTrie {
	t := &hostTrie{
		root:       &hostTrieNode{},
		byHostname: make(map[host.Name]*hostTrieNode, len(services)),
		order:      make(map[*Service]int, len(services)),
	}
	for i, svc := range services {
		t.order[svc] = i
		t.insert(svc)
	}
	return t
}

// insert walks the reversed DNS labels of svc's hostname, then files svc under its namespace at the terminal node.
func (t *hostTrie) insert(svc *Service) {
	node := t.root
	labels := strings.Split(string(svc.Hostname), ".")
	for i := len(labels) - 1; i >= 0; i-- {
		child, ok := node.children[labels[i]]
		if !ok {
			if node.children == nil {
				node.children = make(map[string]*hostTrieNode)
			}
			child = &hostTrieNode{}
			node.children[labels[i]] = child
		}
		node = child
	}
	if node.byNamespace == nil {
		node.byNamespace = make(map[string][]*Service)
	}
	ns := svc.Attributes.Namespace
	node.byNamespace[ns] = append(node.byNamespace[ns], svc)
	t.byHostname[svc.Hostname] = node
}

// servicesFor returns the unfiltered candidate services for "namespace/h"; callers reapply visibility. h is the
// match-all "*" (whole trie), a leading-label wildcard "*.foo.com" (its suffix subtree), or an exact FQDN (the
// services at that node). namespace is a concrete namespace or the wildcard "*" (all namespaces at each node).
func (t *hostTrie) servicesFor(namespace string, h host.Name) []*Service {
	if t == nil {
		return nil
	}
	if h == wildcardService {
		var out []*Service
		t.root.collect(namespace, &out)
		return out
	}
	if h.IsWildCarded() {
		// Strip the leading "*." to obtain the suffix, e.g. "*.foo.com" -> "foo.com".
		suffix := strings.TrimPrefix(strings.TrimPrefix(string(h), "*"), ".")
		node := t.root.find(suffix)
		if node == nil {
			return nil
		}
		var out []*Service
		node.collect(namespace, &out)
		return out
	}
	node, ok := t.byHostname[h]
	if !ok {
		return nil
	}
	// Exact host in a concrete namespace: return the stored slice directly (no copy, no allocation).
	if namespace != wildcardNamespace {
		return node.byNamespace[namespace]
	}
	var out []*Service
	node.appendServices(namespace, &out)
	return out
}

// canonicalServiceFor returns the single service that owns the exact hostname h in namespace, or nil if none.
// The trie retains every service terminating at a (hostname, namespace); this collapses them to the one canonical
// service, matching what HostnameAndNamespace stores (see canonicalService for the precedence rule). Callers that
// need every service (e.g. sidecar egress host selection) use servicesFor instead.
func (t *hostTrie) canonicalServiceFor(namespace string, h host.Name) *Service {
	if t == nil {
		return nil
	}
	node, ok := t.byHostname[h]
	if !ok {
		return nil
	}
	return canonicalService(node.byNamespace[namespace])
}

// canonicalServicesByNamespace returns the canonical service (see canonicalService) for the exact hostname h in
// each namespace where it exists, mirroring HostnameAndNamespace[h]. It returns nil when h is absent.
func (t *hostTrie) canonicalServicesByNamespace(h host.Name) map[string]*Service {
	if t == nil {
		return nil
	}
	node, ok := t.byHostname[h]
	if !ok {
		return nil
	}
	out := make(map[string]*Service, len(node.byNamespace))
	for ns, svcs := range node.byNamespace {
		if svc := canonicalService(svcs); svc != nil {
			out[ns] = svc
		}
	}
	return out
}

// canonicalService picks the one service that owns a (hostname, namespace) from all services terminating there,
// reproducing the precedence PushContext.initServiceRegistry applies when populating HostnameAndNamespace: the
// oldest service wins, except a Kubernetes service takes precedence over a non-Kubernetes one (to prevent domain
// squatting on a hostname before a Kubernetes Service is created). services is in creation-time (build) order.
func canonicalService(services []*Service) *Service {
	var canonical *Service
	for _, s := range services {
		if canonical == nil ||
			(canonical.Attributes.ServiceRegistry != provider.Kubernetes && s.Attributes.ServiceRegistry == provider.Kubernetes) {
			canonical = s
		}
	}
	return canonical
}

// find walks the reversed labels of name from this node and returns the terminal node, or nil if absent. An
// empty name returns the receiver (whose subtree is every service under it).
func (n *hostTrieNode) find(name string) *hostTrieNode {
	if name == "" {
		return n
	}
	node := n
	labels := strings.Split(name, ".")
	for i := len(labels) - 1; i >= 0; i-- {
		child, ok := node.children[labels[i]]
		if !ok {
			return nil
		}
		node = child
	}
	return node
}

// collect appends the services at this node and all of its descendants that match namespace to out.
func (n *hostTrieNode) collect(namespace string, out *[]*Service) {
	n.appendServices(namespace, out)
	for _, child := range n.children {
		child.collect(namespace, out)
	}
}

// appendServices appends this node's services for namespace (or all namespaces when namespace is the wildcard).
func (n *hostTrieNode) appendServices(namespace string, out *[]*Service) {
	if namespace == wildcardNamespace {
		for _, svcs := range n.byNamespace {
			*out = append(*out, svcs...)
		}
		return
	}
	*out = append(*out, n.byNamespace[namespace]...)
}
