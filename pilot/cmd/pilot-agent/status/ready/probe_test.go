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

package ready

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	. "github.com/onsi/gomega"
)

var (
	liveServerStats = "cluster_manager.cds.update_success: 1\nlistener_manager.lds.update_success: 1\nserver.state: 0\nlistener_manager.workers_started: 1"
	onlyServerStats = "server.state: 0"
	initServerStats = "cluster_manager.cds.update_success: 1\nlistener_manager.lds.update_success: 1\nserver.state: 2"
	noServerStats   = ""
)

func TestEnvoyStatsCompleteAndSuccessful(t *testing.T) {
	g := NewGomegaWithT(t)

	server := createAndStartServer(liveServerStats)
	defer server.Close()
	probe := Probe{AdminPort: 1234}

	err := probe.Check()

	g.Expect(err).NotTo(HaveOccurred())
}

func TestEnvoyStats(t *testing.T) {
	prefix := "config not received from Pilot (is Pilot running?): "
	cases := []struct {
		name   string
		stats  string
		result string
	}{
		{
			"only lds",
			"listener_manager.lds.update_success: 1",
			prefix + "cds updates: 0 successful, 0 rejected; lds updates: 1 successful, 0 rejected",
		},
		{
			"only cds",
			"cluster_manager.cds.update_success: 1",
			prefix + "cds updates: 1 successful, 0 rejected; lds updates: 0 successful, 0 rejected",
		},
		{
			"reject CDS",
			`cluster_manager.cds.update_rejected: 1
listener_manager.lds.update_success: 1`,
			prefix + "cds updates: 0 successful, 1 rejected; lds updates: 1 successful, 0 rejected",
		},
		{
			"workers not started",
			`
cluster_manager.cds.update_success: 1
listener_manager.lds.update_success: 1
listener_manager.workers_started: 0
server.state: 0`,
			"workers have not yet started",
		},
		{
			"full",
			`
cluster_manager.cds.update_success: 1
listener_manager.lds.update_success: 1
listener_manager.workers_started: 1
server.state: 0`,
			"",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			server := createAndStartServer(tt.stats)
			defer server.Close()
			probe := Probe{AdminPort: 1234}

			err := probe.Check()

			// Expect no error
			if tt.result == "" {
				if err != nil {
					t.Fatalf("Expected no error, got: %v", err)
				}
				return
			}
			// Expect error
			if err.Error() != tt.result {
				t.Fatalf("Expected: \n'%v', got: \n'%v'", tt.result, err.Error())
			}
		})
	}
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

func TestEnvoyReadinessCache(t *testing.T) {
	g := NewWithT(t)

	server := createAndStartServer(noServerStats)
	probe := Probe{AdminPort: 1234}
	err := probe.Check()
	g.Expect(err).To(HaveOccurred())
	g.Expect(probe.atleastOnceReady).Should(BeFalse())
	err = probe.Check()
	g.Expect(err).To(HaveOccurred())
	g.Expect(probe.atleastOnceReady).Should(BeFalse())
	server.Close()

	server = createAndStartServer(liveServerStats)
	err = probe.Check()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(probe.atleastOnceReady).Should(BeTrue())
	server.Close()

	server = createAndStartServer(noServerStats)
	err = probe.Check()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(probe.atleastOnceReady).Should(BeTrue())
	server.Close()

	err = probe.Check()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(probe.atleastOnceReady).Should(BeTrue())
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
