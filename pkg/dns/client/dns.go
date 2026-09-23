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
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/miekg/dns"

	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	dnsutil "istio.io/istio/pkg/dns"
	dnsProto "istio.io/istio/pkg/dns/proto"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	netutil "istio.io/istio/pkg/util/net"
	"istio.io/istio/pkg/util/sets"
)

var log = istiolog.RegisterScope("dns", "Istio DNS proxy")

// LocalDNSServer holds configurations for the DNS downstreamUDPServer in Istio Agent
type LocalDNSServer struct {
	// Holds the pointer to the DNS lookup table
	lookupTable atomic.Value

	// nameTable holds the original NameTable, for debugging
	nameTable atomic.Value

	dnsProxies []*dnsProxy

	resolvConfServers []string
	searchNamespaces  []string
	// The namespace where the proxy resides
	// determines the hosts used for shortname resolution
	proxyNamespace string
	// Optimizations to save space and time
	proxyDomain      string
	proxyDomainParts []string

	respondBeforeSync         bool
	forwardToUpstreamParallel bool
	upstreamTimeout           time.Duration
}

// LookupTable is borrowed from https://github.com/coredns/coredns/blob/master/plugin/hosts/hostsfile.go
type LookupTable struct {
	// This table will be first looked up to see if the host is something that we got a Nametable entry for
	// (i.e. came from istiod's service registry). If it is, then we will be able to confidently return
	// NXDOMAIN errors for AAAA records for such hosts when only A records exist (or vice versa). If the
	// host does not exist in this map, then we will return nil, causing the caller to query the upstream
	// DNS server to resolve the host. Without this map, we would end up making unnecessary upstream DNS queries
	// for hosts that will never resolve (e.g., AAAA for svc1.ns1.svc.cluster.local.svc.cluster.local.)
	allHosts sets.String

	// The key is a FQDN matching a DNS query (like example.com.), the value is pre-created DNS RR records
	// of A or AAAA type as appropriate.
	name4 map[string][]dns.RR
	name6 map[string][]dns.RR
	// Search-expanded CNAMEs are precomputed only for the legacy full NameTable path.
	cname      map[string][]dns.RR
	exact      bool
	exactTable *exactLookupTable
}

const exactLookupShardCount = 64

type exactLookupRecord struct {
	name4 []dns.RR
	name6 []dns.RR
	info  *dnsProto.NameTable_NameInfo
	known bool
}

type exactLookupTable struct {
	shards [exactLookupShardCount]map[string]exactLookupRecord
	size   int
}

const (
	// In case the client decides to honor the TTL, keep it low so that we can always serve
	// the latest IP for a host.
	// TODO: make it configurable
	defaultTTLInSeconds = 30

	// DefaultUpstreamTimeout is the default timeout for DNS upstream queries
	DefaultUpstreamTimeout = 5 * time.Second
)

// LocalDNSServerOption is a functional option for configuring LocalDNSServer.
type LocalDNSServerOption func(*LocalDNSServer)

// WithParallelForwarding enables or disables parallel forwarding to upstream DNS servers.
func WithParallelForwarding(parallel bool) LocalDNSServerOption {
	return func(s *LocalDNSServer) {
		s.forwardToUpstreamParallel = parallel
	}
}

// WithUpstreamTimeout sets the timeout for upstream DNS queries.
func WithUpstreamTimeout(timeout time.Duration) LocalDNSServerOption {
	return func(s *LocalDNSServer) {
		s.upstreamTimeout = timeout
	}
}

