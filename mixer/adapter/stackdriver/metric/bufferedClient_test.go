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
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	gax "github.com/googleapis/gax-go/v2"
	xcontext "golang.org/x/net/context"
	monitoring "google.golang.org/genproto/googleapis/monitoring/v3"
	grpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/testing/protocmp"

	"istio.io/istio/mixer/pkg/adapter/test"
)

func TestBuffered_Record(t *testing.T) {
	b := &buffered{buffer: []*monitoring.TimeSeries{}, mergeTrigger: 1000}
	b.Record([]*monitoring.TimeSeries{})
	if len(b.buffer) != 0 {
		t.Fatalf("Recorded empty array, expected empty buffer; got: %v", b)
	}

	in := []*monitoring.TimeSeries{nil, nil, nil}
	b.Record(in)
	if len(b.buffer) != len(in) {
		t.Fatalf("Recorded %d vals, expected buffer to have %d elements: %v", len(in), len(b.buffer), b)
	}
}

func TestBuffered_Send(t *testing.T) {
	env := test.NewEnv(t)
	b := buffered{
		l:                   env,
		timeSeriesBatchSize: 100,
		retryLimit:          1,
		mergeTrigger:        1000,
		buffer:              []*monitoring.TimeSeries{},
		retryBuffer:         []*monitoring.TimeSeries{},
		pushInterval:        100 * time.Millisecond,
		mergedTS:            make(map[uint64]*monitoring.TimeSeries),
	}

	// We'll panic if we call the pushMetrics fn
	panicFn := func(ctx xcontext.Context, req *monitoring.CreateTimeSeriesRequest, opts ...gax.CallOption) error {
		panic("Should not be called!")
	}
	b.pushMetrics = panicFn
	b.Record([]*monitoring.TimeSeries{})
	defer func() {
		if s := recover(); s != nil {
			t.Fatalf("Called pushMetrics with no values!")
		}
	}()
	b.mergeTimeSeries()
	b.Send()

	in := []*monitoring.TimeSeries{makeTS(m1, mr1, 1, 1), makeTS(m1, mr1, 1, 1), makeTS(m1, mr1, 1, 1)}

	errorFn := func(ctx xcontext.Context, req *monitoring.CreateTimeSeriesRequest, opts ...gax.CallOption) error {
		return errors.New("expected")
	}
	happyFn := func(ctx xcontext.Context, req *monitoring.CreateTimeSeriesRequest, opts ...gax.CallOption) error {
		return nil
	}

	tests := []struct {
		name string
		in   []*monitoring.TimeSeries
		fn   pushFunc
		out  string
	}{
		{"error", in, errorFn, "Stackdriver returned: expected"},
		{"happy", in, happyFn, "Successfully sent data to Stackdriver."},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			env = test.NewEnv(t)
			b = buffered{
				l:                   env,
				pushMetrics:         tt.fn,
				timeSeriesBatchSize: 100,
				retryLimit:          1,
				mergeTrigger:        1000,
				buffer:              []*monitoring.TimeSeries{},
				retryBuffer:         []*monitoring.TimeSeries{},
				pushInterval:        100 * time.Millisecond,
				mergedTS:            make(map[uint64]*monitoring.TimeSeries),
			}
			b.Record(tt.in)
			b.mergeTimeSeries()
			b.Send()
			found := false
			for _, l := range env.GetLogs() {
				found = found || strings.Contains(l, tt.out)
			}
			if !found {
				t.Errorf("b.Send() with errorFn didn't log an expected error; got logs: %v", env.GetLogs())
			}
		})
	}
}

func TestBuffered_BatchSend(t *testing.T) {
	in := []*monitoring.TimeSeries{makeTS(m1, mr1, 1, 1), makeTS(m2, mr2, 1, 1), makeTS(m3, mr3, 1, 1)}

	tests := []struct {
		name      string
		batchSize int
		pushTimes int
	}{
		{"two pushes", 2, 2},
		{"single push", 4, 1},
		{"batch size", 3, 1},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			env := test.NewEnv(t)
			pushTimes := 0
			pushFunc := func(ctx xcontext.Context, req *monitoring.CreateTimeSeriesRequest, opts ...gax.CallOption) error {
				pushTimes++
				return nil
			}
			b := buffered{
				l:                   env,
				pushMetrics:         pushFunc,
				mergeTrigger:        1000,
				timeSeriesBatchSize: tt.batchSize,
				pushInterval:        100 * time.Millisecond,
				mergedTS:            make(map[uint64]*monitoring.TimeSeries),
			}
			b.Record(in)
			b.mergeTimeSeries()
			b.Send()
			if pushTimes != tt.pushTimes {
				t.Errorf("pushMetrics is called with unexpected times. got %v want %v", pushTimes, tt.pushTimes)
			}
		})
	}
}

