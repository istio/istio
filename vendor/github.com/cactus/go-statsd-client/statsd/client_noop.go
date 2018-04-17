// Copyright (c) 2012-2016 Eli Janssen
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package statsd

import "time"

// A NoopClient is a client that does nothing.
type NoopClient struct{}

// Close closes the connection and cleans up.
func (s *NoopClient) Close() error {
	return nil
}

// Inc increments a statsd count type.
// stat is a string name for the metric.
// value is the integer value
// rate is the sample rate (0.0 to 1.0)
func (s *NoopClient) Inc(stat string, value int64, rate float32) error {
	return nil
}

// Dec decrements a statsd count type.
// stat is a string name for the metric.
// value is the integer value.
// rate is the sample rate (0.0 to 1.0).
func (s *NoopClient) Dec(stat string, value int64, rate float32) error {
	return nil
}

// Gauge submits/Updates a statsd gauge type.
// stat is a string name for the metric.
// value is the integer value.
// rate is the sample rate (0.0 to 1.0).
func (s *NoopClient) Gauge(stat string, value int64, rate float32) error {
	return nil
}

// GaugeDelta submits a delta to a statsd gauge.
// stat is the string name for the metric.
// value is the (positive or negative) change.
// rate is the sample rate (0.0 to 1.0).
func (s *NoopClient) GaugeDelta(stat string, value int64, rate float32) error {
	return nil
}

// Timing submits a statsd timing type.
// stat is a string name for the metric.
// delta is the time duration value in milliseconds
// rate is the sample rate (0.0 to 1.0).
func (s *NoopClient) Timing(stat string, delta int64, rate float32) error {
	return nil
}

// TimingDuration submits a statsd timing type.
// stat is a string name for the metric.
// delta is the timing value as time.Duration
// rate is the sample rate (0.0 to 1.0).
func (s *NoopClient) TimingDuration(stat string, delta time.Duration, rate float32) error {
	return nil
}

// Set submits a stats set type.
// stat is a string name for the metric.
// value is the string value
// rate is the sample rate (0.0 to 1.0).
func (s *NoopClient) Set(stat string, value string, rate float32) error {
	return nil
}

// SetInt submits a number as a stats set type.
// convenience method for Set with number.
// stat is a string name for the metric.
// value is the integer value
// rate is the sample rate (0.0 to 1.0).
func (s *NoopClient) SetInt(stat string, value int64, rate float32) error {
	return nil
}

// Raw formats the statsd event data, handles sampling, prepares it,
// and sends it to the server.
// stat is the string name for the metric.
// value is the preformatted "raw" value string.
// rate is the sample rate (0.0 to 1.0).
func (s *NoopClient) Raw(stat string, value string, rate float32) error {
	return nil
}

// SetPrefix sets/updates the statsd client prefix
func (s *NoopClient) SetPrefix(prefix string) {}

// NewSubStatter returns a SubStatter with appended prefix
func (s *NoopClient) NewSubStatter(prefix string) SubStatter {
	return &NoopClient{}
}

// SetSamplerFunc sets the sampler function
func (s *NoopClient) SetSamplerFunc(sampler SamplerFunc) {}

// NewNoopClient returns a pointer to a new NoopClient, and an error (always
// nil, just supplied to support api convention).
// Use variadic arguments to support identical format as NewClient, or a more
// conventional no argument form.
func NewNoopClient(a ...interface{}) (Statter, error) {
	return &NoopClient{}, nil
}

// NewNoop is a compatibility alias for NewNoopClient
var NewNoop = NewNoopClient
