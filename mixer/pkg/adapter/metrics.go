// Copyright 2017 The Istio Authors.
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

package adapter

import (
	"errors"
	"fmt"
	"time"

	dpb "istio.io/api/mixer/v1/config/descriptor"
)

// Metric kinds supported by mixer.
const (
	Gauge   MetricKind = iota // records instantaneous (non-cumulative) measurements
	Counter                   // records increasing cumulative values
)

// Label kinds supported by mixer.
const (
	String LabelType = iota
	Int64
	Float64
	Bool
	Time
	Duration
	IPAddress
	EmailAddress
	URI
	DNSName
)

type (
	// MetricsAspect handles metric reporting within the mixer.
	MetricsAspect interface {
		Aspect

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
		Kind MetricKind
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

		// The value of this metric; this should be accessed type-safely via value.String(), value.Bool(), etc.
		MetricValue interface{}
	}

	// MetricKind defines the set of known metrics types that can be generated
	// by the mixer.
	MetricKind int

	// MetricsBuilder builds instances of the Metrics aspect.
	MetricsBuilder interface {
		Builder

		// NewMetricsAspect returns a new instance of the Metrics aspect.
		NewMetricsAspect(env Env, config AspectConfig, metrics []MetricDefinition) (MetricsAspect, error)
	}

	// MetricDefinition provides the basic description of a metric schema
	// for which metrics adapters will be sent Values at runtime.
	MetricDefinition struct {
		// Name is the canonical name of the metric.
		Name string
		// Description provides information about this metric.
		Description string
		// Kind provides type information about the metric.
		Kind MetricKind
		// Labels are the names of keys for dimensional data that will
		// be generated at runtime and passed along with metric values.
		Labels map[string]LabelType
	}

	// LabelType defines the set of known label types that can be generated
	// by the mixer.
	LabelType int
)

// String returns the string-valued metric value for a metrics.Value.
func (v Value) String() (string, error) {
	if v, ok := v.MetricValue.(string); ok {
		return v, nil
	}
	return "", errors.New("metric value is not a string")
}

// Bool returns the boolean metric value for a metrics.Value.
func (v Value) Bool() (bool, error) {
	if v, ok := v.MetricValue.(bool); ok {
		return v, nil
	}
	return false, errors.New("metric value is not a boolean")
}

// Int64 returns the int64-valued metric value for a metrics.Value.
func (v Value) Int64() (int64, error) {
	if v, ok := v.MetricValue.(int64); ok {
		return v, nil
	}
	return 0, errors.New("metric value is not an int64")
}

// Float64 returns the float64-valued metric value for a metrics.Value.
func (v Value) Float64() (float64, error) {
	if v, ok := v.MetricValue.(float64); ok {
		return v, nil
	}
	return 0, errors.New("metric value is not a float64")
}

// MetricKindFromProto translates from MetricDescriptor_MetricKind to Metric Kind.
func MetricKindFromProto(pbk dpb.MetricDescriptor_MetricKind) (MetricKind, error) {
	switch pbk {
	case dpb.GAUGE:
		return Gauge, nil
	case dpb.COUNTER:
		return Counter, nil
	default:
		return 0, fmt.Errorf("invalid proto MetricKind %v", pbk)
	}
}

// LabelTypeFromProto translates from ValueType to LabelType.
func LabelTypeFromProto(pbt dpb.ValueType) (LabelType, error) {
	switch pbt {
	case dpb.STRING:
		return String, nil
	case dpb.BOOL:
		return Bool, nil
	case dpb.INT64:
		return Int64, nil
	case dpb.DOUBLE:
		return Float64, nil
	case dpb.TIMESTAMP:
		return Time, nil
	case dpb.DNS_NAME:
		return DNSName, nil
	case dpb.DURATION:
		return Duration, nil
	case dpb.EMAIL_ADDRESS:
		return EmailAddress, nil
	case dpb.IP_ADDRESS:
		return IPAddress, nil
	case dpb.URI:
		return URI, nil
	default:
		return 0, fmt.Errorf("invalid proto ValueType %v", pbt)
	}
}
