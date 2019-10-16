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
func (ms Messages) Sort() {
	sort.Slice(ms, func(i, j int) bool {
		switch {
		case ms[i].Type.Level() != ms[j].Type.Level():
			return ms[i].Type.Level() < ms[j].Type.Level()
		case ms[i].Type.Code() != ms[j].Type.Code():
			return ms[i].Type.Code() < ms[j].Type.Code()
		case ms[i].Origin.FriendlyName() != ms[j].Origin.FriendlyName():
			return ms[i].Origin.FriendlyName() < ms[j].Origin.FriendlyName()
		default:
			return ms[i].String() < ms[j].String()
		}
	})
}

// Return a different sorted Messages struct
func (ms Messages) Sorted() Messages {
	var newMs Messages
	for _, val := range ms {
		newMs.Add(val)
	}
	newMs.Sort()
	return newMs
}
