package size

import (
	"encoding/json"

	units "github.com/docker/go-units"
)

// ByteSize represents a number of bytes. It can be unmarshalled from a JSON
// number or a JSON string such as "10k" or "16mb" or "32 PB".
type ByteSize uint64

func (z ByteSize) String() string {
	return units.BytesSize(float64(z))
}

// MarshalJSON encodes the ByteSize as a JSON string.
func (z ByteSize) MarshalJSON() ([]byte, error) {
	return json.Marshal(z.String())
}

// UnmarshalJSON converts a JSON number or string to a ByteSize. If b is a JSON
// number, it must be a positive integer. If b is a JSON string, it must be
// parsable by units.RAMInBytes.
func (z *ByteSize) UnmarshalJSON(b []byte) (err error) {
	isJSONString := b[0] == '"'
	if isJSONString {
		var s string
		err = json.Unmarshal(b, &s)
		if err != nil {
			return
		}
		*z, err = FromString(s)
		if err != nil {
			return
		}
	} else {
		var x int64
		err = json.Unmarshal(b, &x)
		if err != nil {
			return
		}
		*z, err = FromInt64(x)
		if err != nil {
			return
		}
	}
	return
}

// FromString converts a string like "10 mb" or "16k" to a ByteSize. See
// units.RAMInBytes for more details.
func FromString(s string) (size ByteSize, err error) {
	x, err := units.RAMInBytes(s)
	if err != nil {
		return
	}
	return FromInt64(x)
}

// FromInt64 converts an int64 to a ByteSize if it is non-negative.
func FromInt64(x int64) (size ByteSize, err error) {
	if x < 0 {
		err = NegativeSizeError{x}
	} else {
		size = ByteSize(uint64(x))
	}
	return
}
