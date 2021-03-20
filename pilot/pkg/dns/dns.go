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

package dns

import (
	"net"
	"strings"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/miekg/dns"

	nds "istio.io/istio/pilot/pkg/proto"
	istiolog "istio.io/pkg/log"
)

var log = istiolog.RegisterScope("dns", "Istio DNS proxy", 0)

// Holds configurations for the DNS downstreamUDPServer in Istio Agent
type LocalDNSServer struct {
	// Holds the pointer to the DNS lookup table
	lookupTable atomic.Value

	udpDNSProxy *dnsProxy
	tcpDNSProxy *dnsProxy

	resolvConfServers []string
	searchNamespaces  []string
	// The namespace where the proxy resides
	// determines the hosts used for shortname resolution
	proxyNamespace string
	// Optimizations to save space and time
	proxyDomain      string
	proxyDomainParts []string
}

// Borrowed from https://github.com/coredns/coredns/blob/master/plugin/hosts/hostsfile.go
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

func NewLocalDNSServer(proxyNamespace, proxyDomain string) (*LocalDNSServer, error) {
	h := &LocalDNSServer{
		proxyNamespace: proxyNamespace,
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

	// We will use the local resolv.conf for resolving unknown names.
	dnsConfig, err := dns.ClientConfigFromFile("/etc/resolv.conf")
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

	if h.udpDNSProxy, err = newDNSProxy("udp", h); err != nil {
		return nil, err
	}
	if h.tcpDNSProxy, err = newDNSProxy("tcp", h); err != nil {
		return nil, err
	}

	return h, nil
}

// StartDNS starts the DNS-over-UDP downstreamUDPServer.
func (h *LocalDNSServer) StartDNS() {
	go h.udpDNSProxy.start()
	go h.tcpDNSProxy.start()
}

func (h *LocalDNSServer) UpdateLookupTable(nt *nds.NameTable) {
	lookupTable := &LookupTable{
		allHosts: map[string]struct{}{},
		name4:    map[string][]dns.RR{},
		name6:    map[string][]dns.RR{},
		cname:    map[string][]dns.RR{},
	}
	for host, ni := range nt.Table {
		// Given a host
		// if its a non-k8s host, store the host+. as the key with the pre-computed DNS RR records
		// if its a k8s host, store all variants (i.e. shortname+., shortname+namespace+., fqdn+., etc.)
		// shortname+. is only for hosts in current namespace
		var altHosts map[string]struct{}
		if ni.Registry == "Kubernetes" {
			altHosts = generateAltHosts(host, ni, h.proxyNamespace, h.proxyDomain, h.proxyDomainParts)
		} else {
			altHosts = map[string]struct{}{host + ".": {}}
		}
		ipv4, ipv6 := separateIPtypes(ni.Ips)
		if len(ipv6) == 0 && len(ipv4) == 0 {
			// malformed ips
			continue
		}
		lookupTable.buildDNSAnswers(altHosts, ipv4, ipv6, h.searchNamespaces)
	}
	h.lookupTable.Store(lookupTable)
	log.Debugf("updated lookup table with %d hosts", len(lookupTable.allHosts))
}

// ServerDNS is the implementation of DNS interface
func (h *LocalDNSServer) ServeDNS(proxy *dnsProxy, w dns.ResponseWriter, req *dns.Msg) {
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
	// we expect only one question in the query even though the spec allows many
	// clients usually do not do more than one query either.

	lp := h.lookupTable.Load()
	if lp == nil {
		response = new(dns.Msg)
		response.SetReply(req)
		response.Rcode = dns.RcodeServerFailure
		log.Debugf("dns request before lookup table is loaded")
		_ = w.WriteMsg(response)
		return
	}
	lookupTable := lp.(*LookupTable)
	var answers []dns.RR

	// This name will always end in a dot
	hostname := strings.ToLower(req.Question[0].Name)
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
		roundRobinResponse(response)
		// Truncate response if its too big for the request. In practice, this will only happen with
		// ~28 headless service replicas. When there are more than that clients need to use eDNS or
		// TCP (almost every client does this transparently).
		// In other cases, this is a NOP.
		response.Truncate(size(proxy.protocol, req))
		log.Debugf("response for hostname %q (found=true): %v", hostname, response)
		_ = w.WriteMsg(response)
		return
	}
	// We did not find the host in our internal cache. Query upstream and return the response as is.
	response = h.queryUpstream(proxy.upstreamClient, req, log)
	// Compress the response - we don't know if the incoming response was compressed or not. If it was,
	// but we don't compress on the outbound, we will run into issues. For example, if the compressed
	// size is 450 bytes but uncompressed 1000 bytes now we are outside of the non-eDNS UDP size limits
	response.Truncate(size(proxy.protocol, req))
	log.Debugf("response for hostname %q (found=false): %v", hostname, response)
	_ = w.WriteMsg(response)
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
	cname := []dns.RR{}
	address := []dns.RR{}
	mx := []dns.RR{}
	rest := []dns.RR{}
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
	h.udpDNSProxy.close()
	h.tcpDNSProxy.close()
}

