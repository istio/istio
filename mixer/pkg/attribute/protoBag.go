// Copyright 2017 Istio Authors
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

	mixerpb "istio.io/api/mixer/v1"
)

type attributeRef struct {
	Name   string
	MapKey string
}

// ReferencedAttributeSnapshot keeps track of the attribute reference state for a mutable bag.
// You can snapshot the referenced attributes with SnapshotReferencedAttributes and later
// reinstall them with RestoreReferencedAttributes. Note that a snapshot can only be used
// once, the RestoreReferencedAttributes call is destructive.
type ReferencedAttributeSnapshot struct {
	referencedAttrs map[attributeRef]mixerpb.ReferencedAttributes_Condition
}

// ProtoBag implements the Bag interface on top of an Attributes proto.
type ProtoBag struct {
	proto               *mixerpb.CompressedAttributes
	globalDict          map[string]int32
	globalWordList      []string
	messageDict         map[string]int32
	convertedStringMaps map[int32]StringMap
	stringMapMutex      sync.RWMutex

	// to keep track of attributes that are referenced
	referencedAttrs      map[attributeRef]mixerpb.ReferencedAttributes_Condition
	referencedAttrsMutex sync.Mutex
}

// referencedAttrsSize is the size of referenced attributes.
const referencedAttrsSize = 16

var protoBags = sync.Pool{
	New: func() interface{} {
		return &ProtoBag{
			referencedAttrs: make(map[attributeRef]mixerpb.ReferencedAttributes_Condition, referencedAttrsSize),
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
		pb.trackReference(name, mixerpb.ABSENCE)
		return nil, false
	}

	result, ok := pb.internalGet(name, index)
	if !ok {
		// the named attribute was not present
		pb.trackReference(name, mixerpb.ABSENCE)
		return nil, false
	}

	// Do not record StringMap access. Keys in it will be recorded separately.
	if _, smFound := result.(StringMap); !smFound {
		pb.trackReference(name, mixerpb.EXACT)
	}

	return result, ok
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
			Condition: v,
		}
		i++
	}

	output.Words = ds.getMessageWordList()

	return output
}

// ClearReferencedAttributes clears the list of referenced attributes being tracked by this bag
func (pb *ProtoBag) ClearReferencedAttributes() {
	for k := range pb.referencedAttrs {
		delete(pb.referencedAttrs, k)
	}
}

// RestoreReferencedAttributes sets the list of referenced attributes being tracked by this bag
func (pb *ProtoBag) RestoreReferencedAttributes(snap ReferencedAttributeSnapshot) {
	ra := make(map[attributeRef]mixerpb.ReferencedAttributes_Condition, len(snap.referencedAttrs))
	for k, v := range snap.referencedAttrs {
		ra[k] = v
	}
	pb.referencedAttrs = ra
}

// SnapshotReferencedAttributes grabs a snapshot of the currently referenced attributes
func (pb *ProtoBag) SnapshotReferencedAttributes() ReferencedAttributeSnapshot {
	var snap ReferencedAttributeSnapshot

	pb.referencedAttrsMutex.Lock()
	snap.referencedAttrs = make(map[attributeRef]mixerpb.ReferencedAttributes_Condition, len(pb.referencedAttrs))
	for k, v := range pb.referencedAttrs {
		snap.referencedAttrs[k] = v
	}
	pb.referencedAttrsMutex.Unlock()
	return snap
}

func (pb *ProtoBag) trackMapReference(name string, key string, condition mixerpb.ReferencedAttributes_Condition) {
	pb.referencedAttrsMutex.Lock()
	pb.referencedAttrs[attributeRef{Name: name, MapKey: key}] = condition
	pb.referencedAttrsMutex.Unlock()
}

func (pb *ProtoBag) trackReference(name string, condition mixerpb.ReferencedAttributes_Condition) {
	pb.referencedAttrsMutex.Lock()
	pb.referencedAttrs[attributeRef{Name: name}] = condition
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
		ssm := StringMap{name: name, entries: m, pb: pb}
		pb.stringMapMutex.Lock()
		if pb.convertedStringMaps == nil {
			pb.convertedStringMaps = make(map[int32]StringMap)
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
	pb.convertedStringMaps = make(map[int32]StringMap)
	pb.stringMapMutex.Unlock()
	pb.referencedAttrsMutex.Lock()
	pb.referencedAttrs = make(map[attributeRef]mixerpb.ReferencedAttributes_Condition, referencedAttrsSize)
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