func NewLocalDNSServer(proxyNamespace, proxyDomain string, addr string, opts ...LocalDNSServerOption) (*LocalDNSServer, error) {
	h := &LocalDNSServer{
		proxyNamespace:            proxyNamespace,
		forwardToUpstreamParallel: false,
		upstreamTimeout:           DefaultUpstreamTimeout,
	}

	// Apply functional options
	for _, opt := range opts {
		opt(h)
	}

	// proxyDomain could contain the namespace making it redundant.
	// we just need the .svc.cluster.local piece
	parts := strings.Split(proxyDomain, ".")
	if len(parts) > 0 {
		if parts[0] == proxyNamespace {
			parts = parts[1:]
		}
		h.proxyDomainParts = parts
		h.proxyDomain = strings.Join(parts, ".")
	}

	resolvConf := "/etc/resolv.conf"
	// If running as root and the alternate resolv.conf file exists, use it instead.
	// This is used when running in Docker or VMs, without iptables DNS interception.
	if strings.HasSuffix(addr, ":53") {
		if os.Getuid() == 0 {
			h.respondBeforeSync = true
			// TODO: we can also copy /etc/resolv.conf to /var/lib/istio/resolv.conf and
			// replace it with 'nameserver 127.0.0.1'
			if _, err := os.Stat("/var/lib/istio/resolv.conf"); !os.IsNotExist(err) {
				resolvConf = "/var/lib/istio/resolv.conf"
			}
		} else {
			log.Error("DNS address :53 and not running as root, use default")
			addr = constants.DefaultDNSProxyAddr
		}
	}

	// We will use the local resolv.conf for resolving unknown names.
	dnsConfig, err := dns.ClientConfigFromFile(resolvConf)
	if err != nil {
		log.Warnf("failed to load %s: %v", resolvConf, err)
		return nil, err
	}

	// Unlike traditional DNS resolvers, we do not need to append the search
	// namespace to a given query and try to resolve it. This is because the
	// agent acts as a DNS interceptor for DNS queries made by the application.
	// The application's resolver is already sending us DNS queries, one for each
	// of the DNS search namespaces. We simply need to check the existence of this
	// name in our local nametable. If not, we will forward the query to the
	// upstream resolvers as is.
	if dnsConfig != nil {
		for _, s := range dnsConfig.Servers {
			h.resolvConfServers = append(h.resolvConfServers, net.JoinHostPort(s, dnsConfig.Port))
		}
		h.searchNamespaces = dnsConfig.Search
	}

	log.WithLabels("search", h.searchNamespaces, "servers", h.resolvConfServers).Debugf("initialized DNS")

	if addr == "" {
		addr = constants.DefaultDNSProxyAddr
	}
	v4, v6 := netutil.ParseIPsSplitToV4V6(dnsConfig.Servers)
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("dns address must be a valid host:port: %v", err)
	}
	addresses := []string{addr}
	if host == "localhost" && len(v4)+len(v6) > 0 {
		addresses = []string{}
		// When binding to "localhost", go will pick v4 OR v6. In dual stack, we may need v4 AND v6.
		// If we are in this situation, explicitly listen to v4, v6, or both.
		if len(v4) > 0 {
			addresses = append(addresses, net.JoinHostPort("127.0.0.1", port))
		}
		if len(v6) > 0 {
			addresses = append(addresses, net.JoinHostPort("::1", port))
		}
	}
	for _, ipAddr := range addresses {
		for _, proto := range []string{"udp", "tcp"} {
			proxy, err := newDNSProxy(proto, ipAddr, h, h.upstreamTimeout)
			if err != nil {
				return nil, err
			}
			h.dnsProxies = append(h.dnsProxies, proxy)

		}
	}

	return h, nil
}

// StartDNS starts DNS-over-UDP and DNS-over-TCP servers.
func (h *LocalDNSServer) StartDNS() {
	for _, p := range h.dnsProxies {
		go p.start()
	}
}

func (h *LocalDNSServer) UpdateLookupTable(nt *dnsProto.NameTable) {
	lookupTable := &LookupTable{
		allHosts: sets.String{},
		name4:    map[string][]dns.RR{},
		name6:    map[string][]dns.RR{},
		cname:    map[string][]dns.RR{},
	}
	h.BuildAlternateHosts(nt, lookupTable.buildDNSAnswers)
	h.lookupTable.Store(lookupTable)
	h.nameTable.Store(nt)
	log.Debugf("updated lookup table with %d hosts", len(lookupTable.allHosts))
}

// RebuildExact replaces the lookup table with final DNS names supplied by Istiod.
func (h *LocalDNSServer) RebuildExact(table map[string]*dnsProto.NameTable_NameInfo) {
	exact := &exactLookupTable{}
	for hostname, info := range table {
		if info == nil {
			continue
		}
		exact.set(hostname, info)
	}
	lookupTable := &LookupTable{exact: true, exactTable: exact}
	h.lookupTable.Store(lookupTable)
	log.Debugf("rebuilt exact lookup table with %d hosts", exact.size)
}

