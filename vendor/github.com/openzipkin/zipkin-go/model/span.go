package model

import (
	"encoding/json"
	"errors"
	"time"
)

// unmarshal errors
var (
	ErrValidTraceIDRequired  = errors.New("valid traceId required")
	ErrValidIDRequired       = errors.New("valid span id required")
	ErrValidDurationRequired = errors.New("valid duration required")
)

// SpanContext holds the context of a Span.
type SpanContext struct {
	TraceID  TraceID `json:"traceId"`
	ID       ID      `json:"id"`
	ParentID *ID     `json:"parentId,omitempty"`
	Debug    bool    `json:"debug,omitempty"`
	Sampled  *bool   `json:"-"`
	Err      error   `json:"-"`
}

// SpanModel structure.
//
// If using this library to instrument your application you will not need to
// directly access or modify this representation. The SpanModel is exported for
// use cases involving 3rd party Go instrumentation libraries desiring to
// export data to a Zipkin server using the Zipkin V2 Span model.
type SpanModel struct {
	SpanContext
	Name           string            `json:"name,omitempty"`
	Kind           Kind              `json:"kind,omitempty"`
	Timestamp      time.Time         `json:"timestamp,omitempty"`
	Duration       time.Duration     `json:"duration,omitempty"`
	Shared         bool              `json:"shared,omitempty"`
	LocalEndpoint  *Endpoint         `json:"localEndpoint,omitempty"`
	RemoteEndpoint *Endpoint         `json:"remoteEndpoint,omitempty"`
	Annotations    []Annotation      `json:"annotations,omitempty"`
	Tags           map[string]string `json:"tags,omitempty"`
}

// MarshalJSON exports our Model into the correct format for the Zipkin V2 API.
func (s SpanModel) MarshalJSON() ([]byte, error) {
	type Alias SpanModel

	var timestamp int64
	if !s.Timestamp.IsZero() {
		if s.Timestamp.Unix() < 1 {
			// Zipkin does not allow Timestamps before Unix epoch
			return nil, ErrValidTimestampRequired
		}
		timestamp = s.Timestamp.Round(time.Microsecond).UnixNano() / 1e3
	}

	if s.Duration < time.Microsecond {
		if s.Duration < 0 {
			// negative duration is not allowed and signals a timing logic error
			return nil, ErrValidDurationRequired
		} else if s.Duration > 0 {
			// sub microsecond durations are reported as 1 microsecond
			s.Duration = 1 * time.Microsecond
		}
	} else {
		// Duration will be rounded to nearest microsecond representation.
		//
		// NOTE: Duration.Round() is not available in Go 1.8 which we still support.
		// To handle microsecond resolution rounding we'll add 500 nanoseconds to
		// the duration. When truncated to microseconds in the call to marshal, it
		// will be naturally rounded. See TestSpanDurationRounding in span_test.go
		s.Duration += 500 * time.Nanosecond
	}

	if s.LocalEndpoint.Empty() {
		s.LocalEndpoint = nil
	}

	if s.RemoteEndpoint.Empty() {
		s.RemoteEndpoint = nil
	}

	return json.Marshal(&struct {
		Timestamp int64 `json:"timestamp,omitempty"`
		Duration  int64 `json:"duration,omitempty"`
		Alias
	}{
		Timestamp: timestamp,
		Duration:  s.Duration.Nanoseconds() / 1e3,
		Alias:     (Alias)(s),
	})
}

// UnmarshalJSON imports our Model from a Zipkin V2 API compatible span
// representation.
func (s *SpanModel) UnmarshalJSON(b []byte) error {
	type Alias SpanModel
	span := &struct {
		TimeStamp uint64 `json:"timestamp,omitempty"`
		Duration  uint64 `json:"duration,omitempty"`
		*Alias
	}{
		Alias: (*Alias)(s),
	}
	if err := json.Unmarshal(b, &span); err != nil {
		return err
	}
	if s.ID < 1 {
		return ErrValidIDRequired
	}
	if span.TimeStamp > 0 {
		s.Timestamp = time.Unix(0, int64(span.TimeStamp)*1e3)
	}
	s.Duration = time.Duration(span.Duration*1e3) * time.Nanosecond
	if s.LocalEndpoint.Empty() {
		s.LocalEndpoint = nil
	}

	if s.RemoteEndpoint.Empty() {
		s.RemoteEndpoint = nil
	}
	return nil
}
