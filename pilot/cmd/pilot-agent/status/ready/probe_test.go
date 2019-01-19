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

	. "github.com/onsi/gomega"
)

var probe = Probe{AdminPort: 1234}

func TestEnvoyStatsCompleteAndSuccessful(t *testing.T) {
	g := NewGomegaWithT(t)
	stats := "cluster_manager.cds.update_success: 1\nlistener_manager.lds.update_success: 1"

	server := createAndStartServer(stats)
	defer server.Close()

	err := probe.Check()

	g.Expect(err).NotTo(HaveOccurred())
}

func TestEnvoyStatsIncompleteCDS(t *testing.T) {
	g := NewGomegaWithT(t)
	stats := "listener_manager.lds.update_success: 1"

	server := createAndStartServer(stats)
	defer server.Close()

	err := probe.Check()

	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("cds updates: 0"))
}

func TestEnvoyStatsIncompleteLDS(t *testing.T) {
	g := NewGomegaWithT(t)
	stats := "cluster_manager.cds.update_success: 1"

	server := createAndStartServer(stats)
	defer server.Close()

	err := probe.Check()

	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("lds updates: 0"))
}

func TestEnvoyStatsCompleteAndRejectedCDS(t *testing.T) {
	g := NewGomegaWithT(t)
	stats := "cluster_manager.cds.update_rejected: 1\nlistener_manager.lds.update_success: 1"

	server := createAndStartServer(stats)
	defer server.Close()

	err := probe.Check()

	g.Expect(err).NotTo(HaveOccurred())
}

func TestEnvoyStatsCompleteAndRejectedLDS(t *testing.T) {
	g := NewGomegaWithT(t)
	stats := "cluster_manager.cds.update_success: 1\nlistener_manager.lds.update_rejected: 1"

	server := createAndStartServer(stats)
	defer server.Close()

	err := probe.Check()

	g.Expect(err).NotTo(HaveOccurred())
}

func TestEnvoyCheckFailsIfStatsUnparsableNoSeparator(t *testing.T) {
	g := NewGomegaWithT(t)
	stats := "cluster_manager.cds.update_success; 1\nlistener_manager.lds.update_success: 1"

	server := createAndStartServer(stats)
	defer server.Close()

	err := probe.Check()

	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("missing separator"))
}

func TestEnvoyCheckFailsIfStatsUnparsableNoNumber(t *testing.T) {
	g := NewGomegaWithT(t)
	stats := "cluster_manager.cds.update_success: a\nlistener_manager.lds.update_success: 1"

	server := createAndStartServer(stats)
	defer server.Close()

	err := probe.Check()

	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("failed parsing Envoy stat"))
}

func createAndStartServer(statsToReturn string) *httptest.Server {
	// Start a local HTTP server
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		// Send response to be tested
		rw.Write([]byte(statsToReturn))
	}))
	l, err := net.Listen("tcp", "127.0.0.1:1234")
	if err != nil {
		panic("Could not create listener for test: " + err.Error())
	}
	server.Listener = l
	server.Start()
	return server
}
