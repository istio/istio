package model

import (
	"fmt"
	"strconv"
)

// TraceID is a 128 bit number internally stored as 2x uint64 (high & low).
// In case of 64 bit traceIDs, the value can be found in Low.
type TraceID struct {
	High uint64
	Low  uint64
}

// Empty returns if TraceID has zero value.
func (t TraceID) Empty() bool {
	return t.Low == 0 && t.High == 0
}

// String outputs the 128-bit traceID as hex string.
func (t TraceID) String() string {
	if t.High == 0 {
		return fmt.Sprintf("%016x", t.Low)
	}
	return fmt.Sprintf("%016x%016x", t.High, t.Low)
}

// TraceIDFromHex returns the TraceID from a hex string.
func TraceIDFromHex(h string) (t TraceID, err error) {
	if len(h) > 16 {
		if t.High, err = strconv.ParseUint(h[0:len(h)-16], 16, 64); err != nil {
			return
		}
		t.Low, err = strconv.ParseUint(h[len(h)-16:], 16, 64)
		return
	}
	t.Low, err = strconv.ParseUint(h, 16, 64)
	return
}

// MarshalJSON custom JSON serializer to export the TraceID in the required
// zero padded hex representation.
func (t TraceID) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("%q", t.String())), nil
}

// UnmarshalJSON custom JSON deserializer to retrieve the traceID from the hex
// encoded representation.
func (t *TraceID) UnmarshalJSON(traceID []byte) error {
	if len(traceID) < 3 {
		return ErrValidTraceIDRequired
	}
	tID, err := TraceIDFromHex(string(traceID[1 : len(traceID)-1]))
	if err != nil {
		return err
	}
	*t = tID
	return nil
}
