// Copyright 2017 Istio Authors
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
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	dnsapi "istio.io/istio/pkg/dns/coredns/pb"
	"istio.io/pkg/env"
	"istio.io/pkg/log"

	"google.golang.org/grpc"
)

// Based on istio-ecosystem/istio-coredns-plugin
// Changes from original:
// - runs inside istiod, using Istio main gRPC server
// - instead of directly reading from K8S, use istiod store.
// - removed "log" - switching to istio log.
// - refactored Query, so both DNS native interface and coredns grpc plugin are implemented
// - added parts of istio-ecosystem/dns-discovery, to provide in process DNS

// K8S DNS uses container port 10053, and appears to have a dnsmasq container
// alongside.

// IstioServiceEntries exposes a DNS interface to internal Istiod service database.
// This can be used:
// - as a CoreDNS gRPC plugin
// - as a DNS-over-TLS resolver, with support for forwarding to k8s or upstream
// - for debug - a plain DNS-over-UDP interface.
//
// The code is currently targeted for Istiod, with a per/pod or per/VM coreDNS
// forwarding to it, and using the same XDS grpc server and cert.
//
// In future we may embed it in istio-agent as well, using XDS to sync the config store.
type IstioServiceEntries struct {
	configStore model.IstioConfigStore
	mapMutex    sync.RWMutex
	mux         *dns.ServeMux
	dnsEntries  map[string][]net.IP
	client      *dns.Client
	server      *dns.Server
}

var (
	// DNSAddr is the env controlling the DNS-over-TLS server.
	// By default will be active, set to empty string to disable DNS functionality.
	DNSAddr = env.RegisterStringVar("dnsAddr", ":15053", "DNS listen address")
)

// Initialize the CoreDNS gRPC plugin.
// vip is the default IP to assign to entries that are not found.
func InitCoreDNS(grpcServer *grpc.Server, vip string, cfgStore model.IstioConfigStore) *IstioServiceEntries {

	h := &IstioServiceEntries{
		configStore: cfgStore,
		mux:         dns.NewServeMux(),
	}
	h.readServiceEntries(vip)
	stop := make(chan bool)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				h.readServiceEntries(vip)
			}
		}
	}()
	h.client = &dns.Client{Net: "udp"}
	h.server = &dns.Server{Handler: h.mux}

	h.mux.Handle(".", h)

	if grpcServer != nil {
		// Will be nil on tests, when only subsets of the server are started.
		dnsapi.RegisterDnsServiceServer(grpcServer, h)
	}
	return h
}

// StartDNS starts the DNS-over-TLS, using a pre-configured listener.
// Also creates a normal DNS on UDP port 15053, for local debugging
func (h *IstioServiceEntries) StartDNS(tlsListener net.Listener) {
	h.server.Listener = tlsListener
	var err error
	// UDP - on same port as the MTLS.
	h.server.PacketConn, err = net.ListenPacket("udp", DNSAddr.Get())
	if err != nil {
		log.Warna("Failed to start debugging DNS port", err)
	}
	err = h.server.ActivateAndServe()
	if err != nil {
		log.Warna("Failed to start debugging DNS", err)
	}
}

// Sync the entries from store to the serving map.
// Should be called on config change ( original code used a 5-sec timer - on the cached entries)
// TODO: hook it into the config change event.
func (h *IstioServiceEntries) readServiceEntries(vip string) {
	dnsEntries := make(map[string][]net.IP)
	serviceEntries := h.configStore.ServiceEntries()

	for _, e := range serviceEntries {
		entry := e.Spec.(*networking.ServiceEntry)

		if entry.Resolution == networking.ServiceEntry_NONE {
			// NO DNS based service discovery for service entries
			// that specify NONE as the resolution. NONE implies
			// that Istio should use the IP provided by the caller
			continue
		}

		addresses := entry.Addresses
		if len(addresses) == 0 && vip != "" {
			// If the ServiceEntry has no Addresses, map to a user-supplied default value, if provided
			addresses = []string{vip}
		}

		vips := convertToVIPs(addresses)
		if len(vips) == 0 {
			continue
		}

		for _, host := range entry.Hosts {
			key := fmt.Sprintf("%s.", host)
			if strings.Contains(host, "*") {
				// Validation will ensure that the host is of the form *.foo.com
				parts := strings.SplitN(host, ".", 2)
				// Prefix wildcards with a . so that we can distinguish these entries in the map
				key = fmt.Sprintf(".%s.", parts[1])
			}
			dnsEntries[key] = vips
		}
	}
	h.mapMutex.Lock()
	h.dnsEntries = make(map[string][]net.IP)
	for k, v := range dnsEntries {
		h.dnsEntries[k] = v
	}
	h.mapMutex.Unlock()
}

