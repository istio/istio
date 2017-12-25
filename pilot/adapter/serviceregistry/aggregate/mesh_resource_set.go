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

package aggregate

import (
	"math"
)

// Key used to track resources like service or service instance objects maintained by a registry
// in this controller
type resourceKey string

// A unique set of resource keys
type resourceKeySet map[resourceKey]bool

// A map of the value of a label to the resource key
type labelValueKeysMap map[string]resourceKeySet

// A map of a label name to label values and their associated
type nameValueKeysMap map[string]labelValueKeysMap

type resourceLabel struct {
	name  string
	value *string
}

type resourceLabels []resourceLabel

func(k *resourceKey) String() string {
    return string(*k)
}

func resourceLabelsForName(name string) resourceLabels {
	rl := make(resourceLabels, 1)
	rl[0] = resourceLabel{name, nil}
	return rl
}

func (rl *resourceLabels) appendNameValue(name, value string) {
	*rl = append(*rl, resourceLabel{name, &value})
}

func (rl *resourceLabels) appendFrom(other resourceLabels) {
	for _, l := range other {
		*rl = append(*rl, l)
	}
}

func (ks *resourceKeySet) appendFrom(other *resourceKeySet) {
	for k := range *other {
		(*ks)[k] = true
	}
}

func (nameValueKeysMap *nameValueKeysMap) addLabel(k resourceKey, labelName, labelValue string) {
	valueKeySetMap, labelNameFound := (*nameValueKeysMap)[labelName]
	if !labelNameFound {
		valueKeySetMap = make(labelValueKeysMap)
		(*nameValueKeysMap)[labelName] = valueKeySetMap
	}
	keySet, labelValueFound := valueKeySetMap[labelValue]
	if !labelValueFound {
		keySet = make(resourceKeySet)
		valueKeySetMap[labelValue] = keySet
	}
	keySet[k] = true
}

func (nameValueKeysMap *nameValueKeysMap) deleteLabel(k resourceKey, labelName, labelValue string) {
	valueKeySetMap, labelNameFound := (*nameValueKeysMap)[labelName]
	if !labelNameFound {
		return
	}
	keySet, labelValueFound := valueKeySetMap[labelValue]
	if !labelValueFound {
		return
	}
	if keySet != nil {
		delete(keySet, k)
	}
	if len(keySet) > 0 {
		return
	}
	delete(valueKeySetMap, labelValue)
	if len(valueKeySetMap) > 0 {
		return
	}
	delete(*nameValueKeysMap, labelName)
}

func (nameValueKeysMap *nameValueKeysMap) getResourceKeysMatching(labels resourceLabels) resourceKeySet {
	type matchingSet struct {
		labelName  string
		labelValue *string
		keySet     resourceKeySet
	}
	countLabels := len(labels)
	if countLabels == 0 {
		// There must be at least one label else return nothing
		return resourceKeySet{}
	}
	// Note: 0th index has the smallest keySet
	matchingSets := make([]matchingSet, countLabels)
	smallestSetLen := math.MaxInt32
	setIdx := 0
	for _, l := range labels {
		k, v := l.name, l.value
		valueKeysetMap := (*nameValueKeysMap)[k]
		if len(valueKeysetMap) == 0 {
			// Nothing matched at least one label name
			return resourceKeySet{}
		}
		matchingSets[setIdx] = matchingSet{k, v, resourceKeySet{}}
		if v != nil {
			matchingSets[setIdx].keySet = valueKeysetMap[*v]
		} else {
			// We get everything that matches the label name
			// irrespective of the value
			for _, resourceKeySet := range valueKeysetMap {
				matchingSets[setIdx].keySet.appendFrom(&resourceKeySet)
			}
		}
		lenKeySet := len(matchingSets[setIdx].keySet)
		if lenKeySet == 0 {
			// There were no service keys for this label name
			return resourceKeySet{}
		}
		if lenKeySet < smallestSetLen {
			smallestSetLen = lenKeySet
			if setIdx > 0 {
				swappedMatchingSet := matchingSets[0]
				matchingSets[0] = matchingSets[setIdx]
				matchingSets[setIdx] = swappedMatchingSet
			}
		}
		setIdx++
	}
	finalKeySet := matchingSets[0].keySet
	if countLabels > 1 {
		finalKeySet = make(resourceKeySet)
		finalKeySet.appendFrom(&matchingSets[0].keySet)
		for k := range finalKeySet {
			for setIdx := 1; setIdx < countLabels; setIdx++ {
				_, found := matchingSets[setIdx].keySet[k]
				if !found {
					delete(finalKeySet, k)
					break
				}
			}
		}
	}
	return finalKeySet
}