// ApplyDelta atomically applies named NDS resources without mutating the table read by DNS requests.
func (h *LocalDNSServer) ApplyDelta(added map[string]*dnsProto.NameTable_NameInfo, removed []string) {
	currentLookup := h.lookupTable.Load()
	if currentLookup == nil || !currentLookup.(*LookupTable).exact {
		h.RebuildExact(added)
		return
	}
	previous := currentLookup.(*LookupTable)
	updated := previous.exactTable.applyDelta(added, removed)
	lookupTable := &LookupTable{exact: true, exactTable: updated}
	h.lookupTable.Store(lookupTable)
	log.Debugf("applied exact lookup table delta +%d -%d, total %d hosts", len(added), len(removed), updated.size)
}

func newExactLookupRecord(hostname string, info *dnsProto.NameTable_NameInfo) exactLookupRecord {
	record := exactLookupRecord{info: info}
	ipv4, ipv6 := netutil.ParseIPsSplitToV4V6(info.Ips)
	if len(ipv4) == 0 && len(ipv6) == 0 {
		return record
	}
	record.known = true
	if len(ipv4) > 0 {
		record.name4 = a(hostname, ipv4)
	}
	if len(ipv6) > 0 {
		record.name6 = aaaa(hostname, ipv6)
	}
	return record
}

func (table *exactLookupTable) set(hostname string, info *dnsProto.NameTable_NameInfo) {
	hostname = normalizeExactName(hostname)
	shard := exactLookupShard(hostname)
	if table.shards[shard] == nil {
		table.shards[shard] = make(map[string]exactLookupRecord)
	}
	if _, found := table.shards[shard][hostname]; !found {
		table.size++
	}
	table.shards[shard][hostname] = newExactLookupRecord(hostname, info)
}

func (table *exactLookupTable) applyDelta(added map[string]*dnsProto.NameTable_NameInfo, removed []string) *exactLookupTable {
	updated := *table
	touched := sets.New[int]()
	cloneShard := func(hostname string) int {
		shard := exactLookupShard(hostname)
		if touched.InsertContains(shard) {
			return shard
		}
		updated.shards[shard] = maps.Clone(table.shards[shard])
		if updated.shards[shard] == nil {
			updated.shards[shard] = make(map[string]exactLookupRecord)
		}
		return shard
	}
	for _, hostname := range removed {
		hostname = normalizeExactName(hostname)
		shard := cloneShard(hostname)
		if _, found := updated.shards[shard][hostname]; found {
			delete(updated.shards[shard], hostname)
			updated.size--
		}
	}
	for hostname, info := range added {
		hostname = normalizeExactName(hostname)
		shard := cloneShard(hostname)
		_, found := updated.shards[shard][hostname]
		if info == nil {
			if found {
				delete(updated.shards[shard], hostname)
				updated.size--
			}
			continue
		}
		if !found {
			updated.size++
		}
		updated.shards[shard][hostname] = newExactLookupRecord(hostname, info)
	}
	return &updated
}

func (table *exactLookupTable) get(hostname string) (exactLookupRecord, bool) {
	hostname = normalizeExactName(hostname)
	record, found := table.shards[exactLookupShard(hostname)][hostname]
	return record, found
}

func (table *exactLookupTable) nameTable() *dnsProto.NameTable {
	out := &dnsProto.NameTable{Table: make(map[string]*dnsProto.NameTable_NameInfo, table.size)}
	for i := range table.shards {
		for hostname, record := range table.shards[i] {
			out.Table[normalizeResourceName(hostname)] = record.info
		}
	}
	return out
}

func exactLookupShard(hostname string) int {
	var hash uint64 = 14695981039346656037
	for i := 0; i < len(hostname); i++ {
		hash ^= uint64(hostname[i])
		hash *= 1099511628211
	}
	return int(hash % exactLookupShardCount)
}

func normalizeExactName(hostname string) string {
	return normalizeResourceName(hostname) + "."
}

