package statsdtest

import (
	"errors"
	"sync"
)

// RecordingSender implements statsd.Sender but parses individual Stats into a
// buffer that can be later inspected instead of sending to some server. It
// should constructed with NewRecordingSender().
type RecordingSender struct {
	m      sync.Mutex
	buffer Stats
	closed bool
}

// NewRecordingSender creates a new RecordingSender for use by a statsd.Client.
func NewRecordingSender() *RecordingSender {
	rs := &RecordingSender{}
	rs.buffer = make(Stats, 0)
	return rs
}

// GetSent returns the stats that have been sent. Locks and copies the current
// state of the sent Stats.
//
// The entire buffer of Stat objects (including Stat.Raw is copied).
func (rs *RecordingSender) GetSent() Stats {
	rs.m.Lock()
	defer rs.m.Unlock()

	results := make(Stats, len(rs.buffer))
	for i, e := range rs.buffer {
		results[i] = e
		results[i].Raw = make([]byte, len(e.Raw))
		for j, b := range e.Raw {
			results[i].Raw[j] = b
		}
	}

	return results
}

// ClearSent locks the sender and clears any Stats that have been recorded.
func (rs *RecordingSender) ClearSent() {
	rs.m.Lock()
	defer rs.m.Unlock()

	rs.buffer = rs.buffer[:0]
}

// Send parses the provided []byte into stat objects and then appends these to
// the buffer of sent stats. Buffer operations are synchronized so it is safe
// to call this from multiple goroutines (though contenion will impact
// performance so don't use this during a benchmark). Send treats '\n' as a
// delimiter between multiple sats in the same []byte.
//
// Calling after the Sender has been closed will return an error (and not
// mutate the buffer).
func (rs *RecordingSender) Send(data []byte) (int, error) {
	sent := ParseStats(data)

	rs.m.Lock()
	defer rs.m.Unlock()
	if rs.closed {
		return 0, errors.New("writing to a closed sender")
	}

	rs.buffer = append(rs.buffer, sent...)
	return len(data), nil
}

// Close marks this sender as closed. Subsequent attempts to Send stats will
// result in an error.
func (rs *RecordingSender) Close() error {
	rs.m.Lock()
	defer rs.m.Unlock()

	rs.closed = true
	return nil
}
