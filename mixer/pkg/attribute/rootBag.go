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
	"sync"
	"time"

	ptypes "github.com/gogo/protobuf/types"
	me "github.com/hashicorp/go-multierror"

	mixerpb "istio.io/api/mixer/v1"
)

// The rootBag is always the base of a chain of bags. Its state is updated
// only via an Attributes proto. This code is not protected by locks because
// we know it is always mutated in a single-threaded context.
type rootBag struct {
	strings   map[string]string
	int64s    map[string]int64
	float64s  map[string]float64
	bools     map[string]bool
	times     map[string]time.Time
	durations map[string]time.Duration
	bytes     map[string][]uint8
}

var rootBags = sync.Pool{
	New: func() interface{} {
		return &rootBag{
			strings:   make(map[string]string),
			int64s:    make(map[string]int64),
			float64s:  make(map[string]float64),
			bools:     make(map[string]bool),
			times:     make(map[string]time.Time),
			durations: make(map[string]time.Duration),
			bytes:     make(map[string][]uint8),
		}
	},
}

func getRootBag() *rootBag {
	return rootBags.Get().(*rootBag)
}

func (rb *rootBag) Done() {
	rb.reset()
	rootBags.Put(rb)
}

func (rb *rootBag) String(name string) (string, bool) {
	r, b := rb.strings[name]
	return r, b
}

func (rb *rootBag) StringKeys() []string {
	i := 0
	keys := make([]string, len(rb.strings))
	for k := range rb.strings {
		keys[i] = k
		i++
	}
	return keys
}

func (rb *rootBag) Int64(name string) (int64, bool) {
	r, b := rb.int64s[name]
	return r, b
}

func (rb *rootBag) Int64Keys() []string {
	i := 0
	keys := make([]string, len(rb.int64s))
	for k := range rb.int64s {
		keys[i] = k
		i++
	}
	return keys
}

func (rb *rootBag) Float64(name string) (float64, bool) {
	r, b := rb.float64s[name]
	return r, b
}

func (rb *rootBag) Float64Keys() []string {
	i := 0
	keys := make([]string, len(rb.float64s))
	for k := range rb.float64s {
		keys[i] = k
		i++
	}
	return keys
}

func (rb *rootBag) Bool(name string) (bool, bool) {
	r, b := rb.bools[name]
	return r, b
}

func (rb *rootBag) BoolKeys() []string {
	i := 0
	keys := make([]string, len(rb.bools))
	for k := range rb.bools {
		keys[i] = k
		i++
	}
	return keys
}

func (rb *rootBag) Time(name string) (time.Time, bool) {
	r, b := rb.times[name]
	return r, b
}

func (rb *rootBag) TimeKeys() []string {
	i := 0
	keys := make([]string, len(rb.times))
	for k := range rb.times {
		keys[i] = k
		i++
	}
	return keys
}

func (rb *rootBag) Duration(name string) (time.Duration, bool) {
	r, b := rb.durations[name]
	return r, b
}

func (rb *rootBag) DurationKeys() []string {
	i := 0
	keys := make([]string, len(rb.durations))
	for k := range rb.durations {
		keys[i] = k
		i++
	}
	return keys
}

func (rb *rootBag) Bytes(name string) ([]uint8, bool) {
	r, b := rb.bytes[name]
	return r, b
}

func (rb *rootBag) BytesKeys() []string {
	i := 0
	keys := make([]string, len(rb.bytes))
	for k := range rb.bytes {
		keys[i] = k
		i++
	}
	return keys
}

func (rb *rootBag) reset() {
	// my kingdom for a clear method on maps!

	for k := range rb.strings {
		delete(rb.strings, k)
	}

	for k := range rb.int64s {
		delete(rb.int64s, k)
	}

	for k := range rb.float64s {
		delete(rb.float64s, k)
	}

	for k := range rb.bools {
		delete(rb.bools, k)
	}

	for k := range rb.times {
		delete(rb.times, k)
	}

	for k := range rb.durations {
		delete(rb.durations, k)
	}

	for k := range rb.bytes {
		delete(rb.bytes, k)
	}
}

