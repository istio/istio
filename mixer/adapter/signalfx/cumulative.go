// Copyright 2018 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package signalfx

import (
	"sync/atomic"

	"github.com/signalfx/golib/datapoint"
	"github.com/signalfx/golib/sfxclient"
)

// A CumulativeCollector tracks an ever-increasing cumulative counter
type CumulativeCollector struct {
	MetricName string
	Dimensions map[string]string

	count int64
}

var _ sfxclient.Collector = &CumulativeCollector{}

// Add an item to the bucket, later reporting the result in the next report cycle.
func (c *CumulativeCollector) Add(val int64) {
	atomic.AddInt64(&c.count, val)
}

// Datapoints returns the counter datapoint, or nil if there is no set metric name
func (c *CumulativeCollector) Datapoints() []*datapoint.Datapoint {
	if c.MetricName == "" {
		return []*datapoint.Datapoint{}
	}
	return []*datapoint.Datapoint{
		sfxclient.CumulativeP(c.MetricName, c.Dimensions, &c.count),
	}
}
