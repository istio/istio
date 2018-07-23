package pct

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Percentage is a float64 between 0 and 1. It can be unmarshalled from a JSON
// number between 0 and 1 or a JSON string between "0%" and "100%".
type Percentage float64

func (p Percentage) String() string {
	return fmt.Sprintf("%0.2f%%", p*100)
}

// MarshalJSON encodes the Percentage as a JSON number.
func (p Percentage) MarshalJSON() ([]byte, error) {
	return json.Marshal(float64(p))
}

// UnmarshalJSON converts and validates a JSON string or number to a Percentage.
// If b is a JSON string, it must match X.X%, that is, a float between 0 and 100
// encoded as a string with a trailing '%'. If b is a JSON number, it must be
// between 0 and 1.
func (p *Percentage) UnmarshalJSON(b []byte) (err error) {
	isJSONString := b[0] == '"'
	if isJSONString {
		var s string
		err = json.Unmarshal(b, &s)
		if err != nil {
			return
		}

		*p, err = FromString(s)
		if err != nil {
			return
		}
	} else {
		var f float64
		err = json.Unmarshal(b, &f)
		if err != nil {
			return
		}

		*p, err = FromFloat64(f)
		if err != nil {
			return
		}
	}
	return
}

// FromString converts a string to a Percentage if it is between "0%" and
// "100%".
func FromString(s string) (Percentage, error) {
	percentIndex := strings.Index(s, "%")
	if percentIndex < 0 {
		return 0, InvalidPercentageStringError{s}
	}
	f, err := strconv.ParseFloat(s[:percentIndex], 64)
	if err != nil {
		return 0, InvalidPercentageStringError{s}
	}
	normalizedPercentage := f / 100
	return FromFloat64(normalizedPercentage)
}

// FromFloat64 converts a float64 to a Percentage if it is between 0 and 1.
func FromFloat64(f float64) (p Percentage, err error) {
	isValidPercentage := 0 <= f && f <= 1
	if isValidPercentage {
		p = Percentage(f)
	} else {
		err = OutOfRangeError{f}
	}
	return
}
