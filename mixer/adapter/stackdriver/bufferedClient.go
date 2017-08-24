// Copyright 2017 Istio Authors.
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

package stackdriver // import "istio.io/mixer/adapter/stackdriver"

import (
	"context"
	"sync"
	"time"

	monitoring "cloud.google.com/go/monitoring/apiv3"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"

	"istio.io/mixer/pkg/adapter"
)

// Abstracts over the specific impl for testing.
type bufferedClient interface {
	Record([]*monitoringpb.TimeSeries)
}

// A wrapper around the stackdriver client SDK that handles batching data before sending.
// TODO: implement size based batching, today we only send batches on a time.Ticker's tick and we don't watch how much data we're storing.
type buffered struct {
	project     string
	pushMetrics pushFunc
	l           adapter.Logger

	// Guards buffer
	m      sync.Mutex
	buffer []*monitoringpb.TimeSeries
}

func (c *buffered) start(env adapter.Env, ticker *time.Ticker) {
	env.ScheduleDaemon(func() {
		for range ticker.C {
			c.Send()
		}
	})
}

func (c *buffered) Record(toSend []*monitoringpb.TimeSeries) {
	c.m.Lock()
	c.buffer = append(c.buffer, toSend...)
	// TODO: gauge metric reporting how many bytes/timeseries we're holding right now
	c.m.Unlock()
}

func (c *buffered) Send() {
	c.m.Lock()
	if len(c.buffer) == 0 {
		c.m.Unlock()
		c.l.Infof("No data to send to Stackdriver.")
		return
	}
	toSend := c.buffer
	c.buffer = make([]*monitoringpb.TimeSeries, 0, len(toSend))
	c.m.Unlock()

	merged := merge(toSend, c.l)
	err := c.pushMetrics(context.Background(),
		&monitoringpb.CreateTimeSeriesRequest{
			Name:       monitoring.MetricProjectPath(c.project),
			TimeSeries: merged,
		})

	// TODO: this is executed in a daemon, so we can't get out info about errors other than logging.
	// We need to build framework level support for these kinds of async tasks. Perhaps a generic batching adapter
	// can handle some of this complexity?
	if err != nil {
		_ = c.l.Errorf("Stackdriver returned: %v\nGiven data: %v", err, merged)
	} else {
		c.l.Infof("Successfully sent data to Stackdriver.")
	}
}
