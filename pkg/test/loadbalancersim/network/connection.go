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

package network

import (
	"istio.io/istio/pkg/test/loadbalancersim/timeseries"
)

type Connection interface {
	Name() string
	Request(onDone func())
	TotalRequests() uint64
	ActiveRequests() uint64
	Latency() *timeseries.Instance
}

func NewConnection(name string, request func(onDone func())) Connection {
	return &connection{
		request: request,
		helper:  NewConnectionHelper(name),
	}
}

type connection struct {
	request func(onDone func())
	helper  *ConnectionHelper
}

func (c *connection) Name() string {
	return c.helper.Name()
}

func (c *connection) TotalRequests() uint64 {
	return c.helper.TotalRequests()
}

func (c *connection) ActiveRequests() uint64 {
	return c.helper.ActiveRequests()
}

func (c *connection) Latency() *timeseries.Instance {
	return c.helper.Latency()
}

func (c *connection) Request(onDone func()) {
	c.helper.Request(c.request, onDone)
}
