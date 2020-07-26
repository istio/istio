// Copyright Istio Authors
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

	"github.com/hashicorp/go-multierror"

	mixerpb "istio.io/api/mixer/v1"
	attr "istio.io/pkg/attribute"
	"istio.io/pkg/log"
	"istio.io/pkg/pool"
)

// Exports of types for backwards compatibility
type (
	Bag        = attr.Bag
	MutableBag = attr.MutableBag
)

// ProtoBag implements the Bag interface on top of an Attributes proto.
type ProtoBag struct {
	proto               *mixerpb.CompressedAttributes
	globalDict          map[string]int32
	globalWordList      []string
	messageDict         map[string]int32
	convertedStringMaps map[int32]attr.StringMap
	stringMapMutex      sync.RWMutex

	// to keep track of attributes that are referenced
	referencedAttrs      map[attr.Reference]attr.Presence
	referencedAttrsMutex sync.Mutex
}

// referencedAttrsSize is the size of referenced attributes.
const referencedAttrsSize = 16

var protoBags = sync.Pool{
	New: func() interface{} {
		return &ProtoBag{
			referencedAttrs: make(map[attr.Reference]attr.Presence, referencedAttrsSize),
		}
	},
}

// GetProtoBag returns a proto-based attribute bag.
// When you are done using the proto bag, call the Done method to recycle it.
func GetProtoBag(proto *mixerpb.CompressedAttributes, globalDict map[string]int32, globalWordList []string) *ProtoBag {
	pb := protoBags.Get().(*ProtoBag)

	// build the message-level dictionary
	d := make(map[string]int32, len(proto.Words))
	for i, name := range proto.Words {
		d[name] = slotToIndex(i)
	}

	pb.proto = proto
	pb.globalDict = globalDict
	pb.globalWordList = globalWordList
	pb.messageDict = d

	scope.Debugf("Returning bag with attributes:\n%v", pb)

	return pb
}

// Get returns an attribute value.
func (pb *ProtoBag) Get(name string) (interface{}, bool) {
	// find the dictionary index for the given string
	index, ok := pb.getIndex(name)
	if !ok {
		scope.Debugf("Attribute '%s' not in either global or message dictionaries", name)
		// the string is not in the dictionary, and hence the attribute is not in the proto either
		pb.Reference(name, attr.Absence)
		return nil, false
	}

	result, ok := pb.internalGet(name, index)
	if !ok {
		// the named attribute was not present
		pb.Reference(name, attr.Absence)
		return nil, false
	}

	// Do not record StringMap access. Keys in it will be recorded separately.
	if _, smFound := result.(attr.StringMap); !smFound {
		pb.Reference(name, attr.Exact)
	}

	return result, ok
}

// ReferenceTracker for a proto bag
func (pb *ProtoBag) ReferenceTracker() attr.ReferenceTracker {
	return pb
}

// GetReferencedAttributes returns the set of attributes that have been referenced through this bag.
func (pb *ProtoBag) GetReferencedAttributes(globalDict map[string]int32, globalWordCount int) *mixerpb.ReferencedAttributes {
	output := &mixerpb.ReferencedAttributes{}

	ds := newDictState(globalDict, globalWordCount)

	output.AttributeMatches = make([]mixerpb.ReferencedAttributes_AttributeMatch, len(pb.referencedAttrs))
	i := 0
	for k, v := range pb.referencedAttrs {
		mk := int32(0)
		if len(k.MapKey) > 0 {
			mk = ds.assignDictIndex(k.MapKey)
		}
		output.AttributeMatches[i] = mixerpb.ReferencedAttributes_AttributeMatch{
			Name:      ds.assignDictIndex(k.Name),
			MapKey:    mk,
			Condition: mixerpb.ReferencedAttributes_Condition(v),
		}
		i++
	}

	output.Words = ds.getMessageWordList()

	return output
}

// Clear the list of referenced attributes being tracked by this bag
func (pb *ProtoBag) Clear() {
	for k := range pb.referencedAttrs {
		delete(pb.referencedAttrs, k)
	}
}

