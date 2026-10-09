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
	"fmt"
	"math/rand/v2"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/miekg/dns"
	"google.golang.org/protobuf/proto"

	dnsProto "istio.io/istio/pkg/dns/proto"
	"istio.io/istio/pkg/maps"
	netutil "istio.io/istio/pkg/util/net"
	"istio.io/istio/pkg/util/sets"
)

func lookupString(t testing.TB, lt *LookupTable, name string) string {
	t.Helper()
	answers, found := lt.lookupHost(dns.TypeA, name)
	if !found {
		return ""
	}
	var out []string
	for _, rr := range answers {
		switch rr := rr.(type) {
		case *dns.A:
			out = append(out, rr.A.String())
		case *dns.CNAME:
			out = append(out, "cname:"+rr.Target)
		}
	}
	return strings.Join(out, ",")
}

func TestApplyNameTables(t *testing.T) {
	kube := func(ip string, aliases ...string) *dnsProto.NameTable_NameInfo {
		return &dnsProto.NameTable_NameInfo{Ips: []string{ip}, Registry: "Kubernetes", Aliases: aliases}
	}
	external := func(ip string) *dnsProto.NameTable_NameInfo {
		return &dnsProto.NameTable_NameInfo{Ips: []string{ip}, Registry: "External"}
	}
	h := &LocalDNSServer{searchNamespaces: []string{"ns1.svc.cluster.local"}}
	h.ApplyNameTables(map[string]*dnsProto.NameTable{
		"productpage.ns1.svc.cluster.local": {Table: map[string]*dnsProto.NameTable_NameInfo{
			"productpage.ns1.svc.cluster.local": kube("10.0.0.1", "productpage", "productpage.ns1", "productpage.ns1.svc"),
		}},
		"productpage":    {Table: map[string]*dnsProto.NameTable_NameInfo{"productpage": external("192.0.2.1")}},
		"*.wildcard.com": {Table: map[string]*dnsProto.NameTable_NameInfo{"*.wildcard.com": external("192.0.2.2")}},
		// An exact name equal to another name's search expansion is not shadowed by a CNAME.
		"details.ns1.ns1.svc.cluster.local": {Table: map[string]*dnsProto.NameTable_NameInfo{
			"details.ns1.ns1.svc.cluster.local": external("192.0.2.3"),
		}},
		"details.ns1.svc.cluster.local": {Table: map[string]*dnsProto.NameTable_NameInfo{
			"details.ns1.svc.cluster.local": kube("10.0.0.2", "details", "details.ns1", "details.ns1.svc"),
		}},
	}, nil)
	lt := h.lookupTable.Load().(*LookupTable)
	check := func(cases map[string]string) {
		t.Helper()
		for name, want := range cases {
			if got := lookupString(t, lt, name); got != want {
				t.Errorf("lookup %q: got %q, want %q", name, got, want)
			}
		}
	}
	check(map[string]string{
		"productpage.ns1.svc.cluster.local.":     "10.0.0.1",
		"productpage.ns1.":                       "10.0.0.1",
		"productpage.ns1.svc.":                   "10.0.0.1",
		"productpage.":                           "192.0.2.1",
		"foo.wildcard.com.":                      "192.0.2.2",
		"details.ns1.ns1.svc.cluster.local.":     "192.0.2.3",
		"details.ns1.":                           "10.0.0.2",
		"details.ns1.svc.cluster.local.":         "10.0.0.2",
		"productpage.ns1.ns1.svc.cluster.local.": "cname:productpage.ns1.,10.0.0.1",
		"unknown.":                               "",
	})
	table, resolved := h.NameTableSnapshot()
	if !resolved || table.GetTable()["productpage.ns1"].GetIps()[0] != "10.0.0.1" {
		t.Fatalf("unexpected snapshot: resolved=%v table=%v", resolved, table)
	}

	// Removals reveal shadowed candidates and search-expanded names in place.
	h.ApplyNameTables(nil, []string{"productpage", "details.ns1.ns1.svc.cluster.local", "*.wildcard.com"})
	if h.lookupTable.Load().(*LookupTable) != lt {
		t.Fatal("incremental update must reuse the published table")
	}
	check(map[string]string{
		"productpage.":                       "10.0.0.1",
		"details.ns1.ns1.svc.cluster.local.": "cname:details.ns1.,10.0.0.2",
		"foo.wildcard.com.":                  "",
	})

	h.UpdateLookupTable(&dnsProto.NameTable{Table: map[string]*dnsProto.NameTable_NameInfo{"a.example.com": external("192.0.2.4")}})
	if _, resolved := h.NameTableSnapshot(); resolved {
		t.Fatal("legacy table must not be reported as resolved")
	}
	// Delta resources after a legacy table start from an empty table.
	h.ApplyNameTables(map[string]*dnsProto.NameTable{"b.example.com": {Table: map[string]*dnsProto.NameTable_NameInfo{
		"b.example.com": external("192.0.2.5"),
	}}}, nil)
	lt = h.lookupTable.Load().(*LookupTable)
	check(map[string]string{"a.example.com.": "", "b.example.com.": "192.0.2.5"})
}