func TestBuffered_SpreadPush(t *testing.T) {
	in := []*monitoring.TimeSeries{makeTS(m1, mr1, 1, 1), makeTS(m2, mr2, 1, 1), makeTS(m3, mr3, 1, 1)}
	l := test.NewEnv(t).Logger()
	pushTimes := 0
	pushFunc := func(ctx xcontext.Context, req *monitoring.CreateTimeSeriesRequest, opts ...gax.CallOption) error {
		pushTimes++
		return nil
	}
	b := buffered{
		l:                   l,
		pushMetrics:         pushFunc,
		timeSeriesBatchSize: 1,
		retryBuffer:         []*monitoring.TimeSeries{},
		mergeTrigger:        1000,
		pushInterval:        1 * time.Second,
		mergedTS:            make(map[uint64]*monitoring.TimeSeries),
	}

	start := time.Now()
	b.Record(in)
	b.mergeTimeSeries()
	b.Send()
	d := time.Since(start)
	if d < 500*time.Millisecond {
		t.Errorf("Duration for send %v is too short, Expect it last at least half second", d)
	}
	if pushTimes != 3 {
		t.Errorf("pushMetrics is called with unexpected times. got %v want 3", pushTimes)
	}
}

type closeMe struct {
	closed bool
}

func (c *closeMe) Close() error {
	c.closed = true
	return nil
}

func TestBuffered_Close(t *testing.T) {
	closeMe := &closeMe{}
	b := &buffered{closeMe: closeMe, l: test.NewEnv(t).Logger(), timeSeriesBatchSize: 100}
	if err := b.Close(); err != nil {
		t.Errorf("Unexpected error calling close on buffered client: %v", err)
	}
	if !closeMe.closed {
		t.Fatalf("buffered.Close() did not call Close() on buffered.closeMe.")
	}
}

func createRetryPushFn(pushCount *int, expReqTS [][]*monitoring.TimeSeries, withError []bool, failedTS [][]*monitoring.TimeSeries,
	overallCode codes.Code, t *testing.T) pushFunc {
	retryPushFn := func(ctx xcontext.Context, req *monitoring.CreateTimeSeriesRequest, opts ...gax.CallOption) error {
		if len(expReqTS) != len(withError) || len(expReqTS) != len(failedTS) || len(expReqTS) <= *pushCount {
			t.Errorf("args size does not match or potential out of bound. abort push operation %v %v %v %v",
				len(expReqTS), len(withError), len(failedTS), *pushCount)
			return nil
		}
		defer func(pc *int) {
			(*pc)++
		}(pushCount)

		// Verify time series in request match the given ones
		if len(expReqTS[*pushCount]) != len(req.TimeSeries) {
			t.Errorf("push %v - number of time series in CreateTimeSeriesRequest is not expected: got %+v want %+v",
				*pushCount, req.TimeSeries, expReqTS[*pushCount])
			return nil
		}
		for _, ets := range expReqTS[*pushCount] {
			found := false
			for _, ts := range req.TimeSeries {
				if cmp.Equal(ts, ets, protocmp.Transform()) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("got %+v, want %+v in push request but cannot find", req.TimeSeries, ets)
				return nil
			}
		}

		// If return with error, check if specific time series should be included in error response
		if !withError[*pushCount] {
			return nil
		}

		return status.ErrorProto(&grpcstatus.Status{
			Code:    int32(overallCode),
			Message: "A subset of time series had errors.",
		})
	}
	return retryPushFn
}

