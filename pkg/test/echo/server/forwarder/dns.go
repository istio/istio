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

type dnsProtocol struct{}

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

func parseRequest(inputURL string) (dnsRequest, error) {
	req := dnsRequest{}
	u, err := url.Parse(inputURL)
	if err != nil {
		return req, err
	}
	qp, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return req, err
	}
	req.protocol = qp.Get("protocol")
	if err := checkIn(req.protocol, "", "udp", "tcp"); err != nil {
		return req, err
	}
	req.dnsServer = qp.Get("server")
	if req.dnsServer != "" {
		if _, _, err := net.SplitHostPort(req.dnsServer); err != nil && strings.Contains(err.Error(), "missing port in address") {
			req.dnsServer += ":53"
		}
	}
	req.hostname = u.Host
	req.query = qp.Get("query")
	if err := checkIn(req.query, "", "A", "AAAA"); err != nil {
		return req, err
	}
	return req, nil
}

func (c *dnsProtocol) makeRequest(ctx context.Context, rreq *request) (string, error) {
	req, err := parseRequest(rreq.URL)
	if err != nil {
		return "", err
	}
	r := newResolver(rreq.Timeout, req.protocol, req.dnsServer)
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
	ips, err := r.LookupIP(ctx, nt, req.hostname)
	if err != nil {
		return "", err
	}

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
