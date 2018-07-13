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

package metric

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	gax "github.com/googleapis/gax-go"
	xcontext "golang.org/x/net/context"
	monitoring "google.golang.org/genproto/googleapis/monitoring/v3"

	"istio.io/istio/mixer/pkg/adapter/test"
)

func TestBuffered_Record(t *testing.T) {
	b := &buffered{}
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
	b := buffered{l: env, timeSeriesBatchSize: 100}

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
			b = buffered{l: env, pushMetrics: tt.fn, timeSeriesBatchSize: 100}
			b.Record(tt.in)
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
			b := buffered{l: env, pushMetrics: pushFunc, timeSeriesBatchSize: tt.batchSize}
			b.Record(in)
			b.Send()
			if pushTimes != tt.pushTimes {
				t.Errorf("pushMetrics is called with unexpected times. got %v want %v", pushTimes, tt.pushTimes)
			}
		})
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