// Restore the list of referenced attributes being tracked by this bag
func (pb *ProtoBag) Restore(snap attr.ReferencedAttributeSnapshot) {
	ra := make(map[attr.Reference]attr.Presence, len(snap.ReferencedAttrs))
	for k, v := range snap.ReferencedAttrs {
		ra[k] = v
	}
	pb.referencedAttrs = ra
}

// Snapshot grabs a snapshot of the currently referenced attributes
func (pb *ProtoBag) Snapshot() attr.ReferencedAttributeSnapshot {
	var snap attr.ReferencedAttributeSnapshot

	pb.referencedAttrsMutex.Lock()
	snap.ReferencedAttrs = make(map[attr.Reference]attr.Presence, len(pb.referencedAttrs))
	for k, v := range pb.referencedAttrs {
		snap.ReferencedAttrs[k] = v
	}
	pb.referencedAttrsMutex.Unlock()
	return snap
}

func (pb *ProtoBag) MapReference(name string, key string, condition attr.Presence) {
	pb.referencedAttrsMutex.Lock()
	pb.referencedAttrs[attr.Reference{Name: name, MapKey: key}] = condition
	pb.referencedAttrsMutex.Unlock()
}

func (pb *ProtoBag) Reference(name string, condition attr.Presence) {
	pb.referencedAttrsMutex.Lock()
	pb.referencedAttrs[attr.Reference{Name: name}] = condition
	pb.referencedAttrsMutex.Unlock()
}

func (pb *ProtoBag) internalGet(name string, index int32) (interface{}, bool) {
	strIndex, ok := pb.proto.Strings[index]
	if ok {
		// found the attribute, now convert its value from a dictionary index to a string
		str, err := pb.lookup(strIndex)
		if err != nil {
			scope.Errorf("string attribute %s: %v", name, err)
			return nil, false
		}

		return str, true
	}

	var value interface{}

	// see if the requested attribute is a string map that's already been converted
	pb.stringMapMutex.RLock()
	value, ok = pb.convertedStringMaps[index]
	pb.stringMapMutex.RUnlock()

	if ok {
		return value, true
	}

	// now see if its an unconverted string map
	sm, ok := pb.proto.StringMaps[index]
	if ok {
		// convert from map[int32]int32 to map[string]string
		m, err := pb.convertStringMap(sm.Entries)
		if err != nil {
			scope.Errorf("string map %s: %v", name, err)
			return nil, false
		}

		// cache the converted string map for later calls
		ssm := attr.NewStringMap(name, m, pb)
		pb.stringMapMutex.Lock()
		if pb.convertedStringMaps == nil {
			pb.convertedStringMaps = make(map[int32]attr.StringMap)
		}
		pb.convertedStringMaps[index] = ssm
		pb.stringMapMutex.Unlock()

		return ssm, true
	}

	value, ok = pb.proto.Int64S[index]
	if ok {
		return value, true
	}

	value, ok = pb.proto.Doubles[index]
	if ok {
		return value, true
	}

	value, ok = pb.proto.Bools[index]
	if ok {
		return value, true
	}

	value, ok = pb.proto.Timestamps[index]
	if ok {
		return value, true
	}

	value, ok = pb.proto.Durations[index]
	if ok {
		return value, true
	}

	value, ok = pb.proto.Bytes[index]
	if ok {
		return value, true
	}

	// not found
	return nil, false
}

// given a string, find the corresponding dictionary index if it exists
func (pb *ProtoBag) getIndex(str string) (int32, bool) {
	if index, ok := pb.messageDict[str]; ok {
		return index, true
	}

	if index, ok := pb.globalDict[str]; ok {
		return index, true
	}

	return 0, false
}

