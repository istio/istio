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
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"net"
	"path"
	"testing"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/gogo/protobuf/proto"
	"github.com/miekg/dns"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy/xds"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/test/env"

	_ "google.golang.org/grpc/xds/experimental" // To install the xds resolvers and balancers.
)

var (
	istiodDNSAddr = "127.0.0.1:14053"
	agentDNSAddr  = "127.0.0.1:14054"

	grpcAddr = "127.0.0.1:14056"

	// Address the tests are connecting to - normally the mock in-process server
	// Can be changed to point to a real server, so tests validate a real deployment.
	grpcUpstreamAddr = "127.0.0.1:15010"
	//grpcUpstreamAddr = "127.0.0.1:14056"
	//grpcUpstreamAddr = "127.0.0.1:32010"

	// Address of the Istiod gRPC service, used in tests.
	istiodSvcAddr = "istiod.istio-system.svc.cluster.local:14056"

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

// Creates an in-process discovery server, using the same code as Istiod, but
// backed by an in-memory config and endpoint store.
func initDS() *xds.Server {
	ds := xds.NewXDS()

	sd := ds.DiscoveryServer.MemRegistry
	sd.AddHTTPService("fortio1.fortio.svc.cluster.local", "10.10.10.1", 8081)
	sd.SetEndpoints("fortio1.fortio.svc.cluster.local", "", []*model.IstioEndpoint{
		{
			Address:         "127.0.0.1",
			EndpointPort:    uint32(14056),
			ServicePortName: "http-main",
		},
	})
	return ds
}


// Test using resolving DNS over GRPC. This uses XDS protocol, and Listener resources
// to represent the names. The protocol is based on GRPC resolution of XDS resources.
func TestADSC(t *testing.T) {
	ds := initDS()

	err := ds.StartGRPC(grpcAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer ds.GRPCListener.Close()

	// Verify we can receive the DNS cluster IPs using XDS
	t.Run("adsc", func(t *testing.T) {
		adscConn, err := adsc.Dial(grpcUpstreamAddr, "", &adsc.Config{
			IP: "1.2.3.4",
			Meta: model.NodeMetadata {
				Generator: "api",
			}.ToStruct(),
		})
		if err != nil {
			t.Fatal("Error connecting ", err)
		}
		store := memory.Make(collections.Pilot)

		configController := memory.NewController(store)
		adscConn.Store = model.MakeIstioStore(configController)

		adscConn.Send(&xdsapi.DiscoveryRequest{
			TypeUrl:       adsc.ListenerType,
		})

		adscConn.WatchConfig()

		data, err := adscConn.WaitVersion(10*time.Second, adsc.ListenerType, "")
		if err != nil {
			t.Fatal("Failed to receive lds", err)
		}

		for _, rs := range data.Resources {
			l := &xdsapi.Listener{}
			err = proto.Unmarshal(rs.Value, l)
			if err != nil {
				t.Fatal("Unmarshall error ", err)
			}

			t.Log("LDS: ", l)
		}
		data, err = adscConn.WaitVersion(10 * time.Second, collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind().String(), "")
		if err != nil {
			t.Fatal("Failed to receive lds", err)
		}

		ses := adscConn.Store.ServiceEntries()
		for _, se := range ses {
			t.Log(se)
		}
		sec, _ := adscConn.Store.List(collections.IstioNetworkingV1Alpha3Envoyfilters.Resource().GroupVersionKind(),"")
		for _, se := range sec {
			t.Log(se)
		}

	})

	t.Run("adsc-gen1", func(t *testing.T) {
		adscConn, err := adsc.Dial(grpcUpstreamAddr, "", &adsc.Config{
			IP: "1.2.3.5",
			Meta: model.NodeMetadata {
			}.ToStruct(),
		})
		if err != nil {
			t.Fatal("Error connecting ", err)
		}

		adscConn.Send(&xdsapi.DiscoveryRequest{
			TypeUrl:       adsc.ClusterType,
		})

		got, err := adscConn.Wait(10*time.Second, "cds")
		if err != nil {
			t.Fatal("Failed to receive lds", err)
		}
		if len(got) == 0 {
			t.Fatal("No LDS response")
		}
		data := adscConn.Received[adsc.ClusterType]
		for _, rs := range data.Resources {
			l := &xdsapi.Cluster{}
			err = proto.Unmarshal(rs.Value, l)
			if err != nil {
				t.Fatal("Unmarshall error ", err)
			}

			t.Log("CDS: ", l)
		}
	})
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
