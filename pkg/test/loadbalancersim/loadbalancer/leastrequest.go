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

package loadbalancer

import (
	"math"
	"math/rand"
	"sync"
	"time"

	"istio.io/istio/pkg/test/loadbalancersim/network"
)

type LeastRequestSettings struct {
	Connections       []*WeightedConnection
	ActiveRequestBias float64
}

func NewLeastRequest(s LeastRequestSettings) network.Connection {
	if len(s.Connections) == 0 {
		panic("attempting to create load balancer with zero connections")
	}

	conn := newLBConnection("LeastRequestLB", s.Connections)

	if conn.AllWeightsEqual() {
		return newUnweightedLeastRequest(conn)
	}

	return newWeightedLeastRequest(conn, s.ActiveRequestBias)
}

type unweightedLeastRequest struct {
	*weightedConnections
	r *rand.Rand
}

func newUnweightedLeastRequest(conn *weightedConnections) network.Connection {
	return &unweightedLeastRequest{
		weightedConnections: conn,
		r:                   rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (lb *unweightedLeastRequest) pick2() (*WeightedConnection, *WeightedConnection) {
	numConnections := len(lb.conns)
	index1 := lb.r.Intn(numConnections)
	index2 := lb.r.Intn(numConnections)
	if index2 == index1 {
		index2 = (index2 + 1) % numConnections
	}

	return lb.get(index1), lb.get(index2)
}

func (lb *unweightedLeastRequest) Request(onDone func()) {
	if len(lb.conns) == 1 {
		lb.doRequest(lb.get(0), onDone)
		return
	}

	// Pick 2 endpoints at random.
	c1, c2 := lb.pick2()

	// Choose the endpoint with fewer active requests.
	selected := c1
	if c2.ActiveRequests() < c1.ActiveRequests() {
		selected = c2
	}

	// Apply the selected endpoint to the metrics decorator and send the request.
	lb.doRequest(selected, onDone)
}

type weightedLeastRequest struct {
	*weightedConnections
	activeRequestBias float64
	edf               *EDF
	edfMutex          sync.Mutex
}

func newWeightedLeastRequest(conn *weightedConnections, activeRequestBias float64) network.Connection {
	lb := &weightedLeastRequest{
		weightedConnections: conn,
		activeRequestBias:   activeRequestBias,
		edf:                 NewEDF(),
	}

	// Add all endpoints to the EDF scheduler.
	for _, c := range conn.conns {
		lb.edf.Add(lb.calcEDFWeight(0, c), c)
	}

	return lb
}

func (lb *weightedLeastRequest) Request(onDone func()) {
	// Pick the next endpoint and re-add it with the updated weight.
	lb.edfMutex.Lock()
	selected := lb.edf.PickAndAdd(lb.calcEDFWeight).(*WeightedConnection)
	lb.edfMutex.Unlock()

	// Make the request.
	lb.doRequest(selected, onDone)
}

func (lb *weightedLeastRequest) calcEDFWeight(_ float64, value any) float64 {
	conn := value.(*WeightedConnection)

	weight := float64(conn.Weight)
	if lb.activeRequestBias >= 1.0 {
		weight /= float64(conn.ActiveRequests() + 1)
	} else if lb.activeRequestBias > 0.0 {
		weight /= math.Pow(float64(conn.ActiveRequests()+1), lb.activeRequestBias)
	}
	return weight
}