func normalizeResourceName(hostname string) string {
	return strings.ToLower(strings.TrimSuffix(hostname, "."))
}

// BuildAlternateHosts builds alternate hosts for Kubernetes services in the name table and
// calls the passed in function with the built alternate hosts.
func (h *LocalDNSServer) BuildAlternateHosts(nt *dnsProto.NameTable,
	apply func(map[string]struct{}, []netip.Addr, []netip.Addr, []string),
) {
	for hostname, ni := range nt.Table {
		// Given a host
		// if its a non-k8s host, store the host+. as the key with the pre-computed DNS RR records
		// if its a k8s host, store all variants (i.e. shortname+., shortname+namespace+., fqdn+., etc.)
		// shortname+. is only for hosts in current namespace
		var altHosts sets.String
		if ni.Registry == string(provider.Kubernetes) {
			altHosts = dnsutil.GenerateAltHosts(hostname, ni, h.proxyNamespace, h.proxyDomain, h.proxyDomainParts)
		} else {
			if !strings.HasSuffix(hostname, ".") {
				hostname += "."
			}
			altHosts = sets.New(hostname)
		}
		ipv4, ipv6 := netutil.ParseIPsSplitToV4V6(ni.Ips)
		if len(ipv6) == 0 && len(ipv4) == 0 {
			// malformed ips
			continue
		}
		apply(altHosts, ipv4, ipv6, h.searchNamespaces)
	}
}

// upstream sends the request to the upstream server, with associated logs and metrics
func (h *LocalDNSServer) upstream(proxy *dnsProxy, req *dns.Msg, hostname string) *dns.Msg {
	upstreamRequests.Increment()
	start := time.Now()
	// We did not find the host in our internal cache. Query upstream and return the response as is.
	log.Debugf("response for hostname %q not found in dns proxy, querying upstream", hostname)
	response := h.queryUpstream(proxy.upstreamClient, req, log)
	requestDuration.Record(time.Since(start).Seconds())
	log.Debugf("upstream response for hostname %q : %v", hostname, response)
	return response
}

// ServeDNS is the implementation of DNS interface
func (h *LocalDNSServer) ServeDNS(proxy *dnsProxy, w dns.ResponseWriter, req *dns.Msg) {
	requests.Increment()
	var response *dns.Msg
	log := log.WithLabels("protocol", proxy.protocol, "edns", req.IsEdns0() != nil)
	if log.DebugEnabled() {
		id := uuid.New()
		log = log.WithLabels("id", id)
	}
	log.Debugf("request %v", req)

	if len(req.Question) == 0 {
		response = new(dns.Msg)
		response.SetReply(req)
		response.Rcode = dns.RcodeServerFailure
		_ = w.WriteMsg(response)
		return
	}

	lp := h.lookupTable.Load()
	hostname := strings.ToLower(req.Question[0].Name)
	if lp == nil {
		if h.respondBeforeSync {
			response = h.upstream(proxy, req, hostname)
			response.Truncate(size(proxy.protocol, req))
			_ = w.WriteMsg(response)
		} else {
			log.Debugf("dns request for host %q before lookup table is loaded", hostname)
			response = new(dns.Msg)
			response.SetReply(req)
			response.Rcode = dns.RcodeServerFailure
			_ = w.WriteMsg(response)
		}
		return
	}
	lookupTable := lp.(*LookupTable)
	var answers []dns.RR

	// This name will always end in a dot.
	// We expect only one question in the query even though the spec allows many
	// clients usually do not do more than one query either.
	var hostFound bool
	if lookupTable.exact {
		answers, hostFound = lookupTable.lookupHostExact(req.Question[0].Qtype, hostname, h.searchNamespaces)
	} else {
		answers, hostFound = lookupTable.lookupHost(req.Question[0].Qtype, hostname)
	}

	if hostFound {
		response = new(dns.Msg)
		response.SetReply(req)
		// We are the authority here, since we control DNS for known hostnames
		response.Authoritative = true
		// Even if answers is empty, we still return NOERROR. This matches expected behavior of DNS
		// servers. NXDOMAIN means we do not know *anything* about the domain; if we set it here then
		// a client (ie curl, see https://github.com/istio/istio/issues/31250) sending parallel
		// requests for A and AAAA may get NXDOMAIN for AAAA and treat the entire thing as a NXDOMAIN
		response.Answer = answers
		// Randomize the responses; this ensures for things like headless services we can do DNS-LB
		// This matches standard kube-dns behavior. We only do this for cached responses as the
		// upstream DNS server would already round robin if desired.
		if len(answers) > 0 {
			roundRobinResponse(response)
		}
		log.Debugf("response for hostname %q (found=true): %v", hostname, response)
	} else {
		response = h.upstream(proxy, req, hostname)
	}
	// Compress the response - we don't know if the incoming response was compressed or not. If it was,
	// but we don't compress on the outbound, we will run into issues. For example, if the compressed
	// size is 450 bytes but uncompressed 1000 bytes now we are outside of the non-eDNS UDP size limits
	response.Truncate(size(proxy.protocol, req))
	_ = w.WriteMsg(response)
}

