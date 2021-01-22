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

package forwarder

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
)

var _ protocol = &dnsProtocol{}

type dnsProtocol struct {
}

type dnsRequest struct {
	hostname  string
	dnsServer string
	query     string
	protocol  string
}

func checkIn(got string, want ...string) error {
	for _, w := range want {
		if w == got {
			return nil
		}
	}
	return fmt.Errorf("got value %q, wanted one of %v", got, want)
}

func parseRequest(inputUrl string) (dnsRequest, error) {
	resp := dnsRequest{}
	u, err := url.Parse(inputUrl)
	if err != nil {
		return resp, err
	}
	qp, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return resp, err
	}
	resp.protocol = qp.Get("protocol")
	if err := checkIn(resp.protocol, "", "udp", "tcp"); err != nil {
		return resp, err
	}
	resp.dnsServer = qp.Get("server")
	if resp.dnsServer != "" {
		if _, _, err := net.SplitHostPort(resp.dnsServer); err != nil && strings.Contains(err.Error(), "missing port in address") {
			resp.dnsServer += ":53"
		}
	}
	resp.hostname = u.Host
	resp.query = qp.Get("query")
	if err := checkIn(resp.query, "", "A", "AAAA"); err != nil {
		return resp, err
	}
	return resp, nil
}

func (c *dnsProtocol) makeRequest(ctx context.Context, rreq *request) (string, error) {
	req, err := parseRequest(rreq.URL)
	if err != nil {
		return "", err
	}
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: rreq.Timeout,
			}
			nt := req.protocol
			if nt == "" {
				nt = network
			}
			addr := req.dnsServer
			if addr == "" {
				addr = address
			}
			return d.DialContext(ctx, nt, addr)
		},
	}
	nt := func() string {
		switch req.query {
		case "A":
			return "ip4"
		case "AAAA":
			return "ip6"
		default:
			return "ip"
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, rreq.Timeout)
	defer cancel()
	ips, _ := r.LookupIP(ctx, nt, req.hostname)

	var outBuffer bytes.Buffer
	outBuffer.WriteString(fmt.Sprintf("[%d] Hostname=%s\n", rreq.RequestID, req.hostname))
	outBuffer.WriteString(fmt.Sprintf("[%d] Protocol=%s\n", rreq.RequestID, req.protocol))
	outBuffer.WriteString(fmt.Sprintf("[%d] Query=%s\n", rreq.RequestID, req.query))
	outBuffer.WriteString(fmt.Sprintf("[%d] DnsServer=%s\n", rreq.RequestID, req.dnsServer))
	for n, i := range ips {
		outBuffer.WriteString(fmt.Sprintf("[%d body] Response%d=%s\n", rreq.RequestID, n, i.String()))
	}
	return outBuffer.String(), nil
}

func (c *dnsProtocol) Close() error {
	return nil
}
