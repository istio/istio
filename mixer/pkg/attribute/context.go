// Copyright 2016 Google Inc.
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

package attribute

import (
	"fmt"
	"time"

	"github.com/golang/protobuf/ptypes"

	mixerpb "istio.io/mixer/api/v1"
)

// This code would be quite a bit nicer with generics... Oh well.

// Context maintains an independent set of attributes.
type Context interface {
	// String returns the named attribute if it exists.
	String(name string) (string, bool)

	// Int64 returns the named attribute if it exists.
	Int64(name string) (int64, bool)

	// Float64 returns the named attribute if it exists.
	Float64(name string) (float64, bool)

	// Bool returns the named attribute if it exists.
	Bool(name string) (bool, bool)

	// Time returns the named attribute if it exists.
	Time(name string) (time.Time, bool)

	// Bytes returns the named attribute if it exists.
	Bytes(name string) ([]uint8, bool)
}

type context struct {
	strings  map[string]string
	int64s   map[string]int64
	float64s map[string]float64
	bools    map[string]bool
	times    map[string]time.Time
	bytes    map[string][]uint8
}

func newContext() *context {
	return &context{}
}

// Ensure that all dictionary indices are valid and that all values
// are in range.
//
// Note that since we don't have the attribute schema, this doesn't validate
// that a given attribute is being treated as the right type. That is, an
// attribute called 'source.ip' which is of type IP_ADDRESS could be listed as
// a string or an int, and we wouldn't catch it here.
func checkPreconditions(dictionary dictionary, attrs *mixerpb.Attributes) error {
	for k := range attrs.StringAttributes {
		if _, present := dictionary[k]; !present {
			return fmt.Errorf("attribute index %d is not defined in the current dictionary", k)
		}
	}

	for k := range attrs.Int64Attributes {
		if _, present := dictionary[k]; !present {
			return fmt.Errorf("attribute index %d is not defined in the current dictionary", k)
		}
	}

	for k := range attrs.DoubleAttributes {
		if _, present := dictionary[k]; !present {
			return fmt.Errorf("attribute index %d is not defined in the current dictionary", k)
		}
	}

	for k := range attrs.BoolAttributes {
		if _, present := dictionary[k]; !present {
			return fmt.Errorf("attribute index %d is not defined in the current dictionary", k)
		}
	}

	for k, v := range attrs.TimestampAttributes {
		if _, present := dictionary[k]; !present {
			return fmt.Errorf("attribute index %d is not defined in the current dictionary", k)
		}

		if _, err := ptypes.Timestamp(v); err != nil {
			return err
		}
	}

	for k := range attrs.BytesAttributes {
		if _, present := dictionary[k]; !present {
			return fmt.Errorf("attribute index %d is not defined in the current dictionary", k)
		}
	}

	return nil
}

func (ac *context) update(dictionary dictionary, attrs *mixerpb.Attributes) error {
	// check preconditions up front and bail if there are any
	// errors without mutating the context.
	if err := checkPreconditions(dictionary, attrs); err != nil {
		return err
	}

	// apply all attributes
	if attrs.StringAttributes != nil {
		if ac.strings == nil {
			ac.strings = make(map[string]string)
		}

		for k, v := range attrs.StringAttributes {
			ac.strings[dictionary[k]] = v
		}
	}

	if attrs.Int64Attributes != nil {
		if ac.int64s == nil {
			ac.int64s = make(map[string]int64)
		}

		for k, v := range attrs.Int64Attributes {
			ac.int64s[dictionary[k]] = v
		}
	}

	if attrs.DoubleAttributes != nil {
		if ac.float64s == nil {
			ac.float64s = make(map[string]float64)
		}

		for k, v := range attrs.DoubleAttributes {
			ac.float64s[dictionary[k]] = v
		}
	}

	if attrs.BoolAttributes != nil {
		if ac.bools == nil {
			ac.bools = make(map[string]bool)
		}

		for k, v := range attrs.BoolAttributes {
			ac.bools[dictionary[k]] = v
		}
	}

	if attrs.TimestampAttributes != nil {
		if ac.times == nil {
			ac.times = make(map[string]time.Time)
		}

		for k, v := range attrs.TimestampAttributes {
			t, _ := ptypes.Timestamp(v)
			ac.times[dictionary[k]] = t
		}
	}

	if attrs.BytesAttributes != nil {
		if ac.bytes == nil {
			ac.bytes = make(map[string][]uint8)
		}

		for k, v := range attrs.BytesAttributes {
			ac.bytes[dictionary[k]] = v
		}
	}

	// delete requested attributes
	for _, d := range attrs.DeletedAttributes {
		if name, present := dictionary[d]; present {
			delete(ac.strings, name)
			delete(ac.int64s, name)
			delete(ac.float64s, name)
			delete(ac.bools, name)
			delete(ac.times, name)
			delete(ac.bytes, name)
		}
	}

	return nil
}

func (ac *context) reset() {
	if len(ac.strings) > 0 {
		ac.strings = make(map[string]string)
	}

	if len(ac.int64s) > 0 {
		ac.int64s = make(map[string]int64)
	}

	if len(ac.float64s) > 0 {
		ac.float64s = make(map[string]float64)
	}

	if len(ac.bools) > 0 {
		ac.bools = make(map[string]bool)
	}

	if len(ac.times) > 0 {
		ac.times = make(map[string]time.Time)
	}

	if len(ac.bytes) > 0 {
		ac.bytes = make(map[string][]uint8)
	}
}

func (ac *context) String(name string) (string, bool) {
	r, b := ac.strings[name]
	return r, b
}

func (ac *context) Int64(name string) (int64, bool) {
	r, b := ac.int64s[name]
	return r, b
}

func (ac *context) Float64(name string) (float64, bool) {
	r, b := ac.float64s[name]
	return r, b
}

func (ac *context) Bool(name string) (bool, bool) {
	r, b := ac.bools[name]
	return r, b
}

func (ac *context) Time(name string) (time.Time, bool) {
	r, b := ac.times[name]
	return r, b
}

func (ac *context) Bytes(name string) ([]uint8, bool) {
	r, b := ac.bytes[name]
	return r, b
}
