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

package metric // import "istio.io/mixer/adapter/stackdriver/metric"

import (
	"context"
	"io"
	"sync"
	"time"

	monitoring "cloud.google.com/go/monitoring/apiv3"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"

	"istio.io/mixer/pkg/adapter"
)

// Abstracts over the specific impl for testing.
type bufferedClient interface {
	io.Closer

	Record([]*monitoringpb.TimeSeries)
}

// A wrapper around the stackdriver client SDK that handles batching data before sending.
// TODO: implement size based batching, today we only send batches on a time.Ticker's tick and we don't watch how much data we're storing.
type buffered struct {
	project     string
	pushMetrics pushFunc
	l           adapter.Logger

	closeMe io.Closer

	// Guards buffer
	m      sync.Mutex
	buffer []*monitoringpb.TimeSeries
}

func (b *buffered) start(env adapter.Env, ticker *time.Ticker) {
	env.ScheduleDaemon(func() {
		for range ticker.C {
			b.Send()
		}
	})
}

func (b *buffered) Record(toSend []*monitoringpb.TimeSeries) {
	b.m.Lock()
	b.buffer = append(b.buffer, toSend...)
	// TODO: gauge metric reporting how many bytes/timeseries we're holding right now
	b.m.Unlock()
}

func (b *buffered) Send() {
	b.m.Lock()
	if len(b.buffer) == 0 {
		b.m.Unlock()
		b.l.Infof("No data to send to Stackdriver.")
		return
	}
	toSend := b.buffer
	b.buffer = make([]*monitoringpb.TimeSeries, 0, len(toSend))
	b.m.Unlock()

	merged := merge(toSend, b.l)
	err := b.pushMetrics(context.Background(),
		&monitoringpb.CreateTimeSeriesRequest{
			Name:       monitoring.MetricProjectPath(b.project),
			TimeSeries: merged,
		})

	// TODO: this is executed in a daemon, so we can't get out info about errors other than logging.
	// We need to build framework level support for these kinds of async tasks. Perhaps a generic batching adapter
	// can handle some of this complexity?
	if err != nil {
		_ = b.l.Errorf("Stackdriver returned: %v\nGiven data: %v", err, merged)
	} else {
		b.l.Infof("Successfully sent data to Stackdriver.")
	}
}

func (b *buffered) Close() error {
	b.l.Infof("Sending last data before shutting down")
	b.Send()
	return b.closeMe.Close()
}
