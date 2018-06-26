package model

import (
	"encoding/json"
	"errors"
	"time"
)

// ErrValidTimestampRequired error
var ErrValidTimestampRequired = errors.New("valid annotation timestamp required")

// Annotation associates an event that explains latency with a timestamp.
type Annotation struct {
	Timestamp time.Time
	Value     string
}

// MarshalJSON implements custom JSON encoding
func (a *Annotation) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Timestamp int64  `json:"timestamp"`
		Value     string `json:"value"`
	}{
		Timestamp: a.Timestamp.Round(time.Microsecond).UnixNano() / 1e3,
		Value:     a.Value,
	})
}

// UnmarshalJSON implements custom JSON decoding
func (a *Annotation) UnmarshalJSON(b []byte) error {
	type Alias Annotation
	annotation := &struct {
		TimeStamp uint64 `json:"timestamp"`
		*Alias
	}{
		Alias: (*Alias)(a),
	}
	if err := json.Unmarshal(b, &annotation); err != nil {
		return err
	}
	if annotation.TimeStamp < 1 {
		return ErrValidTimestampRequired
	}
	a.Timestamp = time.Unix(0, int64(annotation.TimeStamp)*1e3)
	return nil
}
