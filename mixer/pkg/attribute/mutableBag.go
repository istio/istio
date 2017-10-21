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
	"sort"
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

	if parent == nil {
		mb.parent = empty
	} else {
		mb.parent = parent
	}

	return mb
}

// GetFakeMutableBagForTesting returns a Mutable bag based on the specified map
// Use this function only for testing purposes.
func GetFakeMutableBagForTesting(v map[string]interface{}) *MutableBag {
	m := GetMutableBag(nil)
	m.values = v
	return m
}

// CopyBag makes a deep copy of a bag.
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

// Done indicates the bag can be reclaimed.
func (mb *MutableBag) Done() {
	// prevent use of a bag that's in the pool
	if mb.parent == nil {
		panic(fmt.Errorf("attempt to use a bag after its Done method has been called"))
	}

	mb.parent = nil
	mb.Reset()
	mutableBags.Put(mb)
}

// Get returns an attribute value.
func (mb *MutableBag) Get(name string) (interface{}, bool) {
	// prevent use of a bag that's in the pool
	if mb.parent == nil {
		panic(fmt.Errorf("attempt to use a bag after its Done method has been called"))
	}

	var r interface{}
	var b bool
	if r, b = mb.values[name]; !b {
		r, b = mb.parent.Get(name)
	}
	return r, b
}

// Names returns the names of all the attributes known to this bag.
func (mb *MutableBag) Names() []string {
	if mb == nil {
		return []string{}
	}

	if mb.parent == nil {
		panic(fmt.Errorf("attempt to use a bag after its Done method has been called"))
	}

	parentNames := mb.parent.Names()

	m := make(map[string]bool, len(parentNames)+len(mb.values))
	for _, name := range parentNames {
		m[name] = true
	}

	for name := range mb.values {
		m[name] = true
	}

	i := 0
	names := make([]string, len(m))
	for name := range m {
		names[i] = name
		i++
	}

	return names
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

// PreserveMerge combines an array of bags into the current bag.
//
// Any conflicting attribute values in the input bags are ignored.
func (mb *MutableBag) PreserveMerge(bags ...*MutableBag) error {
	return mb.merge(true, bags)
}

// Merge combines an array of bags into the current bag.
//
// The individual bags may not contain any conflicting attribute
// values. If that happens, then the merge fails and no mutation
// will have occurred to the current bag.
func (mb *MutableBag) Merge(bags ...*MutableBag) error {
	return mb.merge(false, bags)
}

func (mb *MutableBag) merge(skipOverrides bool, bags []*MutableBag) error {
	// first step is to make sure there are no redundant definitions of the same attribute
	keys := make(map[string]bool)
	for _, bag := range bags {
		if bag == nil {
			continue
		}
		for k := range bag.values {
			if keys[k] && !skipOverrides {
				return fmt.Errorf("conflicting value for attribute %s", k)
			}
			keys[k] = true
		}
	}

	names := make(map[string]bool)
	for _, name := range mb.Names() {
		names[name] = true
	}

	for _, bag := range bags {
		if bag == nil {
			continue
		}
		for k, v := range bag.values {
			_, found := names[k]
			if !found {
				mb.values[k] = copyValue(v)
				names[k] = true
			}
		}
	}

	return nil
}

// ToProto fills-in an Attributes proto based on the content of the bag.
func (mb *MutableBag) ToProto(output *mixerpb.CompressedAttributes, globalDict map[string]int32, globalWordCount int) {
	ds := newDictState(globalDict, globalWordCount)

	for k, v := range mb.values {
		index := ds.assignDictIndex(k)

		switch t := v.(type) {
		case string:
			if output.Strings == nil {
				output.Strings = make(map[int32]int32)
			}
			output.Strings[index] = ds.assignDictIndex(t)

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
				sm[ds.assignDictIndex(smk)] = ds.assignDictIndex(smv)
			}

			if output.StringMaps == nil {
				output.StringMaps = make(map[int32]mixerpb.StringMap)
			}
			output.StringMaps[index] = mixerpb.StringMap{Entries: sm}
		}
	}

	output.Words = ds.getMessageWordList()
}

