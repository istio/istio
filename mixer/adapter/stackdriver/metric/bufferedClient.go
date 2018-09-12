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

	gprcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"istio.io/istio/mixer/pkg/adapter"
)

// Abstracts over the specific impl for testing.
type bufferedClient interface {
	io.Closer

	Record([]*monitoringpb.TimeSeries)
}

type retryTimeSeries struct {
	// How many times the time series has been retried.
	attempt int
	ts      *monitoringpb.TimeSeries
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
	// Retry buffer is keyed by hash(metric name, monitored resource).
	retryBuffer         map[uint64]retryTimeSeries
	timeSeriesBatchSize int
	retryLimit          int
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
	// Retry timeseries in retry buffer. Retry before normal push so that retried points could be
	// preserved thus view would be more accurate and order is guaranteed.
	// If timeseries is again failed to create, merge it with newer timeseries in normal buffer.
	b.retry()

	b.m.Lock()
	if len(b.buffer) == 0 {
		b.m.Unlock()
		b.l.Debugf("No data to send to Stackdriver.")
		return
	}

	toSend := b.buffer
	b.buffer = make([]*monitoringpb.TimeSeries, 0, len(toSend))
	b.m.Unlock()

	merged := merge(toSend, b.l)
	batches := batchTimeSeries(merged, b.timeSeriesBatchSize)

	for _, timeSeries := range batches {
		err := b.pushMetrics(context.Background(),
			&monitoringpb.CreateTimeSeriesRequest{
				Name:       "projects/" + b.project,
				TimeSeries: timeSeries,
			})

		// TODO: this is executed in a daemon, so we can't get out info about errors other than logging.
		// We need to build framework level support for these kinds of async tasks. Perhaps a generic batching adapter
		// can handle some of this complexity?
		if err != nil {
			ets := handleError(err, timeSeries)
			b.updateRetryBuffer(ets)
			_ = b.l.Errorf("Stackdriver returned: %v\nGiven data: %v", err, timeSeries)
		} else {
			b.l.Debugf("Successfully sent data to Stackdriver.")
		}
	}
}

// Retry all time series in retry buffer. If still fails, add the failed time series to the normal
// buffer so that it will be merged with newer timeseries. Otherwise if a newer timeseries was
// successfully written, it can never be written.
func (b *buffered) retry() {
	toRetry := make([]*monitoringpb.TimeSeries, 0, len(b.retryBuffer))
	keyMap := make(map[*monitoringpb.TimeSeries]uint64)
	for k, r := range b.retryBuffer {
		toRetry = append(toRetry, r.ts)
		keyMap[r.ts] = k
	}
	retryBatches := batchTimeSeries(toRetry, b.timeSeriesBatchSize)
	for _, timeSeries := range retryBatches {
		err := b.pushMetrics(context.Background(),
			&monitoringpb.CreateTimeSeriesRequest{
				Name:       "projects/" + b.project,
				TimeSeries: timeSeries,
			})
		if err != nil {
			ets := handleError(err, timeSeries)
			b.Record(ets)
		} else {
			for _, ts := range timeSeries {
				delete(b.retryBuffer, keyMap[ts])
			}
		}
	}
}

func (b *buffered) Close() error {
	b.l.Infof("Sending last data before shutting down")
	b.Send()
	return b.closeMe.Close()
}

// handleError extract out timeseries that fails to create from response status.
// If no sepecific timeseries listed in error response, retry all time series in batch.
func handleError(err error, tsSent []*monitoringpb.TimeSeries) []*monitoringpb.TimeSeries {
	errorTS := make([]*monitoringpb.TimeSeries, 0, 0)
	retryAll := true
	if s, ok := status.FromError(err); ok {
		sd := s.Details()
		for _, i := range sd {
			if t, ok := i.(*monitoringpb.CreateTimeSeriesError); ok {
				retryAll = false
				if isOutOfOrderWrite(t.GetStatus()) {
					// Don't deal with out of order error: the point can never be written.
					continue
				}
				errorTS = append(errorTS, t.GetTimeSeries())
			}
		}
	}
	if retryAll {
		for _, ts := range tsSent {
			errorTS = append(errorTS, ts)
		}
	}
	return errorTS
}

func (b *buffered) updateRetryBuffer(errorTS []*monitoringpb.TimeSeries) {
	for _, ts := range errorTS {
		k := toKey(ts.Metric, ts.Resource)
		if r, ok := b.retryBuffer[k]; ok {
			attempt := r.attempt + 1
			if attempt >= b.retryLimit {
				delete(b.retryBuffer, k)
				continue
			}
			b.retryBuffer[k] = retryTimeSeries{attempt, ts}
		} else {
			b.retryBuffer[k] = retryTimeSeries{0, ts}
		}
	}
}

func isOutOfOrderWrite(st *gprcstatus.Status) bool {
	return st.Code == int32(codes.InvalidArgument) && strings.Contains(st.Message, "Points must be written in order")
}
