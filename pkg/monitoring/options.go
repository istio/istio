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

package monitoring

// Options encode changes to the options passed to a Metric at creation time.
type Options func(*options)

type options struct {
	enabledCondition func() bool
	unit             Unit
	name             string
	description      string
}

// WithUnit provides configuration options for a new Metric, providing unit of measure
// information for a new Metric.
func WithUnit(unit Unit) Options {
	return func(opts *options) {
		opts.unit = unit
	}
}

// WithEnabled allows a metric to be condition enabled if the provided function returns true.
// If disabled, metric operations will do nothing.
func WithEnabled(enabled func() bool) Options {
	return func(o *options) {
		o.enabledCondition = enabled
	}
}

func createOptions(name, description string, opts ...Options) (options, Metric) {
	o := options{unit: None, name: name, description: description}
	for _, opt := range opts {
		opt(&o)
	}
	if o.enabledCondition != nil && !o.enabledCondition() {
		return o, &disabledMetric{name: name}
	}
	return o, nil
}
