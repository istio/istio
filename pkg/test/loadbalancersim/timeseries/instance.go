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

package timeseries

import (
	"sync"
	"time"
)

type Instance struct {
	data  Data
	times times
	mutex sync.Mutex
}

func (ts *Instance) AddObservation(val float64, t time.Time) {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()
	ts.data = append(ts.data, val)
	ts.times = append(ts.times, t)
}

func (ts *Instance) AddAll(o *Instance) {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()

	oData, oTimes := o.Series()
	ts.data = append(ts.data, oData...)
	ts.times = append(ts.times, oTimes...)
}

func (ts *Instance) Data() Data {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()
	return ts.data.Copy()
}

func (ts *Instance) Series() (Data, []time.Time) {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()
	return ts.data.Copy(), ts.times.copy()
}

func (ts *Instance) SeriesAsDurationSinceEpoch(epoch time.Time) (Data, []time.Duration) {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()
	return ts.data.Copy(), ts.times.asDurationSinceEpoch(epoch)
}

type times []time.Time

func (t times) copy() times {
	out := make(times, 0, len(t))
	out = append(out, t...)
	return out
}

func (t times) asDurationSinceEpoch(epoch time.Time) []time.Duration {
	out := make([]time.Duration, 0, len(t))
	for _, tm := range t {
		out = append(out, tm.Sub(epoch))
	}
	return out
}
