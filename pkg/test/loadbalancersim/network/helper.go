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
	"sync"
	"time"

	"go.uber.org/atomic"

	"istio.io/istio/pkg/test/loadbalancersim/histogram"
)

type ConnectionHelper struct {
	name      string
	hist      histogram.Instance
	histMutex sync.Mutex
	active    *atomic.Uint64
	total     *atomic.Uint64
}

func NewConnectionHelper(name string) *ConnectionHelper {
	return &ConnectionHelper{
		active: atomic.NewUint64(0),
		total:  atomic.NewUint64(0),
		name:   name,
	}
}

func (c *ConnectionHelper) Name() string {
	return c.name
}

func (c *ConnectionHelper) TotalRequests() uint64 {
	return c.total.Load()
}

func (c *ConnectionHelper) ActiveRequests() uint64 {
	return c.active.Load()
}

func (c *ConnectionHelper) Latency() histogram.Instance {
	c.histMutex.Lock()
	out := make(histogram.Instance, 0, len(c.hist))
	out = append(out, c.hist...)
	c.histMutex.Unlock()

	return out
}

func (c *ConnectionHelper) Request(request func(onDone func()), onDone func()) {
	start := time.Now()
	c.total.Inc()
	c.active.Inc()

	wrappedDone := func() {
		// Calculate the latency for this request.
		latency := time.Since(start)

		// Update the histogram.
		c.histMutex.Lock()
		c.hist = append(c.hist, latency.Seconds())
		c.histMutex.Unlock()

		c.active.Dec()

		// Invoke the caller's handler.
		onDone()
	}

	request(wrappedDone)
}
