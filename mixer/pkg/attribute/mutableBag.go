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
	id     int64 // strictly for use in diagnostic messages
	// number of children of this bag. It will not be recycled unless children count == 0
	children int
	sync.Mutex
}

var id int64
var mutableBags = sync.Pool{
	New: func() interface{} {
		return &MutableBag{
			values: make(map[string]interface{}),
			id:     atomic.AddInt64(&id, 1),
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

		for k2 := range v.Map {
			if _, present := dictionary[k2]; !present {
				e = me.Append(e, fmt.Errorf("string map index %d is not defined in the current dictionary", k2))
			}
		}
	}

	// TODO: we should catch the case where the same attribute is being repeated in different types
	//       (that is, an attribute called FOO which is both an int and a string for example)

	return e.ErrorOrNil()
}

// Update the state of the bag based on the content of an Attributes struct
func (mb *MutableBag) update(dictionary dictionary, attrs *mixerpb.Attributes) error {
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
		mb.Reset()
	}

	// delete requested attributes
	for _, d := range attrs.DeletedAttributes {
		if name, present := dictionary[d]; present {
			if log != nil {
				log.WriteString(fmt.Sprintf("  attempting to delete attribute %s\n", name))
			}

			delete(mb.values, name)
		}
	}

	// apply all attributes
	for k, v := range attrs.StringAttributes {
		if log != nil {
			log.WriteString(fmt.Sprintf("  updating string attribute %s from '%v' to '%v'\n", dictionary[k], mb.values[dictionary[k]], v))
		}
		mb.values[dictionary[k]] = v
	}

	for k, v := range attrs.Int64Attributes {
		if log != nil {
			log.WriteString(fmt.Sprintf("  updating int64 attribute %s from '%v' to '%v'\n", dictionary[k], mb.values[dictionary[k]], v))
		}
		mb.values[dictionary[k]] = v
	}

	for k, v := range attrs.DoubleAttributes {
		if log != nil {
			log.WriteString(fmt.Sprintf("  updating double attribute %s from '%v' to '%v'\n", dictionary[k], mb.values[dictionary[k]], v))
		}
		mb.values[dictionary[k]] = v
	}

	for k, v := range attrs.BoolAttributes {
		if log != nil {
			log.WriteString(fmt.Sprintf("  updating bool attribute %s from '%v' to '%v'\n", dictionary[k], mb.values[dictionary[k]], v))
		}
		mb.values[dictionary[k]] = v
	}

	for k, v := range attrs.TimestampAttributes {
		if log != nil {
			log.WriteString(fmt.Sprintf("  updating time attribute %s from '%v' to '%v'\n", dictionary[k], mb.values[dictionary[k]], v))
		}
		mb.values[dictionary[k]] = v
	}

	for k, v := range attrs.DurationAttributes {
		if log != nil {
			log.WriteString(fmt.Sprintf("  updating duration attribute %s from '%v' to '%v'\n", dictionary[k], mb.values[dictionary[k]], v))
		}
		mb.values[dictionary[k]] = v
	}

	for k, v := range attrs.BytesAttributes {
		if log != nil {
			log.WriteString(fmt.Sprintf("  updating bytes attribute %s from '%v' to '%v'\n", dictionary[k], mb.values[dictionary[k]], v))
		}
		mb.values[dictionary[k]] = v
	}

	for k, v := range attrs.StringMapAttributes {
		m, ok := mb.values[dictionary[k]].(map[string]string)
		if !ok {
			m = make(map[string]string, len(v.Map))
			mb.values[dictionary[k]] = m
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

		for k2, v2 := range v.Map {
			m[dictionary[k2]] = v2
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
			glog.Infof("Updating attribute bag %d:\n%s", mb.id, log.String())
		}
	}

	return nil
}