// IsReady returns true if DNS lookup table is updated at least once.
func (h *LocalDNSServer) IsReady() bool {
	return h.lookupTable.Load() != nil
}

func (h *LocalDNSServer) NameTable() *dnsProto.NameTable {
	table, _ := h.NameTableSnapshot()
	return table
}

// NameTableSnapshot returns the current table and whether it already contains alternate names.
func (h *LocalDNSServer) NameTableSnapshot() (table *dnsProto.NameTable, hasAlternateNames bool) {
	if lookup := h.lookupTable.Load(); lookup != nil {
		table := lookup.(*LookupTable)
		if table.exact {
			return table.exactTable.nameTable(), true
		}
	}
	lt := h.nameTable.Load()
	if lt == nil {
		return nil, false
	}
	return lt.(*dnsProto.NameTable), false
}

// Inspired by https://github.com/coredns/coredns/blob/master/plugin/loadbalance/loadbalance.go
func roundRobinResponse(res *dns.Msg) {
	if res.Rcode != dns.RcodeSuccess {
		return
	}

	if res.Question[0].Qtype == dns.TypeAXFR || res.Question[0].Qtype == dns.TypeIXFR {
		return
	}

	res.Answer = roundRobin(res.Answer)
	res.Ns = roundRobin(res.Ns)
	res.Extra = roundRobin(res.Extra)
}

func roundRobin(in []dns.RR) []dns.RR {
	cname := make([]dns.RR, 0)
	address := make([]dns.RR, 0)
	mx := make([]dns.RR, 0)
	rest := make([]dns.RR, 0)
	for _, r := range in {
		switch r.Header().Rrtype {
		case dns.TypeCNAME:
			cname = append(cname, r)
		case dns.TypeA, dns.TypeAAAA:
			address = append(address, r)
		case dns.TypeMX:
			mx = append(mx, r)
		default:
			rest = append(rest, r)
		}
	}

	roundRobinShuffle(address)
	roundRobinShuffle(mx)

	out := append(cname, rest...)
	out = append(out, address...)
	out = append(out, mx...)
	return out
}

func roundRobinShuffle[T any](entries []T) {
	switch l := len(entries); l {
	case 0, 1:
		break
	case 2:
		if dns.Id()%2 == 0 {
			entries[0], entries[1] = entries[1], entries[0]
		}
	default:
		for j := 0; j < l*(int(dns.Id())%4+1); j++ {
			q := int(dns.Id()) % l
			p := int(dns.Id()) % l
			if q == p {
				p = (p + 1) % l
			}
			entries[q], entries[p] = entries[p], entries[q]
		}
	}
}

func (h *LocalDNSServer) Close() {
	for _, p := range h.dnsProxies {
		p.close()
	}
}

func (h *LocalDNSServer) queryUpstream(upstreamClient *dns.Client, req *dns.Msg, scope *istiolog.Scope) *dns.Msg {
	if h.forwardToUpstreamParallel {
		return h.queryUpstreamParallel(upstreamClient, req, scope)
	}

	var response *dns.Msg
	servers := slices.Clone(h.resolvConfServers)
	roundRobinShuffle(servers)
	for _, upstream := range servers {
		cResponse, _, err := upstreamClient.Exchange(req, upstream)
		if err == nil {
			response = cResponse
			break
		}
		scope.Infof("upstream failure: %v", err)
	}

	if response == nil {
		response = serverFailure(req)
	}
	return response
}

