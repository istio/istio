// Copyright 2018 Istio Authors
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
