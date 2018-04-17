// Copyright 2017, OpenCensus Authors
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
//

package stats

// Int64Measure is a measure of type int64.
type Int64Measure struct {
	measure
}

func (m *Int64Measure) subscribe() {
	m.measure.subscribe()
}

func (m *Int64Measure) subscribed() bool {
	return m.measure.subscribed()
}

// M creates a new int64 measurement.
// Use Record to record measurements.
func (m *Int64Measure) M(v int64) Measurement {
	if !m.subscribed() {
		return Measurement{}
	}
	return Measurement{m: m, v: float64(v)}
}

// Int64 creates a new measure of type Int64Measure. It returns an
// error if a measure with the same name already exists.
func Int64(name, description, unit string) (*Int64Measure, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	m := &Int64Measure{
		measure: measure{
			name:        name,
			description: description,
			unit:        unit,
		},
	}
	if _, err := register(m); err != nil {
		return nil, err
	}
	return m, nil
}
