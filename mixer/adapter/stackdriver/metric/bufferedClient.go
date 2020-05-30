// Copyright Istio Authors
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
	"strings"
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

	// Guards merged time series
	tsm          sync.Mutex
	mergeTrigger int
	mergedTS     map[uint64]*monitoringpb.TimeSeries

	// retryBuffer holds timeseries that fail in push.
	retryBuffer []*monitoringpb.TimeSeries
	// retryCounter maps a timeseries to the attempts that it has been retried.
	// Key is the hash of timeseries' metric name, monitored resource and start time.
	retryCounter map[uint64]int

	timeSeriesBatchSize int
	retryLimit          int
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
	if len(b.mergedTS) == 0 && len(b.retryBuffer) == 0 {
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
	merged := append(b.retryBuffer, toSend...)
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
			if isRetryable(status.Code(err)) {
				b.updateRetryBuffer(timeSeries)
			}
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
	case codes.DeadlineExceeded, codes.Unavailable:
		return true
	}
	return false
}

func isOutOfOrderError(err error) bool {
	if s, ok := status.FromError(err); ok {
		if strings.Contains(strings.ToLower(s.Message()), "order") {
			return true
		}
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
