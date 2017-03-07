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
	"sync"

	mixerpb "istio.io/api/mixer/v1"
)

// Tracker is responsible for tracking a set of live attributes over time.
//
// An instance of this type is created for every gRPC stream incoming to the
// mixer. The instance tracks a current dictionary along with a set of
// attribute contexts.
type Tracker interface {
	// StartRequest refreshes the set of attributes tracked based on an incoming proto.
	//
	// This returns a Bag that can be used to query and update the current
	// set of attributes.
	//
	// If this returns a non-nil error, it indicates there was a problem in the
	// supplied Attributes proto. When this happens, none of the tracked attribute
	// state will have been affected.
	StartRequest(attrs *mixerpb.Attributes) (MutableBag, error)

	// EndRequest performs post-request cleanup
	EndRequest()

	// Done indicates the tracker can be reclaimed.
	Done()
}

type tracker struct {
	dictionaries *dictionaries

	// all active attribute contexts
	contexts map[int32]*rootBag

	// the current live dictionary
	currentDictionary dictionary

	// the bag currently in use by an outstanding request
	currentChild *mutableBag
}

var trackers = sync.Pool{
	New: func() interface{} {
		return &tracker{
			contexts: make(map[int32]*rootBag),
		}
	},
}

func getTracker(dictionaries *dictionaries) *tracker {
	at := trackers.Get().(*tracker)
	at.dictionaries = dictionaries
	return at
}

func (at *tracker) Done() {
	for k, rb := range at.contexts {
		rb.Done()
		delete(at.contexts, k)
	}

	at.dictionaries.Release(at.currentDictionary)
	at.currentDictionary = nil
	at.currentChild = nil
	at.dictionaries = nil

	trackers.Put(at)
}

func (at *tracker) StartRequest(attrs *mixerpb.Attributes) (MutableBag, error) {
	// find the context or create it if needed
	rb := at.contexts[attrs.AttributeContext]
	if rb == nil {
		rb = getRootBag()
		at.contexts[attrs.AttributeContext] = rb
	}

	dict := at.currentDictionary
	if len(attrs.Dictionary) > 0 {
		dict = attrs.Dictionary
	}

	if err := rb.update(dict, attrs); err != nil {
		return nil, err
	}

	// remember any new dictionary for later
	if len(attrs.Dictionary) > 0 {
		at.dictionaries.Release(at.currentDictionary)
		at.currentDictionary = at.dictionaries.Intern(attrs.Dictionary)
	}

	// to maintain the integrity of the API-level attribute protocol,
	// we need to ensure that the rest of the mixer's processing pipeline
	// doesn't mutate the set of protocol-level attributes. We do this
	// by returning a child bag.
	at.currentChild = rb.child()
	return at.currentChild, nil
}

func (at *tracker) EndRequest() {
	if at.currentChild != nil {
		at.currentChild.Done()
		at.currentChild = nil
	}
}
