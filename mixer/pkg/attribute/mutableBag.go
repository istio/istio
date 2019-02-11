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

	me "github.com/hashicorp/go-multierror"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/pkg/pool"
	"istio.io/istio/pkg/log"
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

var scope = log.RegisterScope("attributes", "Attribute-related messages.", 0)

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

// GetMutableBagForTesting returns a Mutable bag based on the specified map
// Use this function only for testing purposes.
func GetMutableBagForTesting(values map[string]interface{}) *MutableBag {
	m := GetMutableBag(nil)
	m.values = values
	for k, v := range values {
		if !CheckType(v) {
			panic(fmt.Errorf("unexpected type for the testing bag %T: %q = %q", v, k, v))
		}
	}
	return m
}

// GetProtoForTesting returns a CompressedAttributes struct based on the specified map
// Use this function only for testing purposes.
func GetProtoForTesting(v map[string]interface{}) *mixerpb.CompressedAttributes {
	b := GetMutableBagForTesting(v)
	var ca mixerpb.CompressedAttributes
	b.ToProto(&ca, nil, 0)
	return &ca
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

	case StringMap:
		return t.copyValue()
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

// Contains returns true if the key is present in the bag.
func (mb *MutableBag) Contains(key string) bool {
	if _, found := mb.values[key]; found {
		return true
	}

	return mb.parent.Contains(key)
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
	// prevent use of a bag that's in the pool
	if mb.parent == nil {
		panic(fmt.Errorf("attempt to use a bag after its Done method has been called"))
	}

	if !CheckType(value) {
		panic(fmt.Errorf("invalid type %T for %q with value %v", value, name, value))
	}

	mb.values[name] = value
}

// Delete removes a named item from the local state.
// The item may still be present higher in the hierarchy
func (mb *MutableBag) Delete(name string) {
	delete(mb.values, name)
}

// Reset removes all local state.
func (mb *MutableBag) Reset() {
	mb.values = make(map[string]interface{})
}

// Merge combines an array of bags into the current bag. If the current bag already defines
// a particular attribute, it keeps its value and is not overwritten.
//
// Note that this does a 'shallow' merge. Only the value defined explicitly in the
// mutable bags themselves, and not in any of their parents, are considered.
func (mb *MutableBag) Merge(bag *MutableBag) {
	for k, v := range bag.values {
		// the input bags cannot override values already in the destination bag
		if !mb.Contains(k) {
			mb.values[k] = copyValue(v)
		}
	}
}

// ToProto fills-in an Attributes proto based on the content of the bag.
func (mb *MutableBag) ToProto(output *mixerpb.CompressedAttributes, globalDict map[string]int32, globalWordCount int) {
	ds := newDictState(globalDict, globalWordCount)
	keys := mb.Names()

	for _, k := range keys {
		index := ds.assignDictIndex(k)
		v, found := mb.Get(k)

		// nil can be []byte type, so we should handle not found without type switching
		if !found {
			continue
		}

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

		case int:
			if output.Int64S == nil {
				output.Int64S = make(map[int32]int64)
			}
			output.Int64S[index] = int64(t)

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

		case StringMap:
			sm := make(map[int32]int32, len(t.entries))
			for smk, smv := range t.entries {
				sm[ds.assignDictIndex(smk)] = ds.assignDictIndex(smv)
			}

			if output.StringMaps == nil {
				output.StringMaps = make(map[int32]mixerpb.StringMap)
			}
			output.StringMaps[index] = mixerpb.StringMap{Entries: sm}

		default:
			panic(fmt.Errorf("cannot convert value:%v of type:%T", v, v))
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

// add an attribute to the bag, guarding against duplicate names
func (mb *MutableBag) insertProtoAttr(name string, value interface{},
	seen map[string]struct{}, lg func(string, ...interface{})) error {

	lg("    %s -> '%v'", name, value)
	if _, duplicate := seen[name]; duplicate {
		previous := mb.values[name]
		return fmt.Errorf("duplicate attribute %s (type %T, was %T)", name, value, previous)
	}
	seen[name] = struct{}{}
	mb.values[name] = value
	return nil
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

	// fail if the proto carries multiple attributes by the same name (but different types)
	seen := make(map[string]struct{})

	var buf *bytes.Buffer
	lg := func(format string, args ...interface{}) {}

	if scope.DebugEnabled() {
		buf = pool.GetBuffer()
		lg = func(format string, args ...interface{}) {
			fmt.Fprintf(buf, format, args...)
			buf.WriteString("\n")
		}

		defer func() {
			if buf != nil {
				scope.Debug(buf.String())
				pool.PutBuffer(buf)
			}
		}()
	}

	lg("Updating bag from wire attributes:")

	lg("  setting string attributes:")
	for k, v := range attrs.Strings {
		name, e = lookup(k, e, globalWordList, messageWordList)
		value, e = lookup(v, e, globalWordList, messageWordList)

		// this call cannot fail, given that this is the first map we're iterating through
		_ = mb.insertProtoAttr(name, value, seen, lg)
	}

	lg("  setting int64 attributes:")
	for k, v := range attrs.Int64S {
		name, e = lookup(k, e, globalWordList, messageWordList)
		if err := mb.insertProtoAttr(name, v, seen, lg); err != nil {
			return err
		}
	}

	lg("  setting double attributes:")
	for k, v := range attrs.Doubles {
		name, e = lookup(k, e, globalWordList, messageWordList)
		if err := mb.insertProtoAttr(name, v, seen, lg); err != nil {
			return err
		}
	}

	lg("  setting bool attributes:")
	for k, v := range attrs.Bools {
		name, e = lookup(k, e, globalWordList, messageWordList)
		if err := mb.insertProtoAttr(name, v, seen, lg); err != nil {
			return err
		}
	}

	lg("  setting timestamp attributes:")
	for k, v := range attrs.Timestamps {
		name, e = lookup(k, e, globalWordList, messageWordList)
		if err := mb.insertProtoAttr(name, v, seen, lg); err != nil {
			return err
		}
	}

	lg("  setting duration attributes:")
	for k, v := range attrs.Durations {
		name, e = lookup(k, e, globalWordList, messageWordList)
		if err := mb.insertProtoAttr(name, v, seen, lg); err != nil {
			return err
		}
	}

	lg("  setting bytes attributes:")
	for k, v := range attrs.Bytes {
		name, e = lookup(k, e, globalWordList, messageWordList)
		if err := mb.insertProtoAttr(name, v, seen, lg); err != nil {
			return err
		}
	}

	lg("  setting string map attributes:")
	for k, v := range attrs.StringMaps {
		name, e = lookup(k, e, globalWordList, messageWordList)

		sm := make(map[string]string, len(v.Entries))
		for k2, v2 := range v.Entries {
			var name2 string
			var value2 string
			name2, e = lookup(k2, e, globalWordList, messageWordList)
			value2, e = lookup(v2, e, globalWordList, messageWordList)
			sm[name2] = value2
		}

		v := StringMap{name: name, entries: sm}

		if err := mb.insertProtoAttr(name, v, seen, lg); err != nil {
			return err
		}
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

// String prints out the attributes from the parent bag, then
// walks through the local changes and prints them as well.
func (mb *MutableBag) String() string {
	if len(mb.values) == 0 {
		return mb.parent.String()
	}

	buf := &bytes.Buffer{}
	buf.WriteString(mb.parent.String())
	buf.WriteString("---\n")

	keys := make([]string, 0, len(mb.values))
	for key := range mb.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		fmt.Fprintf(buf, "%-30s: %v\n", key, mb.values[key])
	}
	return buf.String()
}
