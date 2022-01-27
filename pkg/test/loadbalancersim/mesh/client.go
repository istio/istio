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
	Burst    int
	Interval time.Duration
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

func (c *Client) SendRequests(conn network.Connection, duration time.Duration, done func()) {
	go func() {
		wg := sync.WaitGroup{}

		timer := time.NewTimer(duration)
		ticker := time.NewTicker(c.s.Interval)
		for {
			select {
			case <-timer.C:
				timer.Stop()
				ticker.Stop()

				// Wait for all pending requests to complete.
				wg.Wait()
				done()
				return
			case <-ticker.C:
				wg.Add(c.s.Burst)
				for i := 0; i < c.s.Burst; i++ {
					conn.Request(wg.Done)
				}
			}
		}
	}()
}
