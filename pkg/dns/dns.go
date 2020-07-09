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
	"crypto/tls"
	"crypto/x509"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"

	"istio.io/pkg/monitoring"

	"istio.io/pkg/env"
	"istio.io/pkg/log"

	"istio.io/istio/pkg/config/constants"
)

// Based on istio-ecosystem/istio-coredns-plugin
// Changes from original:
// - runs inside istiod, using Istio main gRPC server
// - instead of directly reading from K8S, use istiod store.
// - removed "log" - switching to istio log.
// - refactored Query, so both DNS native interface and coredns grpc plugin are implemented
// - added parts of istio-ecosystem/dns-discovery, to provide in process DNS

// TODO:
// - add metrics, ideally using same names as kubedns/coredns ( Doug ?)
// - config options on what suffix to capture in agent

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
	mux *dns.ServeMux

	// local DNS-UDP server.
	// Active in agent and istiod.
	server *dns.Server

	// TODO: add a dns-over-TCP server and capture, for istio-agent.
	// local DNS-TLS server. This is active only in istiod.
	tlsServer *dns.Server

	resolvConfServers []string

	running bool

	client *dns.Client

	// tlsClient is a client initialized with DNS-over-TLS certificates.
	// If nil, no DNS-TLS requests will be made.
	tlsClient   *dns.Client
	tlsUpstream string
	backoff     time.Duration

	// m protects pending, conn and outID
	m       sync.Mutex
	pending map[uint16]chan *dns.Msg
	conn    *dns.Conn
	// outID is used to match requests to responses in the DNS-TCP.
	outID        uint16
	dnsTLSSuffix []string
}

var (
	// DNSAddr is the env controlling the DNS-over-TLS server in istiod.
	// By default will be active, set to empty string to disable DNS functionality.
	// Do not change in prod - it must match the Service. Used for testing or running
	// on VMs.
	DNSAddr = env.RegisterStringVar("DNS_ADDR", ":15053", "DNS listen address")

	// DNSAgentAddr is the listener address on the agent.
	// This is in the range of hardcoded address used by agent - not customizable
	// except for tests.
	// By default will be active, set to empty string to disable DNS functionality.
	// Iptables interception matches this.
	DNSAgentAddr = ":15053"

	// DNSTLSEnableAgent activates the DNS-over-TLS in istio-agent.
	// This will just attempt to connect to Istiod and start the DNS server on the default port -
	// DNS_CAPTURE controls capturing port 53.
	// Not using a bool - it's error prone in template, annotations, helm.
	// For now any non-empty value will enable TLS in the agent - we may further customize
	// the mode, for example specify DNS-HTTPS vs DNS-TLS
	DNSTLSEnableAgent = env.RegisterStringVar("DNS_AGENT", "", "If set, enable the "+
		"capture of outgoing DNS packets on port 53, redirecting to istio-agent on :15053")

	// DNSUpstream allows overriding the upstream server. By default we use [discovery-address]:853
	// If a secure DNS server is available - set this to point to the server.
	// It is assumed that the server has certificates signed by Istio.
	// TODO: add option to indicate the expected root CA, or use of public certs.
	// TODO: also trust k8s and root CAs
	// TODO: add https and grpc ( tcp-tls, https are the Net names used in miekg library )
	DNSUpstream = env.RegisterStringVar("DNS_SERVER", "",
		"Protocol and DNS server to use. Currently only tcp-tls: is supported.")

	pendingTLS = monitoring.NewGauge(
		"dns_tls_pending",
		"Number of pending DNS-over-TLS requests")

	dnsTLS = monitoring.NewSum("dns_tls_req", "DNS-over-TLS requests")
)

func InitDNS() *IstioDNS {
	h := &IstioDNS{
		mux:     dns.NewServeMux(),
		pending: map[uint16]chan *dns.Msg{},
		backoff: 1 * time.Second,
	}

	h.mux.Handle(".", h)

	// TODO: use a custom dialer
	h.client = &dns.Client{Net: "udp"}
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
		for _, s := range dnsConfig.Servers {
			h.resolvConfServers = append(h.resolvConfServers, s+":53")
		}
	}
	return h
}