// queryUpstreamParallel will send parallel queries to all nameservers and return first successful response immediately.
// The overall approach of parallel resolution is likely not widespread, but there are already some widely used
// clients support it:
//
//   - dnsmasq: setting flag '--all-servers' forces dnsmasq to send all queries to all available servers. The reply from
//     the server which answers first will be returned to the original requester.
//   - tailscale: will either proxy all DNS requests—in which case we query all nameservers in parallel and use the quickest
//     response—or defer to the operating system, which we have no control over.
//   - systemd-resolved: which is used as a default resolver in many Linux distributions nowadays also performs parallel
//     lookups for multiple DNS servers and returns the first successful response.
func (h *LocalDNSServer) queryUpstreamParallel(upstreamClient *dns.Client, req *dns.Msg, scope *istiolog.Scope) *dns.Msg {
	// Guarantee that the ctx we use below is done when this function returns.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	responseCh := make(chan *dns.Msg)
	errCh := make(chan error)

	queryOne := func(upstream string) {
		// Note: After DialContext in ExchangeContext is called, this function cannot be cancelled by context.
		cResponse, _, err := upstreamClient.ExchangeContext(ctx, req, upstream)
		if err == nil {
			// Only reserve first response and ignore others.
			select {
			case responseCh <- cResponse:
			case <-ctx.Done():
			}
			return
		}
		scope.Infof("parallel querying upstream failure: %v", err)
		select {
		case errCh <- err:
		case <-ctx.Done():
		}
	}

	for _, upstream := range h.resolvConfServers {
		go queryOne(upstream)
	}

	errorsCount := 0
	for {
		select {
		case response := <-responseCh:
			// We got the first response.
			return response
		case <-errCh:
			errorsCount++
			// All servers returned error - return failure.
			if errorsCount == len(h.resolvConfServers) {
				scope.Infof("all upstream failed")
				return serverFailure(req)
			}
		}
	}
}

func serverFailure(req *dns.Msg) *dns.Msg {
	failures.Increment()
	response := new(dns.Msg)
	response.SetReply(req)
	response.Rcode = dns.RcodeServerFailure
	return response
}

// Given a host, this function first decides if the host is part of our service registry.
// If it is not part of the registry, return nil so that caller queries upstream. If it is part
// of registry, we will look it up in one of our tables, failing which we will return NXDOMAIN.
func (table *LookupTable) lookupHost(qtype uint16, hostname string) ([]dns.RR, bool) {
	question := string(host.Name(hostname))
	wildcard := false
	// First check if host exists in all hosts.
	hostFound := table.allHosts.Contains(hostname)
	// If it is not found, check if a wildcard host exists for it.
	// For example for "*.example.com", with the question "svc.svcns.example.com",
	// we check if we have entries for "*.svcns.example.com", "*.example.com" etc.
	if !hostFound {
		labels := dns.SplitDomainName(hostname)
		for idx := range labels {
			qhost := "*." + strings.Join(labels[idx+1:], ".") + "."
			if hostFound = table.allHosts.Contains(qhost); hostFound {
				wildcard = true
				hostname = qhost
				break
			}
		}
	}

	if !hostFound {
		return nil, false
	}

	var out []dns.RR
	var ipAnswers []dns.RR
	var wcAnswers []dns.RR
	var cnAnswers []dns.RR

	// Odds are, the first query will always be an expanded hostname
	// (productpage.ns1.svc.cluster.local.ns1.svc.cluster.local)
	// So lookup the cname table first
	for _, cn := range table.cname[hostname] {
		// this was a cname match
		copied := dns.Copy(cn).(*dns.CNAME)
		copied.Header().Name = question
		cnAnswers = append(cnAnswers, copied)
		hostname = copied.Target
	}

	switch qtype {
	case dns.TypeA:
		ipAnswers = table.name4[hostname]
	case dns.TypeAAAA:
		ipAnswers = table.name6[hostname]
	default:
		// TODO: handle PTR records for reverse dns lookups
		return nil, false
	}

	if len(ipAnswers) > 0 {
		// For wildcard hosts, set the host that is being queried for.
		if wildcard {
			for _, answer := range ipAnswers {
				copied := dns.Copy(answer)
				/// If there is a CNAME record for the wildcard host, we will sent a chained response of CNAME + A/AAAA pointer
				/// Otherwise we expand the wildcard to the original question domain
				if len(cnAnswers) > 0 {
					copied.Header().Name = hostname
				} else {
					copied.Header().Name = question
				}
				wcAnswers = append(wcAnswers, copied)
			}
		}

		// We will return a chained response. In a chained response, the first entry is the cname record,
		// and the second one is the A/AAAA record itself. Some clients do not follow cname redirects
		// with additional DNS queries. Instead, they expect all the resolved records to be in the same
		// big DNS response (presumably assuming that a recursive DNS query should do the deed, resolve
		// cname et al and return the composite response).
		out = append(out, cnAnswers...)
		if wildcard {
			out = append(out, wcAnswers...)
		} else {
			out = append(out, ipAnswers...)
		}
	}
	return out, hostFound
}