// given a dictionary index, find the corresponding string if it exists
func (pb *ProtoBag) lookup(index int32) (string, error) {
	if index < 0 {
		slot := indexToSlot(index)
		if slot < len(pb.proto.Words) {
			return pb.proto.Words[slot], nil
		}
	} else if index < int32(len(pb.globalWordList)) {
		return pb.globalWordList[index], nil
	}

	return "", fmt.Errorf("string index %d is not defined in the available dictionaries", index)
}

// convert a map[int32]int32 into a map[string]string, where the int32 are dictionary indices
func (pb *ProtoBag) convertStringMap(s map[int32]int32) (map[string]string, error) {
	d := make(map[string]string, len(s))
	for k, v := range s {
		key, err := pb.lookup(k)
		if err != nil {
			return nil, err
		}

		value, err := pb.lookup(v)
		if err != nil {
			return nil, err
		}

		d[key] = value
	}

	return d, nil
}

// Contains returns true if protobag contains this key.
func (pb *ProtoBag) Contains(key string) bool {
	idx, found := pb.getIndex(key)
	if !found {
		return false
	}

	if _, ok := pb.proto.Strings[idx]; ok {
		return true
	}

	if _, ok := pb.proto.StringMaps[idx]; ok {
		return true
	}

	if _, ok := pb.proto.Int64S[idx]; ok {
		return true
	}

	if _, ok := pb.proto.Doubles[idx]; ok {
		return true
	}

	if _, ok := pb.proto.Bools[idx]; ok {
		return true
	}

	if _, ok := pb.proto.Timestamps[idx]; ok {
		return true
	}

	if _, ok := pb.proto.Durations[idx]; ok {
		return true
	}

	if _, ok := pb.proto.Bytes[idx]; ok {
		return true
	}

	return false
}

// Names returns the names of all the attributes known to this bag.
func (pb *ProtoBag) Names() []string {
	names := make(map[string]bool)

	for k := range pb.proto.Strings {
		if name, err := pb.lookup(k); err == nil {
			names[name] = true
		}
	}

	for k := range pb.proto.Int64S {
		if name, err := pb.lookup(k); err == nil {
			names[name] = true
		}
	}

	for k := range pb.proto.Doubles {
		if name, err := pb.lookup(k); err == nil {
			names[name] = true
		}
	}

	for k := range pb.proto.Bools {
		if name, err := pb.lookup(k); err == nil {
			names[name] = true
		}
	}

	for k := range pb.proto.Timestamps {
		if name, err := pb.lookup(k); err == nil {
			names[name] = true
		}
	}

	for k := range pb.proto.Durations {
		if name, err := pb.lookup(k); err == nil {
			names[name] = true
		}
	}

	for k := range pb.proto.Bytes {
		if name, err := pb.lookup(k); err == nil {
			names[name] = true
		}
	}

	for k := range pb.proto.StringMaps {
		if name, err := pb.lookup(k); err == nil {
			names[name] = true
		}
	}

	n := make([]string, len(names))
	i := 0
	for name := range names {
		n[i] = name
		i++
	}
	return n
}

// Done indicates the bag can be reclaimed.
func (pb *ProtoBag) Done() {
	pb.Reset()
	protoBags.Put(pb)
}

// Reset removes all local state.
func (pb *ProtoBag) Reset() {
	pb.proto = nil
	pb.globalDict = make(map[string]int32)
	pb.globalWordList = nil
	pb.messageDict = make(map[string]int32)
	pb.stringMapMutex.Lock()
	pb.convertedStringMaps = make(map[int32]attr.StringMap)
	pb.stringMapMutex.Unlock()
	pb.referencedAttrsMutex.Lock()
	pb.referencedAttrs = make(map[attr.Reference]attr.Presence, referencedAttrsSize)
	pb.referencedAttrsMutex.Unlock()
}

// String runs through the named attributes, looks up their values,
// and prints them to a string.
func (pb *ProtoBag) String() string {
	buf := &bytes.Buffer{}

	names := pb.Names()
	sort.Strings(names)

	for _, name := range names {
		// find the dictionary index for the given string
		index, _ := pb.getIndex(name)
		if result, ok := pb.internalGet(name, index); ok {
			fmt.Fprintf(buf, "%-30s: %v\n", name, result)
		}
	}
	return buf.String()
}

