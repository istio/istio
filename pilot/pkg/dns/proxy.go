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

	"github.com/miekg/dns"
)

type dnsProxy struct {
	downstreamMux    *dns.ServeMux
	downstreamServer *dns.Server

	// This is the upstream Client used to make upstream DNS queries
	// in case the data is not in our cache.
	upstreamClient *dns.Client
	protocol       string
	resolver       *LocalDNSServer
}

func newDNSProxy(protocol string, resolver *LocalDNSServer) (*dnsProxy, error) {
	p := &dnsProxy{
		downstreamMux:    dns.NewServeMux(),
		downstreamServer: &dns.Server{},
		upstreamClient: &dns.Client{
			Net: protocol,
		},
		protocol: protocol,
		resolver: resolver,
	}

	var err error
	p.downstreamMux.Handle(".", p)
	p.downstreamServer.Handler = p.downstreamMux
	if protocol == "udp" {
		p.downstreamServer.PacketConn, err = net.ListenPacket("udp", "localhost:15053")
	} else {
		p.downstreamServer.Listener, err = net.Listen("tcp", "localhost:15053")
	}
	if err != nil {
		log.Errorf("Failed to listen on %s port 15053: %v", protocol, err)
		return nil, err
	}
	return p, nil
}

func (p *dnsProxy) start() {
	log.Infof("Starting local %s DNS server at localhost:15053", p.protocol)
	err := p.downstreamServer.ActivateAndServe()
	if err != nil {
		log.Errorf("Local %s DNS server terminated: %v", p.protocol, err)
	}
}

func (p *dnsProxy) close() {
	if p.downstreamServer != nil {
		if err := p.downstreamServer.Shutdown(); err != nil {
			log.Errorf("error in shutting down %s dns downstreamUDPServer :%v", p.protocol, err)
		}
	}
}

func (p *dnsProxy) ServeDNS(w dns.ResponseWriter, req *dns.Msg) {
	p.resolver.ServeDNS(p, w, req)
}