// lookupHostExact derives search aliases only after an exact-name miss, preventing them from
// overriding independently owned exact records.
func (table *LookupTable) lookupHostExact(qtype uint16, hostname string, searchNamespaces []string) ([]dns.RR, bool) {
	answers, found := table.exactTable.lookupHost(qtype, hostname, false)
	if found {
		return answers, true
	}
	base, found := searchBaseName(hostname, searchNamespaces)
	if found {
		answers, found = table.exactTable.lookupHost(qtype, base, true)
		if found {
			if len(answers) == 0 {
				return nil, true
			}
			// Some clients do not perform another lookup after a CNAME in a recursive response.
			return append(cname(hostname, base), answers...), true
		}
	}
	return table.exactTable.lookupHost(qtype, hostname, true)
}

func (table *exactLookupTable) lookupHost(qtype uint16, hostname string, allowWildcard bool) ([]dns.RR, bool) {
	question := normalizeExactName(hostname)
	record, found := table.get(question)
	wildcard := false
	if (!found || !record.known) && allowWildcard {
		labels := dns.SplitDomainName(question)
		for idx := range labels {
			candidate := "*." + strings.Join(labels[idx+1:], ".") + "."
			if record, found = table.get(candidate); found && record.known {
				wildcard = true
				break
			}
		}
	}
	if !found || !record.known {
		return nil, false
	}

	var answers []dns.RR
	switch qtype {
	case dns.TypeA:
		answers = record.name4
	case dns.TypeAAAA:
		answers = record.name6
	default:
		return nil, false
	}
	if !wildcard || len(answers) == 0 {
		return answers, true
	}
	expanded := make([]dns.RR, 0, len(answers))
	for _, answer := range answers {
		copied := dns.Copy(answer)
		copied.Header().Name = question
		expanded = append(expanded, copied)
	}
	return expanded, true
}