// Ensure that all dictionary indices are valid and that all values
// are in range.
//
// Note that since we don't have the attribute schema, this doesn't validate
// that a given attribute is being treated as the right type. That is, an
// attribute called 'source.ip' which is of type IP_ADDRESS could be listed as
// a string or an int, and we wouldn't catch it here.
func checkPreconditions(dictionary dictionary, attrs *mixerpb.Attributes) error {
	var e *me.Error

	for k := range attrs.StringAttributes {
		if _, present := dictionary[k]; !present {
			e = me.Append(e, fmt.Errorf("attribute index %d is not defined in the current dictionary", k))
		}
	}

	for k := range attrs.Int64Attributes {
		if _, present := dictionary[k]; !present {
			e = me.Append(e, fmt.Errorf("attribute index %d is not defined in the current dictionary", k))
		}
	}

	for k := range attrs.DoubleAttributes {
		if _, present := dictionary[k]; !present {
			e = me.Append(e, fmt.Errorf("attribute index %d is not defined in the current dictionary", k))
		}
	}

	for k := range attrs.BoolAttributes {
		if _, present := dictionary[k]; !present {
			e = me.Append(e, fmt.Errorf("attribute index %d is not defined in the current dictionary", k))
		}
	}

	for k, v := range attrs.TimestampAttributes {
		if _, present := dictionary[k]; !present {
			e = me.Append(e, fmt.Errorf("attribute index %d is not defined in the current dictionary", k))
		}

		if _, err := ptypes.TimestampFromProto(v); err != nil {
			e = me.Append(e, err)
		}
	}

	for k, v := range attrs.DurationAttributes {
		if _, present := dictionary[k]; !present {
			e = me.Append(e, fmt.Errorf("attribute index %d is not defined in the current dictionary", k))
		}

		if _, err := ptypes.DurationFromProto(v); err != nil {
			e = me.Append(e, err)
		}
	}

	for k := range attrs.BytesAttributes {
		if _, present := dictionary[k]; !present {
			e = me.Append(e, fmt.Errorf("attribute index %d is not defined in the current dictionary", k))
		}
	}

	return e.ErrorOrNil()
}

// Update the state of the bag based on the content of an Attributes struct
func (rb *rootBag) update(dictionary dictionary, attrs *mixerpb.Attributes) error {
	// check preconditions up front and bail if there are any
	// errors without mutating the bag.
	if err := checkPreconditions(dictionary, attrs); err != nil {
		return err
	}

	if attrs.ResetContext {
		rb.reset()
	}

	// apply all attributes
	for k, v := range attrs.StringAttributes {
		rb.strings[dictionary[k]] = v
	}

	for k, v := range attrs.Int64Attributes {
		rb.int64s[dictionary[k]] = v
	}

	for k, v := range attrs.DoubleAttributes {
		rb.float64s[dictionary[k]] = v
	}

	for k, v := range attrs.BoolAttributes {
		rb.bools[dictionary[k]] = v
	}

	for k, v := range attrs.TimestampAttributes {
		rb.times[dictionary[k]], _ = ptypes.TimestampFromProto(v)
	}

	for k, v := range attrs.DurationAttributes {
		rb.durations[dictionary[k]], _ = ptypes.DurationFromProto(v)
	}

	for k, v := range attrs.BytesAttributes {
		rb.bytes[dictionary[k]] = v
	}

	// delete requested attributes
	for _, d := range attrs.DeletedAttributes {
		if name, present := dictionary[d]; present {
			delete(rb.strings, name)
			delete(rb.int64s, name)
			delete(rb.float64s, name)
			delete(rb.bools, name)
			delete(rb.times, name)
			delete(rb.durations, name)
			delete(rb.bytes, name)
		}
	}

	return nil
}

func (rb *rootBag) child() *mutableBag {
	return getMutableBag(rb)
}