func TestNameIndexWinners(t *testing.T) {
	info := func(ip, registry string, aliases ...string) *dnsProto.NameTable_NameInfo {
		return &dnsProto.NameTable_NameInfo{Ips: []string{ip}, Registry: registry, Aliases: aliases}
	}
	cases := []struct {
		name      string
		resources map[string]*dnsProto.NameTable
		want      map[string]string
	}{
		{
			name: "exact beats alias regardless of registry",
			resources: map[string]*dnsProto.NameTable{
				"a.ns.svc.cluster.local": {Table: map[string]*dnsProto.NameTable_NameInfo{"a.ns.svc.cluster.local": info("10.0.0.1", "Kubernetes", "a.ns")}},
				"a.ns":                   {Table: map[string]*dnsProto.NameTable_NameInfo{"a.ns": info("192.0.2.1", "External")}},
			},
			want: map[string]string{"a.ns.svc.cluster.local": "10.0.0.1", "a.ns": "192.0.2.1"},
		},
		{
			name: "kubernetes beats other registries",
			resources: map[string]*dnsProto.NameTable{
				"a.example.com": {Table: map[string]*dnsProto.NameTable_NameInfo{"x.example.com": info("192.0.2.1", "External")}},
				"b.example.com": {Table: map[string]*dnsProto.NameTable_NameInfo{"x.example.com": info("10.0.0.1", "Kubernetes")}},
			},
			want: map[string]string{"x.example.com": "10.0.0.1"},
		},
		{
			name: "colliding aliases resolve by resource then source name",
			resources: map[string]*dnsProto.NameTable{
				"b.ns.svc.cluster.local": {Table: map[string]*dnsProto.NameTable_NameInfo{"b.ns.svc.cluster.local": info("10.0.0.2", "Kubernetes", "shared")}},
				"a.ns.svc.cluster.local": {Table: map[string]*dnsProto.NameTable_NameInfo{
					"a.ns.svc.cluster.local":   info("10.0.0.1", "Kubernetes", "shared"),
					"0.a.ns.svc.cluster.local": info("10.0.0.3", "Kubernetes", "shared"),
				}},
			},
			want: map[string]string{
				"a.ns.svc.cluster.local":   "10.0.0.1",
				"0.a.ns.svc.cluster.local": "10.0.0.3",
				"b.ns.svc.cluster.local":   "10.0.0.2",
				"shared":                   "10.0.0.3",
			},
		},
		{
			name: "candidates without valid IPs do not hide others",
			resources: map[string]*dnsProto.NameTable{
				"a.example.com": {Table: map[string]*dnsProto.NameTable_NameInfo{"x.example.com": info("10.0.0.0/8", "Kubernetes")}},
				"b.example.com": {Table: map[string]*dnsProto.NameTable_NameInfo{"x.example.com": info("192.0.2.1", "External")}},
			},
			want: map[string]string{"x.example.com": "192.0.2.1"},
		},
		{
			name: "names are normalized",
			resources: map[string]*dnsProto.NameTable{
				"A.Example.com": {Table: map[string]*dnsProto.NameTable_NameInfo{"A.Example.com.": info("192.0.2.1", "External")}},
			},
			want: map[string]string{"a.example.com": "192.0.2.1"},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			h := &LocalDNSServer{}
			h.ApplyNameTables(tt.resources, nil)
			table, _ := h.NameTableSnapshot()
			got := map[string]string{}
			for name, info := range table.GetTable() {
				got[name] = info.Ips[0]
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// referenceTable rebuilds the lookup table from every resource, as an oracle for the incremental index.
func referenceTable(searchNamespaces []string, resources map[string]*dnsProto.NameTable) (*LookupTable, map[string]*dnsProto.NameTable_NameInfo) {
	winners := map[string]*nameCandidate{}
	offer := func(c *nameCandidate) {
		if current, found := winners[c.name]; !found || c.preferredTo(current) {
			winners[c.name] = c
		}
	}
	for resource, nt := range resources {
		for source, info := range nt.GetTable() {
			if info == nil {
				continue
			}
			ipv4, ipv6 := netutil.ParseIPsSplitToV4V6(info.Ips)
			entry := &nameEntry{info: info, ipv4: ipv4, ipv6: ipv6}
			if !entry.hasAddresses() {
				continue
			}
			offer(&nameCandidate{name: normalizeName(source), entry: entry, exact: true, resource: resource, source: source})
			for _, alias := range info.Aliases {
				offer(&nameCandidate{name: normalizeName(alias), entry: entry, resource: resource, source: source})
			}
		}
	}
	lt := &LookupTable{allHosts: sets.String{}, name4: map[string][]dns.RR{}, name6: map[string][]dns.RR{}, cname: map[string][]dns.RR{}}
	names := map[string]*dnsProto.NameTable_NameInfo{}
	for name, c := range winners {
		names[strings.TrimSuffix(name, ".")] = c.entry.info
		lt.buildDNSAnswers(sets.New(name), c.entry.ipv4, c.entry.ipv6, nil)
	}
	if len(searchNamespaces) > 0 {
		search := strings.TrimSuffix(strings.ToLower(searchNamespaces[0]), ".") + "."
		for host := range winners {
			expanded := host + search
			if strings.HasSuffix(host, search) || lt.allHosts.Contains(expanded) || !lt.allHosts.Contains(host) {
				continue
			}
			lt.cname[expanded] = cname(expanded, host)
			lt.allHosts.Insert(expanded)
		}
	}
	return lt, names
}

func dumpTable(lt *LookupTable) map[string]string {
	out := map[string]string{}
	for host := range lt.allHosts {
		out[host] = ""
	}
	for _, m := range []map[string][]dns.RR{lt.name4, lt.name6, lt.cname} {
		for host, rrs := range m {
			for _, rr := range rrs {
				out[host] += rr.String() + ";"
			}
		}
	}
	return out
}

func TestNameIndexMatchesFullRebuild(t *testing.T) {
	search := []string{"ns1.svc.cluster.local"}
	pool := []string{
		"a.ns1.svc.cluster.local", "a", "a.ns1", "a.ns1.svc", "A.Ns1", "a.ns1.ns1.svc.cluster.local",
		"b.ns1.svc.cluster.local", "b", "b.ns1", "b.ns1.ns1.svc.cluster.local", "0.b.ns1.svc.cluster.local",
		"*.w.com", "x.w.com", "x.w.com.ns1.svc.cluster.local",
	}
	ipChoices := [][]string{{"10.0.0.1"}, {"10.0.0.2", "10.0.0.3"}, {"fd00::1"}, {"10.0.0.4", "fd00::2"}, nil, {"not-an-ip"}}
	registries := []string{"Kubernetes", "External"}
	// nolint: gosec // deterministic seed keeps failures reproducible
	r := rand.New(rand.NewPCG(1, 2))
	pick := func(s []string) string { return s[r.IntN(len(s))] }
	randomTable := func() *dnsProto.NameTable {
		nt := &dnsProto.NameTable{Table: map[string]*dnsProto.NameTable_NameInfo{}}
		for range 1 + r.IntN(3) {
			info := &dnsProto.NameTable_NameInfo{Ips: ipChoices[r.IntN(len(ipChoices))], Registry: pick(registries)}
			for range r.IntN(4) {
				info.Aliases = append(info.Aliases, pick(pool))
			}
			nt.Table[pick(pool)] = info
		}
		return nt
	}
	resourceNames := []string{"r0", "r1", "r2", "r3", "r4", "r5"}

	h := &LocalDNSServer{searchNamespaces: search}
	state := map[string]*dnsProto.NameTable{}
	for i := range 2000 {
		updated := map[string]*dnsProto.NameTable{}
		for range r.IntN(3) {
			updated[pick(resourceNames)] = randomTable()
		}
		var removed []string
		for range r.IntN(2) {
			removed = append(removed, pick(resourceNames))
		}
		for _, name := range removed {
			delete(state, name)
		}
		maps.Copy(state, updated)
		h.ApplyNameTables(updated, removed)

		want, wantNames := referenceTable(search, state)
		if got := dumpTable(h.lookupTable.Load().(*LookupTable)); !reflect.DeepEqual(got, dumpTable(want)) {
			t.Fatalf("step %d: lookup table mismatch\ngot:  %v\nwant: %v", i, got, dumpTable(want))
		}
		if got, _ := h.NameTableSnapshot(); !reflect.DeepEqual(got.GetTable(), wantNames) {
			t.Fatalf("step %d: name table mismatch\ngot:  %v\nwant: %v", i, got.GetTable(), wantNames)
		}
	}
}

// benchResources returns n Kubernetes services, each as its own resource with the aliases istiod would send.
func benchResources(n int) map[string]*dnsProto.NameTable {
	out := make(map[string]*dnsProto.NameTable, n)
	for i := range n {
		ns := fmt.Sprintf("ns%d", i%50)
		short := fmt.Sprintf("svc%d", i)
		host := short + "." + ns + ".svc.cluster.local"
		out[host] = &dnsProto.NameTable{Table: map[string]*dnsProto.NameTable_NameInfo{host: {
			Ips:       []string{fmt.Sprintf("10.%d.%d.%d", i>>16&0xff, i>>8&0xff, i&0xff)},
			Registry:  "Kubernetes",
			Shortname: short,
			Namespace: ns,
			Aliases:   []string{short + "." + ns, short + "." + ns + ".svc"},
		}}}
	}
	return out
}

func headlessResource(pods, version int) *dnsProto.NameTable {
	host := "headless.ns0.svc.cluster.local"
	nt := &dnsProto.NameTable{Table: map[string]*dnsProto.NameTable_NameInfo{}}
	all := &dnsProto.NameTable_NameInfo{Registry: "Kubernetes", Shortname: "headless", Namespace: "ns0", Aliases: []string{"headless.ns0", "headless.ns0.svc"}}
	for p := range pods {
		ip := fmt.Sprintf("10.255.%d.%d", version&0xff, p)
		all.Ips = append(all.Ips, ip)
		nt.Table[fmt.Sprintf("pod-%d.headless.ns0.svc.cluster.local", p)] = &dnsProto.NameTable_NameInfo{Ips: []string{ip}, Registry: "Kubernetes"}
	}
	nt.Table[host] = all
	return nt
}

func legacyTable(resources map[string]*dnsProto.NameTable) *dnsProto.NameTable {
	out := &dnsProto.NameTable{Table: map[string]*dnsProto.NameTable_NameInfo{}}
	for _, nt := range resources {
		maps.Copy(out.Table, nt.Table)
	}
	return out
}

func BenchmarkNameTableUpdate(b *testing.B) {
	for _, n := range []int{10_000, 100_000} {
		resources := benchResources(n)
		changed := "svc0.ns0.svc.cluster.local"
		update := func(i int) *dnsProto.NameTable {
			info := proto.Clone(resources[changed].Table[changed]).(*dnsProto.NameTable_NameInfo)
			info.Ips = []string{fmt.Sprintf("10.254.%d.%d", i>>8&0xff, i&0xff)}
			return &dnsProto.NameTable{Table: map[string]*dnsProto.NameTable_NameInfo{changed: info}}
		}
		newServer := func(b *testing.B) *LocalDNSServer {
			h, err := NewLocalDNSServer("ns0", "ns0.svc.cluster.local", "localhost:0")
			if err != nil {
				b.Fatal(err)
			}
			return h
		}
		b.Run(fmt.Sprintf("names=%d/legacy-full", n), func(b *testing.B) {
			h := newServer(b)
			legacy := legacyTable(resources)
			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				legacy.Table[changed] = update(i).Table[changed]
				h.UpdateLookupTable(legacy)
			}
		})
		b.Run(fmt.Sprintf("names=%d/reference-full", n), func(b *testing.B) {
			h := newServer(b)
			state := maps.Clone(resources)
			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				state[changed] = update(i)
				lt, _ := referenceTable(h.searchNamespaces, state)
				h.lookupTable.Store(lt)
			}
		})
		b.Run(fmt.Sprintf("names=%d/incremental", n), func(b *testing.B) {
			h := newServer(b)
			h.ApplyNameTables(resources, nil)
			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				h.ApplyNameTables(map[string]*dnsProto.NameTable{changed: update(i)}, nil)
			}
		})
		for _, pods := range []int{10, 100} {
			b.Run(fmt.Sprintf("names=%d/incremental-headless-pods=%d", n, pods), func(b *testing.B) {
				h := newServer(b)
				h.ApplyNameTables(resources, nil)
				b.ReportAllocs()
				for i := 0; b.Loop(); i++ {
					h.ApplyNameTables(map[string]*dnsProto.NameTable{"headless.ns0.svc.cluster.local": headlessResource(pods, i)}, nil)
				}
			})
		}
	}
}

// BenchmarkLookupDuringUpdates measures query latency with and without the table being updated continuously.
func BenchmarkLookupDuringUpdates(b *testing.B) {
	for _, updating := range []bool{false, true} {
		b.Run(fmt.Sprintf("updating=%v", updating), func(b *testing.B) {
			h, err := NewLocalDNSServer("ns0", "ns0.svc.cluster.local", "localhost:0")
			if err != nil {
				b.Fatal(err)
			}
			h.ApplyNameTables(benchResources(10_000), nil)
			var stop atomic.Bool
			done := make(chan struct{})
			go func() {
				defer close(done)
				for i := 0; updating && !stop.Load(); i++ {
					h.ApplyNameTables(map[string]*dnsProto.NameTable{"headless.ns0.svc.cluster.local": headlessResource(100, i)}, nil)
				}
			}()
			lt := h.lookupTable.Load().(*LookupTable)
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					lt.lookupHost(dns.TypeA, "svc1.ns1.svc.cluster.local.")
				}
			})
			stop.Store(true)
			<-done
		})
	}
}
