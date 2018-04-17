// Copyright (c) 2012-2016 Eli Janssen
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package statsd

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"
)

var bufPool = newBufferPool()

// The StatSender interface wraps all the statsd metric methods
type StatSender interface {
	Inc(string, int64, float32) error
	Dec(string, int64, float32) error
	Gauge(string, int64, float32) error
	GaugeDelta(string, int64, float32) error
	Timing(string, int64, float32) error
	TimingDuration(string, time.Duration, float32) error
	Set(string, string, float32) error
	SetInt(string, int64, float32) error
	Raw(string, string, float32) error
}

// The Statter interface defines the behavior of a stat client
type Statter interface {
	StatSender
	NewSubStatter(string) SubStatter
	SetPrefix(string)
	Close() error
}

// The SubStatter interface defines the behavior of a stat child/subclient
type SubStatter interface {
	StatSender
	SetSamplerFunc(SamplerFunc)
	NewSubStatter(string) SubStatter
}

// The SamplerFunc type defines a function that can serve
// as a Client sampler function.
type SamplerFunc func(float32) bool

// DefaultSampler is the default rate sampler function
func DefaultSampler(rate float32) bool {
	if rate < 1 {
		if rand.Float32() < rate {
			return true
		}
		return false
	}
	return true
}

// A Client is a statsd client.
type Client struct {
	// prefix for statsd name
	prefix string
	// packet sender
	sender Sender
	// sampler method
	sampler SamplerFunc
}

// Close closes the connection and cleans up.
func (s *Client) Close() error {
	if s == nil {
		return nil
	}

	err := s.sender.Close()
	return err
}

// Inc increments a statsd count type.
// stat is a string name for the metric.
// value is the integer value
// rate is the sample rate (0.0 to 1.0)
func (s *Client) Inc(stat string, value int64, rate float32) error {
	if !s.includeStat(rate) {
		return nil
	}

	return s.submit(stat, "", value, "|c", rate)
}

// Dec decrements a statsd count type.
// stat is a string name for the metric.
// value is the integer value.
// rate is the sample rate (0.0 to 1.0).
func (s *Client) Dec(stat string, value int64, rate float32) error {
	if !s.includeStat(rate) {
		return nil
	}

	return s.submit(stat, "", -value, "|c", rate)
}

// Gauge submits/updates a statsd gauge type.
// stat is a string name for the metric.
// value is the integer value.
// rate is the sample rate (0.0 to 1.0).
func (s *Client) Gauge(stat string, value int64, rate float32) error {
	if !s.includeStat(rate) {
		return nil
	}

	return s.submit(stat, "", value, "|g", rate)
}

// GaugeDelta submits a delta to a statsd gauge.
// stat is the string name for the metric.
// value is the (positive or negative) change.
// rate is the sample rate (0.0 to 1.0).
func (s *Client) GaugeDelta(stat string, value int64, rate float32) error {
	if !s.includeStat(rate) {
		return nil
	}

	// if negative, the submit formatter will prefix with a - already
	// so only special case the positive value
	if value >= 0 {
		return s.submit(stat, "+", value, "|g", rate)
	}
	return s.submit(stat, "", value, "|g", rate)
}

// Timing submits a statsd timing type.
// stat is a string name for the metric.
// delta is the time duration value in milliseconds
// rate is the sample rate (0.0 to 1.0).
func (s *Client) Timing(stat string, delta int64, rate float32) error {
	if !s.includeStat(rate) {
		return nil
	}

	return s.submit(stat, "", delta, "|ms", rate)
}

// TimingDuration submits a statsd timing type.
// stat is a string name for the metric.
// delta is the timing value as time.Duration
// rate is the sample rate (0.0 to 1.0).
func (s *Client) TimingDuration(stat string, delta time.Duration, rate float32) error {
	if !s.includeStat(rate) {
		return nil
	}

	ms := float64(delta) / float64(time.Millisecond)
	return s.submit(stat, "", ms, "|ms", rate)
}

// Set submits a stats set type
// stat is a string name for the metric.
// value is the string value
// rate is the sample rate (0.0 to 1.0).
func (s *Client) Set(stat string, value string, rate float32) error {
	if !s.includeStat(rate) {
		return nil
	}

	return s.submit(stat, "", value, "|s", rate)
}

