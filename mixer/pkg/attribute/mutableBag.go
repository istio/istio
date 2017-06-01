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
	"time"

	"github.com/golang/glog"
	me "github.com/hashicorp/go-multierror"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/mixer/pkg/pool"
)

// MutableBag is a generic mechanism to read and write a set of attributes.
//
// Bags can be chained together in a parent/child relationship. A child bag
// represents a delta over a parent. By default a child looks identical to
// the parent. But as mutations occur to the child, the two start to diverge.
// Resetting a child makes it look identical to its parent again.
type MutableBag struct {
	parent Bag
	values map[string]interface{}
	// number of children of this bag. It will not be recycled unless children count == 0
	children int
	sync.Mutex
}

var mutableBags = sync.Pool{
	New: func() interface{} {
		return &MutableBag{
			values: make(map[string]interface{}),
		}
	},
}

// GetMutableBag returns an initialized bag.
//
// Bags can be chained in a parent/child relationship. You can pass nil if the
// bag has no parent.
//
// When you are done using the mutable bag, call the Done method to recycle it.
func GetMutableBag(parent Bag) *MutableBag {
	mb := mutableBags.Get().(*MutableBag)
	mb.parent = empty

	pp, _ := parent.(*MutableBag)
	if pp != nil {
		pp.Lock()
		pp.children++
		mb.parent = pp
		pp.Unlock()
	}

	return mb
}

// CopyBag makes a deep copy of MutableBag.
func CopyBag(b Bag) *MutableBag {
	mb := GetMutableBag(nil)
	for _, k := range b.Names() {
		v, _ := b.Get(k)
		mb.Set(k, copyValue(v))
	}

	return mb
}

// Given an attribute value, create a deep copy of it
func copyValue(v interface{}) interface{} {
	switch t := v.(type) {
	case []byte:
		c := make([]byte, len(t))
		copy(c, t)
		return c

	case map[string]string:
		c := make(map[string]string, len(t))
		for k2, v2 := range t {
			c[k2] = v2
		}
		return c
	}

	return v
}

// Child allocates a derived mutable bag.
func (mb *MutableBag) Child() *MutableBag {
	// if code tries to create a child after Done
	if mb.parent == nil {
		panic(fmt.Errorf("bag used after scheduled destruction %#v", mb))
	}
	return GetMutableBag(mb)
}

// Done indicates the bag can be reclaimed.
func (mb *MutableBag) Done() {
	mb.Lock()
	if mb.children == 0 {
		mb.done()
	} else {
		glog.Warningf("attempting to free bag with active children %#v", mb)
	}
	mb.Unlock()
}

// done reclaims the bag. always called under a lock to the bag
func (mb *MutableBag) done() {
	mb.Reset()
	mbparent, _ := mb.parent.(*MutableBag)
	if mbparent != nil {
		mbparent.Lock()
		mbparent.children--
		mbparent.Unlock()
	}
	mb.parent = nil
	if glog.V(4) {
		glog.Infof("freed bag: %#v", mb)
	}
	mutableBags.Put(mb)
}

// Get returns an attribute value.
func (mb *MutableBag) Get(name string) (interface{}, bool) {
	var r interface{}
	var b bool
	if r, b = mb.values[name]; !b {
		r, b = mb.parent.Get(name)
	}
	return r, b
}

// Names return the names of all the attributes known to this bag.
func (mb *MutableBag) Names() []string {
	i := 0
	keys := make([]string, len(mb.values))
	for k := range mb.values {
		keys[i] = k
		i++
	}
	return append(keys, mb.parent.Names()...)
}

// Set creates an override for a named attribute.
func (mb *MutableBag) Set(name string, value interface{}) {
	mb.values[name] = value
}

// Reset removes all local state.
func (mb *MutableBag) Reset() {
	// my kingdom for a clear method on maps!
	for k := range mb.values {
		delete(mb.values, k)
	}
}

// Merge combines an array of bags into the current bag.
//
// The individual bags may not contain any conflicting attribute
// values. If that happens, then the merge fails and no mutation
// will have occurred to the current bag.
func (mb *MutableBag) Merge(bags ...*MutableBag) error {
	// first step is to make sure there are no redundant definitions of the same attribute
	keys := make(map[string]bool)
	for _, bag := range bags {
		if bag == nil {
			continue
		}
		for k := range bag.values {
			if keys[k] {
				return fmt.Errorf("conflicting value for attribute %s", k)
			}
			keys[k] = true
		}
	}

	// now that we know there are no conflicting definitions, do the actual merging...
	for _, bag := range bags {
		if bag == nil {
			continue
		}
		for k, v := range bag.values {
			mb.values[k] = copyValue(v)
		}
	}

	return nil
}

type dictState struct {
	globalDict  map[string]int32
	messageDict map[string]int32
}

