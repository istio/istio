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
	"fmt"
	"sync"
	"time"

	mixerpb "istio.io/api/mixer/v1"
)

// Tracker is responsible for implementing the mixer's attribute protocol.
//
// This tracks a set of attributes over time. The tracker keeps a current set of
// attributes which can be mutated in one of two ways. The first way is by
// applying an attribute proto using the ApplyProto method. The second way
// is by applying a bag of attributes.
type Tracker interface {
	// ApplyProto refreshes the set of attributes based on an update proto.
	//
	// This returns a Bag that reflects the current state of the attribute context
	// that the proto targeted.
	//
	// If this returns a non-nil error, it indicates there was a problem in the
	// supplied Attributes proto. When this happens, none of the tracked attribute
	// state will have been affected.
	ApplyProto(attrs *mixerpb.Attributes) (*MutableBag, error)

	// ApplyBag refreshes the tracked attributes with the content of the supplied bag and fills-in an
	// output proto representing the change in state.
	//
	// An error indicates the attribute bag contained some invalid attribute
	// type. In such a case, the output struct will have been left partially updated
	// and be in an effective unusable state.
	ApplyBag(bag *MutableBag, context int32, output *mixerpb.Attributes) error

	// Done indicates the tracker can be reclaimed.
	Done()
}

type tracker struct {
	dictionaries *dictionaries

	// all active attribute contexts
	contexts map[int32]*MutableBag

	// the current live dictionary
	currentDictionary dictionary

	currentRevDictionary map[string]int32
}

var trackers = sync.Pool{
	New: func() interface{} {
		return &tracker{
			contexts: make(map[int32]*MutableBag),
		}
	},
}

func getTracker(dictionaries *dictionaries) *tracker {
	at := trackers.Get().(*tracker)
	at.dictionaries = dictionaries
	return at
}

func (at *tracker) Done() {
	for k, b := range at.contexts {
		b.Done()
		delete(at.contexts, k)
	}

	at.dictionaries.Release(at.currentDictionary)

	at.currentDictionary = nil
	at.dictionaries = nil

	trackers.Put(at)
}

func (at *tracker) ApplyProto(attrs *mixerpb.Attributes) (*MutableBag, error) {
	// find the context or create it if needed
	mb := at.contexts[attrs.AttributeContext]
	if mb == nil {
		mb = GetMutableBag(nil)
		at.contexts[attrs.AttributeContext] = mb
	}

	dict := at.currentDictionary
	if len(attrs.Dictionary) > 0 {
		dict = attrs.Dictionary
	}

	if err := mb.update(dict, attrs); err != nil {
		return nil, err
	}

	// remember any new dictionary for later
	if len(attrs.Dictionary) > 0 {
		at.dictionaries.Release(at.currentDictionary)
		at.currentDictionary = at.dictionaries.Intern(attrs.Dictionary)
		at.currentRevDictionary = nil
	}

	return CopyBag(mb), nil
}

