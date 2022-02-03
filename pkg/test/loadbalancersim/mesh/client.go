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
	"sync"
	"time"

	"istio.io/istio/pkg/test/loadbalancersim/locality"
	"istio.io/istio/pkg/test/loadbalancersim/network"
)

type ClientSettings struct {
	RPS      int
	Locality locality.Instance
}

type Client struct {
	mesh *Instance
	s    ClientSettings
}

func (c *Client) Mesh() *Instance {
	return c.mesh
}

func (c *Client) Locality() locality.Instance {
	return c.s.Locality
}

func (c *Client) SendRequests(conn network.Connection, numRequests int, done func()) {
	go func() {
		wg := sync.WaitGroup{}

		interval := time.Duration((1.0 / float64(c.s.RPS)) * float64(time.Second))

		ticker := time.NewTicker(interval)
		for {
			// Wait for to send the next request.
			<-ticker.C

			// Send a request
			wg.Add(1)
			conn.Request(wg.Done)
			numRequests--

			if numRequests <= 0 {
				ticker.Stop()

				// Wait for all pending requests to complete.
				wg.Wait()
				done()
				return
			}
		}
	}()
}