// InitDNS will create the IstioDNS and initialize the agent:
// - /etc/resolv.conf will be parsed, and nameservers added to resolvConf list
// - discoveryAddress is the XDS server address, including port. The DNS-TLS server
//   is by default on the same host, port 853 (standard).
// - domain is the same as "--domain" passed to agent.
func InitDNSAgent(discoveryAddress string, domain string, cert []byte, suffixes []string) *IstioDNS {
	dnsTLSServer, discoveryPort, err := net.SplitHostPort(discoveryAddress)
	if err != nil {
		log.Errora("Invalid discovery address, defaulting ", discoveryAddress, " ", err)
		dnsTLSServer = "istiod.istio-system.svc"
	}

	dnsDomainL := strings.Split(domain, ".")
	clusterLocal := constants.DefaultKubernetesDomain
	if len(dnsDomainL) > 3 {
		clusterLocal = strings.Join(dnsDomainL[2:], ".")
	}
	suffixes = append(suffixes, clusterLocal+".")

	h := InitDNS()

	h.dnsTLSSuffix = suffixes
	if dnsTLSServer != "" && cert != nil {
		certPool := x509.NewCertPool()
		ok := certPool.AppendCertsFromPEM(cert)
		if !ok {
			log.Warna("Failed to load certificate ", cert)
		} else {
			h.tlsClient = &dns.Client{
				Net: "tcp-tls",
				TLSConfig: &tls.Config{
					RootCAs: certPool,
				},
				DialTimeout: 2 * time.Second,
			}
			if strings.HasPrefix(dnsTLSServer, "127.0.0.1") {
				// test/debug
				h.tlsClient.TLSConfig.ServerName = "istiod.istio-system.svc"
				// preserve the port
				h.tlsUpstream = dnsTLSServer + ":" + discoveryPort
			} else if DNSUpstream.Get() != "" &&
				strings.HasSuffix(DNSUpstream.Get(), "tcp-tls:") {
				h.tlsUpstream = strings.TrimPrefix(DNSUpstream.Get(), "tcp-tls:")
			} else {
				h.tlsUpstream = dnsTLSServer + ":853"
			}
			// Maintain a connection to the TLS server.
			h.openTLS()
		}
	}
	return h
}

// StartDNS starts the DNS-over-UDP and DNS-over-TLS.
func (h *IstioDNS) StartDNS(udpAddr string, tlsListener net.Listener) {
	var err error
	if tlsListener != nil {
		// In istiod, using same TLS certificate as the main server.
		h.tlsServer = &dns.Server{
			Handler: h.mux,
			IdleTimeout: func() time.Duration {
				// large timeout
				return 60 * time.Second
			},
			ReadTimeout:   60 * time.Second,
			MaxTCPQueries: -1,
		}
		h.tlsServer.Listener = tlsListener
		log.Infoa("Started DNS-TLS", tlsListener.Addr())
		go func() {
			err := h.tlsServer.ActivateAndServe()
			if err != nil {
				log.Errora("Failed to activate DNS-TLS ", err)
			}
		}()

	}
	// UDP
	h.server = &dns.Server{Handler: h.mux}
	h.server.PacketConn, err = net.ListenPacket("udp", udpAddr)
	if err != nil {
		log.Warna("Failed to start DNS UDP", udpAddr, err)
	}

	log.Infoa("Started DNS ", udpAddr)
	go func() {
		err := h.server.ActivateAndServe()
		if err != nil {
			log.Errora("Failed to activate DNS-UDP ", err)
		}
	}()
}

// ServerDNS is the implementation of DNS interface
func (h *IstioDNS) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	t0 := time.Now()
	var err error
	var response *dns.Msg

	useTLS := false
	if len(h.dnsTLSSuffix) > 0 {
		for _, q := range r.Question {
			for _, s := range h.dnsTLSSuffix {
				if strings.HasSuffix(q.Name, s) {
					useTLS = true
					break
				}
			}
			if useTLS {
				break
			}
		}
	}

	// Nothing found - forward to upstream DNS.
	// TODO: this is NOT secured - pilot to k8s will need to use TLS or run a local coredns
	// replica, using k8s plugin ( so this is over localhost )
	if !useTLS || h.tlsClient == nil {
		response, _, err = h.client.Exchange(r, h.resolvConfServers[0])
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

		if false {
			log.Infoa("DNS:", time.Since(t0), r.Question, response.Answer)
		}
		err = w.WriteMsg(response)
		if err != nil {
			log.Debuga("DNS write error ", r, err)
		}
		return
	}

	h.ServeDNSTLS(w, r)
}