// This function stores the list of hostnames along with the precomputed DNS response for that hostname.
// Most hostnames have a DNS response containing the A/AAAA records. In addition, this function stores a
// variant of the host+ the first search domain in resolv.conf as the first query
// is likely to be host.ns.svc.cluster.local (e.g., www.google.com.ns1.svc.cluster.local) due to
// the list of search namespaces in resolv.conf (unless the app explicitly does www.google.com. which is unlikely).
// We will resolve www.google.com.ns1.svc.cluster.local with a CNAME record pointing to www.google.com.
// which will cause the client's resolver to automatically resolve www.google.com. , and short circuit the lengthy
// search process down to just two DNS queries. This will eliminate unnecessary upstream DNS queries from the
// agent, reduce load on DNS servers and improve overall latency. This idea was borrowed and adapted from
// the autopath plugin in coredns. The implementation here is very different from auto path though.
// Autopath does inline computation to see if the given query could potentially match something else
// and then returns a CNAME record. In our case, we preemptively store these random dns names as a host
// in the lookup table with a CNAME record as the DNS response. This technique eliminates the need
// to do string parsing, memory allocations, etc. at query time at the cost of Nx number of entries (i.e. memory) to store
// the lookup table, where N is number of search namespaces.
func (table *LookupTable) buildDNSAnswers(altHosts map[string]struct{}, ipv4 []netip.Addr, ipv6 []netip.Addr, searchNamespaces []string) {
	for h := range altHosts {
		h = strings.ToLower(h)
		table.allHosts.Insert(h)
		if len(ipv4) > 0 {
			table.name4[h] = a(h, ipv4)
		}
		if len(ipv6) > 0 {
			table.name6[h] = aaaa(h, ipv6)
		}
		if len(searchNamespaces) > 0 && !strings.HasSuffix(h, searchNamespaces[0]+".") {
			// NOTE: Right now, rather than storing one expanded host for each one of the search namespace
			// entries, we are going to store just the first one (assuming that most clients will
			// do sequential dns resolution, starting with the first search namespace)

			// host h already ends with a .
			// search namespace might not. So we append one in the end if needed
			expandedHost := strings.ToLower(h + searchNamespaces[0])
			if !strings.HasSuffix(searchNamespaces[0], ".") {
				expandedHost += "."
			}
			// make sure this is not a proper hostname
			// if host is productpage, and search namespace is ns1.svc.cluster.local
			// then the expanded host productpage.ns1.svc.cluster.local is a valid hostname
			// that is likely to be already present in the altHosts
			if _, exists := altHosts[expandedHost]; !exists {
				table.cname[expandedHost] = cname(expandedHost, h)
				table.allHosts.Insert(expandedHost)
			}
		}
	}
}

func searchBaseName(hostname string, searchNamespaces []string) (string, bool) {
	if len(searchNamespaces) == 0 {
		return "", false
	}
	search := strings.ToLower(strings.TrimSuffix(searchNamespaces[0], "."))
	if search == "" {
		return "", false
	}
	suffix := "." + search + "."
	if !strings.HasSuffix(hostname, suffix) {
		return "", false
	}
	base := strings.TrimSuffix(hostname, suffix)
	if base == "" {
		return "", false
	}
	base += "."
	// Reject names that already include the search domain; they are not autopath candidates.
	if strings.HasSuffix(strings.TrimSuffix(base, "."), "."+search) {
		return "", false
	}
	return base, true
}

// Borrowed from https://github.com/coredns/coredns/blob/master/plugin/hosts/hosts.go
// a takes a slice of ip string and returns a slice of A RRs.
func a(host string, ips []netip.Addr) []dns.RR {
	answers := make([]dns.RR, len(ips))
	for i, ip := range ips {
		r := new(dns.A)
		r.Hdr = dns.RR_Header{Name: host, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: defaultTTLInSeconds}
		r.A = ip.AsSlice()
		answers[i] = r
	}
	return answers
}

// aaaa takes a slice of ip string and returns a slice of AAAA RRs.
func aaaa(host string, ips []netip.Addr) []dns.RR {
	answers := make([]dns.RR, len(ips))
	for i, ip := range ips {
		r := new(dns.AAAA)
		r.Hdr = dns.RR_Header{Name: host, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: defaultTTLInSeconds}
		r.AAAA = ip.AsSlice()
		answers[i] = r
	}
	return answers
}

func cname(host string, targetHost string) []dns.RR {
	answer := new(dns.CNAME)
	answer.Hdr = dns.RR_Header{
		Name:   host,
		Rrtype: dns.TypeCNAME,
		Class:  dns.ClassINET,
		Ttl:    defaultTTLInSeconds,
	}
	answer.Target = targetHost
	return []dns.RR{answer}
}

// Size returns if buffer size *advertised* in the requests OPT record.
// Or when the request was over TCP, we return the maximum allowed size of 64K.
func size(proto string, r *dns.Msg) int {
	size := uint16(0)
	if o := r.IsEdns0(); o != nil {
		size = o.UDPSize()
	}

	// normalize size
	size = ednsSize(proto, size)
	return int(size)
}

// ednsSize returns a normalized size based on proto.
func ednsSize(proto string, size uint16) uint16 {
	if proto == "tcp" {
		return dns.MaxMsgSize
	}
	if size < dns.MinMsgSize {
		return dns.MinMsgSize
	}
	return size
}