// TODO: Figure out how to send parallel queries to all nameservers
func (h *LocalDNSServer) queryUpstream(upstreamClient *dns.Client, req *dns.Msg, scope *istiolog.Scope) *dns.Msg {
	var response *dns.Msg
	for _, upstream := range h.resolvConfServers {
		cResponse, _, err := upstreamClient.Exchange(req, upstream)
		if err == nil {
			response = cResponse
			break
		} else {
			scope.Infof("upstream failure: %v", err)
		}
	}
	if response == nil {
		response = new(dns.Msg)
		response.SetReply(req)
		response.Rcode = dns.RcodeServerFailure
	}
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

func generateAltHosts(hostname string, nameinfo *nds.NameTable_NameInfo, proxyNamespace, proxyDomain string,
	proxyDomainParts []string) map[string]struct{} {
	out := make(map[string]struct{})
	out[hostname+"."] = struct{}{}
	// do not generate alt hostnames if the service is in a different domain (i.e. cluster) than the proxy
	// as we have no way to resolve conflicts on name.namespace entries across clusters of different domains
	if proxyDomain == "" || !strings.HasSuffix(hostname, proxyDomain) {
		return out
	}
	out[nameinfo.Shortname+"."+nameinfo.Namespace+"."] = struct{}{}
	if proxyNamespace == nameinfo.Namespace {
		out[nameinfo.Shortname+"."] = struct{}{}
	}
	// Do we need to generate entries for name.namespace.svc, name.namespace.svc.cluster, etc. ?
	// If these are not that frequently used, then not doing so here will save some space and time
	// as some people have very long proxy domains with multiple dots
	// For now, we will generate just one more domain (which is usually the .svc piece).
	out[nameinfo.Shortname+"."+nameinfo.Namespace+"."+proxyDomainParts[0]+"."] = struct{}{}
	return out
}

// Given a host, this function first decides if the host is part of our service registry.
// If it is not part of the registry, return nil so that caller queries upstream. If it is part
// of registry, we will look it up in one of our tables, failing which we will return NXDOMAIN.
func (table *LookupTable) lookupHost(qtype uint16, hostname string) ([]dns.RR, bool) {
	var hostFound bool
	if _, hostFound = table.allHosts[hostname]; !hostFound {
		// this is not from our registry
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
		// We will return a chained response. In a chained response, the first entry is the cname record,
		// and the second one is the A/AAAA record itself. Some clients do not follow cname redirects
		// with additional DNS queries. Instead, they expect all the resolved records to be in the same
		// big DNS response (presumably assuming that a recursive DNS query should do the deed, resolve
		// cname et al and return the composite response).
		out = append(out, cn...)
		out = append(out, ipAnswers...)
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