func (at *tracker) ApplyBag(bag *MutableBag, context int32, output *mixerpb.Attributes) error {
	// TODO: this code has not been optimized for speed. For the moment, we just need
	//       something that works reasonably well so we can evaluate usability and
	//       general perf profile of the mixer API. Depending on what we find during
	//       this evaluation, we might either simplify the attribute protocol, at which
	//       point this code will need to be rewritten, or we'll keep the protocol at
	//       which point we'll come back and optimize this code to avoid allocs.

	output.AttributeContext = context
	output.StringAttributes = make(map[int32]string)
	output.Int64Attributes = make(map[int32]int64)
	output.DoubleAttributes = make(map[int32]float64)
	output.BoolAttributes = make(map[int32]bool)
	output.TimestampAttributes = make(map[int32]time.Time)
	output.DurationAttributes = make(map[int32]time.Duration)
	output.BytesAttributes = make(map[int32][]uint8)
	output.StringMapAttributes = make(map[int32]mixerpb.StringMap)
	output.DeletedAttributes = make([]int32, 0)
	output.ResetContext = false

	// get the protocol-level bag
	mb := at.contexts[context]
	if mb == nil {
		mb = GetMutableBag(nil)
		at.contexts[context] = mb
	}

	// determine the set of attributes to delete
	for attrName := range mb.values {
		if _, ok := bag.Get(attrName); !ok {
			index := at.getWordIndex(attrName)
			output.DeletedAttributes = append(output.DeletedAttributes, index)
		}
	}

	if len(output.DeletedAttributes) == len(mb.values) {
		// everything is being deleted, so just reset the context
		output.ResetContext = true
		output.DeletedAttributes = nil
	}

	// determine the set of attributes to update
	for _, attrName := range bag.Names() {
		newValue, _ := bag.Get(attrName)
		oldValue, _ := mb.Get(attrName)

		same, err := compareAttributeValues(newValue, oldValue)
		if err != nil {
			return fmt.Errorf("unable to compare attribute %s: %v", attrName, err)
		}

		if !same {
			mb.values[attrName] = newValue
			index := at.getWordIndex(attrName)

			switch t := newValue.(type) {
			case string:
				output.StringAttributes[index] = t
			case int64:
				output.Int64Attributes[index] = t
			case float64:
				output.DoubleAttributes[index] = t
			case bool:
				output.BoolAttributes[index] = t
			case time.Time:
				output.TimestampAttributes[index] = t
			case time.Duration:
				output.DurationAttributes[index] = t
			case []byte:
				output.BytesAttributes[index] = t
			case map[string]string:
				sm := make(map[int32]string, len(t))
				for smk, smv := range t {
					sm[at.getWordIndex(smk)] = smv
				}
				output.StringMapAttributes[index] = mixerpb.StringMap{Map: sm}
			}
		}
	}

	// if the reverse dictionary was updated, we need to update the protocol dictionary
	if len(at.currentRevDictionary) != len(at.currentDictionary) {
		newDict := make(dictionary, len(at.currentRevDictionary))
		for word, index := range at.currentRevDictionary {
			newDict[index] = word
		}

		at.currentDictionary = at.dictionaries.Intern(newDict)
		output.Dictionary = at.currentDictionary
	}

	return nil
}

func (at *tracker) getWordIndex(newWord string) int32 {
	if at.currentRevDictionary == nil {
		// TODO: this should be interned and shared
		at.currentRevDictionary = make(map[string]int32, len(at.currentDictionary))
		for index, word := range at.currentDictionary {
			at.currentRevDictionary[word] = index
		}
	}

	index, ok := at.currentRevDictionary[newWord]
	if !ok {
		index = int32(len(at.currentRevDictionary))
		at.currentRevDictionary[newWord] = index
	}

	return index
}

func compareAttributeValues(v1, v2 interface{}) (bool, error) {
	var result bool
	switch t1 := v1.(type) {
	case string:
		t2, ok := v2.(string)
		result = ok && t1 == t2
	case int64:
		t2, ok := v2.(int64)
		result = ok && t1 == t2
	case float64:
		t2, ok := v2.(float64)
		result = ok && t1 == t2
	case bool:
		t2, ok := v2.(bool)
		result = ok && t1 == t2
	case time.Time:
		t2, ok := v2.(time.Time)
		result = ok && t1 == t2
	case time.Duration:
		t2, ok := v2.(time.Duration)
		result = ok && t1 == t2

	case []byte:
		t2, ok := v2.([]byte)
		if result = ok && len(t1) == len(t2); result {
			for i := 0; i < len(t1); i++ {
				if t1[i] != t2[i] {
					result = false
					break
				}
			}
		}

	case map[string]string:
		t2, ok := v2.(map[string]string)
		if result = ok && len(t1) == len(t2); result {
			for k, v := range t1 {
				if v != t2[k] {
					result = false
					break
				}
			}
		}

	default:
		return false, fmt.Errorf("unsupported attribute value type: %T", v1)
	}

	return result, nil
}
