// Copyright 2019 Istio Authors
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

package diag

import "sort"

// Messages is a slice of Message items.
type Messages []Message

// Add a new message to the messages
func (ms *Messages) Add(m Message) {
	*ms = append(*ms, m)
}

// Sort the message lexicographically by level, code, origin, then string.
func (ms *Messages) Sort() {
	sort.Slice(*ms, func(i, j int) bool {
		a, b := (*ms)[i], (*ms)[j]
		switch {
		case a.Type.Level() != b.Type.Level():
			return a.Type.Level().sortOrder < b.Type.Level().sortOrder
		case a.Type.Code() != b.Type.Code():
			return a.Type.Code() < b.Type.Code()
		case a.Origin == nil && b.Origin != nil:
			return true
		case a.Origin != nil && b.Origin == nil:
			return false
		case a.Origin != nil && b.Origin != nil && a.Origin.FriendlyName() != b.Origin.FriendlyName():
			return a.Origin.FriendlyName() < b.Origin.FriendlyName()
		default:
			return a.String() < b.String()
		}
	})
}

// Return a different sorted Messages struct
func (ms *Messages) SortedCopy() Messages {
	newMs := append((*ms)[:0:0], *ms...)
	newMs.Sort()
	return newMs
}
