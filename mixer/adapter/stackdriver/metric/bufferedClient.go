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
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"io"
	"sync"
	"time"

	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
	"google.golang.org/grpc/codes"
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

	// retryBuffer holds timeseries that fail in push.
	retryBuffer []*monitoringpb.TimeSeries
	// retryCounter maps a timeseries to the attempts that it has been retried.
	// Key is the hash of timeseries' metric name, monitored resource and start time.
	retryCounter map[uint64]int

	timeSeriesBatchSize int
	retryLimit          int
	pushInterval        time.Duration
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
	b.m.Lock()
	if len(b.buffer) == 0 && len(b.retryBuffer) == 0 {
		b.m.Unlock()
		b.l.Debugf("No data to send to Stackdriver.")
		return
	}

	toSend := b.buffer
	b.buffer = make([]*monitoringpb.TimeSeries, 0, len(toSend))
	b.m.Unlock()

	mergedToSend := merge(toSend, b.l)
	merged := append(b.retryBuffer, mergedToSend...)
	b.retryBuffer = make([]*monitoringpb.TimeSeries, 0, len(b.retryBuffer))
	batches := batchTimeSeries(merged, b.timeSeriesBatchSize)
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
			ets := handleError(err, timeSeries)
			b.updateRetryBuffer(ets)
			_ = b.l.Errorf("Stackdriver returned: %v\nGiven data: %v", err, timeSeries)
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

// handleError extract out timeseries that fails to create from response status.
// If no sepecific timeseries listed in error response, retry all time series in batch.
func handleError(err error, tsSent []*monitoringpb.TimeSeries) []*monitoringpb.TimeSeries {
	errorTS := make([]*monitoringpb.TimeSeries, 0, 0)
	retryAll := true
	s, ok := status.FromError(err)
	if !ok {
		return errorTS
	}
	sd := s.Details()
	for _, i := range sd {
		if t, ok := i.(*monitoringpb.CreateTimeSeriesError); ok {
			retryAll = false
			if !isRetryable(codes.Code(t.GetStatus().Code)) {
				continue
			}
			errorTS = append(errorTS, t.GetTimeSeries())
		}
	}
	if isRetryable(status.Code(err)) && retryAll {
		for _, ts := range tsSent {
			errorTS = append(errorTS, ts)
		}
	}
	return errorTS
}

func (b *buffered) updateRetryBuffer(errorTS []*monitoringpb.TimeSeries) {
	retryCounter := map[uint64]int{}
	for _, ts := range errorTS {
		k, err := toRetryKey(ts)
		if err != nil {
			b.l.Debugf("cannot generate retry key for timeseries %v: %v", ts, err)
			continue
		}
		if c, ok := b.retryCounter[k]; ok {
			attempt := c + 1
			if attempt >= b.retryLimit {
				continue
			}
			retryCounter[k] = attempt
		} else {
			retryCounter[k] = 0
		}
		b.retryBuffer = append(b.retryBuffer, ts)
	}
	b.retryCounter = retryCounter
}

func isRetryable(c codes.Code) bool {
	switch c {
	case codes.Canceled, codes.DeadlineExceeded, codes.ResourceExhausted, codes.Aborted, codes.Internal, codes.Unavailable:
		return true
	}
	return false
}

func toRetryKey(ts *monitoringpb.TimeSeries) (uint64, error) {
	hash := fnv.New64()
	if len(ts.Points) != 1 {
		return 0, fmt.Errorf("timeseries has to contain exactly one point")
	}

	buf := make([]byte, 8)
	mKey := toKey(ts.Metric, ts.Resource)
	binary.BigEndian.PutUint64(buf, mKey)
	_, _ = hash.Write(buf)

	s := ts.Points[0].Interval.StartTime.GetSeconds()
	binary.BigEndian.PutUint64(buf, uint64(s))
	_, _ = hash.Write(buf)
	return hash.Sum64(), nil
}
