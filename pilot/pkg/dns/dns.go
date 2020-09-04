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
	"time"

	"github.com/miekg/dns"

	nds "istio.io/istio/pilot/pkg/proto"
	"istio.io/pkg/log"
)

// Holds configurations for the DNS downstreamServer in Istio Agent
type LocalDNSServer struct {
	// Holds the pointer to the DNS lookup table
	lookupTable atomic.Value

	downstreamMux    *dns.ServeMux
	downstreamServer *dns.Server

	// This is the upstreamClient used to make upstream DNS queries
	// in case the data is not in our cache.
	upstreamClient    *dns.Client
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
	// The key is a FQDN matching a DNS query (like example.com.), the value is pre-created DNS RR records
	// of A or AAAA type as appropriate.
	name4 map[string][]dns.RR
	name6 map[string][]dns.RR
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
		downstreamMux:  dns.NewServeMux(),
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

	h.downstreamMux.Handle(".", h)

	h.upstreamClient = &dns.Client{
		Net: "udp",
		// TODO: make it configurable
		DialTimeout: 3 * time.Second,
		ReadTimeout: 100 * time.Millisecond,
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
			h.resolvConfServers = append(h.resolvConfServers, s+":53")
		}
		h.searchNamespaces = dnsConfig.Search
	}

	h.downstreamServer = &dns.Server{Handler: h.downstreamMux}
	h.downstreamServer.PacketConn, err = net.ListenPacket("udp", ":15053")
	if err != nil {
		log.Errorf("Failed to listen on port 15053: %v", err)
		return nil, err
	}
	return h, nil
}

// StartDNS starts the DNS-over-UDP downstreamServer.
func (h *LocalDNSServer) StartDNS() {
	log.Infoa("Starting local DNS server at 0.0.0.0:15053")
	go func() {
		err := h.downstreamServer.ActivateAndServe()
		if err != nil {
			log.Errorf("Local DNS server terminated: %v", err)
		}
	}()
}

func (h *LocalDNSServer) UpdateLookupTable(nt *nds.NameTable) {
	lookupTable := &LookupTable{
		name4: map[string][]dns.RR{},
		name6: map[string][]dns.RR{},
		cname: map[string][]dns.RR{},
	}
	for host, ni := range nt.Table {
		// Given a host
		// if its a non-k8s host, store the host+. as the key with the pre-computed DNS RR records
		// if its a k8s host, store all variants (i.e. shortname+., shortname+namespace+., fqdn+., etc.)
		// shortname+. is only for hosts in current namespace
		var altHosts []string
		if ni.Registry == "Kubernetes" {
			altHosts = generateAltHosts(host, ni, h.proxyNamespace, h.proxyDomain, h.proxyDomainParts)
		} else {
			altHosts = []string{host + "."}
		}
		ipv4, ipv6 := separateIPtypes(ni.Ips)
		if len(ipv6) == 0 && len(ipv4) == 0 {
			// malformed ips
			continue
		}
		lookupTable.buildDNSAnswers(altHosts, ipv4, ipv6, h.searchNamespaces)
	}
	h.lookupTable.Store(lookupTable)
}

// ServerDNS is the implementation of DNS interface
func (h *LocalDNSServer) ServeDNS(w dns.ResponseWriter, req *dns.Msg) {
	var response *dns.Msg

	if len(req.Question) == 0 {
		response = new(dns.Msg)
		response.SetReply(req)
		response.Rcode = dns.RcodeNameError
	} else {
		// we expect only one question in the query even though the spec allows many
		// clients usually do not do more than one query either.

		lookupTable := h.lookupTable.Load().(*LookupTable)
		var answers []dns.RR

		// This name will always end in a dot
		hostname := strings.ToLower(req.Question[0].Name)

		switch req.Question[0].Qtype {
		case dns.TypeA:
			answers = lookupTable.lookupHostIPv4(hostname)
		case dns.TypeAAAA:
			answers = lookupTable.lookupHostIPv6(hostname)
			// TODO: handle PTR records for reverse dns lookups
		}

		if len(answers) > 0 {
			response = new(dns.Msg)
			response.SetReply(req)
			response.Answer = answers
		} else {
			response = h.queryUpstream(req)
		}
	}

	_ = w.WriteMsg(response)
}

func (h *LocalDNSServer) Close() {
	if h.downstreamServer != nil {
		if err := h.downstreamServer.Shutdown(); err != nil {
			log.Errorf("error in shutting down dns downstreamServer :%v", err)
		}
	}
}

// TODO: Figure out how to send parallel queries to all nameservers
func (h *LocalDNSServer) queryUpstream(req *dns.Msg) *dns.Msg {
	var response *dns.Msg
	for _, upstream := range h.resolvConfServers {
		cResponse, _, err := h.upstreamClient.Exchange(req, upstream)
		if err == nil && len(cResponse.Answer) > 0 {
			response = cResponse
			break
		}
	}
	if response == nil {
		response = new(dns.Msg)
		response.SetReply(req)
		response.Rcode = dns.RcodeNameError
	}
	return response
}

func separateIPtypes(ips []string) (ipv4, ipv6 []net.IP) {
	for _, ip := range ips {
		addr := net.ParseIP(ip)
		if addr == nil {
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
	proxyDomainParts []string) []string {
	out := make([]string, 0)
	out = append(out, hostname+".")
	// do not generate alt hostnames if the service is in a different domain (i.e. cluster) than the proxy
	// as we have no way to resolve conflicts on name.namespace entries across clusters of different domains
	if proxyDomain == "" || !strings.HasSuffix(hostname, proxyDomain) {
		return out
	}
	out = append(out, nameinfo.Shortname+"."+nameinfo.Namespace+".")
	if proxyNamespace == nameinfo.Namespace {
		out = append(out, nameinfo.Shortname+".")
	}
	// Do we need to generate entries for name.namespace.svc, name.namespace.svc.cluster, etc. ?
	// If these are not that frequently used, then not doing so here will save some space and time
	// as some people have very long proxy domains with multiple dots
	// For now, we will generate just one more domain (which is usually the .svc piece).
	out = append(out, nameinfo.Shortname+"."+nameinfo.Namespace+"."+proxyDomainParts[0]+".")
	return out
}

func (table *LookupTable) lookupHostIPv4(host string) []dns.RR {
	ans := table.name4[host]
	if len(ans) == 0 {
		ans = table.cname[host]
	}
	return ans
}

func (table *LookupTable) lookupHostIPv6(host string) []dns.RR {
	ans := table.name6[host]
	if len(ans) == 0 {
		ans = table.cname[host]
	}
	return ans
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
// to do string parsing, memory allocations, etc. at query time at the cost of 2x number of entries (i.e. memory) to store
// the lookup table.
func (table *LookupTable) buildDNSAnswers(altHosts []string, ipv4 []net.IP, ipv6 []net.IP, searchNamespaces []string) {
	for _, h := range altHosts {
		if len(ipv4) > 0 {
			table.name4[h] = a(h, ipv4)
		}
		if len(ipv6) > 0 {
			table.name6[h] = aaaa(h, ipv6)
		}
		if len(searchNamespaces) > 0 {
			// host h already ends with a .
			// search namespace does not. So we append one in the end
			randomHost := h + searchNamespaces[0] + "."
			table.cname[randomHost] = cname(randomHost, h)
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
	}
	answer.Target = targetHost
	return []dns.RR{answer}
}
