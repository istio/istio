// Copyright 2016 Google Inc.
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

import "time"

type (
	// MetricsReporter is the interface for adapters that will handle metrics
	// data within the mixer.
	MetricsReporter interface {
		Aspect

		// ReportMetrics directs a backend adapter to process a batch of
		// MetricValues derived from potentially several Report() calls.
		ReportMetrics([]MetricValue) error
	}

	// MetricValue holds the value of a metric, along with dimensional and
	// other metadata.
	MetricValue struct {
		// Name is the unique name for the metric. Name is used to identify
		// the descriptor for this MetricValue.
		Name string

		// StartTime identifies the start time of value collection.
		// An instant can be represented by only reporting an end
		// time and no start time.
		StartTime time.Time

		// EndTime identifies the end time of value collection.
		EndTime time.Time

		// Attributes are a set of key-value pairs for dimensional data.
		// These correspond directly to the attributes specified in the
		// schema for this metric value.
		Attributes map[string]string

		// BoolValue provides a boolean metric value.
		BoolValue bool

		// Int64Value provides an integer metric value.
		Int64Value int64

		// Float64Value provides a double-precision floating-point metric
		// value.
		Float64Value float64

		// StringValue provides a string-encoded metric value.
		StringValue string
	}
)
