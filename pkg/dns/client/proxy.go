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
	"net"
	"time"

	"github.com/miekg/dns"
)

type dnsProxy struct {
	serveMux *dns.ServeMux
	server   *dns.Server

	// This is the upstream Client used to make upstream DNS queries
	// in case the data is not in our name table.
	upstreamClient *dns.Client
	protocol       string
	resolver       *LocalDNSServer
}

func newDNSProxy(protocol, addr string, resolver *LocalDNSServer) (*dnsProxy, error) {
	p := &dnsProxy{
		serveMux: dns.NewServeMux(),
		server:   &dns.Server{},
		upstreamClient: &dns.Client{
			Net:          protocol,
			DialTimeout:  5 * time.Second,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
		},
		protocol: protocol,
		resolver: resolver,
	}

	var err error
	p.serveMux.Handle(".", p)
	p.server.Handler = p.serveMux
	if protocol == "udp" {
		p.server.PacketConn, err = net.ListenPacket("udp", addr)
	} else {
		p.server.Listener, err = net.Listen("tcp", addr)
	}
	log.Infof("Starting local %s DNS server on %v", p.protocol, addr)
	if err != nil {
		log.Errorf("Failed to listen on %s port %s: %v", protocol, addr, err)
		return nil, err
	}
	return p, nil
}

func (p *dnsProxy) start() {
	err := p.server.ActivateAndServe()
	if err != nil {
		log.Errorf("Local %s DNS server terminated: %v", p.protocol, err)
	}
}

func (p *dnsProxy) close() {
	if p.server != nil {
		if err := p.server.Shutdown(); err != nil {
			log.Errorf("error in shutting down %s dns downstreamUDPServer :%v", p.protocol, err)
		}
	}
}

func (p *dnsProxy) Address() string {
	if p.server != nil {
		if p.server.Listener != nil {
			return p.server.Listener.Addr().String()
		}
		if p.server.PacketConn != nil {
			return p.server.PacketConn.LocalAddr().String()
		}
	}
	return ""
}

func (p *dnsProxy) ServeDNS(w dns.ResponseWriter, req *dns.Msg) {
	p.resolver.ServeDNS(p, w, req)
}
