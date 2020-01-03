// Copyright 2017 Istio Authors
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

package metric

import (
	"context"
	"io"
	"strings"
	"sync"
	"time"

	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"

	"google.golang.org/grpc/status"

	"istio.io/istio/mixer/pkg/adapter"
)

// Abstracts over the specific impl for testing.
type bufferedClient interface {
	io.Closer

	Record([]*monitoringpb.TimeSeries)
}

// A wrapper around the stackdriver client SDK that handles batching data before sending.
type buffered struct {
	project     string
	pushMetrics pushFunc
	l           adapter.Logger

	closeMe io.Closer

	// Guards buffer
	m      sync.Mutex
	buffer []*monitoringpb.TimeSeries

	// Guards merged time series
	tsm          sync.Mutex
	mergeTrigger int
	mergedTS     map[uint64]*monitoringpb.TimeSeries

	timeSeriesBatchSize int
	pushInterval        time.Duration

	env adapter.Env
}

// batchTimeSeries slices the given time series array with respect to the given batch size limit.
func batchTimeSeries(series []*monitoringpb.TimeSeries, tsLimit int) [][]*monitoringpb.TimeSeries {
	var batches [][]*monitoringpb.TimeSeries
	for i := 0; i < len(series); i += tsLimit {
		e := i + tsLimit
		if e > len(series) {
			e = len(series)
		}
		batches = append(batches, series[i:e])
	}
	return batches
}

func (b *buffered) start(env adapter.Env, ticker *time.Ticker, quit chan struct{}) {
	env.ScheduleDaemon(func() {
		for {
			select {
			case <-ticker.C:
				b.mergeTimeSeries()
				b.Send()
			case <-quit:
				return
			}
		}
	})
}

func (b *buffered) Record(toSend []*monitoringpb.TimeSeries) {
	b.m.Lock()
	b.buffer = append(b.buffer, toSend...)
	if len(b.buffer) >= b.mergeTrigger {
		// pull the trigger to merge buffered time series
		b.env.ScheduleWork(func() {
			b.mergeTimeSeries()
		})
	}
	// TODO: gauge metric reporting how many bytes/timeseries we're holding right now
	b.m.Unlock()
}

func (b *buffered) mergeTimeSeries() {
	b.m.Lock()
	temp := b.buffer
	b.buffer = make([]*monitoringpb.TimeSeries, 0, len(temp))
	b.m.Unlock()

	b.tsm.Lock()
	for _, ts := range temp {
		k := toKey(ts.Metric, ts.Resource)
		if p, ok := b.mergedTS[k]; !ok {
			b.mergedTS[k] = ts
		} else {
			if n, err := mergePoints(p, ts); err != nil {
				b.l.Warningf("failed to merge time series %v and %v: %v", p, ts, err)
			} else {
				b.mergedTS[k] = n
			}
		}
	}
	b.tsm.Unlock()
}

func (b *buffered) Send() {
	b.tsm.Lock()
	if len(b.mergedTS) == 0 {
		b.tsm.Unlock()
		b.l.Debugf("No data to send to Stackdriver.")
		return
	}

	tmpBuffer := b.mergedTS
	b.mergedTS = make(map[uint64]*monitoringpb.TimeSeries)
	b.tsm.Unlock()

	toSend := make([]*monitoringpb.TimeSeries, 0, len(tmpBuffer))
	for _, v := range tmpBuffer {
		toSend = append(toSend, v)
	}
	batches := batchTimeSeries(toSend, b.timeSeriesBatchSize)
	// Spread monitoring API calls evenly over the push interval.
	ns := b.pushInterval.Nanoseconds() / int64(len(batches))
	t := time.NewTicker(time.Duration(ns))
	defer t.Stop()
	for i, timeSeries := range batches {
		err := b.pushMetrics(context.Background(),
			&monitoringpb.CreateTimeSeriesRequest{
				Name:       "projects/" + b.project,
				TimeSeries: timeSeries,
			})

		// TODO: this is executed in a daemon, so we can't get out info about errors other than logging.
		// We need to build framework level support for these kinds of async tasks. Perhaps a generic batching adapter
		// can handle some of this complexity?
		if err != nil {
			b.l.Errorf("%d time series was sent and Stackdriver returned: %v\n", len(timeSeries), err) // nolint: errcheck
			if isOutOfOrderError(err) {
				b.l.Debugf("Given data: %v", timeSeries)
			} else {
				b.l.Errorf("Given data: %v", timeSeries) // nolint: errcheck
			}
		} else {
			b.l.Debugf("Successfully sent data to Stackdriver.")
		}
		if i+1 != len(batches) {
			// Do not block and wait after the last batch pushed. No need to wait any more.
			// This also leaves portion of push interval for other operations in Send().
			<-t.C
		}
	}
}

func (b *buffered) Close() error {
	b.l.Infof("Sending last data before shutting down")
	b.Send()
	return b.closeMe.Close()
}

func isOutOfOrderError(err error) bool {
	if s, ok := status.FromError(err); ok {
		if strings.Contains(strings.ToLower(s.Message()), "order") {
			return true
		}
	}
	return false
}
