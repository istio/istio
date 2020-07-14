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

package status

import (
	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/source/kube/rt"
)

// Messages is a data structure for capturing incoming messages
type Messages struct {
	entries map[key]entry
}

type entry struct {
	origin   *rt.Origin
	messages diag.Messages
}

// NewMessageSet returns a new instance of message set.
func NewMessageSet() Messages {
	return Messages{
		entries: make(map[key]entry),
	}
}

// Add a new message for a given origin.
func (m *Messages) Add(origin *rt.Origin, msg diag.Message) {
	k := key{col: origin.Collection, res: origin.FullName}
	e := m.entries[k]
	e.origin = origin
	e.messages = append(e.messages, msg)
	m.entries[k] = e
}