// ToProto fills-in an Attributes proto based on the content of the bag.
func (mb *MutableBag) ToProto(output *mixerpb.Attributes, globalDict map[string]int32) {
	ds := &dictState{globalDict, nil}
	for k, v := range mb.values {
		index := getIndex(k, ds)

		switch t := v.(type) {
		case string:
			if output.Strings == nil {
				output.Strings = make(map[int32]int32)
			}
			output.Strings[index] = getIndex(t, ds)

		case int64:
			if output.Int64S == nil {
				output.Int64S = make(map[int32]int64)
			}
			output.Int64S[index] = t

		case float64:
			if output.Doubles == nil {
				output.Doubles = make(map[int32]float64)
			}
			output.Doubles[index] = t

		case bool:
			if output.Bools == nil {
				output.Bools = make(map[int32]bool)
			}
			output.Bools[index] = t

		case time.Time:
			if output.Timestamps == nil {
				output.Timestamps = make(map[int32]time.Time)
			}
			output.Timestamps[index] = t

		case time.Duration:
			if output.Durations == nil {
				output.Durations = make(map[int32]time.Duration)
			}
			output.Durations[index] = t

		case []byte:
			if output.Bytes == nil {
				output.Bytes = make(map[int32][]byte)
			}
			output.Bytes[index] = t

		case map[string]string:
			sm := make(map[int32]int32, len(t))
			for smk, smv := range t {
				sm[getIndex(smk, ds)] = getIndex(smv, ds)
			}

			if output.StringMaps == nil {
				output.StringMaps = make(map[int32]mixerpb.StringMap)
			}
			output.StringMaps[index] = mixerpb.StringMap{Entries: sm}
		}
	}

	if len(ds.messageDict) > 0 {
		output.Words = make([]string, len(ds.messageDict))
		for k, v := range ds.messageDict {
			output.Words[-v-1] = k
		}
	}
}

func getIndex(word string, ds *dictState) int32 {
	if index, ok := ds.globalDict[word]; ok {
		return index
	}

	if ds.messageDict == nil {
		ds.messageDict = make(map[string]int32)
	} else if index, ok := ds.messageDict[word]; ok {
		return index
	}

	index := -int32(len(ds.messageDict)) - 1
	ds.messageDict[word] = index
	return index
}

// GetBagFromProto returns an initialized bag from an Attribute proto.
func GetBagFromProto(attrs *mixerpb.Attributes, globalDict []string) (*MutableBag, error) {
	mb := GetMutableBag(nil)
	messageDict := attrs.Words
	var e error
	var name string
	var value string

	// TODO: fail if the proto carries multiple attributes by the same name (but different types)

	var buf *bytes.Buffer
	if glog.V(2) {
		buf = pool.GetBuffer()
		log(buf, "Creating bag from wire attributes:\n")
	}

	log(buf, "  setting string attributes:\n")
	for k, v := range attrs.Strings {
		name, e = lookup(k, e, globalDict, messageDict)
		value, e = lookup(v, e, globalDict, messageDict)
		log(buf, "    %s -> '%s'\n", name, value)
		mb.values[name] = value
	}

	log(buf, "  setting int64 attributes:\n")
	for k, v := range attrs.Int64S {
		name, e = lookup(k, e, globalDict, messageDict)
		log(buf, "    %s -> '%d'\n", name, v)
		mb.values[name] = v
	}

	log(buf, "  setting double attributes:\n")
	for k, v := range attrs.Doubles {
		name, e = lookup(k, e, globalDict, messageDict)
		log(buf, "    %s -> '%f'\n", name, v)
		mb.values[name] = v
	}

	log(buf, "  setting bool attributes:\n")
	for k, v := range attrs.Bools {
		name, e = lookup(k, e, globalDict, messageDict)
		log(buf, "    %s -> '%t'\n", name, v)
		mb.values[name] = v
	}

	log(buf, "  setting timestamp attributes:\n")
	for k, v := range attrs.Timestamps {
		name, e = lookup(k, e, globalDict, messageDict)
		log(buf, "    %s -> '%v'\n", name, v)
		mb.values[name] = v
	}

	log(buf, "  setting duration attributes:\n")
	for k, v := range attrs.Durations {
		name, e = lookup(k, e, globalDict, messageDict)
		log(buf, "    %s -> '%v'\n", name, v)
		mb.values[name] = v
	}

	log(buf, "  setting bytes attributes:\n")
	for k, v := range attrs.Bytes {
		name, e = lookup(k, e, globalDict, messageDict)
		log(buf, "    %s -> '%s'\n", name, v)
		mb.values[name] = v
	}

	log(buf, "  setting string map attributes:\n")
	for k, v := range attrs.StringMaps {
		name, e = lookup(k, e, globalDict, messageDict)
		log(buf, "  %s\n", name)

		sm := make(map[string]string, len(v.Entries))
		for k2, v2 := range v.Entries {
			var name2 string
			var value2 string
			name2, e = lookup(k2, e, globalDict, messageDict)
			value2, e = lookup(v2, e, globalDict, messageDict)
			log(buf, "    %s -> '%v'\n", name2, value2)
			sm[name2] = value2
		}
		mb.values[name] = sm
	}

	if buf != nil {
		glog.Info(buf.String())
		pool.PutBuffer(buf)
	}

	if e != nil {
		return nil, e
	}

	return mb, nil
}

func lookup(index int32, err error, globalDict []string, messageDict []string) (string, error) {
	if index < 0 {
		if -index-1 >= int32(len(messageDict)) {
			return "", me.Append(err, fmt.Errorf("attribute index %d is not defined in the available dictionaries", index))
		}
		return messageDict[-index-1], err
	}

	if index >= int32(len(globalDict)) {
		return "", me.Append(err, fmt.Errorf("attribute index %d is not defined in the available dictionaries", index))
	}

	return globalDict[index], err
}

func log(buf *bytes.Buffer, format string, args ...interface{}) {
	if buf != nil {
		buf.WriteString(fmt.Sprintf(format, args...))
	}
}
