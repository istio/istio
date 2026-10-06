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

package client

import (
	"net/netip"
	"strings"

	"github.com/miekg/dns"

	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	dnsProto "istio.io/istio/pkg/dns/proto"
	"istio.io/istio/pkg/slices"
	netutil "istio.io/istio/pkg/util/net"
	"istio.io/istio/pkg/util/sets"
)

// nameIndex holds every candidate for each name offered by Delta NDS resources, so that an update only recomputes
// the names it touches. It is only accessed by the writer; DNS queries read the LookupTable it maintains.
type nameIndex struct {
	table *LookupTable
	// search is the first search namespace with a trailing dot, or empty.
	search     string
	resources  map[string][]*nameCandidate
	candidates map[string][]*nameCandidate
}

// nameEntry is a NameTable entry with its IPs parsed once, shared by the entry's name and aliases.
type nameEntry struct {
	info       *dnsProto.NameTable_NameInfo
	ipv4, ipv6 []netip.Addr
}

func (e *nameEntry) hasAddresses() bool {
	return len(e.ipv4) > 0 || len(e.ipv6) > 0
}

type nameCandidate struct {
	// name is lowercase with a trailing dot.
	name     string
	entry    *nameEntry
	exact    bool
	resource string
	source   string
}

// preferredTo reports whether c wins a name over other: exact names beat aliases, then Kubernetes beats other
// registries, then the lexically smaller resource and source name win.
func (c *nameCandidate) preferredTo(other *nameCandidate) bool {
	if c.exact != other.exact {
		return c.exact
	}
	kube, otherKube := c.entry.info.Registry == string(provider.Kubernetes), other.entry.info.Registry == string(provider.Kubernetes)
	if kube != otherKube {
		return kube
	}
	if c.resource != other.resource {
		return c.resource < other.resource
	}
	return c.source < other.source
}

// nameRecord is the computed lookup state of one name; found is false if the name must be removed.
type nameRecord struct {
	name           string
	found          bool
	a, aaaa, cname []dns.RR
}

func newNameIndex(searchNamespaces []string) *nameIndex {
	idx := &nameIndex{
		table: &LookupTable{
			allHosts: sets.String{},
			name4:    map[string][]dns.RR{},
			name6:    map[string][]dns.RR{},
			cname:    map[string][]dns.RR{},
		},
		resources:  map[string][]*nameCandidate{},
		candidates: map[string][]*nameCandidate{},
	}
	if len(searchNamespaces) > 0 {
		idx.search = strings.TrimSuffix(strings.ToLower(searchNamespaces[0]), ".") + "."
	}
	return idx
}

// apply updates the index and returns the records of every affected name, without touching the LookupTable.
func (idx *nameIndex) apply(updated map[string]*dnsProto.NameTable, removed []string) []nameRecord {
	dirty := sets.New[string]()
	for _, resource := range removed {
		idx.remove(resource, dirty)
	}
	for resource, nt := range updated {
		idx.remove(resource, dirty)
		idx.add(resource, nt, dirty)
	}
	if idx.search != "" {
		// A name's record decides whether its search-expanded CNAME exists.
		for _, name := range dirty.UnsortedList() {
			if !strings.HasSuffix(name, idx.search) {
				dirty.Insert(name + idx.search)
			}
		}
	}
	records := make([]nameRecord, 0, len(dirty))
	for name := range dirty {
		records = append(records, idx.record(name))
	}
	return records
}

func (idx *nameIndex) remove(resource string, dirty sets.String) {
	for _, c := range idx.resources[resource] {
		remaining := slices.FilterInPlace(idx.candidates[c.name], func(o *nameCandidate) bool { return o != c })
		if len(remaining) == 0 {
			delete(idx.candidates, c.name)
		} else {
			idx.candidates[c.name] = remaining
		}
		dirty.Insert(c.name)
	}
	delete(idx.resources, resource)
}

func (idx *nameIndex) add(resource string, nt *dnsProto.NameTable, dirty sets.String) {
	var offers []*nameCandidate
	offer := func(name string, entry *nameEntry, exact bool, source string) {
		c := &nameCandidate{name: normalizeName(name), entry: entry, exact: exact, resource: resource, source: source}
		idx.candidates[c.name] = append(idx.candidates[c.name], c)
		offers = append(offers, c)
		dirty.Insert(c.name)
	}
	for source, info := range nt.GetTable() {
		if info == nil {
			continue
		}
		ipv4, ipv6 := netutil.ParseIPsSplitToV4V6(info.Ips)
		entry := &nameEntry{info: info, ipv4: ipv4, ipv6: ipv6}
		offer(source, entry, true, source)
		for _, alias := range info.Aliases {
			offer(alias, entry, false, source)
		}
	}
	idx.resources[resource] = offers
}

func (idx *nameIndex) winner(name string) *nameCandidate {
	var best *nameCandidate
	for _, c := range idx.candidates[name] {
		if best == nil || c.preferredTo(best) {
			best = c
		}
	}
	return best
}

func (idx *nameIndex) record(name string) nameRecord {
	if w := idx.winner(name); w != nil && w.entry.hasAddresses() {
		r := nameRecord{name: name, found: true}
		if len(w.entry.ipv4) > 0 {
			r.a = a(name, w.entry.ipv4)
		}
		if len(w.entry.ipv6) > 0 {
			r.aaaa = aaaa(name, w.entry.ipv6)
		}
		return r
	}
	// Unlike the legacy path, a search-expanded name never shadows a name sent by Istiod.
	if base, ok := strings.CutSuffix(name, idx.search); idx.search != "" && ok && !strings.HasSuffix(base, idx.search) {
		if w := idx.winner(base); w != nil && w.entry.hasAddresses() {
			return nameRecord{name: name, found: true, cname: cname(name, base)}
		}
	}
	return nameRecord{name: name}
}

// nameTable returns the winning entry of every name, keyed without the trailing dot, for debugging.
func (idx *nameIndex) nameTable() *dnsProto.NameTable {
	out := &dnsProto.NameTable{Table: make(map[string]*dnsProto.NameTable_NameInfo, len(idx.candidates))}
	for name := range idx.candidates {
		out.Table[strings.TrimSuffix(name, ".")] = idx.winner(name).entry.info
	}
	return out
}

// set applies a record; the caller holds table.mu for writing.
func (table *LookupTable) set(r nameRecord) {
	if !r.found {
		table.allHosts.Delete(r.name)
	} else {
		table.allHosts.Insert(r.name)
	}
	setOrDelete(table.name4, r.name, r.a)
	setOrDelete(table.name6, r.name, r.aaaa)
	setOrDelete(table.cname, r.name, r.cname)
}

func setOrDelete(m map[string][]dns.RR, name string, rrs []dns.RR) {
	if len(rrs) == 0 {
		delete(m, name)
	} else {
		m[name] = rrs
	}
}

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSuffix(name, ".")) + "."
}
