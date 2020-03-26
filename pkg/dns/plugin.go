// Copyright 2020 Istio Authors
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

	"github.com/miekg/dns"

	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

// Based on istio-ecosystem/istio-coredns-plugin
// Changes from original:
// - runs inside istiod, using Istio main gRPC server
// - instead of directly reading from K8S, use istiod store.
// - removed "log" - switching to istio log.
// - refactored Query, so both DNS native interface and coredns grpc plugin are implemented
// - added parts of istio-ecosystem/dns-discovery, to provide in process DNS

// Troubleshooting - on the pod/VM where the library is running, i.e. istiod or
// in next phase on each pod:
//
// - recursive resolver ( external names ):
//     dig @127.0.0.1 -p 15053 www.google.com
//
// -

// IstioDNS exposes a DNS interface to internal Istiod service database.
// This can be used:
// - as a CoreDNS gRPC plugin
// - as a DNS-over-TLS resolver, with support for forwarding to k8s or upstream
// - for debug - a plain DNS-over-UDP interface.
//
// The code is currently targeted for Istiod, with a per/pod or per/VM coreDNS
// forwarding to it, and using the same XDS grpc server and cert.
//
// In future we may embed it in istio-agent as well, using XDS to sync the config store.
type IstioDNS struct {
	mux        *dns.ServeMux
	client     *dns.Client
	server     *dns.Server
	resolvConf *dns.ClientConfig
}

var (
	// DNSAddr is the env controlling the DNS-over-TLS server.
	// By default will be active, set to empty string to disable DNS functionality.
	DNSAddr = env.RegisterStringVar("dnsAddr", ":15053", "DNS listen address")
)

// InitDNS will create the IstioDNS struct.
func InitDNS() *IstioDNS {

	h := &IstioDNS{
		mux: dns.NewServeMux(),
	}
	h.client = &dns.Client{Net: "udp"}
	h.server = &dns.Server{Handler: h.mux}

	h.mux.Handle(".", h)

	// Attempt to find the 'upstream' DNS server, used for entries not known by Istiod
	// That includes external names, may also include stateful sets (not clear we want
	// to cache the entire database if dns is running in agent). Istiod does have all
	// endpoints, including stateful sets - and could resolve over TLS - but not
	// in the initial implementation.
	// TODO: allow env override
	dnsConfig, err := dns.ClientConfigFromFile("/etc/resolv.conf")
	if err != nil {
		// K8S provides one, as well as most VMs.
		log.Warna("Unexpected missing resolv.conf - no upstream DNS", err)
		return h
	}
	if dnsConfig != nil && len(dnsConfig.Servers) > 0 {
		h.resolvConf = dnsConfig
	}
	log.Infoa("Started CoreDNS grpc service")
	return h
}

// StartDNS starts the DNS-over-TLS, using a pre-configured listener.
// Also creates a normal DNS on UDP port 15053, for local debugging
func (h *IstioDNS) StartDNS(tlsListener net.Listener) {
	h.server.Listener = tlsListener
	var err error
	// UDP - on same port as the MTLS.
	h.server.PacketConn, err = net.ListenPacket("udp", DNSAddr.Get())
	if err != nil {
		log.Warna("Failed to start DNS UDP", DNSAddr.Get(), err)
	}
	log.Infoa("Started DNS", DNSAddr.Get())
	err = h.server.ActivateAndServe()
	if err != nil {
		log.Warna("Failed to start DNS", DNSAddr.Get(), err)
	}
}

// ServerDNS is the implementation of DNS interface, when serving directly DNS-over-TLS
func (h *IstioDNS) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	var err error

	// Nothing found - forward to upstream DNS.
	// TODO: this is NOT secured - pilot to k8s will need to use TLS or run a local coredns
	// replica, using k8s plugin ( so this is over localhost )
	response, _, err := h.client.Exchange(r, h.resolvConf.Servers[0]+":53")
	if err != nil {
		log.Debuga("DNS error ", r, err)
		// cResponse will be nil - leave the original response object
		// TODO: increment counter - not clear if DNS client has metrics.
		response = new(dns.Msg)
		response.SetReply(r)
		response.Rcode = dns.RcodeNameError
	}

	if len(response.Answer) == 0 {
		response.Rcode = dns.RcodeNameError
	}

	err = w.WriteMsg(response)
	if err != nil {
		log.Debuga("DNS write error ", r, err)
	}
}
