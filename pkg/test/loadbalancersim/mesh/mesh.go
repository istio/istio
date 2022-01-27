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
	"fmt"
	"time"

	"istio.io/istio/pkg/test/loadbalancersim/locality"
	"istio.io/istio/pkg/test/loadbalancersim/network"
	"istio.io/istio/pkg/test/loadbalancersim/timer"
)

type RouteKey struct {
	Src  locality.Instance
	Dest locality.Instance
}

type Settings struct {
	NetworkLatencies map[RouteKey]time.Duration
}

type Instance struct {
	nodes    Nodes
	clients  []*Client
	s        Settings
	networkQ *timer.Queue
}

func New(s Settings) *Instance {
	return &Instance{
		s:        s,
		networkQ: timer.NewQueue(),
	}
}

func (m *Instance) Nodes() Nodes {
	return m.nodes
}

func (m *Instance) Clients() []*Client {
	return m.clients
}

func (m *Instance) NewConnection(src *Client, dest *Node) network.Connection {
	// Lookup the route between the source and destination
	networkLatency := m.s.NetworkLatencies[RouteKey{
		Src:  src.Locality(),
		Dest: dest.Locality(),
	}]

	request := dest.Request
	if networkLatency > time.Duration(0) {
		request = func(onDone func()) {
			m.networkQ.Schedule(func() {
				dest.Request(onDone)
			}, time.Now().Add(networkLatency))
		}
	}

	return network.NewConnection(dest.Name(), request)
}

func (m *Instance) ShutDown() {
	m.networkQ.ShutDown()
	m.nodes.ShutDown()
}

func (m *Instance) NewNodes(count int, serviceTime time.Duration, enableQueueLatency bool, locality locality.Instance) Nodes {
	out := make(Nodes, 0, count)
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("%s_%d", locality, i)
		out = append(out, newNode(name, serviceTime, enableQueueLatency, locality))
	}

	m.nodes = append(m.nodes, out...)

	return out
}

func (m *Instance) NewClient(s ClientSettings) *Client {
	c := &Client{
		mesh: m,
		s:    s,
	}

	m.clients = append(m.clients, c)
	return c
}
