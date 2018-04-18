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

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"go.opencensus.io/stats/internal"
)

// Measure represents a type of metric to be tracked and recorded.
// For example, latency, request Mb/s, and response Mb/s are measures
// to collect from a server.
//
// Each measure needs to be registered before being used.
// Measure constructors such as Int64 and
// Float64 automatically registers the measure
// by the given name.
// Each registered measure needs to be unique by name.
// Measures also have a description and a unit.
type Measure interface {
	Name() string
	Description() string
	Unit() string

	subscribe()
	subscribed() bool
}

type measure struct {
	subs int32 // access atomically

	name        string
	description string
	unit        string
}

func (m *measure) subscribe() {
	atomic.StoreInt32(&m.subs, 1)
}

func (m *measure) subscribed() bool {
	return atomic.LoadInt32(&m.subs) == 1
}

// Name returns the name of the measure.
func (m *measure) Name() string {
	return m.name
}

// Description returns the description of the measure.
func (m *measure) Description() string {
	return m.description
}

// Unit returns the unit of the measure.
func (m *measure) Unit() string {
	return m.unit
}

var (
	mu       sync.RWMutex
	measures = make(map[string]Measure)
)

var (
	errDuplicate          = errors.New("duplicate measure name")
	errMeasureNameTooLong = fmt.Errorf("measure name cannot be longer than %v", internal.MaxNameLength)
)

// FindMeasure finds the Measure instance, if any, associated with the given name.
func FindMeasure(name string) Measure {
	mu.RLock()
	m := measures[name]
	mu.RUnlock()
	return m
}

func register(m Measure) (Measure, error) {
	key := m.Name()
	mu.Lock()
	defer mu.Unlock()
	if stored, ok := measures[key]; ok {
		return stored, errDuplicate
	}
	measures[key] = m
	return m, nil
}

// Measurement is the numeric value measured when recording stats. Each measure
// provides methods to create measurements of their kind. For example, Int64Measure
// provides M to convert an int64 into a measurement.
type Measurement struct {
	v float64
	m Measure
}

// Value returns the value of the Measurement as a float64.
func (m Measurement) Value() float64 {
	return m.v
}

// Measure returns the Measure from which this Measurement was created.
func (m Measurement) Measure() Measure {
	return m.m
}

func checkName(name string) error {
	if len(name) > internal.MaxNameLength {
		return errMeasureNameTooLong
	}
	if !internal.IsPrintable(name) {
		return errors.New("measure name needs to be an ASCII string")
	}
	return nil
}
