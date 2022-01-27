//go:build lbsim
// +build lbsim

//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package loadbalancersim

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"istio.io/istio/pkg/test/loadbalancersim/histogram"
	"istio.io/istio/pkg/test/loadbalancersim/loadbalancer"
	"istio.io/istio/pkg/test/loadbalancersim/locality"
	"istio.io/istio/pkg/test/loadbalancersim/mesh"
	"istio.io/istio/pkg/test/loadbalancersim/network"
)

func TestLoadBalancing(t *testing.T) {
	serviceTime := 20 * time.Millisecond
	numClients := 1
	clientInterval := 10 * time.Millisecond
	clientBurst := 10
	clientDuration := 2 * time.Second
	activeRequestBias := 1.0
	sameZone := locality.Parse("us-east/ny")
	sameRegion := locality.Parse("us-east/boston")
	otherRegion := locality.Parse("asia-east/hongkong")
	priorityWeights := map[uint32]uint32{
		0: 30,
		1: 20,
		2: 1,
	}
	networkLatencies := map[mesh.RouteKey]time.Duration{
		mesh.RouteKey{
			Src:  sameZone,
			Dest: sameZone,
		}: 1 * time.Millisecond,
		mesh.RouteKey{
			Src:  sameZone,
			Dest: sameRegion,
		}: 10 * time.Millisecond,
		mesh.RouteKey{
			Src:  sameZone,
			Dest: otherRegion,
		}: 50 * time.Millisecond,
	}

	networkLatencyCases := []struct {
		enable    bool
		latencies map[mesh.RouteKey]time.Duration
	}{
		{
			enable:    false,
			latencies: make(map[mesh.RouteKey]time.Duration),
		},
		{
			enable:    true,
			latencies: networkLatencies,
		},
	}

	weightCases := []struct {
		enableWeighting       bool
		newWeightedConnection loadbalancer.WeightedConnectionFactory
	}{
		{
			enableWeighting:       false,
			newWeightedConnection: loadbalancer.EquallyWeightedConnectionFactory(),
		},
		{
			enableWeighting:       true,
			newWeightedConnection: loadbalancer.PriorityWeightedConnectionFactory(loadbalancer.LocalityPrioritySelector, priorityWeights),
		},
	}

	algorithmCases := []struct {
		name  string
		newLB func(conns []*loadbalancer.WeightedConnection) network.Connection
	}{
		{
			name:  "round robin",
			newLB: loadbalancer.NewRoundRobin,
		},
		{
			name: "least request",
			newLB: func(conns []*loadbalancer.WeightedConnection) network.Connection {
				return loadbalancer.NewLeastRequest(loadbalancer.LeastRequestSettings{
					Connections:       conns,
					ActiveRequestBias: activeRequestBias,
				})
			},
		},
	}

	topologyCases := []struct {
		name             string
		countSameZone    int
		countSameRegion  int
		countOtherRegion int
	}{
		{
			name:             "all local",
			countSameZone:    6,
			countSameRegion:  0,
			countOtherRegion: 0,
		},
		{
			name:             "even",
			countSameZone:    2,
			countSameRegion:  2,
			countOtherRegion: 2,
		},
		{
			name:             "one remote",
			countSameZone:    4,
			countSameRegion:  1,
			countOtherRegion: 1,
		},
		{
			name:             "one local",
			countSameZone:    1,
			countSameRegion:  3,
			countOtherRegion: 3,
		},
	}

	var sm suiteMetrics
	for _, enableQueueLatency := range []bool{false, true} {
		t.Run("queue latency "+toggleStr(enableQueueLatency), func(t *testing.T) {
			for _, networkLatencyCase := range networkLatencyCases {
				t.Run("network latency "+toggleStr(networkLatencyCase.enable), func(t *testing.T) {
					for _, weightCase := range weightCases {
						weightCase := weightCase
						t.Run("weighting "+toggleStr(weightCase.enableWeighting), func(t *testing.T) {
							for _, algorithmCase := range algorithmCases {
								algorithmCase := algorithmCase
								t.Run(algorithmCase.name, func(t *testing.T) {
									for _, topologyCase := range topologyCases {
										topologyCase := topologyCase
										t.Run(topologyCase.name, func(t *testing.T) {
											m := mesh.New(mesh.Settings{
												NetworkLatencies: networkLatencyCase.latencies,
											})
											defer m.ShutDown()

											// Create the new test output.
											tm := &testMetrics{
												hasNetworkLatency: networkLatencyCase.enable,
												hasQueueLatency:   enableQueueLatency,
												weighted:          weightCase.enableWeighting,
												algorithm:         algorithmCase.name,
												topology:          topologyCase.name,
											}
											sm = append(sm, tm)

											// Create the clients.
											for i := 0; i < numClients; i++ {
												_ = m.NewClient(mesh.ClientSettings{
													Interval: clientInterval,
													Burst:    clientBurst,
													Locality: sameZone,
												})
											}

											// Allocate the nodes in the configured topology.
											m.NewNodes(topologyCase.countSameZone, serviceTime, enableQueueLatency, sameZone)
											m.NewNodes(topologyCase.countSameRegion, serviceTime, enableQueueLatency, sameRegion)
											m.NewNodes(topologyCase.countOtherRegion, serviceTime, enableQueueLatency, otherRegion)

											runTest(t, testSettings{
												mesh:                  m,
												clientDuration:        clientDuration,
												activeRequestBias:     activeRequestBias,
												newWeightedConnection: weightCase.newWeightedConnection,
												newLB:                 algorithmCase.newLB,
											}, tm)
										})
									}
								})
							}
						})
					}
				})
			}
		})
	}

	outputFile := os.Getenv("LB_SIM_OUTPUT_FILE")
	if len(outputFile) == 0 {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		outputFile = fmt.Sprintf("%s/lb_output.csv", homeDir)
	}

	err := os.WriteFile(outputFile, []byte(sm.toCSV()), 0644)
	if err != nil {
		t.Fatal(err)
	}
}

