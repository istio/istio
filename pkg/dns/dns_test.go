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
	"io/ioutil"
	"net"
	"path"
	"testing"
	"time"

	"github.com/miekg/dns"

	"istio.io/istio/pkg/test/env"
)

var (
	istiodDNSAddr = "127.0.0.1:14053"
	agentDNSAddr  = "127.0.0.1:14054"

	initErr error
)

func init() {
	initErr = initDNS()
}

func initDNS() error {
	key := path.Join(env.IstioSrc, "tests/testdata/certs/pilot/key.pem")
	cert := path.Join(env.IstioSrc, "tests/testdata/certs/pilot/cert-chain.pem")
	root := path.Join(env.IstioSrc, "tests/testdata/certs/pilot/root-cert.pem")

	certP, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		return err
	}

	cp := x509.NewCertPool()
	rootCertBytes, err := ioutil.ReadFile(root)
	if err != nil {
		return err
	}
	cp.AppendCertsFromPEM(rootCertBytes)

	cfg := &tls.Config{
		Certificates: []tls.Certificate{certP},
		ClientAuth:   tls.NoClientCert,
		ClientCAs:    cp,
	}
	// TODO: load certificates for TLS
	istiodDNS := InitDNS()
	l, err := net.Listen("tcp", istiodDNSAddr)
	if err != nil {
		return err
	}
	istiodDNS.StartDNS(istiodDNSAddr, tls.NewListener(l, cfg))

	agentDNS := InitDNSAgent(istiodDNSAddr, "", rootCertBytes, []string{".com."})
	agentDNS.StartDNS(agentDNSAddr, nil)
	return nil
}

func TestDNS(t *testing.T) {
	if initErr != nil {
		t.Fatal(initErr)
	}

	t.Run("simple", func(t *testing.T) {
		c := dns.Client{}
		m := new(dns.Msg)
		m.SetQuestion("www.google.com.", dns.TypeA)
		res, _, err := c.Exchange(m, agentDNSAddr)

		if err != nil {
			t.Error("Failed to resolve", err)
		} else if len(res.Answer) == 0 {
			t.Error("No results")
		}

	})
}

// Benchmark - the actual address will be cached by the local resolver.
// Baseline:
//  - initial impl in 1.6:
//      170 uS on agent
//      116 uS on istiod (UDP to machine resolver)
//      75 uS machine resolver
func BenchmarkDNS(t *testing.B) {
	if initErr != nil {
		t.Fatal(initErr)
	}

	t.Run("TLS", func(b *testing.B) {
		bench(b, agentDNSAddr)
	})

	t.Run("plain", func(b *testing.B) {
		bench(b, istiodDNSAddr)
	})

	t.Run("public", func(b *testing.B) {
		dnsConfig, err := dns.ClientConfigFromFile("/etc/resolv.conf")
		if err != nil {
			b.Fatal(err)
		}

		bench(b, dnsConfig.Servers[0]+":53")
	})
}

func bench(t *testing.B, dest string) {
	errs := 0
	nrs := 0
	for i := 0; i < t.N; i++ {
		c := dns.Client{
			Timeout: 1 * time.Second,
		}
		m := new(dns.Msg)
		m.SetQuestion("www.google.com.", dns.TypeA)
		res, _, err := c.Exchange(m, dest)

		if err != nil {
			errs++
		} else if len(res.Answer) == 0 {
			nrs++
		}
	}

	if errs+nrs > 0 {
		t.Log("Sent", t.N, "err", errs, "no response", nrs)
	}

}
