// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package util

import (
	"bytes"
	"encoding/json"
	"io"
)

// UnmarshalJSON parses the JSON encoded data and stores the result in the value
// pointed to by x.
//
// This function is intended to be used in place of the standard json.Marshal
// function when json.Number is required.
func UnmarshalJSON(bs []byte, x interface{}) (err error) {
	buf := bytes.NewBuffer(bs)
	decoder := NewJSONDecoder(buf)
	return decoder.Decode(x)
}

// NewJSONDecoder returns a new decoder that reads from r.
//
// This function is intended to be used in place of the standard json.NewDecoder
// when json.Number is required.
func NewJSONDecoder(r io.Reader) *json.Decoder {
	decoder := json.NewDecoder(r)
	decoder.UseNumber()
	return decoder
}

// MustUnmarshalJSON parse the JSON encoded data and returns the result.
//
// If the data cannot be decoded, this function will panic. This function is for
// test purposes.
func MustUnmarshalJSON(bs []byte) interface{} {
	var x interface{}
	if err := UnmarshalJSON(bs, &x); err != nil {
		panic(err)
	}
	return x
}

// MustMarshalJSON returns the JSON encoding of x
//
// If the data cannot be encoded, this function will panic. This function is for
// test purposes.
func MustMarshalJSON(x interface{}) []byte {
	bs, err := json.Marshal(x)
	if err != nil {
		panic(err)
	}
	return bs
}