func toggleStrUpper(on bool) string {
	return strings.ToUpper(toggleStr(on))
}

func toggleStr(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

type testSettings struct {
	mesh                  *mesh.Instance
	clientDuration        time.Duration
	newLB                 func(conns []*loadbalancer.WeightedConnection) network.Connection
	newWeightedConnection loadbalancer.WeightedConnectionFactory
	activeRequestBias     float64
}

type testMetrics struct {
	hasQueueLatency     bool
	hasNetworkLatency   bool
	weighted            bool
	algorithm           string
	topology            string
	latencyAvg          float64
	latencyMin          float64
	latencyMax          float64
	nodesSameZone       int
	nodesSameRegion     int
	nodesOtherRegion    int
	requestsSameZone    uint64
	requestsSameRegion  uint64
	requestsOtherRegion uint64
}

func (tm testMetrics) totalRequests() uint64 {
	return tm.requestsSameZone + tm.requestsSameRegion + tm.requestsOtherRegion
}

func (tm testMetrics) sameZonePercent() float64 {
	return (float64(tm.requestsSameZone) / float64(tm.totalRequests())) * 100
}

func (tm testMetrics) sameRegionPercent() float64 {
	return (float64(tm.requestsSameRegion) / float64(tm.totalRequests())) * 100
}

func (tm testMetrics) otherRegionPercent() float64 {
	return (float64(tm.requestsOtherRegion) / float64(tm.totalRequests())) * 100
}

func (tm testMetrics) String() string {
	out := ""
	out += fmt.Sprintf("     Requests: %d\n", tm.totalRequests())
	out += fmt.Sprintf("     Topology: Same Zone=%d, Same Region=%d, Other Region=%d\n", tm.nodesSameZone, tm.nodesSameRegion, tm.nodesOtherRegion)
	out += fmt.Sprintf("Latency (avg): %6.2fs\n", tm.latencyAvg)
	out += fmt.Sprintf("Latency (min): %6.2fs\n", tm.latencyMin)
	out += fmt.Sprintf("Latency (max): %6.2fs\n", tm.latencyMax)
	out += fmt.Sprintf("    Same Zone: %6.2f%%\n", tm.sameZonePercent())
	out += fmt.Sprintf("  Same Region: %6.2f%%\n", tm.sameRegionPercent())
	out += fmt.Sprintf(" Other Region: %6.2f%%\n", tm.otherRegionPercent())
	return out
}

func (tm testMetrics) toCSV() string {
	return fmt.Sprintf("%s,%s,%s,%s,%s,%f,%f,%f,%f,%f,%f", tm.topology,
		toggleStrUpper(tm.weighted), toggleStrUpper(tm.hasNetworkLatency), toggleStrUpper(tm.hasQueueLatency),
		tm.algorithm, tm.latencyMin, tm.latencyAvg, tm.latencyMax, tm.sameZonePercent(), tm.sameRegionPercent(), tm.otherRegionPercent())
}

type suiteMetrics []*testMetrics

func cmpBool(b1, b2 bool) int {
	if b1 == b2 {
		return 0
	}
	if !b1 {
		return -1
	}
	return 1
}

func (sm suiteMetrics) toCSV() string {
	sort.SliceStable(sm, func(i, j int) bool {
		a := sm[i]
		b := sm[j]

		if cmp := cmpBool(a.hasQueueLatency, b.hasQueueLatency); cmp != 0 {
			return cmp < 0
		}

		if cmp := cmpBool(a.hasNetworkLatency, b.hasNetworkLatency); cmp != 0 {
			return cmp < 0
		}

		if cmp := strings.Compare(a.topology, b.topology); cmp != 0 {
			return cmp < 0
		}

		// Sort algorithm in descending order so "round robin" is first
		return strings.Compare(a.algorithm, b.algorithm) > 0
	})
	out := "TOPOLOGY,WEIGHTING,NW LATENCY,Q LATENCY,ALG,LATENCY (MIN),LATENCY (AVG),LATENCY (MAX),IN-ZONE,IN-REGION,OUT-REGION\n"
	for _, tm := range sm {
		out += tm.toCSV() + "\n"
	}
	return out
}

func runTest(t *testing.T, s testSettings, tm *testMetrics) {
	t.Helper()

	wg := sync.WaitGroup{}

	clientLatencies := make([]histogram.Instance, len(s.mesh.Clients()))
	for i, client := range s.mesh.Clients() {
		i := i
		client := client
		wg.Add(1)
		go func() {
			// Assign weights to the endpoints.
			var conns []*loadbalancer.WeightedConnection
			for _, n := range s.mesh.Nodes() {
				conns = append(conns, s.newWeightedConnection(client, n))
			}

			// Create a load balancer
			lb := s.newLB(conns)

			// Send the requests.
			client.SendRequests(lb, s.clientDuration, func() {
				clientLatencies[i] = lb.Latency()
				wg.Done()
			})
		}()
	}

	wg.Wait()

	c := s.mesh.Clients()[0]
	clientLocality := c.Locality()
	clientLatency := clientLatencies[0]

	nodesSameZone := s.mesh.Nodes().Select(locality.MatchZone(clientLocality))
	nodesSameRegion := s.mesh.Nodes().Select(locality.MatchOtherZoneInSameRegion(clientLocality))
	nodesOtherRegion := s.mesh.Nodes().Select(locality.Not(locality.MatchRegion(clientLocality)))

	// Store in the output.
	tm.latencyAvg = clientLatency.Mean()
	tm.latencyMin = clientLatency.Min()
	tm.latencyMax = clientLatency.Max()
	tm.nodesSameZone = len(nodesSameZone)
	tm.nodesSameRegion = len(nodesSameRegion)
	tm.nodesOtherRegion = len(nodesOtherRegion)
	tm.requestsSameZone = nodesSameZone.TotalRequests()
	tm.requestsSameRegion = nodesSameRegion.TotalRequests()
	tm.requestsOtherRegion = nodesOtherRegion.TotalRequests()

	t.Log("Test Results:\n" + tm.String())
}
