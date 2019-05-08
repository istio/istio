package statsdtest

import (
	"bytes"
	"fmt"
	"strings"
)

// Stat contains the raw and extracted stat information from a stat that was
// sent by the RecordingSender. Raw will always have the content that was
// consumed for this specific stat and Parsed will be set if no errors were hit
// pulling information out of it.
type Stat struct {
	Raw    []byte
	Stat   string
	Value  string
	Tag    string
	Rate   string
	Parsed bool
}

// String fulfils the stringer interface
func (s *Stat) String() string {
	return fmt.Sprintf("%s %s %s", s.Stat, s.Value, s.Rate)
}

// ParseStats takes a sequence of bytes destined for a Statsd server and parses
// it out into one or more Stat structs. Each struct includes both the raw
// bytes (copied, so the src []byte may be reused if desired) as well as each
// component it was able to parse out. If parsing was incomplete Stat.Parsed
// will be set to false but no error is returned / kept.
func ParseStats(src []byte) Stats {
	d := make([]byte, len(src))
	for i, b := range src {
		d[i] = b
	}
	// standard protocol indicates one stat per line
	entries := bytes.Split(d, []byte{'\n'})

	result := make(Stats, len(entries))

	for i, e := range entries {
		result[i] = Stat{Raw: e}
		ss := &result[i]

		// : deliniates the stat name from the stat data
		marker := bytes.IndexByte(e, ':')
		if marker == -1 {
			continue
		}
		ss.Stat = string(e[0:marker])

		// stat data folows ':' with the form {value}|{type tag}[|@{sample rate}]
		e = e[marker+1:]
		marker = bytes.IndexByte(e, '|')
		if marker == -1 {
			continue
		}

		ss.Value = string(e[:marker])

		e = e[marker+1:]
		marker = bytes.IndexByte(e, '|')
		if marker == -1 {
			// no sample rate
			ss.Tag = string(e)
		} else {
			ss.Tag = string(e[:marker])
			e = e[marker+1:]
			if len(e) == 0 || e[0] != '@' {
				// sample rate should be prefixed with '@'; bail otherwise
				continue
			}
			ss.Rate = string(e[1:])
		}

		ss.Parsed = true
	}

	return result
}

// Stats is a slice of Stat
type Stats []Stat

// Unparsed returns any stats that were unable to be completely parsed.
func (s Stats) Unparsed() Stats {
	var r Stats
	for _, e := range s {
		if !e.Parsed {
			r = append(r, e)
		}
	}

	return r
}

// CollectNamed returns all data sent for a given stat name.
func (s Stats) CollectNamed(statName string) Stats {
	return s.Collect(func(e Stat) bool {
		return e.Stat == statName
	})
}

// Collect gathers all stats that make some predicate true.
func (s Stats) Collect(pred func(Stat) bool) Stats {
	var r Stats
	for _, e := range s {
		if pred(e) {
			r = append(r, e)
		}
	}
	return r
}

// Values returns the values associated with this Stats object.
func (s Stats) Values() []string {
	if len(s) == 0 {
		return nil
	}

	r := make([]string, len(s))
	for i, e := range s {
		r[i] = e.Value
	}
	return r
}

// String fulfils the stringer interface
func (s Stats) String() string {
	if len(s) == 0 {
		return ""
	}

	r := make([]string, len(s))
	for i, e := range s {
		r[i] = e.String()
	}
	return strings.Join(r, "\n")
}
