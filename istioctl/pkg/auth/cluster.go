// Copyright 2019 Istio Authors
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

package auth

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	v2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"

	"istio.io/istio/pkg/log"
)

type ParsedCluster struct {
	serviceName string
	direction   string
	port        string
	protocol    string

	short      string
	ns         string
	domain     string
	tlsContext *auth.UpstreamTlsContext
}

func (c *ParsedCluster) print(w io.Writer, printAll bool) {
	var hostPort, sni, alpn, cert, validatePeerCert string

	hostPort = fmt.Sprintf("%s:%s", strings.TrimSuffix(c.serviceName, ".svc.cluster.local"), c.port)
	cert = "none"
	validatePeerCert = "none"
	if c.tlsContext != nil {
		sni = c.tlsContext.Sni
		if ctx := c.tlsContext.CommonTlsContext; ctx != nil {
			alpn = strings.Join(ctx.AlpnProtocols, ",")
			cert = getCertificate(ctx)
			validatePeerCert = getValidate(ctx)
		}
	}

	var err error
	if printAll {
		_, err = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", hostPort, sni, alpn, cert, validatePeerCert)
	} else {
		_, err = fmt.Fprintf(w, "%s\t%s\t%s\n", hostPort, cert, validatePeerCert)
	}

	if err != nil {
		log.Errorf("failed to print output: %s", err)
	}
}

func ParseCluster(cluster *v2.Cluster) *ParsedCluster {
	parts := strings.Split(cluster.Name, "|")
	if len(parts) != 4 {
		log.Debugf("invalid cluster name: %s", parts)
		return nil
	}

	splits := strings.SplitN(parts[3], ".", 3)
	var short, ns, domain string
	if len(splits) == 3 {
		short = splits[0]
		ns = splits[1]
		domain = splits[2]
	}

	return &ParsedCluster{
		direction:   parts[0],
		port:        parts[1],
		protocol:    parts[2],
		serviceName: parts[3],
		short:       short,
		ns:          ns,
		domain:      domain,
		tlsContext:  cluster.TlsContext,
	}
}

func PrintParsedClusters(writer io.Writer, parsedClusters []*ParsedCluster, printAll bool) {
	w := new(tabwriter.Writer).Init(writer, 0, 8, 5, ' ', 0)
	col := "HOST:PORT\tSNI\tALPN\tCERTIFICATE\tVALIDATE PEER CERTIFICATE"
	if !printAll {
		col = "HOST:PORT\tCERTIFICATE\tVALIDATE PEER CERTIFICATE"
	}
	if _, err := fmt.Fprintln(w, col); err != nil {
		log.Errorf("failed to print output: %s", err)
		return
	}
	for _, c := range parsedClusters {
		c.print(w, printAll)
	}
	_ = w.Flush()
}
