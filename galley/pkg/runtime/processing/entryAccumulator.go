//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package processing

import "istio.io/istio/galley/pkg/runtime/resource"

type EntryAccumulator struct {
	collection *EntryCollection
}

var _ Handler = &EntryAccumulator{}

func NewEntryAccumulator(c *EntryCollection) *EntryAccumulator {
	return &EntryAccumulator{
		collection: c,
	}
}

// Handle implements Handler
func (a *EntryAccumulator) Handle(ev resource.Event) bool {
	switch ev.Kind {
	case resource.Added, resource.Updated:
		return a.collection.Set(ev.Entry)

	case resource.Deleted:
		return a.collection.Remove(ev.Entry.ID.FullName)

	default:
		scope.Errorf("Unknown event kind encountered when processing %q: %v", ev.Entry.ID.String(), ev.Kind)
		return false
	}
}