// GetBagFromProto returns an initialized bag from an Attribute proto.
func GetBagFromProto(attrs *mixerpb.CompressedAttributes, globalWordList []string) (*MutableBag, error) {
	mb := GetMutableBag(nil)
	err := mb.UpdateBagFromProto(attrs, globalWordList)
	if err != nil {
		mb.Done()
		return nil, err
	}

	return mb, nil
}

// UpdateBagFromProto refreshes the bag based on the content of the attribute proto.
//
// Note that in the case of semantic errors in the supplied proto which leads to
// an error return, it's likely that the bag will have been partially updated.
func (mb *MutableBag) UpdateBagFromProto(attrs *mixerpb.CompressedAttributes, globalWordList []string) error {
	messageWordList := attrs.Words
	var e error
	var name string
	var value string

	// TODO: fail if the proto carries multiple attributes by the same name (but different types)

	var buf *bytes.Buffer
	log := func(format string, args ...interface{}) {}

	if glog.V(2) {
		buf = pool.GetBuffer()
		log = func(format string, args ...interface{}) {
			fmt.Fprintf(buf, format, args...)
			buf.WriteString("\n")
		}
	}

	log("Updating bag from wire attributes:")

	log("  setting string attributes:")
	for k, v := range attrs.Strings {
		name, e = lookup(k, e, globalWordList, messageWordList)
		value, e = lookup(v, e, globalWordList, messageWordList)
		log("    %s -> '%s'", name, value)
		mb.values[name] = value
	}

	log("  setting int64 attributes:")
	for k, v := range attrs.Int64S {
		name, e = lookup(k, e, globalWordList, messageWordList)
		log("    %s -> '%d'", name, v)
		mb.values[name] = v
	}

	log("  setting double attributes:")
	for k, v := range attrs.Doubles {
		name, e = lookup(k, e, globalWordList, messageWordList)
		log("    %s -> '%f'", name, v)
		mb.values[name] = v
	}

	log("  setting bool attributes:")
	for k, v := range attrs.Bools {
		name, e = lookup(k, e, globalWordList, messageWordList)
		log("    %s -> '%t'", name, v)
		mb.values[name] = v
	}

	log("  setting timestamp attributes:")
	for k, v := range attrs.Timestamps {
		name, e = lookup(k, e, globalWordList, messageWordList)
		log("    %s -> '%v'", name, v)
		mb.values[name] = v
	}

	log("  setting duration attributes:")
	for k, v := range attrs.Durations {
		name, e = lookup(k, e, globalWordList, messageWordList)
		log("    %s -> '%v'", name, v)
		mb.values[name] = v
	}

	log("  setting bytes attributes:")
	for k, v := range attrs.Bytes {
		name, e = lookup(k, e, globalWordList, messageWordList)
		log("    %s -> '%s'", name, v)
		mb.values[name] = v
	}

	log("  setting string map attributes:")
	for k, v := range attrs.StringMaps {
		name, e = lookup(k, e, globalWordList, messageWordList)
		log("  %s", name)

		sm := make(map[string]string, len(v.Entries))
		for k2, v2 := range v.Entries {
			var name2 string
			var value2 string
			name2, e = lookup(k2, e, globalWordList, messageWordList)
			value2, e = lookup(v2, e, globalWordList, messageWordList)
			log("    %s -> '%v'", name2, value2)
			sm[name2] = value2
		}
		mb.values[name] = sm
	}

	if buf != nil {
		glog.Info(buf.String())
		pool.PutBuffer(buf)
	}

	return e
}

func lookup(index int32, err error, globalWordList []string, messageWordList []string) (string, error) {
	if index < 0 {
		slot := indexToSlot(index)
		if slot < len(messageWordList) {
			return messageWordList[slot], err
		}
	} else if index < int32(len(globalWordList)) {
		return globalWordList[index], err
	}

	return "", me.Append(err, fmt.Errorf("attribute index %d is not defined in the available dictionaries", index))
}

// DebugString prints out the attributes from the parent bag, then
// walks through the local changes and prints them as well.
func (mb *MutableBag) DebugString() string {
	if len(mb.values) == 0 {
		return mb.parent.DebugString()
	}

	var buf bytes.Buffer
	buf.WriteString(mb.parent.DebugString())
	buf.WriteString("---\n")

	keys := make([]string, 0, len(mb.values))
	for key := range mb.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		buf.WriteString(fmt.Sprintf("%-30s: %v\n", key, mb.values[key]))
	}
	return buf.String()
}
