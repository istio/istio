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
	"testing"
	"time"

	"github.com/miekg/dns"

	nds "istio.io/istio/pilot/pkg/proto"
)

var (
	agentDNSAddr = "127.0.0.1:15053"

	initErr error
)

func init() {
	initErr = initDNS()
}

func initDNS() error {
	// TODO: load certificates for TLS
	agentDNS, err := NewLocalDNSServer("ns1", "ns1.svc.cluster.local")
	if err != nil {
		return err
	}
	agentDNS.StartDNS()
	agentDNS.UpdateLookupTable(&nds.NameTable{
		Table: map[string]*nds.NameTable_NameInfo{
			"www.google.com": {
				Ips:      []string{"1.1.1.1"},
				Registry: "External",
			},
			"productpage.ns1.svc.cluster.local": {
				Ips:       []string{"9.9.9.9"},
				Registry:  "Kubernetes",
				Namespace: "ns1",
				Shortname: "productpage",
			},
			"reviews.ns2.svc.cluster.local": {
				Ips:       []string{"10.10.10.10"},
				Registry:  "Kubernetes",
				Namespace: "ns1",
				Shortname: "reviews",
			},
		},
	})
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
//      119us via agent if cached
//      32ms via machine resolver
// for a lookup, no match and then resolve,
//  it takes 2.3ms while direct resolution takes 2.03ms
// So the cache lookup & forward adds a 300us overhead (needs more data)
func BenchmarkDNS(t *testing.B) {
	if initErr != nil {
		t.Fatal(initErr)
	}

	t.Run("plain", func(b *testing.B) {
		bench(b, agentDNSAddr)
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
		m.SetQuestion("www.bing.com.", dns.TypeA)
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
