// Copyright 2017 Google Inc.
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

// Package metrics defines a metrics aspect for the mixer. The metrics aspect
// will be used by the mixer to enable backend systems to record and report
// values relevant to services.
package metrics

import (
	"errors"
	"time"

	"istio.io/mixer/pkg/adapter"
)

const (
	// GAUGE is used to record instantaneous (non-cumulative) measurement
	GAUGE Kind = iota
	// COUNTER is used to record increasing cumulative values.
	COUNTER
)

type (
	// Aspect is the interface for adapters that will handle metrics
	// reporting within the mixer.
	Aspect interface {
		adapter.Aspect

		// Record directs a backend adapter to record the list of values
		// that have been generated from Report() calls.
		Record([]Value) error
	}

	// Value holds a single metric value that will be generated through
	// a Report() call to the mixer. It is synthesized by the mixer, based
	// on mixer config and the attributes passed to Report().
	Value struct {
		// Name is the canonical name for the metric for which this
		// value is being reported.
		Name string
		// Kind provides type information on the metric itself
		Kind Kind // TODO: will this be needed? Will adapters get descriptors ahead of time?
		// Labels provide metadata about the metric value. They are
		// generated from the set of attributes provided by Report().
		Labels map[string]interface{}
		// StartTime marks the beginning of the period for which the
		// metric value is being reported. For instantaneous metrics,
		// StartTime records the relevant instant.
		StartTime time.Time
		// EndTime marks the end of the period for which the metric
		// value is being reported. For instantaneous metrics, EndTime
		// will be set to the same value as StartTime.
		EndTime time.Time

		metricValue interface{}
	}

	// Kind defines the set of known metrics types that can be generated
	// by istio.
	Kind int

	// Adapter is the interface for building Aspect instances for mixer
	// metrics backends.
	Adapter interface {
		adapter.Adapter

		// NewMetrics returns a new quota implementation, based on the
		// supplied Aspect configuration for the backend.
		NewMetrics(env adapter.Env, config adapter.AspectConfig) (Aspect, error)
	}
)

// String returns the string-valued metric value for a metrics.Value.
func (v Value) String() (string, error) {
	if v, ok := v.metricValue.(string); ok {
		return v, nil
	}
	return "", errors.New("metric value is not a string")
}

// Bool returns the boolean metric value for a metrics.Value.
func (v Value) Bool() (bool, error) {
	if v, ok := v.metricValue.(bool); ok {
		return v, nil
	}
	return false, errors.New("metric value is not a boolean")
}

// Int64 returns the int64-valued metric value for a metrics.Value.
func (v Value) Int64() (int64, error) {
	if v, ok := v.metricValue.(int64); ok {
		return v, nil
	}
	return 0, errors.New("metric value is not an int64")
}

// Float64 returns the float64-valued metric value for a metrics.Value.
func (v Value) Float64() (float64, error) {
	if v, ok := v.metricValue.(float64); ok {
		return v, nil
	}
	return 0, errors.New("metric value is not a float64")
}
