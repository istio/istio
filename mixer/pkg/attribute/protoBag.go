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
	"fmt"
	"sync"

	"github.com/golang/glog"

	mixerpb "istio.io/api/mixer/v1"
)

// TODO: consider implementing a pool of proto bags

// ProtoBag implements the Bag interface on top of an Attributes proto.
type ProtoBag struct {
	proto               *mixerpb.Attributes
	globalDict          map[string]int32
	globalWordList      []string
	messageDict         map[string]int32
	convertedStringMaps map[int32]map[string]string
	stringMapMutex      sync.RWMutex
}

// NewProtoBag creates a new proto-based attribute bag.
func NewProtoBag(proto *mixerpb.Attributes, globalDict map[string]int32, globalWordList []string) *ProtoBag {
	return &ProtoBag{
		proto:          proto,
		globalDict:     globalDict,
		globalWordList: globalWordList,
	}
}

// Get returns an attribute value.
func (pb *ProtoBag) Get(name string) (interface{}, bool) {
	// find the dictionary index for the given string
	index, ok := pb.getIndex(name)
	if !ok {
		// the string is not in the dictionary, and hence the attribute is not in the proto either
		return nil, false
	}

	strIndex, ok := pb.proto.Strings[index]
	if ok {
		// found the attribute, now convert its value from a dictionary index to a string
		str, err := pb.lookup(strIndex)
		if err != nil {
			glog.Errorf("string attribute %s: %v", name, err)
			return nil, false
		}

		return str, true
	}

	var value interface{}

	// see if the requested attribte is a string map that's already been converted
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
			glog.Errorf("string map %s: %v", name, err)
			return nil, false
		}

		// cache the converted string map for later calls
		pb.stringMapMutex.Lock()
		if pb.convertedStringMaps == nil {
			pb.convertedStringMaps = make(map[int32]map[string]string)
		}
		pb.convertedStringMaps[index] = m
		pb.stringMapMutex.Unlock()

		return m, true
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
	if index, ok := pb.globalDict[str]; ok {
		return index, true
	}

	md := pb.getMessageDict()

	if index, ok := md[str]; ok {
		return index, true
	}

	return 0, false
}

// given a dictionary index, find the corresponding string if it exists
func (pb *ProtoBag) lookup(index int32) (string, error) {
	if index < 0 {
		if -index-1 < int32(len(pb.proto.Words)) {
			return pb.proto.Words[-index-1], nil
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
	// NOP
}

// Lazily produce the message-level dictionary
func (pb *ProtoBag) getMessageDict() map[string]int32 {
	if pb.messageDict == nil {
		// build the message-level dictionary

		d := make(map[string]int32, len(pb.proto.Words))
		for i, name := range pb.proto.Words {
			// indexes into the message-level dictionary are negative
			d[name] = -int32(i) - 1
		}

		// potentially racy update, but that's fine since all generate maps are equivalent
		pb.messageDict = d
	}

	return pb.messageDict
}
