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
	"math/rand"
	"sync"

	"istio.io/istio/pkg/test/loadbalancersim/network"
)

func NewRoundRobin(conns []*WeightedConnection) network.Connection {
	// Add instances for each connection based on the weight.
	var lbConns []*WeightedConnection
	for _, conn := range conns {
		for i := uint32(0); i < conn.Weight; i++ {
			lbConns = append(lbConns, conn)
		}
	}

	// Shuffle the connections.
	rand.Shuffle(len(lbConns), func(i, j int) {
		lbConns[i], lbConns[j] = lbConns[j], lbConns[i]
	})

	return &roundRobin{
		weightedConnections: newLBConnection("RoundRobinLB", lbConns),
	}
}

type roundRobin struct {
	*weightedConnections

	next      int
	nextMutex sync.Mutex
}

func (lb *roundRobin) Request(onDone func()) {
	// Select the connection to use for this request.
	lb.nextMutex.Lock()
	selected := lb.get(lb.next)
	lb.next = (lb.next + 1) % len(lb.conns)
	lb.nextMutex.Unlock()

	lb.doRequest(selected, onDone)
}