func convertToVIPs(addresses []string) []net.IP {
	vips := make([]net.IP, 0)

	for _, address := range addresses {
		// check if its CIDR.  If so, reject the address unless its /32 CIDR
		if strings.Contains(address, "/") {
			if ip, network, err := net.ParseCIDR(address); err != nil {
				ones, bits := network.Mask.Size()
				if ones == bits {
					// its a full mask (e.g., /32). Effectively an IP
					vips = append(vips, ip)
				}
			}
		} else {
			if ip := net.ParseIP(address); ip != nil {
				vips = append(vips, ip)
			}
		}
	}

	return vips
}

// ServerDNS is the implementation of DNS interface, when serving directly DNS-over-TLS
func (h *IstioServiceEntries) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	response := h.handle(r)
	var err error

	if len(response.Answer) == 0 {
		// Nothing found - forward to upstream DNS.
		// TODO: this is NOT secured - pilot to k8s will need to use TLS or run a local coredns
		// replica, using k8s plugin ( so this is over localhost )
		response, _, err = h.client.Exchange(r, "")
		if err != nil {
			log.Debuga("DNS error ", r, err)
			// TODO: increment counter - not clear if DNS client has metrics.
		}
	}

	if len(response.Answer) == 0 {
		response.Rcode = dns.RcodeNameError
	}

	err = w.WriteMsg(response)
	if err != nil {
		log.Debuga("DNS write error ", r, err)
	}
}

func (h *IstioServiceEntries) handle(request *dns.Msg) *dns.Msg {
	response := new(dns.Msg)
	response.SetReply(request)
	response.Authoritative = true

	for _, q := range request.Question {
		switch q.Qtype {
		case dns.TypeA:
			var vips []net.IP
			h.mapMutex.RLock()
			if h.dnsEntries != nil {
				vips = h.dnsEntries[q.Name]
				if vips == nil {
					// check for wildcard format
					// Split name into pieces by . (remember that DNS queries have dot in the end as well)
					// Check for each smaller variant of the name, until we have
					pieces := strings.Split(q.Name, ".")
					pieces = pieces[1:]
					for ; len(pieces) > 2; pieces = pieces[1:] {
						if vips = h.dnsEntries[fmt.Sprintf(".%s", strings.Join(pieces, "."))]; vips != nil {
							break
						}
					}
				}
			}
			h.mapMutex.RUnlock()
			if vips != nil {
				response.Answer = a(q.Name, vips)
			}
		}
	}
	return response
}

// Query is used when acting as a coredns grpc plugin
// code based on https://github.com/ahmetb/coredns-grpc-backend-sample
func (h *IstioServiceEntries) Query(ctx context.Context, in *dnsapi.DnsPacket) (*dnsapi.DnsPacket, error) {
	request := new(dns.Msg)
	if err := request.Unpack(in.Msg); err != nil {
		return nil, fmt.Errorf("failed to unmarshall dns query: %v", err)
	}

	response := h.handle(request)
	if len(response.Answer) == 0 {
		response.Rcode = dns.RcodeNameError
	}
	out, err := response.Pack()
	if err != nil {
		return nil, fmt.Errorf("failed to mashall dns response: %v", err)
	}
	return &dnsapi.DnsPacket{Msg: out}, nil
}

// Name implements the plugin.Handle interface.
func (h *IstioServiceEntries) Name() string { return "istio" }

// a takes a slice of net.IPs and returns a slice of A RRs.
func a(zone string, ips []net.IP) []dns.RR {
	answers := []dns.RR{}
	for _, ip := range ips {
		r := new(dns.A)
		r.Hdr = dns.RR_Header{Name: zone, Rrtype: dns.TypeA,
			Class: dns.ClassINET, Ttl: 3600}
		r.A = ip
		answers = append(answers, r)
	}
	return answers
}
