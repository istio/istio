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
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/miekg/dns"

	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/config/host"
	dnsProto "istio.io/istio/pkg/dns/proto"
	"istio.io/istio/pkg/util/sets"
	istiolog "istio.io/pkg/log"
)

var log = istiolog.RegisterScope("dns", "Istio DNS proxy", 0)

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
}

// LookupTable is borrowed from https://github.com/coredns/coredns/blob/master/plugin/hosts/hostsfile.go
type LookupTable struct {
	// This table will be first looked up to see if the host is something that we got a Nametable entry for
	// (i.e. came from istiod's service registry). If it is, then we will be able to confidently return
	// NXDOMAIN errors for AAAA records for such hosts when only A records exist (or vice versa). If the
	// host does not exist in this map, then we will return nil, causing the caller to query the upstream
	// DNS server to resolve the host. Without this map, we would end up making unnecessary upstream DNS queries
	// for hosts that will never resolve (e.g., AAAA for svc1.ns1.svc.cluster.local.svc.cluster.local.)
	allHosts map[string]struct{}

	// The key is a FQDN matching a DNS query (like example.com.), the value is pre-created DNS RR records
	// of A or AAAA type as appropriate.
	name4 map[string][]dns.RR
	name6 map[string][]dns.RR
	// The cname records here (comprised of different variants of the hosts above,
	// expanded by the search namespaces) pointing to the actual host.
	cname map[string][]dns.RR
}

const (
	// In case the client decides to honor the TTL, keep it low so that we can always serve
	// the latest IP for a host.
	// TODO: make it configurable
	defaultTTLInSeconds = 30
)

func NewLocalDNSServer(proxyNamespace, proxyDomain string, addr string, forwardToUpstreamParallel bool) (*LocalDNSServer, error) {
	h := &LocalDNSServer{
		proxyNamespace:            proxyNamespace,
		forwardToUpstreamParallel: forwardToUpstreamParallel,
	}

	registerStats()

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
			addr = "localhost:15053"
		}
	}

	// We will use the local resolv.conf for resolving unknown names.
	dnsConfig, err := dns.ClientConfigFromFile(resolvConf)
	if err != nil {
		log.Warnf("failed to load /etc/resolv.conf: %v", err)
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
		addr = "localhost:15053"
	}
	v4, v6 := separateIPtypes(dnsConfig.Servers)
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("dns address must be a valid host:port")
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
			proxy, err := newDNSProxy(proto, ipAddr, h)
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
		allHosts: map[string]struct{}{},
		name4:    map[string][]dns.RR{},
		name6:    map[string][]dns.RR{},
		cname:    map[string][]dns.RR{},
	}
	h.BuildAlternateHosts(nt, lookupTable.buildDNSAnswers)
	h.lookupTable.Store(lookupTable)
	h.nameTable.Store(nt)
	log.Debugf("updated lookup table with %d hosts", len(lookupTable.allHosts))
}

