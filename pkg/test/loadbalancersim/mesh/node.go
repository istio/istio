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

package mesh

import (
	"math"
	"time"

	"istio.io/istio/pkg/test/loadbalancersim/histogram"
	"istio.io/istio/pkg/test/loadbalancersim/locality"
	"istio.io/istio/pkg/test/loadbalancersim/network"
	"istio.io/istio/pkg/test/loadbalancersim/timer"
)

var _ network.Connection = &Node{}

type Node struct {
	locality            locality.Instance
	helper              *network.ConnectionHelper
	q                   *timer.Queue
	calcRequestDuration func() time.Duration
}

func newNode(name string, serviceTime time.Duration, enableQueueLatency bool, l locality.Instance) *Node {
	q := timer.NewQueue()
	var calcRequestDuration func() time.Duration
	if enableQueueLatency {
		calcRequestDuration = func() time.Duration {
			return serviceTime + time.Duration(math.Pow(float64(q.Len())/2.0, 2.0))*time.Millisecond
		}
	} else {
		calcRequestDuration = func() time.Duration {
			return serviceTime
		}
	}

	n := &Node{
		locality:            l,
		calcRequestDuration: calcRequestDuration,
		helper:              network.NewConnectionHelper(name),
		q:                   q,
	}

	return n
}

func (n *Node) Name() string {
	return n.helper.Name()
}

func (n *Node) TotalRequests() uint64 {
	return n.helper.TotalRequests()
}

func (n *Node) ActiveRequests() uint64 {
	return n.helper.ActiveRequests()
}

func (n *Node) Latency() histogram.Instance {
	return n.helper.Latency()
}

func (n *Node) Request(onDone func()) {
	n.helper.Request(func(wrappedOnDone func()) {
		deadline := time.Now().Add(n.calcRequestDuration())

		// Schedule the done function to be called after the deadline.
		n.q.Schedule(wrappedOnDone, deadline)
	}, onDone)
}

func (n *Node) Locality() locality.Instance {
	return n.locality
}

func (n *Node) ShutDown() {
	n.q.ShutDown()
}

type Nodes []*Node

func (nodes Nodes) Select(match locality.Match) Nodes {
	var out Nodes
	for _, n := range nodes {
		if match(n.locality) {
			out = append(out, n)
		}
	}
	return out
}

func (nodes Nodes) TotalRequests() uint64 {
	var out uint64
	for _, n := range nodes {
		out += n.TotalRequests()
	}
	return out
}

func (nodes Nodes) ShutDown() {
	for _, n := range nodes {
		n.ShutDown()
	}
}