func TestBuffered_Retry(t *testing.T) {
	in := []*monitoring.TimeSeries{makeTS(m1, mr1, 1, 1), makeTS(m2, mr2, 2, 1), makeTS(m3, mr3, 3, 1)}
	tests := []struct {
		name        string
		pushCount   int
		requestTS   [][]*monitoring.TimeSeries
		withError   []bool
		overallCode codes.Code
		failedTS    [][]*monitoring.TimeSeries
	}{
		{
			name:        "retry",
			pushCount:   2,
			requestTS:   [][]*monitoring.TimeSeries{in, in},
			withError:   []bool{true, false},
			overallCode: codes.Unavailable,
			failedTS:    [][]*monitoring.TimeSeries{{}, {}},
		},
		{
			name:        "do not retry",
			pushCount:   1,
			requestTS:   [][]*monitoring.TimeSeries{in},
			withError:   []bool{true},
			overallCode: codes.InvalidArgument,
			failedTS:    [][]*monitoring.TimeSeries{{}},
		},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			pushCount := 0
			env := test.NewEnv(t)
			b := buffered{
				l:                   env,
				pushMetrics:         createRetryPushFn(&pushCount, tt.requestTS, tt.withError, tt.failedTS, tt.overallCode, t),
				timeSeriesBatchSize: 100,
				retryLimit:          5,
				retryBuffer:         []*monitoring.TimeSeries{},
				retryCounter:        map[uint64]int{},
				mergeTrigger:        1000,
				pushInterval:        100 * time.Millisecond,
				mergedTS:            make(map[uint64]*monitoring.TimeSeries),
			}
			b.Record(in)
			b.mergeTimeSeries()
			b.Send()

			// call send again without TS in normal buffer. It should retry failed TS.
			b.Send()
			if pushCount != tt.pushCount {
				t.Errorf("push call count is not expected, got %v want %v", pushCount, tt.pushCount)
			}
		})
	}
}

func TestBuffered_RetryCombine(t *testing.T) {
	in := []*monitoring.TimeSeries{makeTS(m1, mr1, 1, 1), makeTS(m2, mr2, 1, 1), makeTS(m3, mr3, 1, 1)}
	in2 := []*monitoring.TimeSeries{makeTS(m1, mr1, 2, 1), makeTS(m2, mr2, 2, 1)}
	pushCount := 0
	requestTS := [][]*monitoring.TimeSeries{in, append(in, in2...)}
	failedTS := [][]*monitoring.TimeSeries{in, {}}
	// Make first push fail. Retry timeSeries should be combined with newer timeseries.
	withError := []bool{true, false}
	env := test.NewEnv(t)
	b := buffered{
		l:                   env,
		pushMetrics:         createRetryPushFn(&pushCount, requestTS, withError, failedTS, codes.Unavailable, t),
		timeSeriesBatchSize: 100,
		retryLimit:          5,
		retryBuffer:         []*monitoring.TimeSeries{},
		retryCounter:        map[uint64]int{},
		pushInterval:        100 * time.Millisecond,
		mergeTrigger:        1000,
		mergedTS:            make(map[uint64]*monitoring.TimeSeries),
	}
	b.Record(in)
	b.mergeTimeSeries()
	b.Send()

	// Call record and send again with some newer timeseries. Retry buffer should be combined with normal buffer.
	b.Record(in2)
	b.mergeTimeSeries()
	b.Send()
	if pushCount != 2 {
		t.Errorf("push call count is not expected, got %v want 2", pushCount)
	}
}

func TestBuffered_RetryMaxAttempt(t *testing.T) {
	in := []*monitoring.TimeSeries{makeTS(m1, mr1, 1, 1), makeTS(m2, mr2, 1, 1), makeTS(m3, mr3, 1, 1)}
	pushCount := 0
	l := test.NewEnv(t).Logger()
	requestTS := make([][]*monitoring.TimeSeries, 0, 31)
	failedTS := make([][]*monitoring.TimeSeries, 0, 31)
	withError := make([]bool, 0, 31)
	for i := 0; i < 15; i++ {
		requestTS = append(requestTS, in)
		failedTS = append(failedTS, []*monitoring.TimeSeries{})
		withError = append(withError, true)
	}
	b := buffered{
		l:                   l,
		pushMetrics:         createRetryPushFn(&pushCount, requestTS, withError, failedTS, codes.Unavailable, t),
		timeSeriesBatchSize: 100,
		retryLimit:          2,
		retryBuffer:         []*monitoring.TimeSeries{},
		retryCounter:        map[uint64]int{},
		pushInterval:        100 * time.Millisecond,
		mergeTrigger:        1000,
		mergedTS:            make(map[uint64]*monitoring.TimeSeries),
	}
	b.Record(in)
	b.mergeTimeSeries()
	// Call Send() for lots of times. Push should just be called with limited times.
	// In this case, push was called 3 times.
	for i := 0; i < 15; i++ {
		b.Send()
	}
	if pushCount != 3 {
		t.Errorf("push call count is not expected, got %v want 3", pushCount)
	}
}
