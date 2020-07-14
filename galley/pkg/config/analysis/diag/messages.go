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

package diag

import "sort"

// Messages is a slice of Message items.
type Messages []Message

// Add a new message to the messages
func (ms *Messages) Add(m Message) {
	*ms = append(*ms, m)
}

// Sort the message lexicographically by level, code, resource origin name, then string.
func (ms *Messages) Sort() {
	sort.Slice(*ms, func(i, j int) bool {
		a, b := (*ms)[i], (*ms)[j]
		switch {
		case a.Type.Level() != b.Type.Level():
			return a.Type.Level().sortOrder < b.Type.Level().sortOrder
		case a.Type.Code() != b.Type.Code():
			return a.Type.Code() < b.Type.Code()
		case a.Resource == nil && b.Resource != nil:
			return true
		case a.Resource != nil && b.Resource == nil:
			return false
		case a.Resource != nil && b.Resource != nil && a.Resource.Origin.FriendlyName() != b.Resource.Origin.FriendlyName():
			return a.Resource.Origin.FriendlyName() < b.Resource.Origin.FriendlyName()
		default:
			return a.String() < b.String()
		}
	})
}

// SortedDedupedCopy returns a different sorted (and deduped) Messages struct.
func (ms *Messages) SortedDedupedCopy() Messages {
	newMs := append((*ms)[:0:0], *ms...)
	newMs.Sort()

	// Take advantage of the fact that the list is already sorted to dedupe
	// messages (any duplicates should be adjacent).
	var deduped Messages
	for _, m := range newMs {
		// Two messages are duplicates if they have the same string representation.
		if len(deduped) != 0 && deduped[len(deduped)-1].String() == m.String() {
			continue
		}
		deduped = append(deduped, m)
	}
	return deduped
}
