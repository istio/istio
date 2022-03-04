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

	"istio.io/istio/pkg/test/loadbalancersim/locality"
	"istio.io/istio/pkg/test/loadbalancersim/network"
	"istio.io/istio/pkg/test/loadbalancersim/timer"
	"istio.io/istio/pkg/test/loadbalancersim/timeseries"
)

var _ network.Connection = &Node{}

const maxQLatency = 30 * time.Second

type Node struct {
	locality        locality.Instance
	helper          *network.ConnectionHelper
	q               *timer.Queue
	serviceTime     time.Duration
	qLatencyEnabled bool
	qLength         timeseries.Instance
	qLatency        timeseries.Instance
}

func newNode(name string, serviceTime time.Duration, enableQueueLatency bool, l locality.Instance) *Node {
	return &Node{
		locality:        l,
		helper:          network.NewConnectionHelper(name),
		q:               timer.NewQueue(),
		serviceTime:     serviceTime,
		qLatencyEnabled: enableQueueLatency,
	}
}

func (n *Node) Name() string {
	return n.helper.Name()
}

func (n *Node) QueueLength() *timeseries.Instance {
	return &n.qLength
}

func (n *Node) QueueLatency() *timeseries.Instance {
	return &n.qLatency
}

func (n *Node) calcRequestDuration() time.Duration {
	// Get the current queue length.
	qLen := n.q.Len()
	qLatency := n.calcQLatency(qLen)

	// Add the observations
	tnow := time.Now()
	n.qLength.AddObservation(float64(qLen), tnow)
	n.qLatency.AddObservation(qLatency.Seconds(), tnow)

	return n.serviceTime + qLatency
}

func (n *Node) calcQLatency(qlen int) time.Duration {
	if !n.qLatencyEnabled {
		return 0
	}

	// Compute the queue latency in milliseconds.
	latency := math.Pow(1.2, float64(qlen+1))

	// Clip the latency at the maximum value.
	clippedLatency := math.Min(latency, float64(maxQLatency.Milliseconds()))

	// Return the latency as milliseconds.
	return time.Duration(clippedLatency) * time.Millisecond
}

func (n *Node) TotalRequests() uint64 {
	return n.helper.TotalRequests()
}

func (n *Node) ActiveRequests() uint64 {
	return n.helper.ActiveRequests()
}

func (n *Node) Latency() *timeseries.Instance {
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

func (nodes Nodes) Latency() *timeseries.Instance {
	var out timeseries.Instance
	for _, n := range nodes {
		out.AddAll(n.Latency())
	}
	return &out
}

func (nodes Nodes) QueueLatency() *timeseries.Instance {
	var out timeseries.Instance
	for _, n := range nodes {
		out.AddAll(n.QueueLatency())
	}
	return &out
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
