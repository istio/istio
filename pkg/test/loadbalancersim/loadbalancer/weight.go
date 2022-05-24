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
	mesh2 "istio.io/istio/pkg/test/loadbalancersim/mesh"
	network2 "istio.io/istio/pkg/test/loadbalancersim/network"
	"istio.io/istio/pkg/test/loadbalancersim/timeseries"
)

type WeightedConnection struct {
	network2.Connection
	Weight uint32
}

type weightedConnections struct {
	conns  []*WeightedConnection
	helper *network2.ConnectionHelper
}

func newLBConnection(name string, conns []*WeightedConnection) *weightedConnections {
	return &weightedConnections{
		conns:  conns,
		helper: network2.NewConnectionHelper(name),
	}
}

func (lb *weightedConnections) AllWeightsEqual() bool {
	if len(lb.conns) == 0 {
		return true
	}

	weight := lb.conns[0].Weight
	for _, conn := range lb.conns {
		if conn.Weight != weight {
			return false
		}
	}
	return true
}

func (lb *weightedConnections) get(index int) *WeightedConnection {
	return lb.conns[index]
}

func (lb *weightedConnections) doRequest(c *WeightedConnection, onDone func()) {
	lb.helper.Request(c.Request, onDone)
}

func (lb *weightedConnections) Name() string {
	return lb.helper.Name()
}

func (lb *weightedConnections) TotalRequests() uint64 {
	return lb.helper.TotalRequests()
}

func (lb *weightedConnections) ActiveRequests() uint64 {
	return lb.helper.ActiveRequests()
}

func (lb *weightedConnections) Latency() *timeseries.Instance {
	return lb.helper.Latency()
}

type WeightedConnectionFactory func(src *mesh2.Client, n *mesh2.Node) *WeightedConnection

func EquallyWeightedConnectionFactory() WeightedConnectionFactory {
	return func(src *mesh2.Client, dest *mesh2.Node) *WeightedConnection {
		return &WeightedConnection{
			Connection: src.Mesh().NewConnection(src, dest),
			Weight:     1,
		}
	}
}

func PriorityWeightedConnectionFactory(selectPriority PrioritySelector, priorityWeightMap map[uint32]uint32) WeightedConnectionFactory {
	return func(src *mesh2.Client, dest *mesh2.Node) *WeightedConnection {
		// Select the priority for this node.
		priority := selectPriority(src, dest)

		// Get the weight for the priority.
		weight := uint32(1)
		if priorityWeightMap != nil {
			if w := priorityWeightMap[priority]; w > 0 {
				weight = w
			}
		}

		return &WeightedConnection{
			Connection: src.Mesh().NewConnection(src, dest),
			Weight:     weight,
		}
	}
}