var scope = log.RegisterScope("attributes", "Attribute-related messages.", 0)

// GetProtoForTesting returns a CompressedAttributes struct based on the specified map
// Use this function only for testing purposes.
func GetProtoForTesting(v map[string]interface{}) *mixerpb.CompressedAttributes {
	b := attr.GetMutableBagForTesting(v)
	var ca mixerpb.CompressedAttributes
	ToProto(b, &ca, nil, 0)
	return &ca
}

// ToProto fills-in an Attributes proto based on the content of the bag.
func ToProto(mb *attr.MutableBag, output *mixerpb.CompressedAttributes, globalDict map[string]int32, globalWordCount int) {
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

		case attr.StringMap:
			sm := make(map[int32]int32, len(t.Entries()))
			for smk, smv := range t.Entries() {
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
func GetBagFromProto(attrs *mixerpb.CompressedAttributes, globalWordList []string) (*attr.MutableBag, error) {
	mb := attr.GetMutableBag(nil)
	err := UpdateBagFromProto(mb, attrs, globalWordList)
	if err != nil {
		mb.Done()
		return nil, err
	}

	return mb, nil
}

// add an attribute to the bag, guarding against duplicate names
func insertProtoAttr(mb *attr.MutableBag, name string, value interface{},
	seen map[string]struct{}, lg func(string, ...interface{})) error {

	lg("    %s -> '%v'", name, value)
	if _, duplicate := seen[name]; duplicate {
		return fmt.Errorf("duplicate attribute %s (type %T)", name, value)
	}
	seen[name] = struct{}{}
	mb.Set(name, value)
	return nil
}

// UpdateBagFromProto refreshes the bag based on the content of the attribute proto.
//
// Note that in the case of semantic errors in the supplied proto which leads to
// an error return, it's likely that the bag will have been partially updated.
func UpdateBagFromProto(mb *attr.MutableBag, attrs *mixerpb.CompressedAttributes, globalWordList []string) error {
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
		_ = insertProtoAttr(mb, name, value, seen, lg)
	}

	lg("  setting int64 attributes:")
	for k, v := range attrs.Int64S {
		name, e = lookup(k, e, globalWordList, messageWordList)
		if err := insertProtoAttr(mb, name, v, seen, lg); err != nil {
			return err
		}
	}

	lg("  setting double attributes:")
	for k, v := range attrs.Doubles {
		name, e = lookup(k, e, globalWordList, messageWordList)
		if err := insertProtoAttr(mb, name, v, seen, lg); err != nil {
			return err
		}
	}

	lg("  setting bool attributes:")
	for k, v := range attrs.Bools {
		name, e = lookup(k, e, globalWordList, messageWordList)
		if err := insertProtoAttr(mb, name, v, seen, lg); err != nil {
			return err
		}
	}

	lg("  setting timestamp attributes:")
	for k, v := range attrs.Timestamps {
		name, e = lookup(k, e, globalWordList, messageWordList)
		if err := insertProtoAttr(mb, name, v, seen, lg); err != nil {
			return err
		}
	}

	lg("  setting duration attributes:")
	for k, v := range attrs.Durations {
		name, e = lookup(k, e, globalWordList, messageWordList)
		if err := insertProtoAttr(mb, name, v, seen, lg); err != nil {
			return err
		}
	}

	lg("  setting bytes attributes:")
	for k, v := range attrs.Bytes {
		name, e = lookup(k, e, globalWordList, messageWordList)
		if err := insertProtoAttr(mb, name, v, seen, lg); err != nil {
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

		v := attr.NewStringMap(name, sm, nil)

		if err := insertProtoAttr(mb, name, v, seen, lg); err != nil {
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

	return "", multierror.Append(err, fmt.Errorf("attribute index %d is not defined in the available dictionaries", index))
}