// Handles a request using DNS-over-TLS.
func (h *IstioDNS) ServeDNSTLS(w dns.ResponseWriter, r *dns.Msg) {
	t0 := time.Now()
	// DNS-over-TLS - using client.Exchange has horrible performance - the
	// client is not optimized (8ms RTT instead of <1ms). CoreDNS uses a
	//connection pool, which is also not optimal. The RFC recommends pipelining.

	// By using this code, the latency is around 800us - with ~400 us in istiod
	origID := r.MsgHdr.Id
	var key uint16
	ch := make(chan *dns.Msg, 1)
	h.m.Lock()
	h.outID++
	key = h.outID
	// The ID on the TLS connection is different from the one in the UDS.
	r.MsgHdr.Id = h.outID
	h.pending[h.outID] = ch
	pendingTLS.Increment()
	h.m.Unlock()

	defer func() {
		h.m.Lock()
		delete(h.pending, key)
		pendingTLS.Decrement()
		h.m.Unlock()
	}()

	var response *dns.Msg
	currentConn := h.connTLS()
	if currentConn == nil {
		// Not connected - don't write anything, client will retry
		log.Infoa("DNS timeout, no TLS connection")
		response = new(dns.Msg)
		response.SetRcode(r, dns.RcodeServerFailure)
		response.MsgHdr.Id = origID
		_ = w.WriteMsg(response)
		return
	}

	// TODO: optimize - when close restart immediately the connect, maybe
	// keep 2-3 connections open ?
	dnsTLS.Increment()
	err := currentConn.WriteMsg(r)
	if err != nil {
		return
	}

	to := time.NewTimer(2 * time.Second)
	select {
	case m := <-ch:
		to.Stop()
		m.MsgHdr.Id = origID
		response = m
		_ = w.WriteMsg(m)
	case <-to.C:
		return
	}
	if false {
		log.Infoa("DNS-TLS:", time.Since(t0), r.Question, response.Answer)
	}
}

func (h *IstioDNS) Close() {
	h.m.Lock()
	h.running = false
	if h.conn != nil {
		h.conn.Close()
	}
	h.m.Unlock()
	if h.server != nil {
		if err := h.server.Shutdown(); err != nil {
			log.Errorf("error in shutting down dns server :%v", err)
		}
	}
	if h.tlsServer != nil {
		if err := h.tlsServer.Shutdown(); err != nil {
			log.Errorf("error in shutting down tls dns server :%v", err)
		}
	}
}

func (h *IstioDNS) connTLS() *dns.Conn {
	h.m.Lock()
	conn := h.conn
	h.m.Unlock()

	if conn != nil {
		return conn
	}
	// TODO: use a Cond to return when connection succeeded
	time.Sleep(100 * time.Millisecond)
	h.m.Lock()
	conn = h.conn
	h.m.Unlock()

	return conn
}

func (h *IstioDNS) openTLS() {
	h.running = true
	t0 := time.Now()
	conn, err := h.tlsClient.Dial(h.tlsUpstream)
	h.m.Lock()
	h.conn = conn
	h.m.Unlock()
	log.Infoa("DNS: Opened TLS connection to ", h.tlsUpstream, " ", time.Since(t0), err)
	if err != nil {
		log.Warna("Initial failure to open DNS-TLS connection, will retry", err)
	}
	// Maintain the connection
	go func() {
		tclose := time.Now()
		for h.running {
			if conn == nil {
				log.Infoa("DNS: TLS connection reopen ", time.Since(t0))
				conn, err = h.tlsClient.Dial(h.tlsUpstream)
				h.m.Lock()
				h.conn = conn
				h.m.Unlock()
				if err != nil {
					// TODO: exponential backoff
					// TODO: if we are not in strict mode, fallback to UDP
					time.Sleep(h.backoff)
					if h.backoff < 33*time.Second {
						h.backoff = 2 * h.backoff
					}
					continue
				}
				h.backoff = 1 * time.Second
				log.Infoa("DNS: Opened TLS connection to ", h.tlsUpstream, " ",
					time.Since(t0), " ", time.Since(tclose), err)
			}

			msg, err := h.conn.ReadMsg()
			if err != nil {
				log.Infoa("DNS read error, reconnect ", err)
				conn = nil
				h.m.Lock()
				h.conn = nil
				h.m.Unlock()
				tclose = time.Now()
				continue
			}
			uid := msg.MsgHdr.Id
			h.m.Lock()
			pr := h.pending[uid]
			h.m.Unlock()
			if pr != nil {
				pr <- msg
			}
		}
	}()
}
