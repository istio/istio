// Copyright 2018 Istio Authors
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

package ready

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	admin "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"
	envoyapicore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/gogo/protobuf/jsonpb"
	. "github.com/onsi/gomega"

	"istio.io/istio/pilot/pkg/model"
	networking "istio.io/istio/pilot/pkg/networking/core/v1alpha3"
)

var (
	liveServerStats = "cluster_manager.cds.update_success: 1\nlistener_manager.lds.update_success: 1\nserver.state: 0"
	onlyServerStats = "server.state: 0"
	initServerStats = "cluster_manager.cds.update_success: 1\nlistener_manager.lds.update_success: 1\nserver.state: 2"
	noServerStats   = ""
	listeners       = admin.Listeners{
		ListenerStatuses: []*admin.ListenerStatus{
			{
				Name: networking.VirtualInboundListenerName,
				LocalAddress: &envoyapicore.Address{
					Address: &envoyapicore.Address_SocketAddress{
						SocketAddress: &envoyapicore.SocketAddress{
							PortSpecifier: &envoyapicore.SocketAddress_PortValue{
								PortValue: 15006,
							},
						},
					},
				},
			},
		},
	}
)

func TestEnvoyStatsCompleteAndSuccessful(t *testing.T) {
	g := NewGomegaWithT(t)

	server := createAndStartServer(liveServerStats)
	defer server.Close()
	probe := Probe{AdminPort: 1234}

	err := probe.Check()

	g.Expect(err).NotTo(HaveOccurred())
}

func TestEnvoyStatsIncompleteCDS(t *testing.T) {
	g := NewGomegaWithT(t)
	stats := "listener_manager.lds.update_success: 1\nserver.state: 0"

	server := createAndStartServer(stats)
	defer server.Close()
	probe := Probe{AdminPort: 1234}

	err := probe.Check()

	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("cds updates: 0 successful, 0 rejected; lds updates: 1 successful, 0 rejected"))
}

func TestEnvoyStatsIncompleteLDS(t *testing.T) {
	g := NewGomegaWithT(t)
	stats := "cluster_manager.cds.update_success: 1\nserver.state: 0"

	server := createAndStartServer(stats)
	defer server.Close()
	probe := Probe{AdminPort: 1234}

	err := probe.Check()

	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("cds updates: 0 successful, 1 rejected; lds updates: 0 successful, 0 rejected"))
}

func TestEnvoyStatsCompleteAndRejectedCDS(t *testing.T) {
	g := NewGomegaWithT(t)
	stats := "cluster_manager.cds.update_rejected: 1\nlistener_manager.lds.update_success: 1\nserver.state: 0"

	server := createAndStartServer(stats)
	defer server.Close()
	probe := Probe{AdminPort: 1234}

	err := probe.Check()

	g.Expect(err).NotTo(HaveOccurred())
}

func TestEnvoyCheckFailsIfStatsUnparsableNoSeparator(t *testing.T) {
	g := NewGomegaWithT(t)
	stats := "cluster_manager.cds.update_rejected; 1\nlistener_manager.lds.update_success: 1\nserver.state: 0"

	server := createAndStartServer(stats)
	defer server.Close()
	probe := Probe{AdminPort: 1234}

	err := probe.Check()

	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("missing separator"))
}

func TestEnvoyCheckFailsIfStatsUnparsableNoNumber(t *testing.T) {
	g := NewGomegaWithT(t)
	stats := "cluster_manager.cds.update_rejected: a\nlistener_manager.lds.update_success: 1\nserver.state: 0"

	server := createAndStartServer(stats)
	defer server.Close()
	probe := Probe{AdminPort: 1234}

	err := probe.Check()

	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("failed parsing Envoy stat"))
}

func TestEnvoyInitializing(t *testing.T) {
	g := NewGomegaWithT(t)

	server := createAndStartServer(initServerStats)
	defer server.Close()
	probe := Probe{AdminPort: 1234}

	err := probe.Check()

	g.Expect(err).To(HaveOccurred())
}

func TestEnvoyNoClusterManagerStats(t *testing.T) {
	g := NewGomegaWithT(t)

	server := createAndStartServer(onlyServerStats)
	defer server.Close()
	probe := Probe{AdminPort: 1234}

	err := probe.Check()

	g.Expect(err).To(HaveOccurred())
}

func TestEnvoyNoServerStats(t *testing.T) {
	g := NewGomegaWithT(t)

	server := createAndStartServer(noServerStats)
	defer server.Close()
	probe := Probe{AdminPort: 1234}

	err := probe.Check()

	g.Expect(err).To(HaveOccurred())
}

func TestEnvoyInitializingWithVirtualInboundListener(t *testing.T) {
	g := NewGomegaWithT(t)

	funcMap := createDefaultFuncMap(liveServerStats)

	funcMap["/listeners"] = func(rw http.ResponseWriter, _ *http.Request) {
		jsonm := &jsonpb.Marshaler{Indent: "  "}
		listenerBytes, _ := jsonm.MarshalToString(&listeners)

		// Send response to be tested
		rw.Write([]byte(listenerBytes))
	}

	server := createHTTPServer(funcMap)
	defer server.Close()
	probe := Probe{AdminPort: 1234, receivedFirstUpdate: true, NodeType: model.SidecarProxy}

	err := probe.Check()

	g.Expect(err).ToNot(HaveOccurred())
}

func createDefaultFuncMap(statsToReturn string) map[string]func(rw http.ResponseWriter, _ *http.Request) {
	return map[string]func(rw http.ResponseWriter, _ *http.Request){

		"/stats": func(rw http.ResponseWriter, _ *http.Request) {
			// Send response to be tested
			rw.Write([]byte(statsToReturn))
		},
	}
}

func createAndStartServer(statsToReturn string) *httptest.Server {
	return createHTTPServer(createDefaultFuncMap(statsToReturn))
}

func createHTTPServer(handlers map[string]func(rw http.ResponseWriter, _ *http.Request)) *httptest.Server {
	mux := http.NewServeMux()
	for k, v := range handlers {
		mux.HandleFunc(k, http.HandlerFunc(v))
	}

	// Start a local HTTP server
	server := httptest.NewUnstartedServer(mux)

	l, err := net.Listen("tcp", "127.0.0.1:1234")
	if err != nil {
		panic("Could not create listener for test: " + err.Error())
	}
	server.Listener = l
	server.Start()
	return server
}