// BuildAlternateHosts builds alternate hosts for Kubernetes services in the name table and
// calls the passed in function with the built alternate hosts.
func (h *LocalDNSServer) BuildAlternateHosts(nt *dnsProto.NameTable,
	apply func(map[string]struct{}, []net.IP, []net.IP, []string),
) {
	for hostname, ni := range nt.Table {
		// Given a host
		// if its a non-k8s host, store the host+. as the key with the pre-computed DNS RR records
		// if its a k8s host, store all variants (i.e. shortname+., shortname+namespace+., fqdn+., etc.)
		// shortname+. is only for hosts in current namespace
		var altHosts sets.Set
		if ni.Registry == string(provider.Kubernetes) {
			altHosts = generateAltHosts(hostname, ni, h.proxyNamespace, h.proxyDomain, h.proxyDomainParts)
		} else {
			if !strings.HasSuffix(hostname, ".") {
				hostname += "."
			}
			altHosts = sets.New(hostname)
		}
		ipv4, ipv6 := separateIPtypes(ni.Ips)
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
	answers, hostFound := lookupTable.lookupHost(req.Question[0].Qtype, hostname)

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

// IsReady returns true if DNS lookup table is updated atleast once.
func (h *LocalDNSServer) IsReady() bool {
	return h.lookupTable.Load() != nil
}

func (h *LocalDNSServer) NameTable() *dnsProto.NameTable {
	lt := h.nameTable.Load()
	if lt == nil {
		return nil
	}
	return lt.(*dnsProto.NameTable)
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

func roundRobinShuffle(records []dns.RR) {
	switch l := len(records); l {
	case 0, 1:
		break
	case 2:
		if dns.Id()%2 == 0 {
			records[0], records[1] = records[1], records[0]
		}
	default:
		for j := 0; j < l*(int(dns.Id())%4+1); j++ {
			q := int(dns.Id()) % l
			p := int(dns.Id()) % l
			if q == p {
				p = (p + 1) % l
			}
			records[q], records[p] = records[p], records[q]
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

	for _, upstream := range h.resolvConfServers {
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
	response := new(dns.Msg)
	response.SetReply(req)
	response.Rcode = dns.RcodeServerFailure
	return response
}

func separateIPtypes(ips []string) (ipv4, ipv6 []net.IP) {
	for _, ip := range ips {
		addr := net.ParseIP(ip)
		if addr == nil {
			log.Debugf("ignoring un-parsable IP address: %v", ip)
			continue
		}
		if addr.To4() != nil {
			ipv4 = append(ipv4, addr.To4())
		} else {
			ipv6 = append(ipv6, addr)
		}
	}
	return
}

func generateAltHosts(hostname string, nameinfo *dnsProto.NameTable_NameInfo, proxyNamespace, proxyDomain string,
	proxyDomainParts []string,
) sets.Set {
	out := sets.New()
	out.Insert(hostname + ".")
	// do not generate alt hostnames if the service is in a different domain (i.e. cluster) than the proxy
	// as we have no way to resolve conflicts on name.namespace entries across clusters of different domains
	if proxyDomain == "" || !strings.HasSuffix(hostname, proxyDomain) {
		return out
	}
	out.Insert(nameinfo.Shortname + "." + nameinfo.Namespace + ".")
	if proxyNamespace == nameinfo.Namespace {
		out.Insert(nameinfo.Shortname + ".")
	}
	// Do we need to generate entries for name.namespace.svc, name.namespace.svc.cluster, etc. ?
	// If these are not that frequently used, then not doing so here will save some space and time
	// as some people have very long proxy domains with multiple dots
	// For now, we will generate just one more domain (which is usually the .svc piece).
	out.Insert(nameinfo.Shortname + "." + nameinfo.Namespace + "." + proxyDomainParts[0] + ".")

	// Add any additional alt hostnames.
	// nolint: staticcheck
	for _, altHost := range nameinfo.AltHosts {
		out.Insert(altHost + ".")
	}
	return out
}

// Given a host, this function first decides if the host is part of our service registry.
// If it is not part of the registry, return nil so that caller queries upstream. If it is part
// of registry, we will look it up in one of our tables, failing which we will return NXDOMAIN.
func (table *LookupTable) lookupHost(qtype uint16, hostname string) ([]dns.RR, bool) {
	var hostFound bool

	question := host.Name(hostname)
	wildcard := false
	// First check if host exists in all hosts.
	_, hostFound = table.allHosts[hostname]
	// If it is not found, check if a wildcard host exists for it.
	// For example for "*.example.com", with the question "svc.svcns.example.com",
	// we check if we have entries for "*.svcns.example.com", "*.example.com" etc.
	if !hostFound {
		labels := dns.SplitDomainName(hostname)
		for idx := range labels {
			qhost := "*." + strings.Join(labels[idx+1:], ".") + "."
			if _, hostFound = table.allHosts[qhost]; hostFound {
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
	// Odds are, the first query will always be an expanded hostname
	// (productpage.ns1.svc.cluster.local.ns1.svc.cluster.local)
	// So lookup the cname table first
	cn := table.cname[hostname]
	if len(cn) > 0 {
		// this was a cname match
		hostname = cn[0].(*dns.CNAME).Target
	}
	var ipAnswers []dns.RR
	var wcAnswers []dns.RR
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
				copied.Header().Name = string(question)
				wcAnswers = append(wcAnswers, copied)
			}
		}
		// We will return a chained response. In a chained response, the first entry is the cname record,
		// and the second one is the A/AAAA record itself. Some clients do not follow cname redirects
		// with additional DNS queries. Instead, they expect all the resolved records to be in the same
		// big DNS response (presumably assuming that a recursive DNS query should do the deed, resolve
		// cname et al and return the composite response).
		out = append(out, cn...)
		if wildcard {
			out = append(out, wcAnswers...)
		} else {
			out = append(out, ipAnswers...)
		}
	}
	return out, hostFound
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
func (table *LookupTable) buildDNSAnswers(altHosts map[string]struct{}, ipv4 []net.IP, ipv6 []net.IP, searchNamespaces []string) {
	for h := range altHosts {
		h = strings.ToLower(h)
		table.allHosts[h] = struct{}{}
		if len(ipv4) > 0 {
			table.name4[h] = a(h, ipv4)
		}
		if len(ipv6) > 0 {
			table.name6[h] = aaaa(h, ipv6)
		}
		if len(searchNamespaces) > 0 {
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
				table.allHosts[expandedHost] = struct{}{}
			}
		}
	}
}

// Borrowed from https://github.com/coredns/coredns/blob/master/plugin/hosts/hosts.go
// a takes a slice of net.IPs and returns a slice of A RRs.
func a(host string, ips []net.IP) []dns.RR {
	answers := make([]dns.RR, len(ips))
	for i, ip := range ips {
		r := new(dns.A)
		r.Hdr = dns.RR_Header{Name: host, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: defaultTTLInSeconds}
		r.A = ip
		answers[i] = r
	}
	return answers
}

// aaaa takes a slice of net.IPs and returns a slice of AAAA RRs.
func aaaa(host string, ips []net.IP) []dns.RR {
	answers := make([]dns.RR, len(ips))
	for i, ip := range ips {
		r := new(dns.AAAA)
		r.Hdr = dns.RR_Header{Name: host, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: defaultTTLInSeconds}
		r.AAAA = ip
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