// SetInt submits a number as a stats set type.
// stat is a string name for the metric.
// value is the integer value
// rate is the sample rate (0.0 to 1.0).
func (s *Client) SetInt(stat string, value int64, rate float32) error {
	if !s.includeStat(rate) {
		return nil
	}

	return s.submit(stat, "", value, "|s", rate)
}

// Raw submits a preformatted value.
// stat is the string name for the metric.
// value is a preformatted "raw" value string.
// rate is the sample rate (0.0 to 1.0).
func (s *Client) Raw(stat string, value string, rate float32) error {
	if !s.includeStat(rate) {
		return nil
	}

	return s.submit(stat, "", value, "", rate)
}

// SetSamplerFunc sets a sampler function to something other than the default
// sampler is a function that determines whether the metric is
// to be accepted, or discarded.
// An example use case is for submitted pre-sampled metrics.
func (s *Client) SetSamplerFunc(sampler SamplerFunc) {
	s.sampler = sampler
}

// submit an already sampled raw stat
func (s *Client) submit(stat, vprefix string, value interface{}, suffix string, rate float32) error {
	data := bufPool.Get()
	defer bufPool.Put(data)

	if s.prefix != "" {
		data.WriteString(s.prefix)
		data.WriteString(".")
	}

	data.WriteString(stat)
	data.WriteString(":")

	if vprefix != "" {
		data.WriteString(vprefix)
	}

	// sadly, no way to jam this back into the bytes.Buffer without
	// doing a few allocations... avoiding those is the whole point here...
	// so from here on out just use it as a raw []byte
	b := data.Bytes()

	switch v := value.(type) {
	case string:
		b = append(b, v...)
	case int64:
		b = strconv.AppendInt(b, v, 10)
	case float64:
		b = strconv.AppendFloat(b, v, 'f', -1, 64)
	default:
		return fmt.Errorf("No matching type format")
	}

	if suffix != "" {
		b = append(b, suffix...)
	}

	if rate < 1 {
		b = append(b, "|@"...)
		b = strconv.AppendFloat(b, float64(rate), 'f', 6, 32)
	}

	_, err := s.sender.Send(b)
	return err
}

// check for nil client, and perform sampling calculation
func (s *Client) includeStat(rate float32) bool {
	if s == nil {
		return false
	}

	// test for nil in case someone builds their own
	// client without calling new (result is nil sampler)
	if s.sampler != nil {
		return s.sampler(rate)
	}
	return DefaultSampler(rate)
}

// SetPrefix sets/updates the statsd client prefix.
// Note: Does not change the prefix of any SubStatters.
func (s *Client) SetPrefix(prefix string) {
	if s == nil {
		return
	}

	s.prefix = prefix
}

// NewSubStatter returns a SubStatter with appended prefix
func (s *Client) NewSubStatter(prefix string) SubStatter {
	var c *Client
	if s != nil {
		c = &Client{
			prefix:  joinPathComp(s.prefix, prefix),
			sender:  s.sender,
			sampler: s.sampler,
		}
	}
	return c
}

// NewClient returns a pointer to a new Client, and an error.
//
// addr is a string of the format "hostname:port", and must be parsable by
// net.ResolveUDPAddr.
//
// prefix is the statsd client prefix. Can be "" if no prefix is desired.
func NewClient(addr, prefix string) (Statter, error) {
	sender, err := NewSimpleSender(addr)
	if err != nil {
		return nil, err
	}

	return &Client{prefix: prefix, sender: sender}, nil
}

// NewClientWithSender returns a pointer to a new Client and an error.
//
// sender is an instance of a statsd.Sender interface and may not be nil
//
// prefix is the stastd client prefix. Can be "" if no prefix is desired.
func NewClientWithSender(sender Sender, prefix string) (Statter, error) {
	if sender == nil {
		return nil, fmt.Errorf("Client sender may not be nil")
	}

	return &Client{prefix: prefix, sender: sender}, nil
}

// joinPathComp is a helper that ensures we combine path components with a dot
// when it's appropriate to do so; prefix is the existing prefix and suffix is
// the new component being added.
//
// It returns the joined prefix.
func joinPathComp(prefix, suffix string) string {
	suffix = strings.TrimLeft(suffix, ".")
	if prefix != "" && suffix != "" {
		return prefix + "." + suffix
	}
	return prefix + suffix
}

// Dial is a compatibility alias for NewClient
var Dial = NewClient

// New is a compatibility alias for NewClient
var New = NewClient
