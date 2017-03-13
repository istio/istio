// Copyright 2016 Istio Authors
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
	"bytes"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang/glog"
	me "github.com/hashicorp/go-multierror"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/mixer/pkg/pool"
)

// The rootBag is always the base of a chain of bags. Its state is updated
// only via an Attributes proto. This code is not protected by locks because
// we know it is always mutated in a single-threaded context.
type rootBag struct {
	strings    map[string]string
	int64s     map[string]int64
	float64s   map[string]float64
	bools      map[string]bool
	times      map[string]time.Time
	durations  map[string]time.Duration
	bytes      map[string][]uint8
	stringMaps map[string]map[string]string
	id         int64 // strictly for use in diagnostic messages
}

var id int64
var rootBags = sync.Pool{
	New: func() interface{} {
		return &rootBag{
			strings:    make(map[string]string),
			int64s:     make(map[string]int64),
			float64s:   make(map[string]float64),
			bools:      make(map[string]bool),
			times:      make(map[string]time.Time),
			durations:  make(map[string]time.Duration),
			bytes:      make(map[string][]uint8),
			stringMaps: make(map[string]map[string]string),
			id:         atomic.AddInt64(&id, 1),
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

func (rb *rootBag) StringMap(name string) (map[string]string, bool) {
	r, b := rb.stringMaps[name]
	return r, b
}

func (rb *rootBag) StringMapKeys() []string {
	i := 0
	keys := make([]string, len(rb.stringMaps))
	for k := range rb.stringMaps {
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

	for k := range rb.stringMaps {
		delete(rb.stringMaps, k)
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

	for k := range attrs.TimestampAttributes {
		if _, present := dictionary[k]; !present {
			e = me.Append(e, fmt.Errorf("attribute index %d is not defined in the current dictionary", k))
		}
	}

	for k := range attrs.DurationAttributes {
		if _, present := dictionary[k]; !present {
			e = me.Append(e, fmt.Errorf("attribute index %d is not defined in the current dictionary", k))
		}
	}

	for k := range attrs.BytesAttributes {
		if _, present := dictionary[k]; !present {
			e = me.Append(e, fmt.Errorf("attribute index %d is not defined in the current dictionary", k))
		}
	}

	for k, v := range attrs.StringMapAttributes {
		if _, present := dictionary[k]; !present {
			e = me.Append(e, fmt.Errorf("attribute index %d is not defined in the current dictionary", k))
		}

		if v != nil {
			for k2 := range v.Map {
				if _, present := dictionary[k2]; !present {
					e = me.Append(e, fmt.Errorf("string map index %d is not defined in the current dictionary", k2))
				}
			}
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

	var log *bytes.Buffer
	if glog.V(2) {
		log = pool.GetBuffer()
	}

	if attrs.ResetContext {
		if log != nil {
			log.WriteString(" resetting bag to empty state\n")
		}
		rb.reset()
	}

	// delete requested attributes
	for _, d := range attrs.DeletedAttributes {
		if name, present := dictionary[d]; present {
			if log != nil {
				log.WriteString(fmt.Sprintf("  attempting to delete attribute %s\n", name))
			}

			delete(rb.strings, name)
			delete(rb.int64s, name)
			delete(rb.float64s, name)
			delete(rb.bools, name)
			delete(rb.times, name)
			delete(rb.durations, name)
			delete(rb.bytes, name)
			delete(rb.stringMaps, name)
		}
	}

	// apply all attributes
	for k, v := range attrs.StringAttributes {
		if log != nil {
			log.WriteString(fmt.Sprintf("  updating string attribute %s from '%v' to '%v'\n", dictionary[k], rb.strings[dictionary[k]], v))
		}
		rb.strings[dictionary[k]] = v
	}

	for k, v := range attrs.Int64Attributes {
		if log != nil {
			log.WriteString(fmt.Sprintf("  updating int64 attribute %s from '%v' to '%v'\n", dictionary[k], rb.int64s[dictionary[k]], v))
		}
		rb.int64s[dictionary[k]] = v
	}

	for k, v := range attrs.DoubleAttributes {
		if log != nil {
			log.WriteString(fmt.Sprintf("  updating double attribute %s from '%v' to '%v'\n", dictionary[k], rb.float64s[dictionary[k]], v))
		}
		rb.float64s[dictionary[k]] = v
	}

	for k, v := range attrs.BoolAttributes {
		if log != nil {
			log.WriteString(fmt.Sprintf("  updating bool attribute %s from '%v' to '%v'\n", dictionary[k], rb.bools[dictionary[k]], v))
		}
		rb.bools[dictionary[k]] = v
	}

	for k, v := range attrs.TimestampAttributes {
		if log != nil {
			log.WriteString(fmt.Sprintf("  updating time attribute %s from '%v' to '%v'\n", dictionary[k], rb.times[dictionary[k]], v))
		}
		rb.times[dictionary[k]] = v
	}

	for k, v := range attrs.DurationAttributes {
		if log != nil {
			log.WriteString(fmt.Sprintf("  updating duration attribute %s from '%v' to '%v'\n", dictionary[k], rb.durations[dictionary[k]], v))
		}
		rb.durations[dictionary[k]] = v
	}

	for k, v := range attrs.BytesAttributes {
		if log != nil {
			log.WriteString(fmt.Sprintf("  updating bytes attribute %s from '%v' to '%v'\n", dictionary[k], rb.bytes[dictionary[k]], v))
		}
		rb.bytes[dictionary[k]] = v
	}

	for k, v := range attrs.StringMapAttributes {
		m := rb.stringMaps[dictionary[k]]
		if m == nil {
			m = make(map[string]string)
			rb.stringMaps[dictionary[k]] = m
		}

		if log != nil {
			log.WriteString(fmt.Sprintf("  updating stringmap attribute %s from\n", dictionary[k]))

			if len(m) > 0 {
				for k2, v2 := range m {
					log.WriteString(fmt.Sprintf("    %s:%s\n", k2, v2))
				}
			} else {
				log.WriteString("    <empty>\n")
			}

			log.WriteString("  to\n")
		}

		if v != nil {
			for k2, v2 := range v.Map {
				m[dictionary[k2]] = v2
			}
		}

		if log != nil {
			if len(m) > 0 {
				for k2, v2 := range m {
					log.WriteString(fmt.Sprintf("    %s:%s\n", k2, v2))
				}
			} else {
				log.WriteString("    <empty>\n")
			}
		}
	}

	if log != nil {
		if log.Len() > 0 {
			glog.Infof("Updatiing attribute bag %d:\n%s", rb.id, log.String())
		}
	}

	return nil
}

func (rb *rootBag) child() *mutableBag {
	return getMutableBag(rb)
}
