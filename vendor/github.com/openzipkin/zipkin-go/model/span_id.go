package model

import (
	"fmt"
	"strconv"
)

// ID type
type ID uint64

// String outputs the 64-bit ID as hex string.
func (i ID) String() string {
	return fmt.Sprintf("%016x", uint64(i))
}

// MarshalJSON serializes an ID type (SpanID, ParentSpanID) to HEX.
func (i ID) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("%q", i.String())), nil
}

// UnmarshalJSON deserializes an ID type (SpanID, ParentSpanID) from HEX.
func (i *ID) UnmarshalJSON(b []byte) (err error) {
	var id uint64
	if len(b) < 3 {
		return nil
	}
	id, err = strconv.ParseUint(string(b[1:len(b)-1]), 16, 64)
	*i = ID(id)
	return err
}
