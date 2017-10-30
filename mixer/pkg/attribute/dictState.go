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

type dictState struct {
	globalDict      map[string]int32
	messageDict     map[string]int32
	globalWordCount int
}

// newDictState initializes a new dictionary state. Given a set of words,
// this code creates a message word list suitable for use in an Attributes proto.
//
// globalWordCount indicates the maximum allowed global dictionary index to
// use.
func newDictState(globalDict map[string]int32, globalWordCount int) *dictState {
	return &dictState{
		globalDict:      globalDict,
		globalWordCount: globalWordCount,
	}
}

// assignDictIndex determines whether a word is in the allowed subset of the
// global word list or whether it should be added to the message word list
// being accumulated. It then returns the appropriate dictionary index given
// the selected word list.
func (ds *dictState) assignDictIndex(word string) int32 {
	if index, ok := ds.globalDict[word]; ok {

		// ensure the returned index doesn't exceed the max we're allowed
		if index < int32(ds.globalWordCount) {
			return index
		}
	}

	if ds.messageDict == nil {
		ds.messageDict = make(map[string]int32)
	} else if index, ok := ds.messageDict[word]; ok {
		return index
	}

	index := slotToIndex(len(ds.messageDict))
	ds.messageDict[word] = index
	return index
}

// getMessageWordList returns the list of words suitable for use
// in an Attributes message.
func (ds *dictState) getMessageWordList() []string {
	if len(ds.messageDict) == 0 {
		return nil
	}

	words := make([]string, len(ds.messageDict))
	for k, v := range ds.messageDict {
		words[indexToSlot(v)] = k
	}

	return words
}

// slotToIndex converts from a message word list slot into a dictionary index
func slotToIndex(slot int) int32 {
	return int32(-slot - 1)
}

// indexToSlot converts from a dictionary index into a message word list slot
func indexToSlot(index int32) int {
	return int(-index - 1)
}
